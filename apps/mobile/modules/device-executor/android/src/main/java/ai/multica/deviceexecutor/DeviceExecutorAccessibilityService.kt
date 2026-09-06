package ai.multica.deviceexecutor

import android.accessibilityservice.AccessibilityService
import android.content.Intent
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo

/**
 * The phone side of the accessibility track. It decides nothing: the hub on
 * the test host sends one rpc_request at a time and this service carries it
 * out (see multica-device-mcp/src/controller/phone-backend.ts for the other
 * end). The system binds and owns the instance, so the module reaches it
 * through the static handle rather than a binder of its own.
 */
class DeviceExecutorAccessibilityService : AccessibilityService() {
    companion object {
        @Volatile
        var instance: DeviceExecutorAccessibilityService? = null
            private set

        /** Set by the Expo module so JS learns when the service comes and goes. */
        @Volatile
        var listener: ((connected: Boolean) -> Unit)? = null

        private const val SYSTEM_UI = "com.android.systemui"
    }

    /** Package of the last window that took the foreground; what the hub reports as current_app. */
    @Volatile
    var currentPackage: String? = null
        private set

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this
        currentPackage = try {
            rootInActiveWindow?.packageName?.toString()
        } catch (_: Exception) {
            null
        }
        listener?.invoke(true)
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        val e = event ?: return
        if (e.eventType != AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED) return
        val pkg = e.packageName?.toString() ?: return
        // The keyboard and the notification shade are system UI windows; they
        // must not masquerade as the app under test.
        if (pkg != SYSTEM_UI && pkg != packageName) currentPackage = pkg
    }

    override fun onInterrupt() {
        // Nothing to interrupt: every action is a short one-shot.
    }

    override fun onUnbind(intent: Intent?): Boolean {
        if (instance === this) {
            instance = null
            listener?.invoke(false)
        }
        return super.onUnbind(intent)
    }

    override fun onDestroy() {
        super.onDestroy()
        if (instance === this) {
            instance = null
            listener?.invoke(false)
        }
    }

    /** The input-focused node, if any. The caller recycles it. */
    fun focusedInput(): AccessibilityNodeInfo? = try {
        findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
    } catch (_: Exception) {
        null
    }

    /** Drives the hub's type_text gate: never type into a password field. */
    fun focusIsPassword(): Boolean {
        val node = focusedInput() ?: return false
        return try {
            node.isPassword
        } finally {
            @Suppress("DEPRECATION")
            node.recycle()
        }
    }
}
