package app.veilon

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.drawable.ColorDrawable
import android.view.View
import android.view.WindowInsetsController
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
        private const val REQ_QR_IMAGE = 7003
        private const val REQ_CAMERA = 7005
        private const val REQ_NOTIF = 7004
    }

    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newCachedThreadPool()

    private var pendingConnect: Triple<String, String, MethodChannel.Result>? = null
    private var pendingFile: MethodChannel.Result? = null
    private var pendingQrImage: MethodChannel.Result? = null
    private var pendingCamera: MethodChannel.Result? = null

    // 1.0.10 QR scanner: the live camera view (one at a time) and its channel.
    private var qrView: QrCameraView? = null
    private var qrChannel: MethodChannel? = null

    private var eventListener: ((Map<String, Any?>) -> Unit)? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        CrashLog.install(this)
        runCatching { applyWindowTheme(Store.themeMode(this)) }
        runCatching { io.execute { runCatching { Store.installAssets(applicationContext) } } }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        val messenger = flutterEngine.dartExecutor.binaryMessenger
        MethodChannel(messenger, "app.veilon/core").setMethodCallHandler(::onCallGuarded)
        val qr = MethodChannel(messenger, "app.veilon/qr")
        qrChannel = qr
        runCatching {
            flutterEngine.platformViewsController.registry.registerViewFactory(
                "app.veilon/qr-camera",
                QrCameraFactory(this, qr, onCreated = { v -> qrView?.dispose(); qrView = v }, onGone = { v -> if (qrView === v) qrView = null }),
            )
        }.onFailure { CrashLog.report(Thread.currentThread(), it, "qr view factory") }
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
        runCatching { qrView?.resume() }
    }

    override fun onPause() {
        resumed = false
        updateUiAttached()
        runCatching { qrView?.pause() }
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
            // 1.0.8 (H1): approved certificate pins of literal-IP sub servers.
            "setSubPins" -> bg(result) { Veiloncore.setSubPins(s("pins")); true }
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
            "cameraPermission" -> cameraPermission(call.argument<Boolean>("request") == true, result)
            "qrHasTorch" -> result.success(packageManager.hasSystemFeature(PackageManager.FEATURE_CAMERA_FLASH))
            "qrTorch" -> result.success(qrView?.setTorch(call.argument<Boolean>("on") == true) ?: false)
            "openAppSettings" -> result.success(runCatching {
                startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", packageName, null)))
            }.isSuccess)
            "pickQrImage" -> pickQrImage(result)
            "openUrl" -> result.success(openUrl(s("url")))
            // In-app update (1.0.7): see AppUpdate.kt.
            "installedAbis" -> bg(result) { AppUpdate.installedAbis(this) }
            "updateDir" -> bg(result) { AppUpdate.updateDir(this).absolutePath }
            "sha256File" -> bg(result) { AppUpdate.sha256(this, s("path")) }
            "canInstallApks" -> result.success(AppUpdate.canInstall(this))
            "openInstallPermission" -> result.success(AppUpdate.openInstallPermission(this))
            "installApk" -> result.success(AppUpdate.install(this, s("path")))
            "setTheme" -> result.success(runCatching { applyWindowTheme(s("mode")) }.isSuccess)
            "openVpnSettings" -> {
                val ok = runCatching { startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) }.isSuccess ||
                    runCatching { startActivity(Intent(Settings.ACTION_WIRELESS_SETTINGS)) }.isSuccess
                result.success(ok)
            }
            else -> result.notImplemented()
        }
    }

    // ------------------------------------------------------------ theme

    private fun isDarkMode(mode: String): Boolean = when (mode) {
        "light" -> false
        "system" -> (resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
        else -> true
    }

    /**
     * 1.0.8: window colour and system-bar icon brightness for the app theme
     * ("dark" default, "light", "system"), set before Flutter draws its first
     * frame so a light theme never starts on a dark window (and the other
     * way round). On Android 13+ it also picks the starting-window theme the
     * system uses for the next cold starts.
     */
    private fun applyWindowTheme(mode: String) {
        val dark = isDarkMode(mode)
        window.setBackgroundDrawable(ColorDrawable(if (dark) 0xFF07080C.toInt() else 0xFFF4F5FA.toInt()))
        @Suppress("DEPRECATION")
        if (Build.VERSION.SDK_INT >= 30) {
            val c = window.insetsController
            val mask = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS or WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS
            c?.setSystemBarsAppearance(if (dark) 0 else mask, mask)
        } else {
            var f = window.decorView.systemUiVisibility
            f = if (dark) f and View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR.inv() else f or View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
            if (Build.VERSION.SDK_INT >= 26) {
                f = if (dark) f and View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR.inv() else f or View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR
            }
            window.decorView.systemUiVisibility = f
        }
        if (Build.VERSION.SDK_INT >= 33) {
            val res = when (mode) {
                "light" -> R.style.LaunchThemeLight
                "system" -> R.style.LaunchThemeSystem
                else -> R.style.LaunchTheme
            }
            runCatching { splashScreen.setSplashScreenTheme(res) }
        }
    }

    // ------------------------------------------------------------ device

    private fun deviceInfo(): Map<String, Any?> {
        @Suppress("DEPRECATION")
        val ver = runCatching { packageManager.getPackageInfo(packageName, 0).versionName }.getOrNull() ?: ""
        val hwid = Store.hwid(this, Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID) ?: "")
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

    // ------------------------------------------------------------ QR scanner (1.0.10)

    private fun cameraPermission(request: Boolean, result: MethodChannel.Result) {
        if (checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) return result.success("granted")
        if (!request) {
            // Asked before and the system will not show the dialog again.
            val blocked = Store.prefs(this).getBoolean("cameraAsked", false) && !shouldShowRequestPermissionRationale(Manifest.permission.CAMERA)
            return result.success(if (blocked) "blocked" else "denied")
        }
        pendingCamera?.let { runCatching { it.success("denied") } }
        pendingCamera = result
        try {
            requestPermissions(arrayOf(Manifest.permission.CAMERA), REQ_CAMERA)
        } catch (e: Throwable) {
            pendingCamera = null
            result.success("denied")
        }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != REQ_CAMERA) return
        val r = pendingCamera ?: return
        pendingCamera = null
        val granted = grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED
        // An empty result = the request was interrupted (not a refusal).
        if (grantResults.isNotEmpty()) runCatching { Store.prefs(this).edit().putBoolean("cameraAsked", true).apply() }
        val state = when {
            granted -> "granted"
            grantResults.isNotEmpty() && !shouldShowRequestPermissionRationale(Manifest.permission.CAMERA) -> "blocked"
            else -> "denied"
        }
        runCatching { r.success(state) }
    }

    private fun pickQrImage(result: MethodChannel.Result) {
        pendingQrImage?.let { runCatching { it.success(null) } }
        pendingQrImage = result
        val ok = runCatching { startActivityForResult(QrImage.pickIntent(), REQ_QR_IMAGE) }.isSuccess ||
            runCatching { startActivityForResult(QrImage.fallbackIntent(), REQ_QR_IMAGE) }.isSuccess
        if (!ok) {
            pendingQrImage = null
            result.success(mapOf("error" to "unreadable"))
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
            REQ_QR_IMAGE -> {
                val r = pendingQrImage ?: return
                pendingQrImage = null
                val uri = data?.data
                if (resultCode != Activity.RESULT_OK || uri == null) return r.success(null)
                try {
                    io.execute {
                        val m = runCatching { QrImage.decode(applicationContext, uri) }.getOrElse { mapOf("error" to "unreadable") }
                        main.post { runCatching { r.success(m) } }
                    }
                } catch (e: Throwable) {
                    runCatching { r.success(mapOf("error" to "unreadable")) }
                }
            }
            else -> super.onActivityResult(requestCode, resultCode, data)
        }
    }
}
