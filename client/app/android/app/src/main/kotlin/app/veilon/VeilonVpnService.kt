package app.veilon

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.graphics.drawable.Icon
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.Build
import android.content.BroadcastReceiver
import android.content.IntentFilter
import android.os.Handler
import android.os.PowerManager
import android.os.SystemClock
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.os.Process
import org.json.JSONArray
import org.json.JSONObject
import java.util.Locale
import java.util.concurrent.Executors
import veiloncore.Platform
import veiloncore.Veiloncore

/**
 * VpnService hosting the Go core. The core builds the tun through
 * [Platform.openTun]; this service owns the interface fds, the foreground
 * notification, auto-reconnect, the kill switch and network-change hooks.
 */
class VeilonVpnService : VpnService() {

    companion object {
        const val ACTION_START = "app.veilon.action.START"
        const val ACTION_STOP = "app.veilon.action.STOP"
        const val EXTRA_PROFILE = "profile"
        const val EXTRA_SETTINGS = "settings"
        /** Set when started by boot / tile / always-on: retry instead of failing. */
        const val EXTRA_RETRY = "retry"

        private const val CHANNEL = "veilon_vpn"
        private const val NOTIF_ID = 1
        private const val MAX_BACKOFF_S = 60L
        private const val MIN_RETRY_GAP_MS = 5_000L
        /**
         * Ticker periods: screen on (UI visible or not: stats + notification,
         * 1 s like the in-app meter) / screen off (health check only).
         */
        private const val TICK_UI_MS = 1_000L
        private const val TICK_BG_MS = 1_000L
        private const val TICK_OFF_MS = 30_000L
        private const val HEALTH_MS = 3_000L

        @Volatile var instance: VeilonVpnService? = null
            private set

        /**
         * 1.0.6: ONE process-wide, never-shut-down single thread for every
         * call into the core that starts, stops or inspects the session
         * (Start, Stop, IsRunning, NetworkChanged, SetDefaultInterface).
         * Until 1.0.5 each service instance had its own executor (shut down
         * in onDestroy) and onDestroy called Stop on the main thread, so a
         * quick stop → start could run two Start/Stop sequences out of order
         * on two threads, block the main thread on the core's lock, or post
         * work to a shut-down executor (RejectedExecutionException on the
         * main thread = app closes).
         */
        private val coreExec = Executors.newSingleThreadExecutor { r ->
            Thread(r, "veilon-core").apply {
                isDaemon = true
                // An exception escaping a core task must never kill the app.
                setUncaughtExceptionHandler { t, e -> CrashLog.report(t, e) }
            }
        }

        /**
         * 1.0.6: separate never-shut-down thread for server checks (a delay
         * test can take seconds; it must not hold up Start / Stop on
         * [coreExec]).
         */
        private val probeExec = Executors.newSingleThreadExecutor { r ->
            Thread(r, "veilon-probe").apply {
                isDaemon = true
                setUncaughtExceptionHandler { t, e -> CrashLog.report(t, e) }
            }
        }

        internal fun onProbe(what: String, block: () -> Unit) {
            try {
                probeExec.execute {
                    try {
                        block()
                    } catch (e: Throwable) {
                        CrashLog.report(Thread.currentThread(), e, "probe task $what")
                    }
                }
            } catch (e: Throwable) {
                CrashLog.report(Thread.currentThread(), e, "queue probe $what")
            }
        }

        /** Queues [block] on [coreExec]; never throws. */
        internal fun onCore(what: String, block: () -> Unit) {
            try {
                coreExec.execute {
                    try {
                        block()
                    } catch (e: Throwable) {
                        CrashLog.report(Thread.currentThread(), e, "core task $what")
                    }
                }
            } catch (e: Throwable) {
                CrashLog.report(Thread.currentThread(), e, "queue $what")
            }
        }

        fun start(ctx: Context, profile: String?, settings: String?, retry: Boolean) {
            val i = Intent(ctx, VeilonVpnService::class.java).setAction(ACTION_START)
                .putExtra(EXTRA_RETRY, retry)
            if (profile != null) i.putExtra(EXTRA_PROFILE, profile)
            if (settings != null) i.putExtra(EXTRA_SETTINGS, settings)
            if (Build.VERSION.SDK_INT >= 26) ctx.startForegroundService(i) else ctx.startService(i)
        }

        /**
         * Disconnect from the app / tile. Main thread. Idempotent: any number
         * of calls in any state are safe.
         */
        fun stop(ctx: Context) {
            val s = instance
            if (s != null && !s.destroyed) {
                s.main.post { if (!s.destroyed) s.stopVpn() }
                return
            }
            if (VpnState.state == VpnState.CONNECTING) {
                // A start was requested but the service is not up yet: queue a
                // STOP behind that START (intents are delivered in order) so
                // it cannot connect after the user already disconnected.
                val sent = runCatching {
                    ctx.startService(Intent(ctx, VeilonVpnService::class.java).setAction(ACTION_STOP))
                }.isSuccess
                if (sent) return
            }
            // Nothing running: report it (always emit, so a UI waiting in
            // "disconnecting" is released even if the state did not change).
            VpnState.set(VpnState.DISCONNECTED)
        }

        fun profileIdOf(profile: String?): String? =
            profile?.let { runCatching { JSONObject(it).optString("id") }.getOrNull() }?.ifEmpty { null }
    }

