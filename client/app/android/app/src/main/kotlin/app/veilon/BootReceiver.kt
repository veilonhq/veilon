package app.veilon

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

/**
 * Auto-connects after boot when the user enabled "auto connect" in the app
 * (Flutter shared_preferences: FlutterSharedPreferences / "flutter.settings"
 * JSON, field autoConnect) and VPN permission is already granted.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        try {
            receive(context, intent)
        } catch (e: Throwable) {
            runCatching { CrashLog.install(context); CrashLog.report(Thread.currentThread(), e, "boot") }
        }
    }

    private fun receive(context: Context, intent: Intent) {
        when (intent.action) {
            // 1.0.8 (audit L3): BOOT_COMPLETED only. QUICKBOOT_POWERON is not a
            // protected broadcast, so any app could trigger an auto-connect.
            Intent.ACTION_BOOT_COMPLETED -> {}
            else -> return
        }
        val ctx = context.applicationContext
        val enabled = Store.flutterSettings(ctx)?.optBoolean("autoConnect", false)
            ?: Store.options(ctx).autoConnect
        if (!enabled) return
        val profile = Store.lastProfile(ctx) ?: return
        if (VpnService.prepare(ctx) != null) return
        runCatching {
            Store.installAssets(ctx)
            VeilonVpnService.start(ctx, profile, Store.lastSettings(ctx), retry = true)
        }
    }
}
