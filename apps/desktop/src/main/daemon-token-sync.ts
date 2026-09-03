export type DaemonTokenPlan =
  | { kind: "direct"; token: string }
  | { kind: "cached_pat"; token: string }
  | { kind: "mint_pat" };

export function daemonCredentialChanged(
  previousToken: unknown,
  finalToken: string,
  userChanged: boolean,
): boolean {
  return userChanged || previousToken !== finalToken;
}

interface PlanDaemonTokenInput {
  tokenFromRenderer: string;
  cachedToken?: unknown;
  sameUser: boolean;
  useSySso: boolean;
}

export function planDaemonToken({
  tokenFromRenderer,
  cachedToken,
  sameUser,
  useSySso,
}: PlanDaemonTokenInput): DaemonTokenPlan {
  if (useSySso || tokenFromRenderer.startsWith("mul_")) {
    return { kind: "direct", token: tokenFromRenderer };
  }

  if (
    sameUser &&
    typeof cachedToken === "string" &&
    cachedToken.startsWith("mul_")
  ) {
    return { kind: "cached_pat", token: cachedToken };
  }

  return { kind: "mint_pat" };
}
