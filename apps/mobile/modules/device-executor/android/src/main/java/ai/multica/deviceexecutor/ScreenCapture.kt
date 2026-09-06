package ai.multica.deviceexecutor

import android.accessibilityservice.AccessibilityService
import android.graphics.Bitmap
import android.os.Build
import android.util.Base64
import android.view.Display
import androidx.annotation.RequiresApi
import java.io.ByteArrayOutputStream
import java.security.MessageDigest
import java.util.concurrent.Executor
import kotlin.coroutines.resume
import kotlinx.coroutines.delay
import kotlinx.coroutines.suspendCancellableCoroutine

/** One frame in the wire shape of `FrameSchema` (multica-device-mcp/src/protocol.ts). */
class CapturedFrame(
    val jpegBase64: String,
    val width: Int,
    val height: Int,
    val scaleFactor: Double,
    val hash: String,
    val physicalWidth: Int,
    val physicalHeight: Int,
) {
    fun toMap(currentApp: String?): Map<String, Any?> = mapOf(
        "jpeg_base64" to jpegBase64,
        "width" to width,
        "height" to height,
        "scale_factor" to scaleFactor,
        "hash" to hash,
        "current_app" to currentApp,
        "captured_at" to System.currentTimeMillis(),
    )
}

class CaptureException(val code: String, message: String) : Exception(message)

/**
 * Screenshots through AccessibilityService.takeScreenshot (Android 11+): no
 * MediaProjection consent dialog, no "screen is being recorded" chip, and it
 * works on secure windows the adb track sees as black. Downscale, JPEG and
 * hash match the hub's own transcoder (src/image.ts) so a frame from either
 * track looks the same to the agent.
 */
@RequiresApi(Build.VERSION_CODES.R)
object ScreenCapture {
    const val DEFAULT_MAX_WIDTH = 728
    private const val JPEG_QUALITY = 80
    private const val ATTEMPTS = 4
    private const val RATE_LIMIT_RETRY_MS = 400L

    suspend fun capture(
        service: AccessibilityService,
        fullRes: Boolean,
        maxWidth: Int = DEFAULT_MAX_WIDTH,
    ): CapturedFrame {
        var lastCode = -1
        repeat(ATTEMPTS) { attempt ->
            when (val result = takeOnce(service)) {
                is TakeResult.Ok -> return encode(result.bitmap, fullRes, maxWidth)
                is TakeResult.Failed -> {
                    lastCode = result.code
                    val rateLimited =
                        result.code == AccessibilityService.ERROR_TAKE_SCREENSHOT_INTERVAL_TIME_SHORT
                    if (!rateLimited || attempt == ATTEMPTS - 1) {
                        throw CaptureException(codeFor(result.code), "takeScreenshot failed with code ${result.code}")
                    }
                    delay(RATE_LIMIT_RETRY_MS)
                }
            }
        }
        throw CaptureException(codeFor(lastCode), "takeScreenshot kept failing with code $lastCode")
    }

    private sealed class TakeResult {
        class Ok(val bitmap: Bitmap) : TakeResult()
        class Failed(val code: Int) : TakeResult()
    }

    private suspend fun takeOnce(service: AccessibilityService): TakeResult =
        suspendCancellableCoroutine { cont ->
            val inline = Executor { it.run() }
            val callback = object : AccessibilityService.TakeScreenshotCallback {
                override fun onSuccess(screenshot: AccessibilityService.ScreenshotResult) {
                    val buffer = screenshot.hardwareBuffer
                    try {
                        val hardware = Bitmap.wrapHardwareBuffer(buffer, screenshot.colorSpace)
                        if (hardware == null) {
                            if (cont.isActive) cont.resume(TakeResult.Failed(-2))
                            return
                        }
                        // Hardware bitmaps cannot be read back; the hash needs pixels.
                        val software = hardware.copy(Bitmap.Config.ARGB_8888, false)
                        hardware.recycle()
                        if (cont.isActive) {
                            cont.resume(if (software == null) TakeResult.Failed(-3) else TakeResult.Ok(software))
                        }
                    } finally {
                        buffer.close()
                    }
                }

                override fun onFailure(errorCode: Int) {
                    if (cont.isActive) cont.resume(TakeResult.Failed(errorCode))
                }
            }
            try {
                service.takeScreenshot(Display.DEFAULT_DISPLAY, inline, callback)
            } catch (e: Exception) {
                if (cont.isActive) cont.resume(TakeResult.Failed(-1))
            }
        }

    private fun codeFor(code: Int): String = when (code) {
        AccessibilityService.ERROR_TAKE_SCREENSHOT_NO_ACCESSIBILITY_ACCESS -> "screenshot_not_permitted"
        AccessibilityService.ERROR_TAKE_SCREENSHOT_INTERVAL_TIME_SHORT -> "screenshot_rate_limited"
        AccessibilityService.ERROR_TAKE_SCREENSHOT_INVALID_DISPLAY -> "screenshot_invalid_display"
        else -> "screenshot_failed"
    }

    private fun encode(source: Bitmap, fullRes: Boolean, maxWidth: Int): CapturedFrame {
        val physicalWidth = source.width
        val physicalHeight = source.height
        val scaled = if (fullRes || physicalWidth <= maxWidth) {
            source
        } else {
            val targetHeight = maxOf(1, Math.round(physicalHeight * (maxWidth.toFloat() / physicalWidth)))
            Bitmap.createScaledBitmap(source, maxWidth, targetHeight, true).also { source.recycle() }
        }
        val out = ByteArrayOutputStream()
        scaled.compress(Bitmap.CompressFormat.JPEG, JPEG_QUALITY, out)
        val hash = quantisedHash(scaled)
        val width = scaled.width
        val height = scaled.height
        scaled.recycle()
        return CapturedFrame(
            jpegBase64 = Base64.encodeToString(out.toByteArray(), Base64.NO_WRAP),
            width = width,
            height = height,
            scaleFactor = physicalWidth.toDouble() / width,
            hash = hash,
            physicalWidth = physicalWidth,
            physicalHeight = physicalHeight,
        )
    }

    /**
     * Mirrors hashRgba in the hub: 4 bits per channel, SHA-1, 16 hex chars, so
     * a blinking cursor is "unchanged" while a navigation is "changed".
     */
    private fun quantisedHash(bitmap: Bitmap): String {
        val width = bitmap.width
        val height = bitmap.height
        val pixels = IntArray(width * height)
        bitmap.getPixels(pixels, 0, width, 0, 0, width, height)
        val quantised = ByteArray(pixels.size * 3)
        var j = 0
        for (p in pixels) {
            quantised[j++] = (((p shr 16) and 0xFF) shr 4).toByte()
            quantised[j++] = (((p shr 8) and 0xFF) shr 4).toByte()
            quantised[j++] = ((p and 0xFF) shr 4).toByte()
        }
        val digest = MessageDigest.getInstance("SHA-1").digest(quantised)
        val hex = StringBuilder(digest.size * 2)
        for (b in digest) hex.append(String.format("%02x", b))
        return hex.substring(0, 16)
    }
}
