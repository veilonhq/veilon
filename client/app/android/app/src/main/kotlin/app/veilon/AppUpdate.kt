package app.veilon

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import java.io.File
import java.security.MessageDigest
import java.util.zip.ZipFile

/**
 * In-app update helpers (1.0.7). The Dart side checks GitHub, downloads the
 * APK of the installed variant into cache/updates/, verifies its sha256 here
 * and hands it to the system package installer (the user taps Install; same
 * signing key, so it updates in place and keeps the data).
 */
object AppUpdate {
    private const val APK_MIME = "application/vnd.android.package-archive"

    fun updateDir(ctx: Context): File = File(ctx.cacheDir, "updates").apply { mkdirs() }

    /**
     * ABI folders (lib/<abi>/) in the installed APK(s): arm64-v8a only = the
     * arm64 split, armeabi-v7a only = the v7a split, both = universal.
     */
    fun installedAbis(ctx: Context): List<String> {
        val ai = ctx.applicationInfo
        val files = listOfNotNull(ai.sourceDir) + (ai.splitSourceDirs?.toList() ?: emptyList())
        val abis = sortedSetOf<String>()
        for (path in files) {
            runCatching {
                ZipFile(path).use { z ->
                    val e = z.entries()
                    while (e.hasMoreElements()) {
                        val n = e.nextElement().name
                        if (n.startsWith("lib/")) {
                            val abi = n.substring(4).substringBefore('/', "")
                            if (abi.isNotEmpty() && n.endsWith(".so")) abis.add(abi)
                        }
                    }
                }
            }
        }
        return abis.toList()
    }

    private fun inUpdateDir(ctx: Context, path: String): File? {
        if (path.isBlank()) return null
        val f = File(path).canonicalFile
        val dir = updateDir(ctx).canonicalFile
        return if (f.parentFile == dir && f.isFile) f else null
    }

    /** Lower-case hex sha256 of a file in the update folder, or "" when missing. */
    fun sha256(ctx: Context, path: String): String {
        val f = inUpdateDir(ctx, path) ?: return ""
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

    /** Whether this app may open the package installer ("install unknown apps"). */
    fun canInstall(ctx: Context): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return true
        return try {
            ctx.packageManager.canRequestPackageInstalls()
        } catch (e: Throwable) {
            false
        }
    }

    /** Opens the "install unknown apps" page for this app. */
    fun openInstallPermission(ctx: Context): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return false
        return try {
            ctx.startActivity(
                Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + ctx.packageName))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            )
            true
        } catch (e: Throwable) {
            false
        }
    }

    /**
     * Starts the system installer for the APK at [path] (must be in the
     * update folder). Returns "ok", "permission" (the unknown-apps page was
     * opened; try again after the user allows it) or an error text.
     */
    fun install(ctx: Context, path: String): String {
        val f = inUpdateDir(ctx, path) ?: return "file missing"
        if (!canInstall(ctx)) {
            return if (openInstallPermission(ctx)) "permission" else "install not allowed"
        }
        return try {
            val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".updates", f)
            val i = Intent(Intent.ACTION_VIEW)
                .setDataAndType(uri, APK_MIME)
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
            ctx.startActivity(i)
            "ok"
        } catch (e: ActivityNotFoundException) {
            "no installer"
        } catch (e: Throwable) {
            CrashLog.report(Thread.currentThread(), e, "installApk")
            e.message ?: e.toString()
        }
    }
}
