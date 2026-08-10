import { createHash, randomBytes } from "node:crypto";

export interface DesktopAuthorization {
  url: string;
  redirectUri: string;
  state: string;
  verifier: string;
}

export function createDesktopAuthorization(
  apiUrl: string,
  redirectUri: string,
): DesktopAuthorization {
  const state = randomBytes(24).toString("base64url");
  const verifier = randomBytes(32).toString("base64url");
  const challenge = createHash("sha256").update(verifier).digest("base64url");
  const url = new URL("/auth/sso/authorize", `${apiUrl.replace(/\/+$/, "")}/`);
  url.search = new URLSearchParams({
    client_id: "desktop",
    redirect_uri: redirectUri,
    state,
    code_challenge: challenge,
    code_challenge_method: "S256",
  }).toString();
  return { url: url.toString(), redirectUri, state, verifier };
}

export function readDesktopCallback(
  rawUrl: string,
  pending: DesktopAuthorization,
): string {
  const url = parseDesktopCallback(rawUrl);
  if (url.searchParams.has("token")) {
    throw new Error("desktop SSO callback contained a legacy token");
  }
  if (url.searchParams.get("state") !== pending.state) {
    throw new Error("desktop SSO state mismatch");
  }
  const code = url.searchParams.get("code");
  if (!code) throw new Error("desktop SSO callback missing code");
  return code;
}

export function readLegacyDesktopToken(rawUrl: string): string | null {
  const url = parseDesktopCallback(rawUrl);
  const token = url.searchParams.get("token");
  return token || null;
}

function parseDesktopCallback(rawUrl: string): URL {
  const url = new URL(rawUrl);
  if (url.protocol !== "multica:" || url.hostname !== "auth" || url.pathname !== "/callback") {
    throw new Error("invalid desktop auth callback");
  }
  return url;
}
