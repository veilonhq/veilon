package app.veilon

/**
 * 1.0.6: what the service does when the tunnel dies while the core session
 * keeps "running" (server gone, server-side block, sidecar dead).
 *
 * Bug until 1.0.6: with the kill switch OFF the phone still lost its
 * internet when the tunnel dropped. Two causes:
 *  1. Nothing noticed a dead tunnel. The only health check asked the core
 *     IsRunning(), which stays true while the server no longer answers, so
 *     the tun kept capturing every app's traffic into a dead proxy - the
 *     phone behaved as if the kill switch were on.
 *  2. Even on the reconnect path, "kill switch off" only closed the
 *     service's own copy of the tun fd. The core holds a dup() of it, so the
 *     VPN interface stayed up (and still captured everything) until the
 *     next start.
 *
 * Now: the core reports how long proxied connections have waited without
 * any answer (Veiloncore.proxyStalledMs, or proxyStatus "unreachable" after
 * the start check). When that passes [STALL_MS] the service confirms with
 * one real delay test of the server (the ping path, off the VPN). If the
 * server does not answer and the kill switch is off, the core session is
 * stopped and the tun closed - normal internet - and, with auto reconnect
 * on, reconnect attempts (each first checking the server, so the tun only
 * comes back once the server answers) run in the background. With the kill
 * switch ON nothing changes: traffic stays blocked until it reconnects.
 *
 * Pure logic (no Android types) so it is unit-tested off-device
 * (android/kt-tests/TunnelWatchTest.kt).
 */
internal object TunnelWatch {
    /** Proxied connections unanswered this long = suspected tunnel loss. */
    const val STALL_MS = 10_000L
    /** At most one confirming server test per this period. */
    const val PROBE_GAP_MS = 30_000L
    /** Timeout of the confirming server test. */
    const val PROBE_TIMEOUT_MS = 8_000

    enum class Lost { RECONNECT_WITHOUT_VPN, STOP, KEEP_BLOCKING }

    /** Should the service run a confirming server test now? */
    fun shouldProbe(
        killSwitch: Boolean,
        connected: Boolean,
        stalledMs: Long,
        proxyStatus: String,
        msSinceLastProbe: Long,
        probing: Boolean,
    ): Boolean {
        // Kill switch on: blocking while the tunnel is down is wanted.
        if (killSwitch || !connected || probing) return false
        if (msSinceLastProbe < PROBE_GAP_MS) return false
        return stalledMs >= STALL_MS || proxyStatus == "unreachable"
    }

    /** The confirming test failed (server does not answer). */
    fun onLost(killSwitch: Boolean, autoReconnect: Boolean): Lost = when {
        killSwitch -> Lost.KEEP_BLOCKING
        autoReconnect -> Lost.RECONNECT_WITHOUT_VPN
        else -> Lost.STOP
    }

    /**
     * While waiting to reconnect: keep the (dead) tun / a blocking tun only
     * with the kill switch on. Off = stop the core session too (it holds a
     * dup of the tun fd) so the VPN interface really goes away.
     */
    fun blockWhileReconnecting(killSwitch: Boolean): Boolean = killSwitch

    /** Before a background reconnect brings the tun back: check the server first? */
    fun checkServerBeforeReconnect(killSwitch: Boolean): Boolean = !killSwitch

    /** Parses URLTestError's {"delay": n, "error": "..."}: true when the server answered. */
    fun answered(urlTestJson: String?): Boolean {
        if (urlTestJson.isNullOrBlank()) return false
        val m = Regex("\"delay\"\\s*:\\s*(-?\\d+)").find(urlTestJson) ?: return false
        return (m.groupValues[1].toLongOrNull() ?: -1L) >= 0
    }
}
