package app.veilon

import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService

/** Quick Settings tile: toggles the VPN with the last used profile. */
class VeilonTileService : TileService() {

    companion object {
        fun refresh(ctx: Context) {
            runCatching { requestListeningState(ctx, ComponentName(ctx, VeilonTileService::class.java)) }
        }
    }

    private val listener: (Map<String, Any?>) -> Unit = { e -> if (e["type"] == "state") update() }

    override fun onCreate() {
        super.onCreate()
        CrashLog.install(this)
    }

    override fun onStartListening() {
        super.onStartListening()
        VpnState.listeners.add(listener)
        update()
    }

    override fun onStopListening() {
        VpnState.listeners.remove(listener)
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        try {
            click()
        } catch (e: Throwable) {
            CrashLog.report(Thread.currentThread(), e, "tile")
        }
    }

    private fun click() {
        when (VpnState.state) {
            VpnState.CONNECTED, VpnState.CONNECTING -> VeilonVpnService.stop(this)
            VpnState.DISCONNECTING -> {}
            else -> {
                val profile = Store.lastProfile(this)
                if (profile == null || VpnService.prepare(this) != null) {
                    openApp()
                    return
                }
                try {
                    VpnState.profileId = VeilonVpnService.profileIdOf(profile)
                    VpnState.set(VpnState.CONNECTING)
                    VeilonVpnService.start(this, profile, Store.lastSettings(this), retry = false)
                } catch (e: Throwable) {
                    // Background FGS start refused: let the app start it.
                    VpnState.set(VpnState.DISCONNECTED)
                    openApp()
                }
            }
        }
        update()
    }

    private fun openApp() {
        val i = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= 34) {
            startActivityAndCollapse(PendingIntent.getActivity(this, 0, i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(i)
        }
    }

    private fun update() {
        runCatching { updateTile() }
    }

    private fun updateTile() {
        val t = qsTile ?: return
        val fa = Store.options(this).language == "fa"
        t.icon = Icon.createWithResource(this, R.drawable.ic_stat_veilon)
        t.label = "Veilon"
        when (VpnState.state) {
            VpnState.CONNECTED -> {
                t.state = Tile.STATE_ACTIVE
                if (Build.VERSION.SDK_INT >= 29) t.subtitle = if (fa) "متصل" else "Connected"
            }
            VpnState.CONNECTING -> {
                t.state = Tile.STATE_ACTIVE
                if (Build.VERSION.SDK_INT >= 29) t.subtitle = if (fa) "در حال اتصال" else "Connecting"
            }
            else -> {
                t.state = Tile.STATE_INACTIVE
                if (Build.VERSION.SDK_INT >= 29) t.subtitle = if (fa) "قطع" else "Off"
            }
        }
        t.updateTile()
    }
}
