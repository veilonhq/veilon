// Off-device unit test of app.veilon.UpdatePaths (1.0.9 updater fix).
// Compiled with ../app/src/main/kotlin/app/veilon/UpdatePaths.kt (see logs/ktest109.sh).
package app.veilon

import java.io.File
import java.nio.file.Files
import java.security.MessageDigest

private var fails = 0
private fun check(name: String, ok: Boolean) {
    println((if (ok) "PASS " else "FAIL ") + name)
    if (!ok) fails++
}

private inline fun <reified T : Throwable> throwsMsg(block: () -> Unit): String? =
    try { block(); null } catch (e: Throwable) { if (e is T) (e.message ?: "") else "WRONG ${e.javaClass.name}" }

fun main() {
    val root = Files.createTempDirectory("vupd").toFile()
    // Real folder + an alias (like /data/user/0 -> /data/data).
    val realBase = File(root, "data/data/app.veilon/cache").apply { mkdirs() }
    val dir = File(realBase, "updates").apply { mkdirs() }
    val aliasBase = File(root, "data/user/0").apply { mkdirs() }
    Files.createSymbolicLink(File(aliasBase, "app.veilon").toPath(), File(root, "data/data/app.veilon").toPath())
    val aliasDir = File(aliasBase, "app.veilon/cache/updates")
    val bytes = ByteArray(3 * 1024 * 1024 + 17) { (it * 31 + 7).toByte() }
    File(dir, "veilon-client-universal.apk").writeBytes(bytes)
    File(root, "outside.apk").writeBytes(bytes)
    val want = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

    // Folder given as the app sees it (alias) and the file path built from it.
    val viaAlias = aliasDir.path + "/veilon-client-universal.apk"
    check("alias dir + alias path resolves", UpdatePaths.resolve(aliasDir, viaAlias) != null)
    check("alias dir + real path resolves (canonical)", UpdatePaths.resolve(aliasDir, dir.path + "/veilon-client-universal.apk") != null)
    check("real dir + alias path resolves (canonical)", UpdatePaths.resolve(dir, viaAlias) != null)
    check("sha256 via alias = expected", UpdatePaths.sha256(aliasDir, viaAlias) == want)
    check("sha256 via real = expected", UpdatePaths.sha256(dir, dir.path + "/veilon-client-universal.apk") == want)

    // Rejections, each with a reason (never "").
    check("outside folder rejected", UpdatePaths.resolve(dir, root.path + "/outside.apk") == null)
    check("traversal rejected", UpdatePaths.resolve(dir, dir.path + "/../../../../outside.apk") == null)
    check("non-apk rejected", UpdatePaths.resolve(dir, dir.path + "/x.txt") == null)
    check("hidden name rejected", UpdatePaths.resolve(dir, dir.path + "/.x.apk") == null)
    check("blank rejected", UpdatePaths.resolve(dir, "") == null)
    check("missing file: FileNotFound", throwsMsg<java.io.FileNotFoundException> { UpdatePaths.sha256(dir, dir.path + "/nope.apk") } == "file missing")
    check("outside: SecurityException", throwsMsg<SecurityException> { UpdatePaths.sha256(dir, root.path + "/outside.apk") } == "file is not in the update folder")
    check("blank: IllegalArgument", throwsMsg<IllegalArgumentException> { UpdatePaths.sha256(dir, " ") } == "no path")

    root.deleteRecursively()
    if (fails > 0) { println("KTEST_FAIL $fails"); kotlin.system.exitProcess(1) }
    println("KTEST_OK")
}
