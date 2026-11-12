// Off-device unit test of app.veilon.TunnelWatch (1.0.6 kill-switch-off fix).
// Compiled with the Gradle cache's kotlin-compiler-embeddable together with
// ../app/src/main/kotlin/app/veilon/TunnelWatch.kt (see logs/ktest106.sh).
package app.veilon

private var fails = 0
private fun check(name: String, ok: Boolean) {
    println((if (ok) "PASS " else "FAIL ") + name)
    if (!ok) fails++
}

fun main() {
    val W = TunnelWatch
    // Healthy session: never probes.
    check("healthy: no probe", !W.shouldProbe(false, true, 0, "ok", 60_000, false))
    check("short stall: no probe", !W.shouldProbe(false, true, 9_999, "ok", 60_000, false))
    // Dead tunnel with the kill switch off: probe.
    check("stalled 10s, ks off: probe", W.shouldProbe(false, true, 10_000, "ok", 60_000, false))
    check("start check unreachable, ks off: probe", W.shouldProbe(false, true, 0, "unreachable", 60_000, false))
    // Kill switch on: blocking is wanted, never probes / tears down.
    check("ks on: no probe", !W.shouldProbe(true, true, 120_000, "unreachable", 60_000, false))
    // Not connected / already probing / rate limit.
    check("not connected: no probe", !W.shouldProbe(false, false, 120_000, "ok", 60_000, false))
    check("probing: no second probe", !W.shouldProbe(false, true, 120_000, "ok", 60_000, true))
    check("rate limited", !W.shouldProbe(false, true, 120_000, "ok", 29_999, false))
    // Lost: what happens.
    check("ks off + reconnect: VPN released, reconnect", W.onLost(false, true) == TunnelWatch.Lost.RECONNECT_WITHOUT_VPN)
    check("ks off, no reconnect: stop", W.onLost(false, false) == TunnelWatch.Lost.STOP)
    check("ks on: keep blocking", W.onLost(true, true) == TunnelWatch.Lost.KEEP_BLOCKING && W.onLost(true, false) == TunnelWatch.Lost.KEEP_BLOCKING)
    // While reconnecting: tun kept / blocker only with the kill switch on.
    check("reconnect wait, ks off: no tun", !W.blockWhileReconnecting(false))
    check("reconnect wait, ks on: block", W.blockWhileReconnecting(true))
    check("ks off: server checked before the tun comes back", W.checkServerBeforeReconnect(false) && !W.checkServerBeforeReconnect(true))
    // 1.0.8 (M2): core died, auto reconnect off.
    check("ks on + no reconnect: keep blocking", W.blockAfterCoreDeath(true, false))
    check("ks off + no reconnect: VPN off", !W.blockAfterCoreDeath(false, false))
    check("auto reconnect on: reconnect path", !W.blockAfterCoreDeath(true, true) && !W.blockAfterCoreDeath(false, true))
    // 1.0.8 (L1): hashed device ID.
    val h = Hwid.hashed("0123456789abcdef")
    check("hwid: 16 lower hex", h.length == 16 && h.all { it in "0123456789abcdef" })
    check("hwid: stable", h == Hwid.hashed("0123456789abcdef"))
    check("hwid: not the raw id", h != "0123456789abcdef" && Hwid.hashed("0123456789abcdee") != h)
    check("hwid: empty stays empty", Hwid.hashed("") == "")
    check("hwid: legacy installs keep the raw id", Hwid.choose("0123456789abcdef", legacy = true) == "0123456789abcdef")
    check("hwid: new installs send the hash", Hwid.choose("0123456789abcdef", legacy = false) == h)
    // 1.0.8 (L5): update APK signer check.
    check("signers equal", AppUpdateCerts.sameSigners(setOf("aa", "bb"), setOf("bb", "aa")))
    check("signers differ", !AppUpdateCerts.sameSigners(setOf("aa"), setOf("cc")))
    check("signers: empty never matches", !AppUpdateCerts.sameSigners(emptySet(), emptySet()) && !AppUpdateCerts.sameSigners(setOf("aa"), emptySet()))
    // URLTestError parsing.
    check("answered 157", W.answered("{\"delay\":157}"))
    check("answered 0", W.answered("{\"delay\": 0}"))
    check("timeout", !W.answered("{\"delay\":-1,\"error\":\"context deadline exceeded\"}"))
    check("empty/null", !W.answered("") && !W.answered(null) && !W.answered("garbage"))
    println(if (fails == 0) "KTEST_OK" else "KTEST_FAIL $fails")
    if (fails != 0) System.exit(1)
}