    private val main = Handler(Looper.getMainLooper())

    /** Set in onDestroy; posted work of a destroyed instance does nothing. */
    @Volatile private var destroyed = false
    /** startId of the newest onStartCommand; stopSelfResult(it) never stops a newer pending start. */
    private var lastStartId = 0
    /** Guards tunPfd / blockPfd (core thread in openTun, main thread elsewhere). */
    private val tunLock = Any()

    private var profile: String? = null
    private var settings: String? = null
    private var options = ServiceOptions()

    @Volatile private var userStopped = true
    private var tunPfd: ParcelFileDescriptor? = null
    private var blockPfd: ParcelFileDescriptor? = null
    private var attempt = 0
    private var reconnectTask: Runnable? = null
    /**
     * Start generation (main thread). Bumped by every connect / stop / fail so a
     * result posted by an older doStart (e.g. the previous profile, before a live
     * switch) can no longer flip the state or schedule a reconnect.
     */
    @Volatile private var gen = 0
    /** Uptime of the last start attempt; immediate (network-triggered) retries are rate-limited by it. */
    private var lastStartAt = 0L
    /** Tunnel-loss watch (main thread): a server check is running / when the last one started. */
    private var probing = false
    private var lastProbeAt = -TunnelWatch.PROBE_GAP_MS

    // Battery: per-second work only while the screen is on and the UI is visible.
    @Volatile private var screenOn = true
    private var screenReceiver: BroadcastReceiver? = null

    // traffic accounting (own uid: the core's sockets are excluded from the tun)
    private var baseRx = 0L
    private var baseTx = 0L
    private var lastRx = 0L
    private var lastTx = 0L
    private var lastTick = 0L
    private var ticking = false

    private var cm: ConnectivityManager? = null
    private var netCallback: ConnectivityManager.NetworkCallback? = null
    private var currentNetwork: Network? = null

    private val tileListener: (Map<String, Any?>) -> Unit = { e ->
        if (e["type"] == "state") VeilonTileService.refresh(this)
    }

    // ------------------------------------------------------------ lifecycle

    override fun onCreate() {
        super.onCreate()
        CrashLog.install(this)
        instance = this
        guard("onCreate") {
            createChannel()
            VpnState.listeners.add(tileListener)
            registerNetwork()
            registerScreen()
            VpnState.onUiChanged = { main.post { guard("retick") { retick() } } }
        }
    }

    /** Runs a lifecycle / callback body; an exception is recorded, never fatal. */
    private inline fun guard(what: String, block: () -> Unit) {
        try {
            block()
        } catch (e: Throwable) {
            CrashLog.report(Thread.currentThread(), e, "service $what")
        }
    }

    /** Posts [block] to the main thread; skipped once this instance is destroyed. */
    private fun post(what: String, block: () -> Unit) {
        main.post { if (!destroyed) guard(what, block) }
    }

    private fun registerScreen() {
        screenOn = (getSystemService(Context.POWER_SERVICE) as PowerManager).isInteractive
        val r = object : BroadcastReceiver() {
            override fun onReceive(context: Context?, intent: Intent?) {
                if (destroyed) return
                screenOn = intent?.action != Intent.ACTION_SCREEN_OFF
                guard("screen") { retick() }
            }
        }
        val f = IntentFilter().apply {
            addAction(Intent.ACTION_SCREEN_OFF)
            addAction(Intent.ACTION_SCREEN_ON)
        }
        if (Build.VERSION.SDK_INT >= 33) registerReceiver(r, f, Context.RECEIVER_NOT_EXPORTED) else registerReceiver(r, f)
        screenReceiver = r
    }

    private fun unregisterScreen() {
        screenReceiver?.let { runCatching { unregisterReceiver(it) } }
        screenReceiver = null
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        // Every start (also STOP, also a system restart with a null intent)
        // calls startForeground FIRST: a startForegroundService() whose
        // onStartCommand returns without it - or a service brought down
        // before it - makes the system kill the app
        // (ForegroundServiceDidNotStartInTimeException / "did not then call
        // Service.startForeground()").
        val fgOk = try {
            startForegroundCompat(buildNotification())
            true
        } catch (e: Throwable) {
            // e.g. ForegroundServiceStartNotAllowedException for a STOP that
            // arrived via plain startService from the background.
            CrashLog.report(Thread.currentThread(), e, "startForeground")
            false
        }
        try {
            if (intent?.action == ACTION_STOP) {
                stopVpn()
                return START_NOT_STICKY
            }
            if (!fgOk) {
                fail("could not start the foreground service")
                return START_NOT_STICKY
            }
            options = Store.options(this)
            // Started by the user (extras), by boot/tile (no extras, retry) or by
            // the system: null intent after a kill, or always-on VPN.
            val p = intent?.getStringExtra(EXTRA_PROFILE) ?: Store.lastProfile(this)
            val s = intent?.getStringExtra(EXTRA_SETTINGS) ?: Store.lastSettings(this) ?: ""
            val retry = intent?.getBooleanExtra(EXTRA_RETRY, true) ?: true
            if (p == null) {
                fail("no profile")
                return START_NOT_STICKY
            }
            // Live switch: connect while a session (or a start / reconnect) for
            // another profile is up. The core closes the old session inside
            // Start before it opens the new one; the old tun stays up (unread, so
            // nothing leaks) until the new one is established and replaces it.
            val switching = !userStopped && profile != null && profileIdOf(profile) != profileIdOf(p)
            profile = p
            settings = s
            Store.saveLast(this, p, s)
            connect(retry || switching, switching)
            return START_STICKY
        } catch (e: Throwable) {
            CrashLog.report(Thread.currentThread(), e, "onStartCommand")
            runCatching { fail(e.message ?: e.toString()) }
            return START_NOT_STICKY
        }
    }

