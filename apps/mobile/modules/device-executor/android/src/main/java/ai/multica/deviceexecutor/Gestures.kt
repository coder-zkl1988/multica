package ai.multica.deviceexecutor

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.os.Build
import androidx.annotation.RequiresApi
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull

class GestureException(val code: String, message: String) : Exception(message)

/**
 * Touch injection through dispatchGesture. Coordinates arrive as physical
 * pixels (the hub applied the frame scale). The one thing this track does
 * that adb cannot: `swipe` can rest at its end point before lifting, so a
 * list stops where the finger stopped instead of flinging on.
 */
@RequiresApi(Build.VERSION_CODES.R)
object Gestures {
    private const val TAP_MS = 60L
    private const val DOUBLE_TAP_GAP_MS = 160L
    private const val COMPLETION_GRACE_MS = 3_000L

    suspend fun tap(service: AccessibilityService, x: Float, y: Float) {
        dispatch(service, listOf(GestureDescription.StrokeDescription(point(x, y), 0, TAP_MS)))
    }

    suspend fun doubleTap(service: AccessibilityService, x: Float, y: Float) {
        dispatch(
            service,
            listOf(
                GestureDescription.StrokeDescription(point(x, y), 0, TAP_MS),
                GestureDescription.StrokeDescription(point(x, y), DOUBLE_TAP_GAP_MS, TAP_MS),
            ),
        )
    }

    suspend fun longPress(service: AccessibilityService, x: Float, y: Float, durationMs: Long) {
        dispatch(service, listOf(GestureDescription.StrokeDescription(point(x, y), 0, durationMs)))
    }

    /**
     * Straight-line drag. With `endHoldMs` > 0 the stroke is left open
     * (willContinue) and a zero-length continuation rests at the end point,
     * so the lift-off velocity is zero and nothing flings.
     */
    suspend fun swipe(
        service: AccessibilityService,
        x1: Float,
        y1: Float,
        x2: Float,
        y2: Float,
        durationMs: Long,
        endHoldMs: Long,
    ) {
        val path = Path().apply {
            moveTo(x1, y1)
            lineTo(x2, y2)
        }
        if (endHoldMs <= 0) {
            dispatch(service, listOf(GestureDescription.StrokeDescription(path, 0, durationMs)))
            return
        }
        val move = GestureDescription.StrokeDescription(path, 0, durationMs, true)
        val completed = dispatch(service, listOf(move))
        // Cancelled by the system (a real finger, a screen change): the
        // pointer is already gone, there is nothing left to hold.
        if (!completed) return
        val hold = move.continueStroke(point(x2, y2), 0, endHoldMs, false)
        dispatch(service, listOf(hold))
    }

    private fun point(x: Float, y: Float): Path = Path().apply { moveTo(x, y) }

    /**
     * Returns true when the gesture ran to completion and false when the
     * system cancelled it; throws when dispatch rejected it outright.
     */
    private suspend fun dispatch(
        service: AccessibilityService,
        strokes: List<GestureDescription.StrokeDescription>,
    ): Boolean {
        val builder = GestureDescription.Builder()
        for (stroke in strokes) builder.addStroke(stroke)
        val gesture = builder.build()
        val totalMs = strokes.maxOf { it.startTime + it.duration }
        val outcome = withContext(Dispatchers.Main) {
            withTimeoutOrNull(totalMs + COMPLETION_GRACE_MS) {
                suspendCancellableCoroutine<Boolean> { cont ->
                    val callback = object : AccessibilityService.GestureResultCallback() {
                        override fun onCompleted(gestureDescription: GestureDescription) {
                            if (cont.isActive) cont.resume(true)
                        }

                        override fun onCancelled(gestureDescription: GestureDescription) {
                            if (cont.isActive) cont.resume(false)
                        }
                    }
                    val accepted = try {
                        service.dispatchGesture(gesture, callback, null)
                    } catch (e: Exception) {
                        if (cont.isActive) {
                            cont.resumeWithException(GestureException("gesture_rejected", e.message ?: "dispatchGesture threw"))
                        }
                        return@suspendCancellableCoroutine
                    }
                    if (!accepted && cont.isActive) {
                        cont.resumeWithException(
                            GestureException("gesture_rejected", "dispatchGesture refused the gesture; is the accessibility service enabled?"),
                        )
                    }
                }
            }
        }
        return outcome ?: throw GestureException("timeout", "the gesture did not finish within ${totalMs + COMPLETION_GRACE_MS}ms")
    }
}

/** Finger gesture for "show me more content in `direction`", centred on screen; mirrors scrollGesture in the hub's adb backend. */
object ScrollGeometry {
    class Stroke(val x1: Int, val y1: Int, val x2: Int, val y2: Int)

    fun forDirection(width: Int, height: Int, direction: String, permille: Int): Stroke? {
        val cx = Math.round(width / 2f)
        val cy = Math.round(height / 2f)
        val dy = Math.round(height * permille / 2000f)
        val dx = Math.round(width * permille / 2000f)
        // Start points stay clear of the 5% edge band so no system gesture fires.
        fun clampY(y: Int) = y.coerceIn(Math.round(height * 0.05f), Math.round(height * 0.95f))
        fun clampX(x: Int) = x.coerceIn(Math.round(width * 0.05f), Math.round(width * 0.95f))
        return when (direction) {
            // Content direction: "down" = see what is below = finger moves up.
            "down" -> Stroke(cx, clampY(cy + dy), cx, clampY(cy - dy))
            "up" -> Stroke(cx, clampY(cy - dy), cx, clampY(cy + dy))
            "right" -> Stroke(clampX(cx + dx), cy, clampX(cx - dx), cy)
            "left" -> Stroke(clampX(cx - dx), cy, clampX(cx + dx), cy)
            else -> null
        }
    }
}
