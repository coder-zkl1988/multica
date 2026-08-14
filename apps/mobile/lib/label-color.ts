/** Match the desktop label chip's BT.601 contrast rule. */
export function labelContrastTextColor(hex: string): string {
  const value = hex.replace("#", "");
  if (!/^[0-9a-fA-F]{6}$/.test(value)) return "#111827";

  const red = parseInt(value.slice(0, 2), 16) / 255;
  const green = parseInt(value.slice(2, 4), 16) / 255;
  const blue = parseInt(value.slice(4, 6), 16) / 255;
  const luminance = 0.299 * red + 0.587 * green + 0.114 * blue;
  return luminance > 0.55 ? "#111827" : "#f9fafb";
}
