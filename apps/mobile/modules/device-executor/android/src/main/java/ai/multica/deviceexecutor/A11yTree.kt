package ai.multica.deviceexecutor

import android.accessibilityservice.AccessibilityService
import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo
import java.security.MessageDigest

/**
 * Accessibility tree dump in the same node shape the hub derives from
 * `uiautomator dump` on the adb track (summariseUiautomatorXml), so an agent
 * reads one format regardless of which track answered. Also yields a
 * structure fingerprint the hub uses as a second "did the screen change"
 * signal next to the frame hash.
 */
object A11yTree {
    private const val MAX_VISITED = 2_000

    class Dump(val nodes: List<Map<String, Any?>>, val truncated: Boolean, val fingerprint: String)

    fun dump(service: AccessibilityService, maxNodes: Int, actionableOnly: Boolean): Dump {
        val nodes = ArrayList<Map<String, Any?>>()
        val digest = MessageDigest.getInstance("SHA-1")
        val state = WalkState(maxNodes, actionableOnly, nodes, digest)
        for (root in roots(service)) {
            try {
                walk(root, state)
            } finally {
                @Suppress("DEPRECATION")
                root.recycle()
            }
            if (state.visited >= MAX_VISITED) break
        }
        val hash = digest.digest()
        val hex = StringBuilder(32)
        for (i in 0 until 8) hex.append(String.format("%02x", hash[i]))
        return Dump(nodes, state.truncated, hex.toString())
    }

    /**
     * Structure-only fingerprint of the active window, cheap enough to run
     * after every action: class, view id and bounds of every node, no text.
     */
    fun fingerprint(service: AccessibilityService): String? {
        val root = try {
            service.rootInActiveWindow
        } catch (_: Exception) {
            null
        } ?: return null
        return try {
            val digest = MessageDigest.getInstance("SHA-1")
            val state = WalkState(0, true, ArrayList(), digest)
            walk(root, state)
            val hash = digest.digest()
            val hex = StringBuilder(32)
            for (i in 0 until 8) hex.append(String.format("%02x", hash[i]))
            hex.toString()
        } finally {
            @Suppress("DEPRECATION")
            root.recycle()
        }
    }

    /** Active window first, then the other visible app windows (dialogs, split screen). */
    private fun roots(service: AccessibilityService): List<AccessibilityNodeInfo> {
        val out = ArrayList<AccessibilityNodeInfo>()
        val active = try {
            service.rootInActiveWindow
        } catch (_: Exception) {
            null
        }
        if (active != null) out.add(active)
        val activeWindowId = active?.windowId
        val windows = try {
            service.windows
        } catch (_: Exception) {
            emptyList<AccessibilityWindowInfo>()
        }
        for (window in windows) {
            if (window.type == AccessibilityWindowInfo.TYPE_ACCESSIBILITY_OVERLAY) continue
            if (activeWindowId != null && window.id == activeWindowId) continue
            val root = try {
                window.root
            } catch (_: Exception) {
                null
            } ?: continue
            out.add(root)
        }
        return out
    }

    private class WalkState(
        val maxNodes: Int,
        val actionableOnly: Boolean,
        val nodes: MutableList<Map<String, Any?>>,
        val digest: MessageDigest,
    ) {
        var visited = 0
        var truncated = false
    }

    private fun walk(node: AccessibilityNodeInfo, state: WalkState) {
        if (state.visited >= MAX_VISITED) {
            state.truncated = true
            return
        }
        state.visited++

        val bounds = Rect().also(node::getBoundsInScreen)
        val cls = node.className?.toString() ?: ""
        val viewId = node.viewIdResourceName ?: ""
        state.digest.update("$cls|$viewId|${bounds.left},${bounds.top},${bounds.right},${bounds.bottom}\n".toByteArray())

        if (state.maxNodes > 0) {
            val text = if (node.isShowingHintText) "" else node.text?.toString() ?: ""
            val desc = node.contentDescription?.toString() ?: ""
            val informative = text.isNotEmpty() || desc.isNotEmpty()
            val clickable = node.isClickable || node.isLongClickable
            val listed = when {
                state.actionableOnly && !clickable && !informative -> false
                !informative && !clickable -> false
                else -> true
            }
            if (listed) {
                if (state.nodes.size >= state.maxNodes) {
                    state.truncated = true
                    return
                }
                state.nodes.add(
                    mapOf(
                        "text" to text,
                        "desc" to desc,
                        "cls" to cls,
                        "bounds" to "[${bounds.left},${bounds.top}][${bounds.right},${bounds.bottom}]",
                        "clickable" to clickable,
                        "pkg" to (node.packageName?.toString() ?: ""),
                        "id" to viewId,
                        "editable" to node.isEditable,
                        "scrollable" to node.isScrollable,
                        "checked" to node.isChecked,
                        "focused" to node.isFocused,
                    ),
                )
            }
        }

        val childCount = node.childCount
        for (i in 0 until childCount) {
            if (state.truncated) return
            val child = node.getChild(i) ?: continue
            try {
                walk(child, state)
            } finally {
                @Suppress("DEPRECATION")
                child.recycle()
            }
        }
    }
}
