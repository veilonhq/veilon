package app.veilon

import java.io.InputStream

/**
 * 1.0.11: the load steps of "pick QR from gallery", without Android types so
 * they can be tested off-device (kt-tests/QrImageLoadTest.kt).
 *
 * Regression (1.0.10, every picked image showed "unreadable"): the bounds pass
 * was written as `openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) } ?: return null`.
 * With inJustDecodeBounds = true, BitmapFactory.decodeStream ALWAYS returns
 * null (it only fills outWidth/outHeight), so `use {}` yielded null and load
 * returned null every time. Here the bounds reader's return value is ignored
 * on purpose; only a stream that cannot be opened ends the load.
 */
object QrImageLoad {

    /** Longest side the picked image is decoded at (memory bound). */
    const val MAX_SIDE = 2400

    /** Image size filled in by the bounds pass (-1 = unknown). */
    class Size {
        @JvmField var width: Int = -1
        @JvmField var height: Int = -1
        val known: Boolean get() = width > 0 && height > 0
    }

    /** Power-of-two sample size so the longest side is at most [maxSide]; 1 for unknown sizes. */
    fun sampleSize(width: Int, height: Int, maxSide: Int = MAX_SIDE): Int {
        if (width <= 0 || height <= 0 || maxSide <= 0) return 1
        val longest = maxOf(width, height)
        var s = 1
        while (longest / s > maxSide && s < (1 shl 30)) s *= 2
        return s
    }

    /**
     * [open] opens a fresh stream each call (null = the image cannot be opened).
     * [readBounds] fills [Size]; its return value is ignored (BitmapFactory
     * returns null in bounds-only mode by design).
     * [decode] decodes a stream at the given sample size (null = could not decode).
     * [fallback] another decoder (ImageDecoder on Android 9+) tried when the
     * first one gives nothing or the bounds are unknown; gets the bounds if known.
     */
    fun <B : Any> load(
        open: () -> InputStream?,
        readBounds: (InputStream, Size) -> Any?,
        decode: (InputStream, Int) -> B?,
        fallback: ((Size) -> B?)? = null,
    ): B? {
        val size = Size()
        val first = open() ?: return null
        try {
            first.use { readBounds(it, size) }
        } catch (e: Exception) {
            // unknown format for this decoder: leave the size unknown, try the fallback
        }
        if (size.known) {
            val sample = sampleSize(size.width, size.height)
            val bmp = try {
                open()?.use { decode(it, sample) }
            } catch (e: Exception) {
                null
            }
            if (bmp != null) return bmp
        }
        if (fallback == null) return null
        return try {
            fallback(size)
        } catch (e: Exception) {
            null
        }
    }
}
