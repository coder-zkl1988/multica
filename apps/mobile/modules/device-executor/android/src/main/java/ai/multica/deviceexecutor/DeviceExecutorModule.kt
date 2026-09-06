package ai.multica.deviceexecutor

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import expo.modules.kotlin.exception.Exceptions
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

/**
 * JS surface of the device executor. Everything the channel in
 * apps/mobile/data/device-executor needs from Android goes through here;
 * the WebSocket to the hub, the lease UI and the action loop stay in JS.
 * Expected failures come back as `{ ok: false, code, message }` maps rather
 * than rejections so the dispatcher can forward them to the hub verbatim.
 */
class DeviceExecutorModule : Module() {
    private val context: Context
        get() = appContext.reactContext ?: throw Exceptions.ReactContextLost()

    private val supported = Build.VERSION.SDK_INT >= Build.VERSION_CODES.R

    override fun definition() = ModuleDefinition {
        Name("DeviceExecutor")

        Events("onServiceStateChange")

        Constants(
            "isSupported" to supported,
            "supportedActions" to if (supported) ActionExecutor.SUPPORTED_ACTIONS else emptyList<String>(),
        )

        OnCreate {
            DeviceExecutorAccessibilityService.listener = { connected ->
                sendEvent("onServiceStateChange", mapOf("connected" to connected))
            }
        }

        OnDestroy {
            DeviceExecutorAccessibilityService.listener = null
        }

        Function("getDeviceInfo") {
            DeviceFacts.info(context)
        }

        Function("getPermissionState") {
            DeviceFacts.permissions(context)
        }

        Function("getStatus") {
            val service = DeviceExecutorAccessibilityService.instance
            mapOf(
                "service_connected" to (service != null),
                "current_app" to service?.currentPackage,
                "focus_is_password" to (service?.focusIsPassword() ?: false),
                "battery" to DeviceFacts.battery(context),
            )
        }

        Function("openAccessibilitySettings") {
            launch(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
        }

        Function("openBatteryOptimizationSettings") {
            launch(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
        }

        Function("openNotificationSettings") {
            val intent = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                    .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
            } else {
                Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${context.packageName}"))
            }
            launch(intent)
        }

        Function("startForegroundService") { title: String, text: String ->
            DeviceExecutorForegroundService.start(context, title, text)
        }

        Function("stopForegroundService") {
            DeviceExecutorForegroundService.stop(context)
        }

        AsyncFunction("screenshot") Coroutine { fullRes: Boolean ->
            val service = DeviceExecutorAccessibilityService.instance
            if (!supported || Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
                failure("track_unavailable", "the device executor needs Android 11 or newer")
            } else if (service == null) {
                failure("service_not_connected", "the accessibility service is not enabled")
            } else {
                try {
                    val frame = ScreenCapture.capture(service, fullRes)
                    mapOf(
                        "ok" to true,
                        "frame" to frame.toMap(service.currentPackage),
                        "focus_is_password" to service.focusIsPassword(),
                    )
                } catch (e: CaptureException) {
                    failure(e.code, e.message ?: e.code)
                } catch (e: Exception) {
                    failure("internal", e.message ?: e.javaClass.simpleName)
                }
            }
        }

        AsyncFunction("perform") Coroutine { action: String, params: Map<String, Any?> ->
            val service = DeviceExecutorAccessibilityService.instance
            if (!supported || Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
                failure("track_unavailable", "the device executor needs Android 11 or newer")
            } else if (service == null) {
                failure("service_not_connected", "the accessibility service is not enabled")
            } else {
                val outcome = try {
                    ActionExecutor.perform(service, action, params)
                } catch (e: GestureException) {
                    ActionOutcome.fail(e.code, e.message ?: e.code)
                } catch (e: Exception) {
                    ActionOutcome.fail("internal", e.message ?: e.javaClass.simpleName)
                }
                mapOf(
                    "ok" to outcome.ok,
                    "code" to outcome.code,
                    "message" to outcome.message,
                    "data" to outcome.data,
                    "a11y_fingerprint" to if (outcome.ok) A11yTree.fingerprint(service) else null,
                    "focus_is_password" to service.focusIsPassword(),
                    "current_app" to service.currentPackage,
                )
            }
        }
    }

    private fun failure(code: String, message: String): Map<String, Any?> =
        mapOf("ok" to false, "code" to code, "message" to message)

    private fun launch(intent: Intent) {
        val activity = appContext.currentActivity
        if (activity != null) {
            activity.startActivity(intent)
        } else {
            context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }
}