    override fun onRevoke() {
        // Another VPN took over or the user revoked permission: no reconnect.
        guard("onRevoke") { stopVpn() }
    }

    override fun onDestroy() {
        destroyed = true
        userStopped = true
        gen++
        if (instance === this) instance = null
        guard("onDestroy") {
            cancelReconnect()
            stopTicker()
            unregisterNetwork()
            unregisterScreen()
            VpnState.onUiChanged = null
            // Stop on the core thread (never the main thread: Stop / IsRunning
            // wait for a Start in progress). Queued behind any pending start,
            // so the core never keeps a session of a destroyed service.
            onCore("destroy stop") { runCatching { Veiloncore.stop() } }
            closeTun()
            closeBlocker()
            if (VpnState.state != VpnState.ERROR && VpnState.state != VpnState.DISCONNECTED) {
                VpnState.set(VpnState.DISCONNECTED)
            }
            VpnState.listeners.remove(tileListener)
            VeilonTileService.refresh(this)
        }
        super.onDestroy()
    }

    // ------------------------------------------------------------ connect / stop

    private fun connect(retryOnFail: Boolean, switching: Boolean = false) {
        cancelReconnect()
        // No stale speeds / health checks of the old session while switching;
        // the ticker restarts when the new session is up.
        stopTicker()
        userStopped = false
        attempt = 0
        val g = ++gen
        VpnState.profileId = profileIdOf(profile)
        if (switching) {
            // New session: its own timer and counters.
            VpnState.since = 0
            VpnState.sessionDown = 0
            VpnState.sessionUp = 0
        }
        VpnState.set(VpnState.CONNECTING)
        val p = profile
        val st = settings
        onCore("start") { doStart(retryOnFail, g, p, st) }
    }

    /** Runs on [coreExec]. [g] / [p] / [st] are the generation and profile it was queued for. */
    private fun doStart(retryOnFail: Boolean, g: Int, p: String?, st: String?) {
        if (p == null || destroyed) return
        // A newer connect / stop is already queued behind us: skip this start.
        if (g != gen) return
        lastStartAt = SystemClock.elapsedRealtime()
        try {
            Veiloncore.start(p, Store.withPaths(this, st), platform)
            if (destroyed) {
                // The service went away while the core was starting: do not
                // leave a session bound to a dead VpnService.
                runCatching { Veiloncore.stop() }
                return
            }
            post("started") {
                if (userStopped || g != gen) return@post
                closeBlocker()
                attempt = 0
                resetCounters()
                VpnState.set(VpnState.CONNECTED)
                startTicker()
                updateNotification()
            }
        } catch (e: Throwable) {
            val msg = e.message ?: e.toString()
            runCatching { Veiloncore.writeLog("error", "start: $msg") }
            post("start failed") {
                if (userStopped || g != gen) return@post
                if (retryOnFail || VpnState.since > 0) scheduleReconnect(msg) else fail(msg)
            }
        }
    }

