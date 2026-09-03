import { describe, expect, it } from "vitest";
import { daemonCredentialChanged, planDaemonToken } from "./daemon-token-sync";

describe("daemonCredentialChanged", () => {
  it("detects a rotated token for the same user", () => {
    expect(daemonCredentialChanged("mul_old", "mul_new", false)).toBe(true);
  });

  it("detects a missing cached token", () => {
    expect(daemonCredentialChanged(undefined, "mul_new", false)).toBe(true);
  });

  it("keeps an unchanged same-user credential stable", () => {
    expect(daemonCredentialChanged("mul_same", "mul_same", false)).toBe(false);
  });

  it("keeps the user-switch signal authoritative even for equal tokens", () => {
    expect(daemonCredentialChanged("mul_same", "mul_same", true)).toBe(true);
  });
});

describe("planDaemonToken", () => {
  it("uses the SSO internal token directly", () => {
    expect(
      planDaemonToken({
        tokenFromRenderer: "sso-jwt",
        cachedToken: "mul_stale",
        sameUser: true,
        useSySso: true,
      }),
    ).toEqual({ kind: "direct", token: "sso-jwt" });
  });

  it("reuses a cached PAT for the same legacy user", () => {
    expect(
      planDaemonToken({
        tokenFromRenderer: "legacy-jwt",
        cachedToken: "mul_cached",
        sameUser: true,
        useSySso: false,
      }),
    ).toEqual({ kind: "cached_pat", token: "mul_cached" });
  });

  it("mints a PAT for a legacy JWT without a reusable cache", () => {
    expect(
      planDaemonToken({
        tokenFromRenderer: "legacy-jwt",
        cachedToken: "mul_previous_user",
        sameUser: false,
        useSySso: false,
      }),
    ).toEqual({ kind: "mint_pat" });
  });

  it("writes a renderer-provided PAT directly in legacy mode", () => {
    expect(
      planDaemonToken({
        tokenFromRenderer: "mul_fresh",
        sameUser: false,
        useSySso: false,
      }),
    ).toEqual({ kind: "direct", token: "mul_fresh" });
  });
});
