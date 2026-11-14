package app.veilon

import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import kotlin.random.Random

// 1.0.10 off-device test of the gallery QR decoder (pure ZXing, no Android).
private var fails = 0
private fun check(name: String, ok: Boolean, detail: String = "") {
    println((if (ok) "PASS " else "FAIL ") + name + (if (detail.isNotEmpty()) " ($detail)" else ""))
    if (!ok) fails++
}

private const val BLACK = 0xFF000000.toInt()
private const val WHITE = 0xFFFFFFFF.toInt()

/** QR as ARGB pixels: [moduleColor] on [bg] at [px] pixels per module, placed at (ox, oy) on a canvas. */
private fun canvas(w: Int, h: Int, fill: Int): IntArray = IntArray(w * h) { fill }

private fun drawQr(dst: IntArray, w: Int, text: String, ox: Int, oy: Int, size: Int, fg: Int, bg: Int?) {
    val m = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, size, size, mapOf(EncodeHintType.CHARACTER_SET to "UTF-8", EncodeHintType.MARGIN to 4))
    for (y in 0 until m.height) for (x in 0 until m.width) {
        val c = if (m.get(x, y)) fg else (bg ?: continue)
        dst[(oy + y) * w + ox + x] = c
    }
}

private fun rotate90(src: IntArray, w: Int, h: Int): IntArray {
    val o = IntArray(w * h)
    for (y in 0 until h) for (x in 0 until w) o[x * h + (h - 1 - y)] = src[y * w + x]
    return o
}

fun main() {
    val link = "vless://00000000-0000-4000-8000-000000000000@server.example.com:443?security=reality&type=tcp#Test"
    // 1) plain screenshot-like QR
    run {
        val w = 600; val img = canvas(w, w, WHITE); drawQr(img, w, link, 50, 50, 500, BLACK, WHITE)
        val r = QrDecode.decodeArgb(img, w, w); check("plain", r == link, r ?: "null")
    }
    // 2) UTF-8 Persian text
    run {
        val t = "سرور آلمان https://sub.example.com/abc"
        val w = 400; val img = canvas(w, w, WHITE); drawQr(img, w, t, 0, 0, 400, BLACK, WHITE)
        val r = QrDecode.decodeArgb(img, w, w); check("utf8", r == t, r ?: "null")
    }
    // 3) transparent background (PNG export): light modules are alpha 0 = (0,0,0,0)
    run {
        val w = 500; val img = canvas(w, w, 0x00000000); drawQr(img, w, link, 0, 0, 500, BLACK, null)
        val r = QrDecode.decodeArgb(img, w, w); check("transparent-bg", r == link, r ?: "null")
    }
    // 4) inverted (light modules on dark, dark-mode exports)
    run {
        val w = 500; val img = canvas(w, w, BLACK); drawQr(img, w, link, 0, 0, 500, WHITE, BLACK)
        val r = QrDecode.decodeArgb(img, w, w); check("inverted", r == link, r ?: "null")
    }
    // 5) big photo-like image: grey noisy background, QR off-centre and rotated
    run {
        val w = 3000; val h = 4000
        val rnd = Random(7)
        val img = IntArray(w * h) { val g = 120 + rnd.nextInt(60); (0xFF shl 24) or (g shl 16) or (g shl 8) or g }
        drawQr(img, w, link, 900, 2100, 1100, 0xFF202020.toInt(), 0xFFEAEAEA.toInt())
        val rot = rotate90(img, w, h)
        val r = QrDecode.decodeArgb(rot, h, w); check("large-rotated-noisy", r == link, r ?: "null")
    }
    // 6) small QR in a large screenshot (phone screenshot of a chat)
    run {
        val w = 1080; val h = 2400; val img = canvas(w, h, 0xFF101218.toInt())
        drawQr(img, w, link, 340, 900, 400, BLACK, WHITE)
        val r = QrDecode.decodeArgb(img, w, h); check("small-in-screenshot", r == link, r ?: "null")
    }
    // 7) no QR: noise and a gradient
    run {
        val rnd = Random(3); val w = 800; val h = 600
        val noise = IntArray(w * h) { if (rnd.nextBoolean()) BLACK else WHITE }
        check("noise-none", QrDecode.decodeArgb(noise, w, h) == null)
        val grad = IntArray(w * h) { i -> val g = (i % w) * 255 / w; (0xFF shl 24) or (g shl 16) or (g shl 8) or g }
        check("gradient-none", QrDecode.decodeArgb(grad, w, h) == null)
        check("blank-none", QrDecode.decodeArgb(canvas(300, 300, WHITE), 300, 300) == null)
    }
    // 8) bad input
    check("empty-dims", QrDecode.decodeArgb(IntArray(0), 0, 0) == null)
    check("short-array", QrDecode.decodeArgb(IntArray(10), 100, 100) == null)
    // 9) alpha compositing value
    val lum = QrDecode.luminance(intArrayOf(0x00000000, BLACK, WHITE, 0x80000000.toInt()), 4, 1)
    check("luminance-alpha", (lum[0].toInt() and 0xff) == 255 && lum[1].toInt() == 0 && (lum[2].toInt() and 0xff) == 255 && (lum[3].toInt() and 0xff) in 125..128,
        lum.joinToString { (it.toInt() and 0xff).toString() })
    println(if (fails == 0) "KTEST_OK" else "KTEST_FAILED $fails")
    if (fails != 0) System.exit(1)
}