    /** Core died or a start failed: retry with exponential backoff. */
    private fun scheduleReconnect(reason: String) {
        if (userStopped || destroyed) return
        options = Store.options(this)
        if (!options.autoReconnect) {
            // 1.0.8 (audit M2): with the kill switch on, a dead core must not
            // hand traffic back to the clear network. Keep blocking until the
            // user disconnects (until 1.0.7 fail() closed the tun and the
            // blocker, so everything went out unprotected).
            if (TunnelWatch.blockAfterCoreDeath(options.killSwitch, options.autoReconnect)) failBlocking(reason)
            else fail(reason)
            return
        }
        stopTicker()
        if (TunnelWatch.blockWhileReconnecting(options.killSwitch)) {
            // Keep the (dead) tun up: nothing reads it, so traffic is dropped
            // instead of leaking. Without a tun, install a blocking one.
            if (synchronized(tunLock) { tunPfd == null }) openBlocker()
        } else {
            // Kill switch off: no VPN while the tunnel is down. Closing our
            // copy of the tun fd alone (until 1.0.6) left the interface up:
            // the core holds a dup of it. Stop the core session as well, so
            // apps use the normal network until a reconnect succeeds. Queued
            // on coreExec before any reconnect start, so the order holds.
            onCore("release tun") { runCatching { Veiloncore.stop() } }
            closeTun()
            closeBlocker()
        }
        VpnState.set(VpnState.CONNECTING, reason)
        // 1, 2, 4 … 60 s (capped), ±20 % jitter; never below MIN_RETRY_GAP_MS
        // after the previous attempt, so a failing start cannot spin.
        val baseMs = minOf(MAX_BACKOFF_S, 1L shl minOf(attempt, 6)) * 1000
        val jitter = (baseMs * (Math.random() * 0.4 - 0.2)).toLong()
        val sinceLast = SystemClock.elapsedRealtime() - lastStartAt
        val delayMs = maxOf(baseMs + jitter, MIN_RETRY_GAP_MS - sinceLast, 500L)
        attempt = minOf(attempt + 1, 30)
        val g = gen
        val r = Runnable {
            reconnectTask = null
            if (!destroyed && !userStopped && g == gen) {
                val p = profile
                val st = settings
                if (TunnelWatch.checkServerBeforeReconnect(Store.options(this).killSwitch)) {
                    // Kill switch off: bring the tun back only once the
                    // server answers; until then the phone keeps its normal
                    // internet (a start against a dead server would capture
                    // all traffic again).
                    reconnectWhenServerAnswers(g, p, st)
                } else {
                    onCore("reconnect") { doStart(true, g, p, st) }
                }
            }
        }
        reconnectTask = r
        main.postDelayed(r, delayMs)
        updateNotification()
    }

    /** Main thread. Checks the server off the VPN, then starts (or backs off again). */
    private fun reconnectWhenServerAnswers(g: Int, p: String?, st: String?) {
        lastStartAt = SystemClock.elapsedRealtime()
        onProbe("reconnect check") {
            val ok = serverAnswers(p, st)
            post("reconnect checked") {
                if (userStopped || g != gen) return@post
                if (ok) onCore("reconnect") { doStart(true, g, p, st) }
                else scheduleReconnect("server does not answer")
            }
        }
    }

    /**
     * One delay test of [p] through the regular ping path (isolated
     * instance, not through the VPN: the app is excluded from it). Probe
     * thread. A missing profile counts as answered (never tear down on it).
     */
    private fun serverAnswers(p: String?, st: String?): Boolean {
        if (p == null) return true
        val url = runCatching { Store.flutterSettings(this)?.optString("pingUrl") }.getOrNull() ?: ""
        val r = runCatching {
            Veiloncore.urlTestError(p, Store.withPaths(this, st), url, TunnelWatch.PROBE_TIMEOUT_MS)
        }.getOrNull()
        return TunnelWatch.answered(r)
    }

    /**
     * Health tick, main thread, CONNECTED: notices a dead tunnel the core
     * session does not report (it keeps running). See [TunnelWatch].
     */
    private fun watchTunnel(g: Int) {
        val ks = Store.options(this).killSwitch
        val stalled = runCatching { Veiloncore.proxyStalledMs() }.getOrDefault(0L)
        val now = SystemClock.elapsedRealtime()
        if (!TunnelWatch.shouldProbe(ks, VpnState.state == VpnState.CONNECTED, stalled, proxyStatus(), now - lastProbeAt, probing)) return
        probing = true
        lastProbeAt = now
        val p = profile
        val st = settings
        onProbe("tunnel check") {
            val ok = serverAnswers(p, st)
            post("tunnel checked") {
                probing = false
                if (ok || g != gen || userStopped || VpnState.state != VpnState.CONNECTED) return@post
                val o = Store.options(this)
                val reason = "connection lost"
                when (TunnelWatch.onLost(o.killSwitch, o.autoReconnect)) {
                    TunnelWatch.Lost.KEEP_BLOCKING -> {}
                    TunnelWatch.Lost.RECONNECT_WITHOUT_VPN -> {
                        runCatching { Veiloncore.writeLog("warn", "tunnel lost: the server does not answer; kill switch off, so the VPN is released (normal internet) and reconnects in the background") }
                        scheduleReconnect(reason)
                    }
                    TunnelWatch.Lost.STOP -> {
                        runCatching { Veiloncore.writeLog("warn", "tunnel lost: the server does not answer; kill switch and auto reconnect off, so the VPN is turned off") }
                        fail(reason)
                    }
                }
            }
        }
    }

    private fun cancelReconnect() {
        reconnectTask?.let { main.removeCallbacks(it) }
        reconnectTask = null
    }

    /**
     * User / tile / notification disconnect. Main thread. Idempotent: a
     * second call while stopping just queues another (no-op) Stop.
     */
    fun stopVpn() {
        if (destroyed) return
        userStopped = true
        val g = ++gen
        cancelReconnect()
        stopTicker()
        if (VpnState.state != VpnState.DISCONNECTED) VpnState.set(VpnState.DISCONNECTING)
        onCore("stop") {
            runCatching { Veiloncore.stop() }
            post("stopped") { teardown(g, VpnState.DISCONNECTED, "") }
        }
    }

