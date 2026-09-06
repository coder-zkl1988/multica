/**
 * The `match` constraints of a capability requirement are edited as one line
 * of `key=value` pairs (`os_version=>=13, browser=chromium`) rather than a
 * key/value grid: a case rarely needs more than one, and the operator lives
 * inside the value (`>=13`) exactly as the server's matcher reads it.
 */
export function formatCapabilityMatch(match: Record<string, string> | undefined): string {
  if (!match) return "";
  return Object.entries(match)
    .map(([key, value]) => `${key}=${value}`)
    .join(", ");
}

/**
 * Inverse of formatCapabilityMatch. Splits on commas, then each pair on its
 * FIRST `=` so an operator value such as `>=13` survives. Pairs without a key
 * or value are dropped; an empty result is `undefined` so the requirement
 * serialises without a `match` field.
 */
export function parseCapabilityMatch(text: string): Record<string, string> | undefined {
  const match: Record<string, string> = {};
  for (const raw of text.split(",")) {
    const pair = raw.trim();
    if (!pair) continue;
    const eq = pair.indexOf("=");
    if (eq <= 0) continue;
    const key = pair.slice(0, eq).trim();
    const value = pair.slice(eq + 1).trim();
    if (!key || !value) continue;
    match[key] = value;
  }
  return Object.keys(match).length > 0 ? match : undefined;
}
