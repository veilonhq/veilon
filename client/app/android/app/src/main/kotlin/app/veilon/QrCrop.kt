package app.veilon

/**
 * 1.0.11: pure helpers of the gallery "adjust" screen (no Android types, tested
 * off-device in kt-tests/QrCropTest.kt).
 *
 * The app shows the picked image upright (EXIF orientation applied) and sends
 * the framed area as a rect in that upright ("oriented") image's pixels. The
 * file itself stores the pixels as they were taken ("raw"); [orientedToRaw]
 * maps the rect back so the native region decoder reads the right pixels, and
 * [toUpright] turns the decoded raw region upright again.
 *
 * Orientation values are the EXIF ones (1 normal … 8 rotate 270 cw).
 */
object QrCrop {

    /** Longest side a cropped region is decoded at. */
    const val REGION_MAX_SIDE = 2048

    /** Small regions are enlarged to about this longest side before ZXing. */
    const val UPSCALE_TO = 1000

    fun normOrientation(o: Int): Int = if (o in 1..8) o else 1

    fun swapsSides(o: Int): Boolean = normOrientation(o) >= 5

    /** Size of the upright image: [width, height]. */
    fun orientedSize(rawW: Int, rawH: Int, o: Int): IntArray =
        if (swapsSides(o)) intArrayOf(rawH, rawW) else intArrayOf(rawW, rawH)

    /**
     * Maps a rect (left, top, width, height) in upright pixels to the raw file
     * pixels; clamped to the image. Returns null when nothing is left.
     */
    fun orientedToRaw(left: Int, top: Int, width: Int, height: Int, rawW: Int, rawH: Int, o: Int): IntArray? {
        val ori = orientedSize(rawW, rawH, o)
        val l = left.coerceIn(0, ori[0]); val t = top.coerceIn(0, ori[1])
        val r = (left.toLong() + width).coerceIn(0, ori[0].toLong()).toInt()
        val b = (top.toLong() + height).coerceIn(0, ori[1].toLong()).toInt()
        if (r <= l || b <= t) return null
        val a = toRawPoint(l, t, rawW, rawH, o)
        val c = toRawPoint(r, b, rawW, rawH, o)
        val x0 = minOf(a[0], c[0]); val x1 = maxOf(a[0], c[0])
        val y0 = minOf(a[1], c[1]); val y1 = maxOf(a[1], c[1])
        return intArrayOf(x0, y0, x1 - x0, y1 - y0)
    }

    /** Continuous (pixel-edge) point mapping, upright → raw. */
    private fun toRawPoint(x: Int, y: Int, rw: Int, rh: Int, o: Int): IntArray = when (normOrientation(o)) {
        2 -> intArrayOf(rw - x, y)
        3 -> intArrayOf(rw - x, rh - y)
        4 -> intArrayOf(x, rh - y)
        5 -> intArrayOf(y, x)
        6 -> intArrayOf(y, rh - x)
        7 -> intArrayOf(rw - y, rh - x)
        8 -> intArrayOf(rw - y, x)
        else -> intArrayOf(x, y)
    }

    /** Raw pixels (w × h) turned upright; returns the pixels and the new width/height. */
    fun toUpright(px: IntArray, w: Int, h: Int, o: Int): Triple<IntArray, Int, Int> {
        val n = normOrientation(o)
        if (n == 1) return Triple(px, w, h)
        val ow = if (n >= 5) h else w
        val oh = if (n >= 5) w else h
        val out = IntArray(ow * oh)
        for (y in 0 until h) {
            val row = y * w
            for (x in 0 until w) {
                val X: Int; val Y: Int
                when (n) {
                    2 -> { X = w - 1 - x; Y = y }
                    3 -> { X = w - 1 - x; Y = h - 1 - y }
                    4 -> { X = x; Y = h - 1 - y }
                    5 -> { X = y; Y = x }
                    6 -> { X = h - 1 - y; Y = x }
                    7 -> { X = h - 1 - y; Y = w - 1 - x }
                    else -> { X = y; Y = w - 1 - x } // 8
                }
                out[Y * ow + X] = px[row + x]
            }
        }
        return Triple(out, ow, oh)
    }

    /** Power-of-two sample size for decoding a region of w × h. */
    fun regionSample(w: Int, h: Int, maxSide: Int = REGION_MAX_SIDE): Int = QrImageLoad.sampleSize(w, h, maxSide)

    /** Integer nearest-neighbour enlargement so the longest side reaches about [target] (crisp module edges). */
    fun upscale(px: IntArray, w: Int, h: Int, target: Int = UPSCALE_TO): Triple<IntArray, Int, Int> {
        val m = maxOf(w, h)
        if (w <= 0 || h <= 0 || m >= target) return Triple(px, w, h)
        var k = (target + m - 1) / m
        while (k > 1 && w.toLong() * h * k * k > 6_000_000L) k--
        if (k <= 1) return Triple(px, w, h)
        val nw = w * k; val nh = h * k
        val out = IntArray(nw * nh)
        for (y in 0 until nh) {
            val src = (y / k) * w
            val dst = y * nw
            for (x in 0 until nw) out[dst + x] = px[src + x / k]
        }
        return Triple(out, nw, nh)
    }

    /** Raw region pixels → upright → enlarged when small → ZXing (same pipeline as the camera / gallery). */
    fun decodeRegion(px: IntArray, w: Int, h: Int, o: Int): String? {
        val (u, uw, uh) = toUpright(px, w, h, o)
        val direct = QrDecode.decodeArgb(u, uw, uh)
        if (!direct.isNullOrEmpty()) return direct
        val (s, sw, sh) = upscale(u, uw, uh)
        if (sw == uw) return null
        return QrDecode.decodeArgb(s, sw, sh)
    }
}
