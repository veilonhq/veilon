package app.veilon

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.BitmapRegionDecoder
import android.graphics.ImageDecoder
import android.graphics.Rect
import android.media.ExifInterface
import android.net.Uri
import android.os.Build
import android.os.ext.SdkExtensions
import android.provider.MediaStore
import java.io.File

/**
 * 1.0.10: "pick from gallery" in the QR scanner. The system photo picker
 * needs no storage permission (Android 13+, and Android 11/12 with the
 * picker module); older phones get the system document picker for images
 * (ACTION_GET_CONTENT), which also needs no permission: the returned URI
 * carries a one-time read grant.
 *
 * 1.0.11: a picked image is no longer decoded at once. [prepare] copies it to
 * the app cache (cache/qr-pick, one image at a time) and makes an upright
 * preview; the app shows it on the adjust screen, where the user zooms / drags
 * the QR into the frame, and [decodeArea] reads only that area (then, when
 * asked, the whole image). [discard] deletes the copy when the screen closes.
 */
object QrImage {

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

    private const val PREVIEW_SIDE = 1600
    private const val WORK_SIDE = 4096
    private const val MAX_BYTES = 64L * 1024 * 1024

    private fun dir(ctx: Context): File = File(ctx.cacheDir, "qr-pick").apply { mkdirs() }

    private fun clearDir(ctx: Context) {
        dir(ctx).listFiles()?.forEach { runCatching { it.delete() } }
    }

    /** The cached copy at [path], or null when [path] is not one of ours. */
    private fun ours(ctx: Context, path: String): File? {
        if (path.isBlank()) return null
        val f = File(path)
        return runCatching {
            if (f.canonicalFile.parentFile == dir(ctx).canonicalFile && f.name.startsWith("pick-") && f.isFile) f else null
        }.getOrNull()
    }

