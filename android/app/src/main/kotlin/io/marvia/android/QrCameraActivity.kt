package io.marvia.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.provider.Settings
import android.view.Gravity
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.core.view.isVisible
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Отдельный экран связывает камеру со своим жизненным циклом: уход назад
 * освобождает её, а сворачивание приложения останавливает съёмку.
 * Этот экран открывают только кнопкой «Камера», поэтому разрешение не
 * появляется при запуске приложения или выборе картинки из галереи.
 */
class QrCameraActivity : AppCompatActivity() {
    private lateinit var preview: PreviewView
    private lateinit var message: TextView
    private lateinit var settings: Button
    private val executor = Executors.newSingleThreadExecutor()
    private val accepted = AtomicBoolean(false)
    private var provider: ProcessCameraProvider? = null
    private var analysis: ImageAnalysis? = null

    private val permission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) startCamera() else {
            message.setText(R.string.camera_denied)
            settings.isVisible = true
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val pad = (16 * resources.displayMetrics.density).toInt()
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, pad, pad, pad)
            fitsSystemWindows = true
        }
        message = TextView(this).apply {
            setText(R.string.camera_hint)
            textSize = 18f
            gravity = Gravity.CENTER
            setPadding(0, pad, 0, pad)
        }
        preview = PreviewView(this).apply {
            // TextureView корректно переживает перекрытие системным окном
            // разрешения, которое на некоторых телефонах ломает SurfaceView.
            implementationMode = PreviewView.ImplementationMode.COMPATIBLE
        }
        settings = Button(this).apply {
            setText(R.string.camera_settings)
            isVisible = false
            setOnClickListener {
                startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
            }
        }
        box.addView(message)
        box.addView(preview, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f))
        box.addView(settings)
        box.addView(Button(this).apply {
            setText(R.string.camera_close)
            setOnClickListener { finish() }
        })
        setContentView(box)
        if (hasPermission()) startCamera()
        else if (savedInstanceState == null) permission.launch(Manifest.permission.CAMERA)
        else {
            message.setText(R.string.camera_denied)
            settings.isVisible = true
        }
    }

    override fun onResume() {
        super.onResume()
        if (hasPermission() && analysis == null) startCamera()
    }

    private fun hasPermission() = ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED

    private var starting = false

    private fun startCamera() {
        if (starting || analysis != null || isFinishing || isDestroyed) return
        starting = true
        val future = ProcessCameraProvider.getInstance(this)
        future.addListener({
            starting = false
            if (isFinishing || isDestroyed) return@addListener
            try {
                val cameraProvider = future.get()
                provider = cameraProvider
                val selector = if (cameraProvider.hasCamera(CameraSelector.DEFAULT_BACK_CAMERA)) CameraSelector.DEFAULT_BACK_CAMERA
                    else CameraSelector.DEFAULT_FRONT_CAMERA
                val view = Preview.Builder().build().also { it.setSurfaceProvider(preview.surfaceProvider) }
                val frames = ImageAnalysis.Builder()
                    .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                    .setOutputImageFormat(ImageAnalysis.OUTPUT_IMAGE_FORMAT_YUV_420_888)
                    .build()
                frames.setAnalyzer(executor) { frame ->
                    try {
                        if (!accepted.get()) {
                            val plane = frame.planes[0]
                            val crop = frame.cropRect
                            val text = QrImport.decodeYuv(
                                crop.width(), crop.height(), plane.buffer, plane.rowStride, plane.pixelStride,
                                frame.imageInfo.rotationDegrees, crop.left, crop.top,
                            )
                            if (text != null && accepted.compareAndSet(false, true)) {
                                runOnUiThread {
                                    if (!isFinishing && !isDestroyed) {
                                        setResult(RESULT_OK, Intent().putExtra(EXTRA_KEY, text))
                                        finish()
                                    }
                                }
                            }
                        }
                    } finally {
                        // Каждый кадр возвращается CameraX даже после успеха:
                        // иначе очередь зависнет и камера останется занятой.
                        frame.close()
                    }
                }
                cameraProvider.bindToLifecycle(this, selector, view, frames)
                analysis = frames
                settings.isVisible = false
                message.setText(R.string.camera_hint)
            } catch (_: Exception) {
                message.setText(R.string.camera_unavailable)
            }
        }, ContextCompat.getMainExecutor(this))
    }

    override fun onDestroy() {
        analysis?.clearAnalyzer()
        provider?.unbindAll()
        executor.shutdown()
        super.onDestroy()
    }

    companion object {
        const val EXTRA_KEY = "qr_key"
    }
}
