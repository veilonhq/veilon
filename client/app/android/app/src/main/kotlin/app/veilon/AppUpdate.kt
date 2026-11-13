package app.veilon

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.content.pm.Signature
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

    /** 1.0.9: see [UpdatePaths.resolve] (no Android dependency, unit-tested). */
    private fun inUpdateDir(ctx: Context, path: String): File? = UpdatePaths.resolve(updateDir(ctx), path)

    /**
     * Lower-case hex sha256 of a file in the update folder. 1.0.9: throws
     * with the reason (missing / outside the folder / read error) instead of
     * returning "", so the Dart side can tell a failed check from a real
     * mismatch (the Dart side hashes first; this is its fallback).
     */
    fun sha256(ctx: Context, path: String): String {
        return UpdatePaths.sha256(updateDir(ctx), path)
    }

    /** SHA-256 (hex) of each signing certificate of [pi], current signers only. */
    @Suppress("DEPRECATION")
    private fun signerDigests(pi: PackageInfo?): Set<String> {
        if (pi == null) return emptySet()
        val sigs: Array<Signature>? = if (Build.VERSION.SDK_INT >= 28) {
            val si = pi.signingInfo ?: return emptySet()
            if (si.hasMultipleSigners()) si.apkContentsSigners else si.signingCertificateHistory?.let { h ->
                // Current signer = last entry of the rotation history.
                if (h.isEmpty()) null else arrayOf(h.last())
            }
        } else pi.signatures
        val md = { b: ByteArray -> MessageDigest.getInstance("SHA-256").digest(b).joinToString("") { "%02x".format(it) } }
        return sigs?.map { md(it.toByteArray()) }?.toSet() ?: emptySet()
    }

    /** The downloaded APK is signed by the same certificate(s) as the installed app. */
    @Suppress("DEPRECATION")
    fun signerMatches(ctx: Context, apk: File): Boolean = try {
        val pm = ctx.packageManager
        val flags = if (Build.VERSION.SDK_INT >= 28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
        val archive = pm.getPackageArchiveInfo(apk.absolutePath, flags)
        val installed = pm.getPackageInfo(ctx.packageName, flags)
        val apkCerts = signerDigests(archive)
        when {
            archive == null || archive.packageName != ctx.packageName -> false
            // Android 7–8.1 may not report the certificates of a v2/v3-only
            // archive; the package installer still refuses an update signed
            // by another key there, so let it decide.
            apkCerts.isEmpty() && Build.VERSION.SDK_INT < 28 -> true
            else -> AppUpdateCerts.sameSigners(apkCerts, signerDigests(installed))
        }
    } catch (e: Throwable) {
        CrashLog.report(Thread.currentThread(), e, "update signer check")
        false
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
        // 1.0.8 (audit L5): only hand over an APK signed with exactly the
        // installed app's certificate(s).
        if (!signerMatches(ctx, f)) return "signature mismatch"
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