    private fun fail(msg: String) {
        if (destroyed) return
        userStopped = true
        val g = ++gen
        cancelReconnect()
        stopTicker()
        onCore("fail stop") {
            runCatching { Veiloncore.stop() }
            post("failed") { teardown(g, VpnState.ERROR, msg) }
        }
    }

    /**
     * 1.0.8 (audit M2): kill switch on + auto reconnect off and the session
     * died. The blocking tun goes up BEFORE the core releases its own (no
     * gap); the service stays in the foreground in ERROR + blocking until the
     * user disconnects (stopVpn → teardown closes the blocker) or connects
     * again (the new tun replaces the blocker).
     */
    private fun failBlocking(msg: String) {
        if (destroyed) return
        userStopped = true
        val g = ++gen
        cancelReconnect()
        stopTicker()
        openBlocker()
        onCore("fail stop (blocking)") {
            runCatching { Veiloncore.stop() }
            post("failed (blocking)") {
                if (g != gen) return@post
                closeTun()
                // establish() of the blocker can fail (permission revoked):
                // then there is nothing to keep, fall back to a normal stop.
                if (synchronized(tunLock) { blockPfd == null }) {
                    teardown(g, VpnState.ERROR, msg)
                    return@post
                }
                runCatching { Veiloncore.writeLog("warn", "connection lost: kill switch on and auto reconnect off, so all traffic stays blocked until you disconnect") }
                VpnState.set(VpnState.ERROR, msg, block = true)
                updateNotification()
            }
        }
    }

    /**
     * Main thread, after the core stopped. Skipped when a connect arrived
     * after the stop (its start is queued behind the Stop: leave its tun,
     * state and service alone). Stops the service with stopSelfResult(the
     * newest startId we saw): when the app already asked for a new start
     * whose onStartCommand has not run yet, the system keeps the service
     * alive for it. A plain stopSelf() here (1.0.0–1.0.5) destroyed the
     * service while that startForegroundService() was still waiting for its
     * startForeground() call, and the system then kills the whole app -
     * the rapid connect/disconnect crash.
     */
    private fun teardown(g: Int, state: String, msg: String) {
        if (g != gen) return
        closeTun()
        closeBlocker()
        VpnState.set(state, msg)
        runCatching { stopForegroundCompat() }
        runCatching { stopSelfResult(lastStartId) }
    }

    // ------------------------------------------------------------ Platform (core → app)

    private val platform = object : Platform {
        // Called on core (Go) threads. openTun may throw: gomobile turns the
        // exception into the Go error. protect / onLog have no error result:
        // a Java exception escaping them stays pending on the JNI thread
        // (gomobile does not clear it), which can abort the process on the
        // next JNI call - so they must never throw.
        override fun openTun(optionsJSON: String?): Int {
            if (destroyed) throw IllegalStateException("VPN service stopped")
            return establishTun(optionsJSON ?: "{}")
        }

        override fun protect(fd: Int): Boolean =
            try { this@VeilonVpnService.protect(fd) } catch (_: Throwable) { false }

        override fun onLog(level: Int, message: String?) {
            try {
                if (VpnState.uiAttached) {
                    VpnState.emit(mapOf("type" to "log", "level" to level, "message" to (message ?: "")))
                }
            } catch (_: Throwable) {
            }
        }
    }

