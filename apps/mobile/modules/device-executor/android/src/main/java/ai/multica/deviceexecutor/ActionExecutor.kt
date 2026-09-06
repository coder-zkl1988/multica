package ai.multica.deviceexecutor

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.accessibility.AccessibilityNodeInfo
import android.accessibilityservice.AccessibilityService
import androidx.annotation.RequiresApi

/** Outcome of one action, before the module adds the frame and focus state. */
class ActionOutcome(
    val ok: Boolean,
    val code: String? = null,
    val message: String? = null,
    val data: Any? = null,
) {
    companion object {
        val OK = ActionOutcome(true)
        fun fail(code: String, message: String) = ActionOutcome(false, code, message)
    }
}

/**
 * Maps a hub rpc_request (action name + physical-pixel params, see
 * ActionParamsSchemas in multica-device-mcp/src/protocol.ts) onto the
 * accessibility service. `track_unavailable` is the code the hub treats as
 * "hand this to the other track", so it is what every action this track
 * cannot do returns.
 */
@RequiresApi(Build.VERSION_CODES.R)
object ActionExecutor {
    /** Advertised in the hello frame; the hub only routes these here. */
    val SUPPORTED_ACTIONS: List<String> = listOf(
        "screenshot",
        "tap",
        "double_tap",
        "long_press",
        "swipe",
        "scroll",
        "type_text",
        "press_key",
        "launch_app",
        "open_url",
        "wait",
        "a11y_tree",
    )

    private const val TRACK_UNAVAILABLE = "track_unavailable"

    suspend fun perform(
        service: DeviceExecutorAccessibilityService,
        action: String,
        params: Map<String, Any?>,
    ): ActionOutcome {
        val screen = DeviceFacts.screen(service)
        return when (action) {
            "tap" -> {
                val (x, y) = pointOrNull(params, screen) ?: return badParams("x and y are required")
                Gestures.tap(service, x, y)
                ActionOutcome.OK
            }
            "double_tap" -> {
                val (x, y) = pointOrNull(params, screen) ?: return badParams("x and y are required")
                Gestures.doubleTap(service, x, y)
                ActionOutcome.OK
            }
            "long_press" -> {
                val (x, y) = pointOrNull(params, screen) ?: return badParams("x and y are required")
                Gestures.longPress(service, x, y, long(params, "duration_ms", 800).coerceIn(200, 10_000))
                ActionOutcome.OK
            }
            "swipe" -> {
                val x1 = num(params, "x1")
                val y1 = num(params, "y1")
                val x2 = num(params, "x2")
                val y2 = num(params, "y2")
                if (x1 == null || y1 == null || x2 == null || y2 == null) return badParams("x1, y1, x2 and y2 are required")
                Gestures.swipe(
                    service,
                    clampX(x1, screen), clampY(y1, screen), clampX(x2, screen), clampY(y2, screen),
                    long(params, "duration_ms", 600).coerceIn(50, 10_000),
                    long(params, "end_hold_ms", 150).coerceIn(0, 2_000),
                )
                ActionOutcome.OK
            }
            "scroll" -> {
                val direction = str(params, "direction") ?: return badParams("direction is required")
                val permille = long(params, "distance_permille", 450).toInt().coerceIn(150, 700)
                val stroke = ScrollGeometry.forDirection(screen.width, screen.height, direction, permille)
                    ?: return badParams("direction must be up, down, left or right")
                // A reading scroll: slow enough to be deliberate, held at the end so it never flings.
                Gestures.swipe(
                    service,
                    stroke.x1.toFloat(), stroke.y1.toFloat(), stroke.x2.toFloat(), stroke.y2.toFloat(),
                    600, 150,
                )
                ActionOutcome.OK
            }
            "type_text" -> typeText(service, str(params, "text") ?: "", bool(params, "submit"))
            "press_key" -> pressKey(service, str(params, "key") ?: "")
            "launch_app" -> launchApp(service, str(params, "package"), str(params, "name"))
            "stop_app" -> ActionOutcome.fail(TRACK_UNAVAILABLE, "the accessibility track cannot force-stop apps; adb can")
            "open_url" -> openUrl(service, str(params, "url") ?: "")
            "a11y_tree" -> {
                val dump = A11yTree.dump(
                    service,
                    long(params, "max_nodes", 400).toInt().coerceIn(1, 2_000),
                    bool(params, "actionable_only", true),
                )
                ActionOutcome(true, data = mapOf("nodes" to dump.nodes, "truncated" to dump.truncated))
            }
            else -> ActionOutcome.fail("unknown_action", "the executor does not implement $action")
        }
    }

