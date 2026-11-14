package app.veilon

import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import java.awt.image.BufferedImage
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import javax.imageio.ImageIO
import javax.imageio.ImageReadParam

// 1.0.11 off-device regression test of the gallery image load (QrImageLoad).
// 1.0.10 bug: the bounds pass used BitmapFactory.decodeStream's return value,
// which is ALWAYS null with inJustDecodeBounds, so every picked image was
// "unreadable". The fake bounds reader below behaves exactly like
// BitmapFactory in bounds-only mode (fills the size, returns null).
private var fails = 0
private fun check(name: String, ok: Boolean, detail: String = "") {
    println((if (ok) "PASS " else "FAIL ") + name + (if (detail.isNotEmpty()) " ($detail)" else ""))
    if (!ok) fails++
}

private const val LINK = "vless://00000000-0000-4000-8000-000000000000@server.example.com:443?security=reality&type=tcp#Test"

private fun qrPng(w: Int, h: Int, qrSide: Int): ByteArray {
    val img = BufferedImage(w, h, BufferedImage.TYPE_INT_ARGB)
    for (y in 0 until h) for (x in 0 until w) img.setRGB(x, y, 0xFFFFFFFF.toInt())
    val m = QRCodeWriter().encode(LINK, BarcodeFormat.QR_CODE, qrSide, qrSide, mapOf(EncodeHintType.MARGIN to 4))
    val ox = (w - m.width) / 2; val oy = (h - m.height) / 2
    for (y in 0 until m.height) for (x in 0 until m.width) if (m.get(x, y)) img.setRGB(ox + x, oy + y, 0xFF000000.toInt())
    return ByteArrayOutputStream().also { ImageIO.write(img, "png", it) }.toByteArray()
}

/** BitmapFactory-like bounds pass: fills the size, returns null (like inJustDecodeBounds). */
private val boundsLikeBitmapFactory: (InputStream, QrImageLoad.Size) -> Any? = { input, size ->
    val iis = ImageIO.createImageInputStream(input)
    val r = ImageIO.getImageReaders(iis).next()
    r.input = iis
    size.width = r.getWidth(0); size.height = r.getHeight(0)
    r.dispose()
    null
}

private val decodeSampled: (InputStream, Int) -> BufferedImage? = { input, sample ->
    val iis = ImageIO.createImageInputStream(input)
    val r = ImageIO.getImageReaders(iis).next()
    r.input = iis
    val p: ImageReadParam = r.defaultReadParam
    p.setSourceSubsampling(sample, sample, 0, 0)
    r.read(0, p).also { r.dispose() }
}

private fun decodeQr(b: BufferedImage): String? {
    val px = b.getRGB(0, 0, b.width, b.height, null, 0, b.width)
    return QrDecode.decodeArgb(px, b.width, b.height)
}

fun main() {
    // 1) sample sizes: longest side ≤ 2400, powers of two
    check("sample-small", QrImageLoad.sampleSize(1080, 2400) == 1)
    check("sample-2401", QrImageLoad.sampleSize(2401, 100) == 2)
    check("sample-12mp", QrImageLoad.sampleSize(4000, 3000) == 2)
    check("sample-50mp", QrImageLoad.sampleSize(8160, 6120) == 4)
    check("sample-huge", QrImageLoad.sampleSize(20000, 100) == 16)
    check("sample-unknown", QrImageLoad.sampleSize(-1, -1) == 1 && QrImageLoad.sampleSize(0, 500) == 1)
    for ((w, h) in listOf(1 to 1, 2400 to 2400, 2401 to 1, 9999 to 7777, 100000 to 3)) {
        val s = QrImageLoad.sampleSize(w, h)
        check("sample-bound-${w}x$h", maxOf(w, h) / s <= 2400 && (s == 1 || maxOf(w, h) / (s / 2) > 2400) && Integer.bitCount(s) == 1, "s=$s")
    }

    // 2) the 1.0.10 regression: bounds reader returns null (like BitmapFactory) → still loads
    val png = qrPng(900, 1200, 600)
    var opened = 0
    val img = QrImageLoad.load(
        open = { opened++; ByteArrayInputStream(png) },
        readBounds = boundsLikeBitmapFactory,
        decode = decodeSampled,
    )
    check("bounds-null-return-still-loads", img != null && img.width == 900 && img.height == 1200, "${img?.width}x${img?.height}")
    check("two-streams-opened", opened == 2, "$opened")
    check("loaded-image-decodes", img != null && decodeQr(img) == LINK)

    // The exact 1.0.10 pattern, to show it fails with a BitmapFactory-like reader.
    val old: Any? = ByteArrayInputStream(png).use { boundsLikeBitmapFactory(it, QrImageLoad.Size()) }
    check("old-pattern-would-return-null", old == null)

    // 3) large image is sampled (5000×3000 → sample 4 → 1250×750) and still decodes
    val big = qrPng(5000, 3000, 1600)
    var usedSample = 0
    val bigImg = QrImageLoad.load(
        open = { ByteArrayInputStream(big) },
        readBounds = boundsLikeBitmapFactory,
        decode = { s, n -> usedSample = n; decodeSampled(s, n) },
    )
    check("big-sampled", usedSample == 4 && bigImg != null && bigImg.width == 1250, "sample=$usedSample ${bigImg?.width}")
    check("big-decodes", bigImg != null && decodeQr(bigImg) == LINK)

    // 4) stream cannot be opened → null, fallback not called
    var fb = 0
    val none = QrImageLoad.load(open = { null }, readBounds = boundsLikeBitmapFactory, decode = decodeSampled, fallback = { fb++; null })
    check("no-stream-null", none == null && fb == 0)

    // 5) primary decoder cannot read the format (bounds unknown, e.g. HEIC) → fallback used
    val viaFallback = QrImageLoad.load(
        open = { ByteArrayInputStream(byteArrayOf(0, 0, 0, 0x18, 0x66, 0x74, 0x79, 0x70)) },
        readBounds = { _, _ -> null },
        decode = { _, _ -> error("must not decode without bounds") },
        fallback = { size -> if (!size.known) "fallback" else null },
    )
    check("unknown-bounds-fallback", viaFallback == "fallback")

    // 6) bounds known but decode fails (null or exception) → fallback gets the size
    var seen = ""
    val r6 = QrImageLoad.load(
        open = { ByteArrayInputStream(png) },
        readBounds = boundsLikeBitmapFactory,
        decode = { _, _ -> throw IllegalStateException("decoder broke") },
        fallback = { size -> seen = "${size.width}x${size.height}"; "fb" },
    )
    check("decode-throws-fallback", r6 == "fb" && seen == "900x1200", seen)
    // bounds reader throws → fallback
    val r7 = QrImageLoad.load(open = { ByteArrayInputStream(png) }, readBounds = { _, _ -> throw RuntimeException("x") }, decode = decodeSampled, fallback = { "fb7" })
    check("bounds-throws-fallback", r7 == "fb7")
    // fallback throws → null (no crash)
    val r8 = QrImageLoad.load<String>(open = { ByteArrayInputStream(png) }, readBounds = { _, _ -> null }, decode = { _, _ -> null }, fallback = { throw RuntimeException("y") })
    check("fallback-throws-null", r8 == null)

    println(if (fails == 0) "KTEST_OK" else "KTEST_FAILED $fails")
    if (fails != 0) System.exit(1)
}