    private fun exifOrientation(f: File): Int = runCatching {
        ExifInterface(f.absolutePath).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)
    }.getOrDefault(1).let { QrCrop.normOrientation(it) }

    private fun rawBounds(f: File): QrImageLoad.Size {
        val size = QrImageLoad.Size()
        runCatching {
            val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeFile(f.absolutePath, o) // null by design in bounds-only mode
            size.width = o.outWidth
            size.height = o.outHeight
        }
        return size
    }

    private fun pixels(bmp: Bitmap): Triple<IntArray, Int, Int> {
        val b = toSoftwareArgb(bmp)
        val w = b.width
        val h = b.height
        val px = IntArray(w * h)
        b.getPixels(px, 0, w, 0, 0, w, h)
        b.recycle()
        return Triple(px, w, h)
    }

    /**
     * Copies the picked image to the cache and writes an upright preview.
     * {"path", "preview", "width", "height"} (width/height of the upright
     * original, the space the app's crop rect is in) or {"error": "unreadable"}.
     * Runs off the main thread.
     */
    fun prepare(ctx: Context, uri: Uri): Map<String, Any?> {
        clearDir(ctx)
        val f = File(dir(ctx), "pick-${System.nanoTime()}.img")
        try {
            val copied = ctx.contentResolver.openInputStream(uri)?.use { input ->
                f.outputStream().use { out ->
                    val buf = ByteArray(64 * 1024)
                    var total = 0L
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        total += n
                        if (total > MAX_BYTES) throw IllegalArgumentException("image too large")
                        out.write(buf, 0, n)
                    }
                }
                true
            } ?: false
            if (!copied) throw IllegalStateException("cannot open")
            var orient = exifOrientation(f)
            var raw = rawBounds(f)
            var preview: Bitmap? = null
            if (raw.known) {
                preview = runCatching {
                    val opts = BitmapFactory.Options().apply {
                        inSampleSize = QrImageLoad.sampleSize(raw.width, raw.height, PREVIEW_SIDE)
                        inPreferredConfig = Bitmap.Config.ARGB_8888
                    }
                    BitmapFactory.decodeFile(f.absolutePath, opts)
                }.getOrNull()
            }
            if (preview == null && Build.VERSION.SDK_INT >= 28) {
                // BitmapFactory cannot read it (e.g. HEIC on some phones): keep an
                // upright JPEG working copy instead (ImageDecoder applies EXIF).
                val work = runCatching { decodeWithImageDecoder(ImageDecoder.createSource(f), WORK_SIDE) }.getOrNull()
                if (work != null) {
                    val sw = toSoftwareArgb(work)
                    f.outputStream().use { sw.compress(Bitmap.CompressFormat.JPEG, 95, it) }
                    orient = 1
                    raw = rawBounds(f)
                    val s = QrImageLoad.sampleSize(sw.width, sw.height, PREVIEW_SIDE)
                    preview = if (s > 1) Bitmap.createScaledBitmap(sw, sw.width / s, sw.height / s, true) else sw
                    if (preview !== sw) sw.recycle()
                }
            }
            if (preview == null || !raw.known) throw IllegalStateException("cannot decode")
            val (px, pw, ph) = pixels(preview)
            val (up, uw, uh) = QrCrop.toUpright(px, pw, ph, orient)
            val upBmp = Bitmap.createBitmap(up, uw, uh, Bitmap.Config.ARGB_8888)
            val pf = File(f.absolutePath + ".preview.jpg")
            pf.outputStream().use { upBmp.compress(Bitmap.CompressFormat.JPEG, 88, it) }
            upBmp.recycle()
            val size = QrCrop.orientedSize(raw.width, raw.height, orient)
            return mapOf("path" to f.absolutePath, "preview" to pf.absolutePath, "width" to size[0], "height" to size[1])
        } catch (t: Throwable) { // OOM included
            clearDir(ctx)
            return mapOf("error" to "unreadable")
        }
    }

    /**
     * Decodes the framed area ([left], [top], [width], [height] in upright
     * original pixels); when nothing is found there and [whole] is set, the
     * whole image. {"text"} or {"error": "noQr" | "unreadable"}.
     */
    fun decodeArea(ctx: Context, path: String, left: Int, top: Int, width: Int, height: Int, whole: Boolean): Map<String, Any?> {
        val f = ours(ctx, path) ?: return mapOf("error" to "unreadable")
        var opened = false
        try {
            val orient = exifOrientation(f)
            val raw = rawBounds(f)
            val rect = if (raw.known) QrCrop.orientedToRaw(left, top, width, height, raw.width, raw.height, orient) else null
            if (rect != null) {
                val region = runCatching { decodeRegion(f, rect, raw) }.getOrNull()
                if (region != null) {
                    opened = true
                    val (px, w, h) = region
                    val text = QrCrop.decodeRegion(px, w, h, orient)
                    if (!text.isNullOrEmpty()) return mapOf("text" to text)
                }
            }
        } catch (t: Throwable) {
            // fall through to the whole image
        }
        if (whole) {
            val bmp = try {
                load(f)
            } catch (t: Throwable) {
                null
            }
            if (bmp != null) {
                opened = true
                try {
                    val (px, w, h) = pixels(bmp)
                    val text = QrDecode.decodeArgb(px, w, h)
                    if (!text.isNullOrEmpty()) return mapOf("text" to text)
                } catch (t: Throwable) {
                    // OOM / bad bitmap: report below
                }
            }
        }
        return mapOf("error" to if (opened) "noQr" else "unreadable")
    }

    /** Raw pixels of [rect] (x, y, w, h in the file's pixels), sampled to at most REGION_MAX_SIDE. */
    private fun decodeRegion(f: File, rect: IntArray, raw: QrImageLoad.Size): Triple<IntArray, Int, Int>? {
        val sample = QrCrop.regionSample(rect[2], rect[3])
        val r = Rect(rect[0], rect[1], rect[0] + rect[2], rect[1] + rect[3])
        val opts = BitmapFactory.Options().apply {
            inSampleSize = sample
            inPreferredConfig = Bitmap.Config.ARGB_8888
        }
        val viaRegion = runCatching {
            @Suppress("DEPRECATION")
            val dec = if (Build.VERSION.SDK_INT >= 31) BitmapRegionDecoder.newInstance(f.absolutePath)
            else BitmapRegionDecoder.newInstance(f.absolutePath, false)
            try {
                dec?.decodeRegion(r, opts)
            } finally {
                dec?.recycle()
            }
        }.getOrNull()
        if (viaRegion != null) return pixels(viaRegion)
        // No region decoder for this format: decode the whole file (sampled) and cut the area out.
        val s = QrImageLoad.sampleSize(raw.width, raw.height, WORK_SIDE)
        val full = BitmapFactory.decodeFile(f.absolutePath, BitmapFactory.Options().apply {
            inSampleSize = s
            inPreferredConfig = Bitmap.Config.ARGB_8888
        }) ?: return null
        val sx = full.width.toDouble() / raw.width
        val sy = full.height.toDouble() / raw.height
        val x = (r.left * sx).toInt().coerceIn(0, full.width - 1)
        val y = (r.top * sy).toInt().coerceIn(0, full.height - 1)
        val w = (r.width() * sx).toInt().coerceIn(1, full.width - x)
        val h = (r.height() * sy).toInt().coerceIn(1, full.height - y)
        val cut = Bitmap.createBitmap(full, x, y, w, h)
        if (cut !== full) full.recycle()
        return pixels(cut)
    }

    /** Deletes the cached copy and its preview. */
    fun discard(ctx: Context, path: String): Boolean {
        val f = ours(ctx, path) ?: return false
        runCatching { File(f.absolutePath + ".preview.jpg").delete() }
        return runCatching { f.delete() }.getOrDefault(false)
    }

    /**
     * Whole image as a bitmap (longest side ≤ MAX_SIDE).
     * 1.0.11 fix: the bounds pass ignores BitmapFactory's return value (always
     * null with inJustDecodeBounds) and only reads outWidth/outHeight; see
     * QrImageLoad. When BitmapFactory cannot decode the file (e.g. HEIC on some
     * devices), Android 9+ falls back to ImageDecoder. The result is always a
     * software ARGB_8888 bitmap, so getPixels works.
     */
    private fun load(f: File): Bitmap? {
        val bmp = QrImageLoad.load(
            open = { f.inputStream() },
            readBounds = { input, size ->
                val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
                BitmapFactory.decodeStream(input, null, bounds) // null by design in bounds-only mode
                size.width = bounds.outWidth
                size.height = bounds.outHeight
                null
            },
            decode = { input, sample ->
                val opts = BitmapFactory.Options().apply {
                    inSampleSize = sample
                    inPreferredConfig = Bitmap.Config.ARGB_8888
                    inMutable = true
                }
                BitmapFactory.decodeStream(input, null, opts)
            },
            fallback = if (Build.VERSION.SDK_INT >= 28) { _ -> decodeWithImageDecoder(ImageDecoder.createSource(f), QrImageLoad.MAX_SIDE) } else null,
        ) ?: return null
        return toSoftwareArgb(bmp)
    }

    @androidx.annotation.RequiresApi(28)
    private fun decodeWithImageDecoder(src: ImageDecoder.Source, maxSide: Int): Bitmap? {
        return ImageDecoder.decodeBitmap(src) { decoder, info, _ ->
            val s = info.size
            decoder.setTargetSampleSize(QrImageLoad.sampleSize(s.width, s.height, maxSide))
            decoder.setAllocator(ImageDecoder.ALLOCATOR_SOFTWARE)
            decoder.setMutableRequired(true)
        }
    }

    /** getPixels needs a software bitmap; QrDecode expects ARGB_8888 colours. */
    private fun toSoftwareArgb(bmp: Bitmap): Bitmap {
        val hardware = Build.VERSION.SDK_INT >= 26 && bmp.config == Bitmap.Config.HARDWARE
        if (!hardware && bmp.config == Bitmap.Config.ARGB_8888) return bmp
        val copy = bmp.copy(Bitmap.Config.ARGB_8888, true) ?: return bmp
        if (copy !== bmp) bmp.recycle()
        return copy
    }

    @Suppress("unused")
    private fun isOk(resultCode: Int) = resultCode == Activity.RESULT_OK
}
