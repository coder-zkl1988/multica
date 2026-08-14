/**
 * Mobile PriorityIcon — react-native-svg implementation.
 *
 * Geometry mirrors packages/views/issues/components/priority-icon.tsx:
 * low / medium / high share a three-bar frame, urgent is an interrupt badge,
 * and none is a quiet dash.
 *
 * Differences from web:
 *   - No urgent pulse animation in v1 (would need reanimated; defer until
 *     animation polish iteration).
 */
import Svg, { Circle, Line, Rect } from "react-native-svg";
import type { IssuePriority } from "@multica/core/types";

const BARS: Record<IssuePriority, number> = {
  urgent: 3,
  high: 3,
  medium: 2,
  low: 1,
  none: 0,
};

// Mirrors PRIORITY_CONFIG.color in packages/core/issues/config/priority.ts.
const COLOR: Record<IssuePriority, string> = {
  urgent: "#dc2626", // destructive
  high: "#eab308", // warning
  medium: "#eab308", // warning
  low: "#3b82f6", // info
  none: "#71717a", // muted-foreground
};

export function PriorityIcon({
  priority,
  size = 14,
}: {
  priority: IssuePriority;
  size?: number;
}) {
  if (priority === "none") {
    return (
      <Svg width={size} height={size} viewBox="0 0 16 16">
        <Line
          x1={3}
          y1={8}
          x2={13}
          y2={8}
          stroke={COLOR.none}
          strokeWidth={1.5}
          strokeLinecap="round"
        />
      </Svg>
    );
  }

  if (priority === "urgent") {
    return (
      <Svg width={size} height={size} viewBox="0 0 16 16">
        <Rect x={2} y={2} width={12} height={12} rx={3} fill={COLOR.urgent} />
        <Line
          x1={8}
          y1={5}
          x2={8}
          y2={8.6}
          stroke="#ffffff"
          strokeWidth={1.7}
          strokeLinecap="round"
        />
        <Circle cx={8} cy={11} r={0.95} fill="#ffffff" />
      </Svg>
    );
  }

  const filled = BARS[priority];
  const color = COLOR[priority];

  return (
    <Svg width={size} height={size} viewBox="0 0 16 16">
      {[6, 9, 12].map((height, i) => {
        return (
          <Rect
            key={i}
            x={2 + i * 4.25}
            y={14 - height}
            width={3.5}
            height={height}
            rx={1}
            fill={color}
            opacity={i < filled ? 1 : 0.35}
          />
        );
      })}
    </Svg>
  );
}
