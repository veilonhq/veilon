package app.veilon

import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import kotlin.random.Random

// 1.0.11 off-device test of the adjust screen's native side (QrCrop): EXIF
// orientation mapping of the crop rect, upright conversion, upscaling, and a
// small QR in a large busy photo that is found only after cropping.
private var fails = 0
private fun check(name: String, ok: Boolean, detail: String = "") {
    println((if (ok) "PASS " else "FAIL ") + name + (if (detail.isNotEmpty()) " ($detail)" else ""))
    if (!ok) fails++
}

private const val LINK = "vless://00000000-0000-4000-8000-000000000000@server.example.com:443?security=reality&type=tcp#Test"

/** Busy photo-like picture: colourful blocks, stripes and fake finder-like squares. */
private fun busy(w: Int, h: Int, seed: Int): IntArray {
    val rnd = Random(seed)
    val px = IntArray(w * h)
    for (y in 0 until h) for (x in 0 until w) {
        val g = 90 + ((x * 7 + y * 3) % 80)
        px[y * w + x] = (0xFF shl 24) or ((g + 20) shl 16) or (g shl 8) or (g - 10)
    }
    repeat(9000) {
        val bw = 3 + rnd.nextInt(28); val bh = 3 + rnd.nextInt(28)
        val bx = rnd.nextInt(w - bw); val by = rnd.nextInt(h - bh)
        val c = if (rnd.nextBoolean()) 0xFF101010.toInt() else (0xFF000000.toInt() or rnd.nextInt(0xFFFFFF))
        for (y in by until by + bh) for (x in bx until bx + bw) px[y * w + x] = c
    }
    return px
}

private fun drawQr(dst: IntArray, w: Int, ox: Int, oy: Int, size: Int) {
    val m = QRCodeWriter().encode(LINK, BarcodeFormat.QR_CODE, size, size, mapOf(EncodeHintType.MARGIN to 2))
    for (y in 0 until m.height) for (x in 0 until m.width)
        dst[(oy + y) * w + ox + x] = if (m.get(x, y)) 0xFF000000.toInt() else 0xFFFFFFFF.toInt()
}

/** Inverse of toUpright, to build a "raw" file image from an upright one. */
private fun toRaw(up: IntArray, uw: Int, uh: Int, o: Int): Triple<IntArray, Int, Int> {
    val rw = if (o >= 5) uh else uw
    val rh = if (o >= 5) uw else uh
    // brute force: raw (x, y) → upright via toUpright of a coordinate-index image
    val idx = IntArray(rw * rh) { it }
    val (map, mw, mh) = QrCrop.toUpright(idx, rw, rh, o)
    check("toRaw-size-$o", mw == uw && mh == uh)
    val raw = IntArray(rw * rh)
    for (i in map.indices) raw[map[i]] = up[i]
    return Triple(raw, rw, rh)
}

private fun crop(px: IntArray, w: Int, r: IntArray): IntArray {
    val out = IntArray(r[2] * r[3])
    for (y in 0 until r[3]) System.arraycopy(px, (r[1] + y) * w + r[0], out, y * r[2], r[2])
    return out
}

/** Nearest-neighbour downsample by an integer factor (what inSampleSize does, roughly). */
private fun sample(px: IntArray, w: Int, h: Int, s: Int): Triple<IntArray, Int, Int> {
    val nw = w / s; val nh = h / s
    return Triple(IntArray(nw * nh) { i -> px[(i / nw) * s * w + (i % nw) * s] }, nw, nh)
}

