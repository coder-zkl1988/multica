package ai.multica.deviceexecutor

import android.accessibilityservice.AccessibilityServiceInfo
import android.app.NotificationManager
import android.content.Context
import android.os.BatteryManager
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.view.WindowManager
import android.view.accessibility.AccessibilityManager

/** Read-only facts about the phone and the executor's permissions. */
object DeviceFacts {
    class Screen(val width: Int, val height: Int)

    /**
     * Full display size in physical pixels, including system bars: the same
     * number `adb shell wm size` reports, so both tracks share one
     * coordinate space.
     */
    fun screen(context: Context): Screen {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            val wm = context.getSystemService(WindowManager::class.java)
            val bounds = wm?.maximumWindowMetrics?.bounds
            if (bounds != null && bounds.width() > 0 && bounds.height() > 0) {
                return Screen(bounds.width(), bounds.height())
            }
        }
        val metrics = context.resources.displayMetrics
        return Screen(metrics.widthPixels, metrics.heightPixels)
    }

    /** `settings get secure android_id`: the id adb reports too, so the hub merges both tracks into one device. */
    fun androidId(context: Context): String =
        Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID) ?: ""

    fun battery(context: Context): Int? {
        val bm = context.getSystemService(BatteryManager::class.java) ?: return null
        val level = bm.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)
        return if (level in 0..100) level else null
    }

    fun info(context: Context): Map<String, Any?> {
        val screen = screen(context)
        return mapOf(
            "android_id" to androidId(context),
            "model" to Build.MODEL,
            "manufacturer" to Build.MANUFACTURER,
            "os_version" to Build.VERSION.RELEASE,
            "sdk" to Build.VERSION.SDK_INT,
            "screen" to mapOf("width" to screen.width, "height" to screen.height),
            "battery" to battery(context),
        )
    }

    /** Whether the user enabled our accessibility service in system settings (independent of it being bound right now). */
    fun accessibilityEnabled(context: Context): Boolean {
        val am = context.getSystemService(AccessibilityManager::class.java) ?: return false
        val ours = "${context.packageName}/${DeviceExecutorAccessibilityService::class.java.name}"
        val enabled = am.getEnabledAccessibilityServiceList(AccessibilityServiceInfo.FEEDBACK_ALL_MASK)
        if (enabled.any { it.id == ours }) return true
        // Some OEM builds lag the manager list behind the setting string.
        val setting = Settings.Secure.getString(context.contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES)
            ?: return false
        return setting.split(':').any { it.equals(ours, ignoreCase = true) }
    }

    fun permissions(context: Context): Map<String, Any?> {
        val nm = context.getSystemService(NotificationManager::class.java)
        val pm = context.getSystemService(PowerManager::class.java)
        return mapOf(
            "accessibility_enabled" to accessibilityEnabled(context),
            "service_connected" to (DeviceExecutorAccessibilityService.instance != null),
            "notifications_enabled" to (nm?.areNotificationsEnabled() ?: false),
            "ignoring_battery_optimizations" to (pm?.isIgnoringBatteryOptimizations(context.packageName) ?: false),
        )
    }
}
