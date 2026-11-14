package app.veilon

import android.app.Activity
import android.content.Context
import android.view.View
import com.google.zxing.BarcodeFormat
import com.journeyapps.barcodescanner.BarcodeCallback
import com.journeyapps.barcodescanner.BarcodeView
import com.journeyapps.barcodescanner.CameraPreview
import com.journeyapps.barcodescanner.DefaultDecoderFactory
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.StandardMessageCodec
import io.flutter.plugin.platform.PlatformView
import io.flutter.plugin.platform.PlatformViewFactory

/**
 * 1.0.10: the camera preview of the Flutter QR scanner page, a plain ZXing
 * [BarcodeView] (no decorations: the frame, buttons and texts are drawn by
 * Flutter). Results go to Dart on channel "app.veilon/qr" ("scanned"), camera
 * failures as "cameraError". Only one scanner is open at a time; the
 * activity pauses / resumes it with its own lifecycle.
 */
class QrCameraView(private val activity: Activity, private val channel: MethodChannel, private val onGone: (QrCameraView) -> Unit) : PlatformView {

    private val barcode = BarcodeView(activity)
    private var done = false
    private var disposed = false
    private var running = false

    init {
        barcode.decoderFactory = DefaultDecoderFactory(listOf(BarcodeFormat.QR_CODE))
        // TextureView composes like any other view inside the hybrid-composition host.
        barcode.setUseTextureView(true)
        // Decode area: a centred square of 90 % of the width (the Flutter
        // frame sits inside it, centred).
        barcode.marginFraction = 0.05
        barcode.addStateListener(object : CameraPreview.StateListener {
            override fun previewSized() {}
            override fun previewStarted() {}
            override fun previewStopped() {}
            override fun cameraClosed() {}
            override fun cameraError(error: Exception?) {
                if (!disposed) runCatching { channel.invokeMethod("cameraError", error?.message ?: "camera") }
            }
        })
        barcode.decodeContinuous(BarcodeCallback { r ->
            val text = r?.text
            if (done || disposed || text.isNullOrEmpty()) return@BarcodeCallback
            done = true
            runCatching { barcode.pause() }
            runCatching { channel.invokeMethod("scanned", text) }
        })
        resume()
    }

    fun resume() {
        if (disposed || done || running) return
        if (activity.checkSelfPermission(android.Manifest.permission.CAMERA) != android.content.pm.PackageManager.PERMISSION_GRANTED) return
        running = runCatching { barcode.resume() }.isSuccess
    }

    fun pause() {
        if (!running) return
        running = false
        runCatching { barcode.pause() }
    }

    fun setTorch(on: Boolean): Boolean = runCatching { barcode.setTorch(on) }.isSuccess && running

    override fun getView(): View = barcode

    override fun dispose() {
        if (disposed) return
        runCatching { barcode.setTorch(false) }
        pause()
        disposed = true
        onGone(this)
    }
}

class QrCameraFactory(
    private val activity: Activity,
    private val channel: MethodChannel,
    private val onCreated: (QrCameraView) -> Unit,
    private val onGone: (QrCameraView) -> Unit,
) : PlatformViewFactory(StandardMessageCodec.INSTANCE) {
    override fun create(context: Context?, viewId: Int, args: Any?): PlatformView =
        QrCameraView(activity, channel, onGone).also(onCreated)
}