fun main() {
    // 1) orientation sizes
    for (o in 1..8) {
        val s = QrCrop.orientedSize(4000, 3000, o)
        check("oriented-size-$o", if (o >= 5) s[0] == 3000 && s[1] == 4000 else s[0] == 4000 && s[1] == 3000)
    }
    check("bad-orientation-is-normal", QrCrop.normOrientation(0) == 1 && QrCrop.normOrientation(9) == 1)

    // 2) rect mapping == brute force over pixels, all 8 orientations, random rects
    val rnd = Random(3)
    val rw = 37; val rh = 23
    for (o in 1..8) {
        val idx = IntArray(rw * rh) { it }
        val (up, uw, uh) = QrCrop.toUpright(idx, rw, rh, o)
        var ok = true
        var detail = ""
        repeat(200) {
            val l = rnd.nextInt(uw); val t = rnd.nextInt(uh)
            val w = 1 + rnd.nextInt(uw - l); val h = 1 + rnd.nextInt(uh - t)
            val r = QrCrop.orientedToRaw(l, t, w, h, rw, rh, o)!!
            // raw pixels inside r must be exactly the upright pixels inside (l,t,w,h)
            val want = HashSet<Int>()
            for (y in t until t + h) for (x in l until l + w) want.add(up[y * uw + x])
            val got = HashSet<Int>()
            for (y in r[1] until r[1] + r[3]) for (x in r[0] until r[0] + r[2]) got.add(y * rw + x)
            if (want != got) { ok = false; detail = "o=$o rect=($l,$t,$w,$h) raw=${r.toList()}" }
        }
        check("rect-mapping-$o", ok, detail)
    }
    check("rect-clamped", QrCrop.orientedToRaw(-50, -50, 100, 100, 40, 30, 1)!!.toList() == listOf(0, 0, 40, 30))
    check("rect-outside-null", QrCrop.orientedToRaw(500, 0, 10, 10, 40, 30, 6) == null && QrCrop.orientedToRaw(0, 0, 0, 10, 40, 30, 1) == null)
    // rotate-90 photo (orientation 6): top-left of the upright picture is the bottom-left of the file
    check("rect-orient6-corner", QrCrop.orientedToRaw(0, 0, 10, 20, 400, 300, 6)!!.toList() == listOf(0, 290, 20, 10))

    // 3) upscale
    val (u, uw, uh) = QrCrop.upscale(IntArray(100 * 50) { it }, 100, 50)
    check("upscale-10x", uw == 1000 && uh == 500 && u[0] == 0 && u[9] == 0 && u[10] == 1 && u[1000 * 10] == 100)
    check("upscale-large-unchanged", QrCrop.upscale(IntArray(4), 1200, 1).second == 1200)
    check("region-sample", QrCrop.regionSample(1000, 800) == 1 && QrCrop.regionSample(4000, 100) == 2)

    // 4) small QR in a big busy photo, stored rotated (EXIF 6 / 8 / 3), found only after cropping
    val W = 3000; val H = 4000 // upright size
    val up = busy(W, H, 11)
    val qx = 2140; val qy = 3010; val qs = 60 // small QR far from the centre
    drawQr(up, W, qx, qy, qs)
    for (o in listOf(1, 6, 8, 3)) {
        val (raw, rW, rH) = toRaw(up, W, H, o)
        // what the 1.0.10 auto decode did: whole image sampled to ≤ 2400
        val s = QrImageLoad.sampleSize(rW, rH)
        val (sp, sw, sh) = sample(raw, rW, rH, s)
        val auto = QrDecode.decodeArgb(sp, sw, sh)
        check("auto-whole-misses-o$o", auto == null, auto ?: "null")
        // the user framed the QR (upright coords) with some room around it
        val rr = QrCrop.orientedToRaw(qx - 40, qy - 30, qs + 90, qs + 70, rW, rH, o)!!
        val region = crop(raw, rW, rr)
        val text = QrCrop.decodeRegion(region, rr[2], rr[3], o)
        check("crop-finds-o$o", text == LINK, text ?: "null")
    }
    // the frame on a wrong area finds nothing
    val wrong = QrCrop.orientedToRaw(200, 200, 300, 300, W, H, 1)!!
    check("crop-elsewhere-none", QrCrop.decodeRegion(crop(up, W, wrong), wrong[2], wrong[3], 1) == null)

    println(if (fails == 0) "KTEST_OK" else "KTEST_FAILED $fails")
    if (fails != 0) System.exit(1)
}