    /** Builds the VPN interface from a core TunRequest. Called on a core thread. */
    private fun establishTun(json: String): Int {
        val j = JSONObject(json)
        val b = Builder()
        b.setSession(options.profileName.ifEmpty { "Veilon" })
        b.setMtu(j.optInt("mtu", 9000).takeIf { it in 1280..65535 } ?: 9000)

        val v4 = j.optJSONArray("inet4Address").strings()
        val v6 = j.optJSONArray("inet6Address").strings()
        if (v4.isEmpty() && v6.isEmpty()) throw IllegalArgumentException("tun has no address")
        v4.forEach { b.addCidrAddress(it) }
        v6.forEach { b.addCidrAddress(it) }

        val r4 = j.optJSONArray("inet4Routes").strings()
        val r6 = j.optJSONArray("inet6Routes").strings()
        if (v4.isNotEmpty()) {
            if (r4.isEmpty()) b.addRoute("0.0.0.0", 0) else r4.forEach { b.addCidrRoute(it) }
        }
        if (v6.isNotEmpty()) {
            if (r6.isEmpty()) b.addRoute("::", 0) else r6.forEach { b.addCidrRoute(it) }
        }

        j.optString("dnsServer").takeIf { it.isNotBlank() }?.let { runCatching { b.addDnsServer(it) } }

        val include = j.optJSONArray("includePackages").strings()
        val exclude = j.optJSONArray("excludePackages").strings().toMutableSet()
        if (include.isNotEmpty()) {
            var any = false
            for (pkg in include) {
                if (pkg == packageName) continue
                try { b.addAllowedApplication(pkg); any = true } catch (_: PackageManager.NameNotFoundException) {}
            }
            // An allow-list with no installed app would route everything; keep self out instead.
            if (!any) b.addDisallowedApplication(packageName)
        } else {
            exclude.add(packageName)
            for (pkg in exclude) {
                try { b.addDisallowedApplication(pkg) } catch (_: PackageManager.NameNotFoundException) {}
            }
        }

        // Unmetered VPN: Android then derives metering from the underlying
        // network (a metered Wi-Fi / mobile link still shows as metered).
        if (Build.VERSION.SDK_INT >= 29) b.setMetered(false)
        // Underlying = the real default network, so the status bar keeps the
        // Wi-Fi / mobile icon (and signal) instead of only the VPN key.
        val underlying = currentNetwork ?: physicalDefaultNetwork()?.also { currentNetwork = it }
        underlying?.let { b.setUnderlyingNetworks(arrayOf(it)) }
        b.setConfigureIntent(openAppIntent())

        val pfd = b.establish() ?: throw IllegalStateException("VPN permission not granted")
        val stale = synchronized(tunLock) { destroyed }
        if (stale) {
            runCatching { pfd.close() }
            throw IllegalStateException("VPN service stopped")
        }
        val old = synchronized(tunLock) { tunPfd.also { tunPfd = pfd } }
        // The core dup()s the fd, so closing our copies never closes its tun.
        if (old !== pfd) runCatching { old?.close() }
        post("tun up") {
            closeBlocker()
            // Re-assert after establish (the default network may have changed
            // while the core was starting).
            currentNetwork?.let { n -> runCatching { setUnderlyingNetworks(arrayOf(n)) } }
        }
        return pfd.fd
    }

    /** Kill switch: a tun that captures everything and is never read. */
    private fun openBlocker() {
        if (synchronized(tunLock) { blockPfd != null }) return
        try {
            val b = Builder().setSession("Veilon (blocking)").setMtu(1500)
                .addAddress("172.19.0.1", 30).addRoute("0.0.0.0", 0)
            runCatching { b.addAddress("fdfe:dcba:9876::1", 126); b.addRoute("::", 0) }
            runCatching { b.addDisallowedApplication(packageName) }
            if (Build.VERSION.SDK_INT >= 29) b.setMetered(false)
            currentNetwork?.let { b.setUnderlyingNetworks(arrayOf(it)) }
            val pfd = b.establish()
            synchronized(tunLock) { blockPfd = pfd }
        } catch (e: Throwable) {
            runCatching { Veiloncore.writeLog("warn", "kill switch: ${e.message}") }
        }
    }

    /** Idempotent (take-and-clear under the lock: never closes one pfd twice). */
    private fun closeBlocker() {
        val p = synchronized(tunLock) { blockPfd.also { blockPfd = null } }
        runCatching { p?.close() }
    }

    /** Idempotent (take-and-clear under the lock). */
    private fun closeTun() {
        val p = synchronized(tunLock) { tunPfd.also { tunPfd = null } }
        runCatching { p?.close() }
    }

    // ------------------------------------------------------------ network changes

    private fun registerNetwork() {
        val c = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        cm = c
        // Seed synchronously: the first callback arrives asynchronously and
        // the tun can be established before it (no underlying network then).
        physicalDefaultNetwork()?.let { currentNetwork = it }
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                if (destroyed || isVpnNetwork(network)) return
                post("network available") { onNetwork(network) }
            }

            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                if (destroyed || caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return
                // Same network, new transport/metering/validation: refresh the
                // VPN's underlying network so the status bar follows it.
                post("network caps") {
                    if (currentNetwork == null || network == currentNetwork) {
                        if (currentNetwork == null) onNetwork(network)
                        else runCatching { setUnderlyingNetworks(arrayOf(network)) }
                    }
                }
            }

