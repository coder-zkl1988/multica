import { describe, expect, it } from "vitest";
import { planDaemonToken } from "./daemon-token-sync";

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
