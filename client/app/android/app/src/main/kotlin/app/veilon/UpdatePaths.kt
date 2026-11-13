package app.veilon

import java.io.File
import java.io.FileNotFoundException
import java.security.MessageDigest

/**
 * Update-folder file rules (1.0.9), free of Android classes so they can be
 * unit-tested off-device (kt-tests/UpdatePathsTest.kt).
 */
object UpdatePaths {
    private val APK_NAME = Regex("^[A-Za-z0-9_-][A-Za-z0-9._-]*\\.apk$")

    /**
     * The file [path] names inside [dir], or null. The file is resolved by
     * its plain name inside [dir] (letters, digits, '.', '_', '-', ending in
     * .apk; no separators, no leading dot), so the same folder spelled
     * /data/user/0/... or /data/data/... is still found, and nothing outside
     * the folder can be named. The given parent must be [dir] as given,
     * absolute, or canonical.
     */
    fun resolve(dir: File, path: String): File? {
        if (path.isBlank()) return null
        val given = File(path)
        val name = given.name
        if (!APK_NAME.matches(name)) return null
        val parent = given.parentFile ?: return null
        val sameDir = parent.path == dir.path || parent.absolutePath == dir.absolutePath ||
            runCatching { parent.canonicalPath == dir.canonicalPath }.getOrDefault(false)
        if (!sameDir) return null
        val f = File(dir, name)
        return if (f.isFile) f else null
    }

    /**
     * Lower-case hex sha256 of the update file [path] in [dir]. Throws with
     * the reason (no path / file missing / not in the update folder / read
     * error); never returns "".
     */
    fun sha256(dir: File, path: String): String {
        if (path.isBlank()) throw IllegalArgumentException("no path")
        val f = resolve(dir, path) ?: run {
            if (!File(path).exists()) throw FileNotFoundException("file missing")
            throw SecurityException("file is not in the update folder")
        }
        val md = MessageDigest.getInstance("SHA-256")
        f.inputStream().use { input ->
            val buf = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(buf)
                if (n < 0) break
                md.update(buf, 0, n)
            }
        }
        return md.digest().joinToString("") { "%02x".format(it) }
    }
}
