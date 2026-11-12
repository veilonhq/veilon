package app.veilon

import java.security.MessageDigest

/**
 * 1.0.8 (audit L1): the device ID sent to subscription panels (X-Hwid).
 *
 * Until 1.0.7 it was the raw Settings.Secure.ANDROID_ID. New installs now
 * send a hash of it instead (16 hex chars, the same shape as an ANDROID_ID),
 * so the raw platform ID never leaves the phone. Installs that already
 * talked to a panel keep sending the raw ID: panels with a device limit
 * bound that value, and a new ID would count as another device (and could
 * be refused). The choice is stored once per install (Store.hwidMode).
 *
 * Pure (no Android types): unit-tested off-device (kt-tests).
 */
internal object Hwid {
    private const val SALT = "app.veilon/hwid/v1:"

    fun hashed(androidId: String): String {
        if (androidId.isEmpty()) return ""
        val d = MessageDigest.getInstance("SHA-256").digest((SALT + androidId).toByteArray(Charsets.UTF_8))
        return d.take(8).joinToString("") { "%02x".format(it) }
    }

    fun choose(androidId: String, legacy: Boolean): String = if (legacy) androidId else hashed(androidId)
}

/** Pure part of the update signer check (AppUpdate.signerCheck). */
internal object AppUpdateCerts {
    /** Same non-empty set of signing-certificate SHA-256 digests. */
    fun sameSigners(apk: Set<String>, installed: Set<String>): Boolean =
        apk.isNotEmpty() && installed.isNotEmpty() && apk == installed
}
