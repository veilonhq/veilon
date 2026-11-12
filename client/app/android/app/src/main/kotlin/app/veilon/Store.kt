package app.veilon

import android.content.Context
import android.content.SharedPreferences
import android.os.Handler
import android.os.Looper
import org.json.JSONObject
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList

/** Native-side persistence: last connect arguments + service options. */
object Store {
    private const val FILE = "veilon_native"
    private const val K_PROFILE = "lastProfile"
    private const val K_SETTINGS = "lastSettings"
    private const val K_OPTIONS = "serviceOptions"

    private fun prefs(ctx: Context): SharedPreferences =
        ctx.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    fun saveLast(ctx: Context, profile: String, settings: String) {
        prefs(ctx).edit().putString(K_PROFILE, profile).putString(K_SETTINGS, settings).apply()
    }

    fun lastProfile(ctx: Context): String? = prefs(ctx).getString(K_PROFILE, null)

    /**
     * The user edited a profile in the app: if it is the one the tile / boot
     * receiver would reconnect with, store the edited JSON. Returns true when replaced.
     */
    fun updateLastProfile(ctx: Context, profile: String): Boolean {
        val id = runCatching { JSONObject(profile).optString("id") }.getOrDefault("")
        val last = lastProfile(ctx) ?: return false
        val lastId = runCatching { JSONObject(last).optString("id") }.getOrDefault("")
        if (id.isEmpty() || id != lastId) return false
        prefs(ctx).edit().putString(K_PROFILE, profile).apply()
        return true
    }
    fun lastSettings(ctx: Context): String? = prefs(ctx).getString(K_SETTINGS, null)

    fun saveOptions(ctx: Context, json: String) {
        prefs(ctx).edit().putString(K_OPTIONS, json).apply()
    }

    fun options(ctx: Context): ServiceOptions =
        ServiceOptions.parse(prefs(ctx).getString(K_OPTIONS, null))

    /**
     * Reads the Flutter-side settings (shared_preferences legacy store:
     * file "FlutterSharedPreferences", keys prefixed "flutter.").
     */
    fun flutterSettings(ctx: Context): JSONObject? {
        val p = ctx.applicationContext.getSharedPreferences("FlutterSharedPreferences", Context.MODE_PRIVATE)
        val s = p.getString("flutter.settings", null) ?: return null
        return runCatching { JSONObject(s) }.getOrNull()
    }

    /** Rule-set files the core reads from Settings.assetDir. */
    private val RULE_FILES = listOf("geoip-ir.srs", "geosite-ir.srs", "geosite-category-ads-all.srs")

    fun assetDir(ctx: Context): File = File(ctx.filesDir, "rules")

    /** Writable core state dir (sing-box cache.db, working dir): filesDir/core. */
    fun dataDir(ctx: Context): File = File(ctx.filesDir, "core").apply { mkdirs() }

    /**
     * Adds absolute app-storage paths (assetDir, dataDir, tempDir) to a core
     * settings JSON. The process cwd on Android is "/" (read-only), so the
     * cores must never fall back to relative paths (v1.0.0 cache.db bug).
     */
    fun withPaths(ctx: Context, settings: String?): String {
        val o = runCatching { JSONObject(if (settings.isNullOrBlank()) "{}" else settings) }.getOrElse { return settings ?: "" }
        o.put("assetDir", assetDir(ctx).absolutePath)
        o.put("dataDir", dataDir(ctx).absolutePath)
        o.put("tempDir", ctx.cacheDir.absolutePath)
        return o.toString()
    }

    /** Copies bundled Flutter assets (assets/rules/\*.srs) to filesDir/rules when the APK changed. */
    fun installAssets(ctx: Context) {
        val dir = assetDir(ctx)
        val stamp = File(dir, ".stamp")
        val ver = runCatching {
            @Suppress("DEPRECATION")
            ctx.packageManager.getPackageInfo(ctx.packageName, 0).lastUpdateTime.toString()
        }.getOrDefault("0")
        if (stamp.exists() && stamp.readText() == ver && RULE_FILES.all { File(dir, it).exists() }) return
        dir.mkdirs()
        for (name in RULE_FILES) {
            runCatching {
                ctx.assets.open("flutter_assets/assets/rules/$name").use { input ->
                    File(dir, "$name.tmp").outputStream().use { input.copyTo(it) }
                }
                File(dir, "$name.tmp").renameTo(File(dir, name))
            }
        }
        stamp.writeText(ver)
    }
}

data class ServiceOptions(
    val killSwitch: Boolean = false,
    val autoReconnect: Boolean = true,
    val autoConnect: Boolean = false,
    val language: String = "fa",
    val profileName: String = "",
) {
    companion object {
        fun parse(s: String?): ServiceOptions {
            if (s.isNullOrBlank()) return ServiceOptions()
            val j = runCatching { JSONObject(s) }.getOrNull() ?: return ServiceOptions()
            return ServiceOptions(
                killSwitch = j.optBoolean("killSwitch", false),
                autoReconnect = j.optBoolean("autoReconnect", true),
                autoConnect = j.optBoolean("autoConnect", false),
                language = j.optString("language", "fa"),
                profileName = j.optString("profileName", ""),
            )
        }
    }
}

/** Process-wide connection state shared by the service, activity, tile. */
object VpnState {
    const val DISCONNECTED = "disconnected"
    const val CONNECTING = "connecting"
    const val CONNECTED = "connected"
    const val DISCONNECTING = "disconnecting"
    const val ERROR = "error"

    @Volatile var state: String = DISCONNECTED
        private set
    @Volatile var error: String = ""
        private set
    @Volatile var profileId: String? = null
    @Volatile var since: Long = 0
    @Volatile var sessionDown: Long = 0
    @Volatile var sessionUp: Long = 0
    /** Core ProxyStatus: "" checking, "ok", "unreachable" (one-time proxy check failed, nothing came back since). */
    @Volatile var proxy: String = ""
    @Volatile var down: Double = 0.0
    @Volatile var up: Double = 0.0

    private val main = Handler(Looper.getMainLooper())
    /** Listeners always called on the main thread. */
    val listeners = CopyOnWriteArrayList<(Map<String, Any?>) -> Unit>()

    /**
     * True while the Flutter UI has the event stream open. The service only
     * emits per-second stats / log events while this is set (battery).
     */
    @Volatile var uiAttached = false
        set(v) {
            field = v
            onUiChanged?.invoke()
        }
    var onUiChanged: (() -> Unit)? = null

    fun set(newState: String, err: String = "") {
        state = newState
        error = err
        if (newState == CONNECTED && since == 0L) since = System.currentTimeMillis()
        if (newState != CONNECTED && newState != CONNECTING) {
            since = 0; down = 0.0; up = 0.0
        }
        emit(stateMap())
    }

    fun stats(d: Double, u: Double, sd: Long, su: Long) {
        down = d; up = u; sessionDown = sd; sessionUp = su
        emit(statsMap())
    }

    fun stateMap(): Map<String, Any?> = mapOf(
        "type" to "state", "state" to state, "error" to error, "profileId" to profileId,
        "since" to since, "sessionDown" to sessionDown, "sessionUp" to sessionUp,
    )

    fun statsMap(): Map<String, Any?> = mapOf(
        "type" to "stats", "down" to down, "up" to up, "sessionDown" to sessionDown, "sessionUp" to sessionUp,
        "proxy" to proxy,
    )

    fun emit(event: Map<String, Any?>) {
        if (Looper.myLooper() == Looper.getMainLooper()) listeners.forEach { it(event) }
        else main.post { listeners.forEach { it(event) } }
    }
}