    private fun typeText(service: DeviceExecutorAccessibilityService, text: String, submit: Boolean): ActionOutcome {
        val node = service.focusedInput()
            ?: return ActionOutcome.fail("no_focused_field", "no input field has focus; tap the field first")
        try {
            if (node.isPassword) {
                return ActionOutcome.fail("password_field_blocked", "the focused field is a password field")
            }
            if (!node.isEditable) {
                return ActionOutcome.fail("no_focused_field", "the focused node is not editable")
            }
            if (text.isNotEmpty()) {
                // adb `input text` appends at the cursor; setText replaces, so
                // carry the existing text along (a hint is not text).
                val existing = if (node.isShowingHintText) "" else node.text?.toString() ?: ""
                val args = Bundle().apply {
                    putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, existing + text)
                }
                if (!node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
                    return ActionOutcome.fail("set_text_failed", "the field rejected ACTION_SET_TEXT")
                }
            }
            if (submit) {
                node.refresh()
                if (!node.performAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_IME_ENTER.id)) {
                    return ActionOutcome(true, data = mapOf("submit" to "the field has no IME enter action"))
                }
            }
            return ActionOutcome.OK
        } finally {
            @Suppress("DEPRECATION")
            node.recycle()
        }
    }

    private fun pressKey(service: DeviceExecutorAccessibilityService, key: String): ActionOutcome {
        val global = when (key) {
            "back" -> AccessibilityService.GLOBAL_ACTION_BACK
            "home" -> AccessibilityService.GLOBAL_ACTION_HOME
            "recents" -> AccessibilityService.GLOBAL_ACTION_RECENTS
            "enter" -> {
                val node = service.focusedInput()
                    ?: return ActionOutcome.fail(TRACK_UNAVAILABLE, "enter needs a focused field on this track")
                val ok = try {
                    node.performAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_IME_ENTER.id)
                } finally {
                    @Suppress("DEPRECATION")
                    node.recycle()
                }
                return if (ok) ActionOutcome.OK else ActionOutcome.fail(TRACK_UNAVAILABLE, "the focused field has no IME enter action")
            }
            "volume_up", "volume_down", "power" ->
                return ActionOutcome.fail(TRACK_UNAVAILABLE, "$key is a hardware key; only adb can press it")
            else -> return badParams("unknown key $key")
        }
        return if (service.performGlobalAction(global)) {
            ActionOutcome.OK
        } else {
            ActionOutcome.fail("global_action_failed", "performGlobalAction($key) returned false")
        }
    }

    private fun launchApp(service: DeviceExecutorAccessibilityService, pkg: String?, name: String?): ActionOutcome {
        val pm = service.packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val target: Intent? = when {
            !pkg.isNullOrBlank() -> pm.getLaunchIntentForPackage(pkg)
            !name.isNullOrBlank() -> {
                val wanted = name.trim().lowercase()
                @Suppress("DEPRECATION")
                val candidates = pm.queryIntentActivities(launcher, 0)
                val exact = candidates.firstOrNull { it.loadLabel(pm).toString().lowercase() == wanted }
                val partial = exact ?: candidates.firstOrNull { it.loadLabel(pm).toString().lowercase().contains(wanted) }
                partial?.let { pm.getLaunchIntentForPackage(it.activityInfo.packageName) }
            }
            else -> return badParams("package or name is required")
        }
        if (target == null) {
            return ActionOutcome.fail("app_not_found", "no launchable app matches ${pkg ?: name}")
        }
        return try {
            target.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_RESET_TASK_IF_NEEDED)
            service.startActivity(target)
            ActionOutcome(true, data = mapOf("package" to target.component?.packageName))
        } catch (e: Exception) {
            ActionOutcome.fail("launch_failed", e.message ?: "startActivity threw")
        }
    }

    private fun openUrl(service: DeviceExecutorAccessibilityService, url: String): ActionOutcome {
        if (url.isBlank()) return badParams("url is required")
        return try {
            service.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            ActionOutcome.OK
        } catch (e: Exception) {
            ActionOutcome.fail("open_url_failed", e.message ?: "no activity handles $url")
        }
    }

    // ── param helpers: JS numbers arrive as Double, booleans as Boolean ──

    private fun badParams(message: String) = ActionOutcome.fail("bad_params", message)

    private fun num(params: Map<String, Any?>, key: String): Float? = (params[key] as? Number)?.toFloat()

    private fun long(params: Map<String, Any?>, key: String, default: Long): Long =
        (params[key] as? Number)?.toLong() ?: default

    private fun str(params: Map<String, Any?>, key: String): String? = params[key] as? String

    private fun bool(params: Map<String, Any?>, key: String, default: Boolean = false): Boolean =
        (params[key] as? Boolean) ?: default

    private fun pointOrNull(params: Map<String, Any?>, screen: DeviceFacts.Screen): Pair<Float, Float>? {
        val x = num(params, "x") ?: return null
        val y = num(params, "y") ?: return null
        return clampX(x, screen) to clampY(y, screen)
    }

    // Negative or off-screen path points make GestureDescription throw.
    private fun clampX(x: Float, screen: DeviceFacts.Screen) = x.coerceIn(0f, (screen.width - 1).toFloat())

    private fun clampY(y: Float, screen: DeviceFacts.Screen) = y.coerceIn(0f, (screen.height - 1).toFloat())
}
