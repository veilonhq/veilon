package app.veilon

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Build
import android.os.ext.SdkExtensions
import android.provider.MediaStore

/**
 * 1.0.10: "pick from gallery" in the QR scanner. The system photo picker
 * needs no storage permission (Android 13+, and Android 11/12 with the
 * picker module); older phones get the system document picker for images
 * (ACTION_GET_CONTENT), which also needs no permission: the returned URI
 * carries a one-time read grant.
 */
object QrImage {

    /** Longest side the picked image is decoded at (memory bound). */
    private const val MAX_SIDE = 2400

    fun hasPhotoPicker(): Boolean = when {
        Build.VERSION.SDK_INT >= 33 -> true
        Build.VERSION.SDK_INT >= 30 -> runCatching { SdkExtensions.getExtensionVersion(Build.VERSION_CODES.R) >= 2 }.getOrDefault(false)
        else -> false
    }

    fun pickIntent(): Intent =
        if (hasPhotoPicker()) Intent(MediaStore.ACTION_PICK_IMAGES).setType("image/*")
        else fallbackIntent()

    fun fallbackIntent(): Intent = Intent(Intent.ACTION_GET_CONTENT)
        .setType("image/*")
        .addCategory(Intent.CATEGORY_OPENABLE)
        .putExtra(Intent.EXTRA_LOCAL_ONLY, true)

    /** {"text": …} or {"error": "noQr" | "unreadable"}. Runs off the main thread. */
    fun decode(ctx: Context, uri: Uri): Map<String, Any?> {
        val bmp = try {
            load(ctx, uri)
        } catch (t: Throwable) { // OOM included: a huge image must not take the app down
            null
        } ?: return mapOf("error" to "unreadable")
        return try {
            val w = bmp.width
            val h = bmp.height
            val px = IntArray(w * h)
            bmp.getPixels(px, 0, w, 0, 0, w, h)
            bmp.recycle()
            val text = QrDecode.decodeArgb(px, w, h)
            if (text.isNullOrEmpty()) mapOf("error" to "noQr") else mapOf("text" to text)
        } catch (t: Throwable) {
            mapOf("error" to "unreadable")
        }
    }

    private fun load(ctx: Context, uri: Uri): Bitmap? {
        val cr = ctx.contentResolver
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) } ?: return null
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / sample > MAX_SIDE) sample *= 2
        val opts = BitmapFactory.Options().apply {
            inSampleSize = sample
            inPreferredConfig = Bitmap.Config.ARGB_8888
        }
        return cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, opts) }
    }

    @Suppress("unused")
    private fun isOk(resultCode: Int) = resultCode == Activity.RESULT_OK
}