            override fun onLost(network: Network) {
                if (destroyed) return
                post("network lost") {
                    if (network == currentNetwork) {
                        currentNetwork = null
                        reportDefaultInterface(null)
                        runCatching { setUnderlyingNetworks(null) }
                    }
                }
            }
        }
        try {
            c.registerDefaultNetworkCallback(cb)
            netCallback = cb
        } catch (e: Throwable) {
            runCatching { Veiloncore.writeLog("warn", "network callback: ${e.message}") }
        }
    }

    /** The system default network if it is a physical one (not a VPN). */
    private fun physicalDefaultNetwork(): Network? {
        if (Build.VERSION.SDK_INT < 23) return null
        val n = runCatching { cm?.activeNetwork }.getOrNull() ?: return null
        return if (isVpnNetwork(n)) null else n
    }

    private fun isVpnNetwork(n: Network): Boolean =
        runCatching { cm?.getNetworkCapabilities(n)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) }
            .getOrNull() == true

    /** Tells the core which interface carries the default (underlying) network. */
    private fun reportDefaultInterface(network: Network?) {
        val name = network?.let { runCatching { cm?.getLinkProperties(it)?.interfaceName }.getOrNull() } ?: ""
        val index = if (name.isEmpty()) 0 else runCatching { java.net.NetworkInterface.getByName(name)?.index ?: 0 }.getOrDefault(0)
        onCore("default interface") { runCatching { Veiloncore.setDefaultInterface(name, index) } }
    }

    private fun onNetwork(network: Network) {
        val prev = currentNetwork
        currentNetwork = network
        reportDefaultInterface(network)
        runCatching { setUnderlyingNetworks(arrayOf(network)) }
        if (prev == null || prev == network) {
            // First network (or regained one): retry a pending reconnect now.
            if (prev == null && reconnectTask != null) retryNowIfAllowed()
            return
        }
        when {
            VpnState.state == VpnState.CONNECTED -> onCore("network changed") { runCatching { Veiloncore.networkChanged() } }
            reconnectTask != null -> retryNowIfAllowed()
        }
    }

    /**
     * A network appeared while a reconnect is pending: retry now instead of
     * waiting out the backoff, but at most once per MIN_RETRY_GAP_MS and
     * without resetting the backoff (flapping networks must not cause a loop).
     */
    private fun retryNowIfAllowed() {
        if (userStopped || destroyed) return
        if (SystemClock.elapsedRealtime() - lastStartAt < MIN_RETRY_GAP_MS) return
        cancelReconnect()
        val g = gen
        val p = profile
        val st = settings
        onCore("retry now") { doStart(true, g, p, st) }
    }

    private fun unregisterNetwork() {
        netCallback?.let { cb -> runCatching { cm?.unregisterNetworkCallback(cb) } }
        netCallback = null
    }

    // ------------------------------------------------------------ stats ticker / health check

    // Session traffic from the core (Run 105): payload of the connections
    // other apps open through the tun, an upload only counted once the
    // remote side answered on that connection. Until 1.0.3 this was our
    // uid's socket counters, which also moved with a dead server (local
    // sidecar sockets, handshake retries), so a dead server showed traffic.
    // The core resets its counters for every session.
    private fun uidRx(): Long = runCatching { Veiloncore.trafficDown() }.getOrDefault(0L).coerceAtLeast(0)
    private fun uidTx(): Long = runCatching { Veiloncore.trafficUp() }.getOrDefault(0L).coerceAtLeast(0)
    private fun proxyStatus(): String = runCatching { Veiloncore.proxyStatus() }.getOrDefault("")

    private fun resetCounters() {
        baseRx = 0L; baseTx = 0L
        VpnState.proxy = ""
        lastRx = uidRx(); lastTx = uidTx()
        lastTick = System.nanoTime()
    }

    /**
     * Stats + notification + health check. Period depends on what can be seen:
     * 1 s whenever the screen is on (notification every second with the
     * last-second byte delta of our uid = the cores' sockets, no averaging;
     * stats events too while the app UI is visible), 30 s with the screen
     * off (health check only: no stats, no notification updates). Plain Handler callbacks, no wakelock: in Doze they simply
     * run late. Screen on/off and UI attach/detach re-tick immediately.
     */
    private val ticker = object : Runnable {
        override fun run() {
            if (!ticking || destroyed) return
            try {
                tick()
            } catch (e: Throwable) {
                CrashLog.report(Thread.currentThread(), e, "ticker")
                if (ticking && !destroyed) main.postDelayed(this, TICK_UI_MS)
            }
        }

        private fun tick() {
            val period = tickPeriod()
            if (screenOn) {
                val now = System.nanoTime()
                val rx = uidRx(); val tx = uidTx()
                // A new core session (reconnect) restarts its counters.
                if (rx < lastRx || tx < lastTx) { lastRx = rx; lastTx = tx }
                VpnState.proxy = proxyStatus()
                val dt = ((now - lastTick) / 1e9).coerceAtLeast(0.001)
                val down = ((rx - lastRx).coerceAtLeast(0)) / dt
                val up = ((tx - lastTx).coerceAtLeast(0)) / dt
                lastRx = rx; lastTx = tx; lastTick = now
                val sd = (rx - baseRx).coerceAtLeast(0)
                val su = (tx - baseTx).coerceAtLeast(0)
                if (VpnState.uiAttached) {
                    VpnState.stats(down, up, sd, su)
                } else {
                    VpnState.down = down; VpnState.up = up; VpnState.sessionDown = sd; VpnState.sessionUp = su
                }
                updateNotification()
            }
            // Health check: the core session ended without us stopping it.
            healthElapsed += period
            if (healthElapsed >= HEALTH_MS && !userStopped && VpnState.state == VpnState.CONNECTED) {
                healthElapsed = 0
                // IsRunning takes the core's session lock (held during a
                // Start / Stop): ask on the core thread, never block main.
                val g = gen
                onCore("health") {
                    val alive = runCatching { Veiloncore.isRunning() }.getOrDefault(true)
                    if (!alive) post("health lost") {
                        if (g == gen && !userStopped && VpnState.state == VpnState.CONNECTED) {
                            scheduleReconnect("connection lost")
                        }
                    }
                }
                // Session alive but the tunnel dead (server not answering).
                watchTunnel(g)
            }
            main.postDelayed(this, period)
        }
    }

    private var healthElapsed = 0L

    private fun tickPeriod(): Long = when {
        !screenOn -> TICK_OFF_MS
        VpnState.uiAttached -> TICK_UI_MS
        else -> TICK_BG_MS
    }

    private fun startTicker() {
        if (ticking) return
        ticking = true
        healthElapsed = 0
        main.postDelayed(ticker, tickPeriod())
    }

    /** Screen or UI visibility changed: apply the new period now. Main thread. */
    private fun retick() {
        if (!ticking) return
        main.removeCallbacks(ticker)
        if (screenOn) {
            // Restart the rate window so the first sample after a long pause
            // is not an average over the whole screen-off period.
            lastRx = uidRx(); lastTx = uidTx(); lastTick = System.nanoTime()
        }
        main.postDelayed(ticker, if (screenOn) TICK_UI_MS else tickPeriod())
    }

    private fun stopTicker() {
        ticking = false
        main.removeCallbacks(ticker)
    }

    // ------------------------------------------------------------ notification

    private val fa get() = options.language == "fa"

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= 26) {
            val ch = NotificationChannel(CHANNEL, "VPN", NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
                description = "Veilon connection status"
            }
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager).createNotificationChannel(ch)
        }
    }

    private fun openAppIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0,
        Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )

    private fun buildNotification(): Notification {
        val b = newBuilder()
        val name = options.profileName.ifEmpty { "Veilon" }
        val (title, text) = when (VpnState.state) {
            VpnState.CONNECTED -> name to if (VpnState.proxy == "unreachable")
                (if (fa) "سرور در دسترس نیست" else "Server unreachable")
            else speedLine(VpnState.up, VpnState.down)
            VpnState.ERROR -> name to if (VpnState.blocking)
                (if (fa) "اتصال قطع شد؛ کیل سوییچ اینترنت را مسدود نگه داشته" else "Connection lost; kill switch is blocking the internet")
            else (if (fa) "خطا" else "Error")
            else -> name to if (fa) "در حال اتصال…" else "Connecting…"
        }
        val stop = PendingIntent.getService(
            this, 1,
            Intent(this, VeilonVpnService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        b.setSmallIcon(R.drawable.ic_stat_veilon)
            .setContentTitle(title)
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setContentIntent(openAppIntent())
            .addAction(Notification.Action.Builder(null as Icon?, if (fa) "قطع اتصال" else "Disconnect", stop).build())
        if (Build.VERSION.SDK_INT >= 31) b.setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)
        return b.build()
    }

    @Suppress("DEPRECATION")
    private fun newBuilder(): Notification.Builder =
        if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, CHANNEL)
        else Notification.Builder(this).setPriority(Notification.PRIORITY_LOW)

    private fun updateNotification() {
        if (destroyed) return
        if (userStopped && VpnState.state != VpnState.CONNECTING && !VpnState.blocking) return
        runCatching {
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager).notify(NOTIF_ID, buildNotification())
        }
    }

    private fun startForegroundCompat(n: Notification) {
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIF_ID, n)
        }
    }

    private fun stopForegroundCompat() {
        stopForeground(Service.STOP_FOREGROUND_REMOVE)
    }
}

