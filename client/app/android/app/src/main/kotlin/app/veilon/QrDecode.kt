package app.veilon

import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.LuminanceSource
import com.google.zxing.MultiFormatReader
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer

/**
 * 1.0.10: decodes a QR code from an image picked from the gallery, with the
 * same ZXing library the camera scanner uses. Pure JVM (no android.*), so it
 * is tested off-device (kt-tests/QrDecodeTest.kt).
 */
object QrDecode {

    /**
     * [argb] = width*height ARGB pixels (Bitmap.getPixels order). Returns the
     * QR text or null when none is found. Transparent pixels count as white
     * (QR PNGs with a transparent background), and dark-on-light, inverted
     * and very large images are tried.
     */
    fun decodeArgb(argb: IntArray, width: Int, height: Int): String? {
        if (width <= 0 || height <= 0 || argb.size < width * height) return null
        return decodeLuminance(luminance(argb, width, height), width, height)
    }

    /** Luminance (0 = black) with alpha composited over white. */
    fun luminance(argb: IntArray, width: Int, height: Int): ByteArray {
        val n = width * height
        val out = ByteArray(n)
        for (i in 0 until n) {
            val p = argb[i]
            val a = (p ushr 24) and 0xff
            val r = (p shr 16) and 0xff
            val g = (p shr 8) and 0xff
            val b = p and 0xff
            val y = (r * 299 + g * 587 + b * 114) / 1000
            out[i] = ((y * a + 255 * (255 - a)) / 255).toByte()
        }
        return out
    }

    fun decodeLuminance(lum: ByteArray, width: Int, height: Int): String? {
        var w = width
        var h = height
        var data = lum
        // Up to two halvings: phone photos are large and ZXing works best when
        // the code is not hundreds of pixels per module.
        for (round in 0..2) {
            tryAll(data, w, h)?.let { return it }
            if (max(w, h) < 900 || w < 4 || h < 4) break
            val (d2, w2, h2) = half(data, w, h)
            data = d2; w = w2; h = h2
        }
        return null
    }

    private fun max(a: Int, b: Int) = if (a > b) a else b

    private fun tryAll(data: ByteArray, w: Int, h: Int): String? {
        val src: LuminanceSource = PlanarYUVLuminanceSource(data, w, h, 0, 0, w, h, false)
        for (s in listOf(src, src.invert())) {
            decode(BinaryBitmap(HybridBinarizer(s)))?.let { return it }
            decode(BinaryBitmap(GlobalHistogramBinarizer(s)))?.let { return it }
        }
        return null
    }

    private val hints: Map<DecodeHintType, Any> = mapOf(
        DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
        DecodeHintType.TRY_HARDER to true,
        DecodeHintType.CHARACTER_SET to "UTF-8",
    )

    private fun decode(b: BinaryBitmap): String? = try {
        val reader = MultiFormatReader()
        reader.setHints(hints)
        reader.decodeWithState(b).text?.takeIf { it.isNotEmpty() }
    } catch (e: Exception) {
        null
    }

    /** 2x2 box downscale. */
    private fun half(d: ByteArray, w: Int, h: Int): Triple<ByteArray, Int, Int> {
        val w2 = w / 2
        val h2 = h / 2
        val o = ByteArray(w2 * h2)
        for (y in 0 until h2) {
            val r0 = 2 * y * w
            val r1 = r0 + w
            for (x in 0 until w2) {
                val s = (d[r0 + 2 * x].toInt() and 0xff) + (d[r0 + 2 * x + 1].toInt() and 0xff) +
                    (d[r1 + 2 * x].toInt() and 0xff) + (d[r1 + 2 * x + 1].toInt() and 0xff)
                o[y * w2 + x] = (s / 4).toByte()
            }
        }
        return Triple(o, w2, h2)
    }
}
