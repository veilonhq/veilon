package app.veilon

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import android.provider.Settings
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.util.concurrent.Executors
import veiloncore.Veiloncore

/**
 * Flutter bridge. Method channel "app.veilon/core", event channel
 * "app.veilon/events" (maps with a "type": state, stats, log).
 */
class MainActivity : FlutterActivity() {

    companion object {
        private const val REQ_VPN = 7001
        private const val REQ_FILE = 7002
        private const val REQ_QR = 7003
        private const val REQ_NOTIF = 7004
    }

    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newCachedThreadPool()

    private var pendingConnect: Triple<String, String, MethodChannel.Result>? = null
    private var pendingFile: MethodChannel.Result? = null
    private var pendingQr: MethodChannel.Result? = null

    private var eventListener: ((Map<String, Any?>) -> Unit)? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        CrashLog.install(this)
        runCatching { io.execute { runCatching { Store.installAssets(applicationContext) } } }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        val messenger = flutterEngine.dartExecutor.binaryMessenger
        MethodChannel(messenger, "app.veilon/core").setMethodCallHandler(::onCallGuarded)
        EventChannel(messenger, "app.veilon/events").setStreamHandler(object : EventChannel.StreamHandler {
            override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
                eventListener?.let { VpnState.listeners.remove(it) }
                // Listeners run on the main thread (VpnState.emit). A sink of a
                // detached engine must not take the app down.
                val l: (Map<String, Any?>) -> Unit = { e ->
                    try { events.success(e) } catch (t: Throwable) { CrashLog.report(Thread.currentThread(), t, "event sink") }
                }
                eventListener = l
                VpnState.listeners.add(l)
                runCatching { events.success(VpnState.stateMap()) }
                streamOpen = true
                updateUiAttached()
            }

            override fun onCancel(arguments: Any?) {
                eventListener?.let { VpnState.listeners.remove(it) }
                eventListener = null
                streamOpen = false
                updateUiAttached()
            }
        })
    }

    // The service streams per-second stats only while the UI is visible.
    private var streamOpen = false
    private var resumed = false

    private fun updateUiAttached() {
        VpnState.uiAttached = streamOpen && resumed
    }

    override fun onResume() {
        super.onResume()
        resumed = true
        updateUiAttached()
    }

    override fun onPause() {
        resumed = false
        updateUiAttached()
        super.onPause()
    }

    override fun onDestroy() {
        eventListener?.let { VpnState.listeners.remove(it) }
        eventListener = null
        streamOpen = false
        resumed = false
        updateUiAttached()
        io.shutdown()
        super.onDestroy()
    }

    /** Runs [block] off the main thread and replies on it. Never throws. */
    private fun bg(result: MethodChannel.Result, block: () -> Any?) {
        try {
            io.execute {
                val r = runCatching(block)
                main.post {
                    runCatching {
                        r.fold({ result.success(it) }, { result.error("native", it.message ?: it.toString(), null) })
                    }
                }
            }
        } catch (e: Throwable) {
            // Executor already shut down (activity finishing).
            runCatching { result.error("native", e.message ?: e.toString(), null) }
        }
    }

    /** Channel entry point: an exception in any handler becomes an error reply. */
    private fun onCallGuarded(call: MethodCall, result: MethodChannel.Result) {
        try {
            onCall(call, result)
        } catch (e: Throwable) {
            CrashLog.report(Thread.currentThread(), e, "method ${call.method}")
            runCatching { result.error("native", e.message ?: e.toString(), null) }
        }
    }

    /** Calls a String-only static method of [Veiloncore] if this AAR has it, else null. */
    private fun coreOptional(name: String, vararg args: String): String? {
        val m = try {
            Veiloncore::class.java.getMethod(name, *Array(args.size) { String::class.java })
        } catch (_: NoSuchMethodException) {
            return null
        }
        return try {
            m.invoke(null, *args) as String?
        } catch (e: java.lang.reflect.InvocationTargetException) {
            throw (e.targetException ?: e)
        }
    }

    private fun onCall(call: MethodCall, result: MethodChannel.Result) {
        fun s(k: String) = call.argument<String>(k) ?: ""
        fun i(k: String, d: Int) = (call.argument<Any>(k) as? Number)?.toInt() ?: d
        fun sp() = Store.withPaths(this, s("settings"))
        when (call.method) {
            "deviceInfo" -> result.success(deviceInfo())
            "versions" -> bg(result) { Veiloncore.coreVersions() }
            "parseText" -> bg(result) { Veiloncore.parseText(s("text")) }
            "parseConf" -> bg(result) { Veiloncore.parseConf(s("text"), s("name")) }
            "fetchSub" -> bg(result) { Veiloncore.fetchSubscription(s("url"), s("device"), proxyAddr(), 20000) }
            "urlTest" -> bg(result) { Veiloncore.urlTestError(s("profile"), sp(), s("url"), i("timeout", 8000)) }
            "checkProfile" -> bg(result) { Veiloncore.checkProfile(s("profile"), sp()) }
            "coreFor" -> bg(result) { Veiloncore.coreFor(s("profile")) }
            "resolveCore" -> bg(result) { Veiloncore.resolveCoreFor(s("profile"), sp()) }
            "buildConfig" -> bg(result) { Veiloncore.buildConfig(s("profile"), sp()) }
            "profileEdited" -> result.success(Store.updateLastProfile(this, s("profile")))
            // Newer core API (NormalizeProfile / ProfileToLink); resolved at run
            // time so the app still builds and runs against an older AAR (→ null).
            "normalizeProfile" -> bg(result) { coreOptional("normalizeProfile", s("profile")) }
            "profileToLink" -> bg(result) { coreOptional("profileToLink", s("profile")) }
            "parseProfileLink" -> bg(result) { coreOptional("parseProfileLink", s("link")) }
            "connect" -> connect(s("profile"), sp(), s("options"), result)
            "disconnect" -> {
                try {
                    VeilonVpnService.stop(this)
                } catch (e: Throwable) {
                    CrashLog.report(Thread.currentThread(), e, "disconnect")
                    VpnState.set(VpnState.DISCONNECTED)
                }
                result.success(null)
            }
            "state" -> result.success(VpnState.stateMap() + VpnState.statsMap().minus("type"))
            "saveServiceOptions" -> {
                saveOptions(s("options"))
                result.success(null)
            }
            "logs" -> bg(result) { Veiloncore.getLogs((call.argument<Any>("after") as? Number)?.toLong() ?: 0L) }
            "clearLogs" -> bg(result) { Veiloncore.clearLogs(); null }
            "lastCrash" -> bg(result) { Veiloncore.lastCrash() }
            "clearLastCrash" -> bg(result) { Veiloncore.clearLastCrash(); null }
            "installedApps" -> bg(result) { installedApps() }
            "appIcon" -> bg(result) { appIcon(s("package")) }
            "pickFile" -> pickFile(result)
            "scanQr" -> scanQr(result)
            "openUrl" -> result.success(openUrl(s("url")))
            "openVpnSettings" -> {
                val ok = runCatching { startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) }.isSuccess ||
                    runCatching { startActivity(Intent(Settings.ACTION_WIRELESS_SETTINGS)) }.isSuccess
                result.success(ok)
            }
            else -> result.notImplemented()
        }
    }

    // ------------------------------------------------------------ device

    private fun deviceInfo(): Map<String, Any?> {
        @Suppress("DEPRECATION")
        val ver = runCatching { packageManager.getPackageInfo(packageName, 0).versionName }.getOrNull() ?: ""
        val hwid = Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID) ?: ""
        return mapOf(
            "package" to packageName,
            "assetDir" to Store.assetDir(this).absolutePath,
            "hwid" to hwid,
            "os" to "Android",
            "osVersion" to Build.VERSION.RELEASE,
            "sdk" to Build.VERSION.SDK_INT,
            "model" to listOf(Build.MANUFACTURER, Build.MODEL).filter { it.isNotBlank() }.joinToString(" "),
            "appVersion" to ver,
        )
    }

    /** Route subscription fetches through the tunnel's local mixed port when connected. */
    private fun proxyAddr(): String {
        if (VpnState.state != VpnState.CONNECTED) return ""
        val port = runCatching { JSONObject(Store.lastSettings(this) ?: "{}").optInt("mixedPort", 0) }.getOrDefault(0)
        return if (port > 0) "127.0.0.1:$port" else ""
    }

    // ------------------------------------------------------------ VPN

    private fun saveOptions(json: String) {
        // Keep keys the caller did not send (e.g. profileName from the last connect).
        val old = Store.options(this)
        val merged = JSONObject()
            .put("killSwitch", old.killSwitch).put("autoReconnect", old.autoReconnect)
            .put("autoConnect", old.autoConnect).put("language", old.language)
            .put("profileName", old.profileName)
        runCatching {
            val n = JSONObject(json)
            n.keys().forEach { merged.put(it, n.get(it)) }
        }
        Store.saveOptions(this, merged.toString())
    }

    private fun connect(profile: String, settings: String, options: String, result: MethodChannel.Result) {
        if (profile.isBlank()) return result.success("no profile")
        saveOptions(options)
        Store.saveLast(this, profile, settings)
        requestNotificationPermission()
        val prep = VpnService.prepare(this)
        if (prep == null) {
            startVpn(profile, settings, result)
            return
        }
        pendingConnect?.third?.success("denied")
        pendingConnect = Triple(profile, settings, result)
        try {
            startActivityForResult(prep, REQ_VPN)
        } catch (e: ActivityNotFoundException) {
            pendingConnect = null
            result.success("VPN is not supported on this device")
        }
    }

    private fun startVpn(profile: String, settings: String, result: MethodChannel.Result) {
        try {
            VpnState.profileId = VeilonVpnService.profileIdOf(profile)
            VpnState.set(VpnState.CONNECTING)
            VeilonVpnService.start(this, profile, settings, retry = false)
            result.success(null)
        } catch (e: Throwable) {
            VpnState.set(VpnState.ERROR, e.message ?: e.toString())
            result.success(e.message ?: e.toString())
        }
    }

    private fun requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQ_NOTIF)
        }
    }

    // ------------------------------------------------------------ apps

    private fun installedApps(): List<Map<String, Any?>> {
        val pm = packageManager
        @Suppress("DEPRECATION")
        val apps = pm.getInstalledApplications(0)
        return apps.filter { it.packageName != packageName }.map { ai ->
            mapOf(
                "package" to ai.packageName,
                "label" to (runCatching { ai.loadLabel(pm).toString() }.getOrNull() ?: ai.packageName),
                "system" to ((ai.flags and (ApplicationInfo.FLAG_SYSTEM or ApplicationInfo.FLAG_UPDATED_SYSTEM_APP)) != 0),
                "internet" to (pm.checkPermission(Manifest.permission.INTERNET, ai.packageName) == PackageManager.PERMISSION_GRANTED),
            )
        }
    }

    /** App icon as PNG bytes (96 px), or null. */
    private fun appIcon(pkg: String): ByteArray? {
        if (pkg.isBlank()) return null
        val d = runCatching { packageManager.getApplicationIcon(pkg) }.getOrNull() ?: return null
        val size = 96
        val bmp = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
        val c = Canvas(bmp)
        d.setBounds(0, 0, size, size)
        d.draw(c)
        val out = ByteArrayOutputStream()
        bmp.compress(Bitmap.CompressFormat.PNG, 100, out)
        bmp.recycle()
        return out.toByteArray()
    }

    // ------------------------------------------------------------ file picker / QR / links

    private fun pickFile(result: MethodChannel.Result) {
        pendingFile?.success(null)
        pendingFile = result
        val i = Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*")
            .putExtra(Intent.EXTRA_MIME_TYPES, arrayOf("text/plain", "application/octet-stream", "application/x-wireguard-profile", "*/*"))
        try {
            startActivityForResult(i, REQ_FILE)
        } catch (e: ActivityNotFoundException) {
            pendingFile = null
            result.success(null)
        }
    }

    private fun readPicked(uri: Uri): Map<String, Any?>? {
        var name = uri.lastPathSegment ?: "config.conf"
        runCatching {
            contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst()) c.getString(0)?.let { name = it }
            }
        }
        val bytes = contentResolver.openInputStream(uri)?.use { input ->
            val out = ByteArrayOutputStream()
            val buf = ByteArray(8192)
            var total = 0
            while (true) {
                val n = input.read(buf)
                if (n < 0) break
                total += n
                if (total > 4 * 1024 * 1024) throw IllegalArgumentException("file too large")
                out.write(buf, 0, n)
            }
            out.toByteArray()
        } ?: return null
        return mapOf("name" to name, "text" to String(bytes, Charsets.UTF_8))
    }

    private fun scanQr(result: MethodChannel.Result) {
        pendingQr?.success(null)
        pendingQr = result
        try {
            startActivityForResult(Intent(this, QrScanActivity::class.java), REQ_QR)
        } catch (e: Throwable) {
            pendingQr = null
            result.success(null)
        }
    }

    private fun openUrl(url: String): Boolean {
        if (url.isBlank()) return false
        return try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            true
        } catch (e: ActivityNotFoundException) {
            // tg:// without Telegram installed: fall back to t.me.
            if (url.startsWith("tg://")) {
                val web = url.replaceFirst("tg://", "https://t.me/")
                runCatching { startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(web))) }.isSuccess
            } else false
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        when (requestCode) {
            REQ_VPN -> {
                val p = pendingConnect ?: return
                pendingConnect = null
                if (resultCode == Activity.RESULT_OK) startVpn(p.first, p.second, p.third)
                else {
                    VpnState.set(VpnState.DISCONNECTED)
                    p.third.success("denied")
                }
            }
            REQ_FILE -> {
                val r = pendingFile ?: return
                pendingFile = null
                val uri = data?.data
                if (resultCode != Activity.RESULT_OK || uri == null) return r.success(null)
                try {
                    io.execute {
                        val m = runCatching { readPicked(uri) }.getOrNull()
                        main.post { runCatching { r.success(m) } }
                    }
                } catch (e: Throwable) {
                    runCatching { r.success(null) }
                }
            }
            REQ_QR -> {
                val r = pendingQr ?: return
                pendingQr = null
                r.success(if (resultCode == Activity.RESULT_OK) data?.getStringExtra(QrScanActivity.EXTRA_TEXT) else null)
            }
            else -> super.onActivityResult(requestCode, resultCode, data)
        }
    }
}