// ------------------------------------------------------------ helpers

private fun JSONArray?.strings(): List<String> {
    if (this == null) return emptyList()
    return (0 until length()).mapNotNull { optString(it).takeIf { s -> s.isNotBlank() } }
}

private fun splitCidr(cidr: String, defaultV4: Int, defaultV6: Int): Pair<String, Int> {
    val i = cidr.indexOf('/')
    if (i < 0) return cidr to (if (cidr.contains(':')) defaultV6 else defaultV4)
    return cidr.substring(0, i) to cidr.substring(i + 1).toInt()
}

private fun VpnService.Builder.addCidrAddress(cidr: String) {
    val (a, p) = splitCidr(cidr, 32, 128)
    addAddress(a, p)
}

private fun VpnService.Builder.addCidrRoute(cidr: String) {
    val (a, p) = splitCidr(cidr, 32, 128)
    addRoute(a, p)
}

/**
 * "↑ up   ↓ down", forced LTR (LRI … PDI) so upload is on the left and
 * download on the right in every locale: with the fa locale the line used to
 * resolve its direction from the first strong character and the order was
 * not under our control.
 */
internal fun speedLine(up: Double, down: Double): String =
    "\u2066↑ ${fmtSpeed(up)}   ↓ ${fmtSpeed(down)}\u2069"

internal fun fmtSpeed(bytesPerSec: Double): String {
    val units = arrayOf("B", "KB", "MB", "GB")
    var v = bytesPerSec
    var i = 0
    while (v >= 1024 && i < units.size - 1) { v /= 1024; i++ }
    return if (i == 0) "${v.toInt()} B/s" else String.format(Locale.US, "%.1f %s/s", v, units[i])
}
