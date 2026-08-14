import Svg, { Circle } from "react-native-svg";

export function ChildProgressRing({
  done,
  total,
  color,
  completeColor,
  size = 14,
}: {
  done: number;
  total: number;
  color: string;
  completeColor: string;
  size?: number;
}) {
  const stroke = 1.5;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const ratio = total > 0 ? Math.min(done / total, 1) : 0;
  const offset = circumference * (1 - ratio);
  const complete = total > 0 && done >= total;
  const strokeColor = complete ? completeColor : color;

  return (
    <Svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
      <Circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke={strokeColor}
        strokeOpacity={0.25}
        strokeWidth={stroke}
      />
      <Circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke={strokeColor}
        strokeWidth={stroke}
        strokeDasharray={`${circumference} ${circumference}`}
        strokeDashoffset={offset}
        strokeLinecap="round"
        rotation={-90}
        origin={`${size / 2}, ${size / 2}`}
      />
    </Svg>
  );
}
