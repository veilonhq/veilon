package app.veilon

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Bundle
import android.view.Gravity
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.FrameLayout
import android.widget.LinearLayout
import com.google.zxing.BarcodeFormat
import com.journeyapps.barcodescanner.BarcodeCallback
import com.journeyapps.barcodescanner.DecoratedBarcodeView
import com.journeyapps.barcodescanner.DefaultDecoderFactory

/**
 * Full-screen QR scanner (ZXing embedded). Returns the decoded text in
 * [EXTRA_TEXT] with RESULT_OK, or RESULT_CANCELED.
 */
class QrScanActivity : Activity() {

    companion object {
        const val EXTRA_TEXT = "text"
        private const val REQ_CAMERA = 1
    }

    private lateinit var barcodeView: DecoratedBarcodeView
    private var torch = false
    private var done = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        val fa = Store.options(this).language == "fa"

        val view = DecoratedBarcodeView(this)
        view.getBarcodeView().decoderFactory = DefaultDecoderFactory(listOf(BarcodeFormat.QR_CODE))
        view.setStatusText(if (fa) "کد QR را در کادر قرار دهید" else "Point the camera at a QR code")
        barcodeView = view

        val root = FrameLayout(this).apply { setBackgroundColor(Color.BLACK) }
        root.addView(barcodeView, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))

        val bar = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            val pad = (16 * resources.displayMetrics.density).toInt()
            setPadding(pad, pad * 2, pad, pad)
        }
        val close = Button(this).apply {
            text = if (fa) "بستن" else "Close"
            setOnClickListener { finishCancel() }
        }
        val light = Button(this).apply {
            text = if (fa) "چراغ" else "Torch"
            setOnClickListener {
                torch = !torch
                if (torch) barcodeView.setTorchOn() else barcodeView.setTorchOff()
            }
        }
        bar.addView(close)
        bar.addView(android.view.View(this), LinearLayout.LayoutParams(0, 1, 1f))
        if (packageManager.hasSystemFeature(PackageManager.FEATURE_CAMERA_FLASH)) bar.addView(light)
        root.addView(bar, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.TOP))
        setContentView(root)

        barcodeView.decodeContinuous(BarcodeCallback { r ->
            val text = r?.text
            if (done || text.isNullOrEmpty()) return@BarcodeCallback
            done = true
            setResult(RESULT_OK, Intent().putExtra(EXTRA_TEXT, text))
            finish()
        })

        if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.CAMERA), REQ_CAMERA)
        }
    }

    override fun onResume() {
        super.onResume()
        if (checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) barcodeView.resume()
    }

    override fun onPause() {
        super.onPause()
        barcodeView.pause()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != REQ_CAMERA) return
        if (grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED) barcodeView.resume() else finishCancel()
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        finishCancel()
    }

    private fun finishCancel() {
        setResult(RESULT_CANCELED)
        finish()
    }
}
