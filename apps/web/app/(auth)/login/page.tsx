"use client";

import { Suspense, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { api } from "@multica/core/api";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import {
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { paths, resolvePostAuthDestination } from "@multica/core/paths";
import type { User, Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { Loader2, RefreshCw } from "lucide-react";
import { LoginPage, validateCliCallback } from "@multica/views/auth";
import { useT } from "@multica/views/i18n";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";

async function destinationFor(
  qc: QueryClient,
  user: User,
  workspaces: Workspace[],
  nextUrl: string | null,
): Promise<string> {
  if (nextUrl) return nextUrl;
  if (!user.onboarded_at) {
    try {
      const invitations = await api.listMyInvitations();
      if (invitations.length > 0) {
        qc.setQueryData(workspaceKeys.myInvitations(), invitations);
        return paths.invitations();
      }
    } catch {
      // Invitation lookup is not required to finish authentication.
    }
  }
  return resolvePostAuthDestination(workspaces, user.onboarded_at != null);
}

function AuthModeStatus({
  error,
  onRetry,
}: {
  error: string | null;
  onRetry: () => void;
}) {
  return (
    <main className="flex min-h-svh items-center justify-center px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle>
            {error ? "Unable to load sign-in" : "Loading sign-in configuration"}
          </CardTitle>
          <CardDescription>
            {error || "Checking the server authentication mode..."}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          {error ? (
            <Button onClick={onRetry}>
              <RefreshCw />
              Retry
            </Button>
          ) : (
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          )}
        </CardContent>
      </Card>
    </main>
  );
}

function SSOLoginContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const loginWithSSO = useAuthStore((state) => state.loginWithSSO);
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState("");
  const nextUrl = sanitizeNextUrl(searchParams.get("next"));

  useEffect(() => {
    let active = true;
    setError("");
    void (async () => {
      try {
        const user = await loginWithSSO();
        const workspaces = await api.listWorkspaces();
        qc.setQueryData(workspaceKeys.list(), workspaces);
        const destination = await destinationFor(qc, user, workspaces, nextUrl);
        if (active) router.replace(destination);
      } catch (err) {
        if (active) {
          setError(err instanceof Error ? err.message : "SSO sign-in failed");
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [attempt, loginWithSSO, nextUrl, qc, router]);

  return (
    <main className="flex min-h-svh items-center justify-center px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle>{error ? "Sign-in failed" : "Signing in"}</CardTitle>
          <CardDescription>
            {error || "Completing company SSO authentication..."}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          {error ? (
            <Button onClick={() => setAttempt((value) => value + 1)}>
              <RefreshCw />
              Retry
            </Button>
          ) : (
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          )}
        </CardContent>
      </Card>
    </main>
  );
}

function LegacyLoginContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const { t } = useT("auth");
  const googleClientId = useConfigStore((state) => state.googleClientId);
  const user = useAuthStore((state) => state.user);
  const isLoading = useAuthStore((state) => state.isLoading);
  const cliCallbackRaw = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state") || "";
  const platform = searchParams.get("platform");
  const isDesktopHandoff = platform === "desktop" && !cliCallbackRaw;
  const nextUrl = sanitizeNextUrl(searchParams.get("next"));
  const [desktopToken, setDesktopToken] = useState<string | null>(null);
  const [desktopError, setDesktopError] = useState("");

  // Latched once auth has been observed settled as logged-out on this page.
  // Any `user` that appears afterwards came from the login form in this
  // session — not from an existing session found on arrival.
  const settledLoggedOutRef = useRef(false);

  // Already authenticated ON ARRIVAL — honor ?next= or fall back to first
  // workspace (or /onboarding if the user has none). Skip this entire path
  // when the user arrived to authorize the CLI.
  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      settledLoggedOutRef.current = true;
      return;
    }
    if (cliCallbackRaw) return;
    if (isDesktopHandoff) {
      api
        .issueCliToken()
        .then(({ token }) => {
          setDesktopToken(token);
          window.location.href = `multica://auth/callback?token=${encodeURIComponent(token)}`;
        })
        .catch((err) => {
          setDesktopError(
            err instanceof Error
              ? err.message
              : t(($) => $.web.desktop_handoff.prepare_failed),
          );
        });
      return;
    }
    // A fresh form login updates the auth store before its submit handler has
    // finished seeding the workspace cache. Let that handler own navigation;
    // this effect only redirects users who arrived already authenticated.
    if (settledLoggedOutRef.current) return;
    if (nextUrl) {
      router.replace(nextUrl);
      return;
    }
    void qc
      .ensureQueryData(workspaceListOptions())
      .catch(() => [] as Workspace[])
      .then((workspaces) => destinationFor(qc, user, workspaces, null))
      .then((destination) => router.replace(destination));
  }, [
    cliCallbackRaw,
    isDesktopHandoff,
    isLoading,
    nextUrl,
    qc,
    router,
    t,
    user,
  ]);

  const handleSuccess = async () => {
    const currentUser = useAuthStore.getState().user;
    const workspaces =
      qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    if (!currentUser) {
      router.push(resolvePostAuthDestination(workspaces, false));
      return;
    }
    router.push(await destinationFor(qc, currentUser, workspaces, nextUrl));
  };

  // Build Google OAuth state: encode platform, next URL, and CLI callback
  // params so the callback can redirect to the right place after login.
  // CLI callback/state must survive the Google OAuth round-trip so the
  // post-login callback page can redirect the JWT back to the CLI's local
  // HTTP listener (critical for headless / WSL2 environments).
  const googleState = [
    platform === "desktop" ? "platform:desktop" : "",
    nextUrl ? `next:${nextUrl}` : "",
    cliCallbackRaw && validateCliCallback(cliCallbackRaw)
      ? `cli_callback:${encodeURIComponent(cliCallbackRaw)}`
      : "",
    cliState ? `cli_state:${encodeURIComponent(cliState)}` : "",
  ]
    .filter(Boolean)
    .join(",") || undefined;

  if (isDesktopHandoff && user) {
    if (desktopError) {
      return (
        <div className="flex min-h-screen items-center justify-center">
          <Card className="w-full max-w-sm">
            <CardHeader className="text-center">
              <CardTitle className="text-display-sm">
                {t(($) => $.web.desktop_handoff.failed_title)}
              </CardTitle>
              <CardDescription>{desktopError}</CardDescription>
            </CardHeader>
          </Card>
        </div>
      );
    }
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-display-sm">
              {t(($) => $.web.desktop_handoff.opening_title)}
            </CardTitle>
            <CardDescription>
              {desktopToken
                ? t(($) => $.web.desktop_handoff.opening_description)
                : t(($) => $.web.desktop_handoff.preparing)}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            {desktopToken ? (
              <Button
                variant="outline"
                onClick={() => {
                  window.location.href = `multica://auth/callback?token=${encodeURIComponent(desktopToken)}`;
                }}
              >
                {t(($) => $.web.desktop_handoff.open_button)}
              </Button>
            ) : (
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            )}
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <LoginPage
      onSuccess={handleSuccess}
      google={
        googleClientId
          ? {
              clientId: googleClientId,
              redirectUri: `${window.location.origin}/auth/callback`,
              state: googleState,
            }
          : undefined
      }
      cliCallback={
        cliCallbackRaw && validateCliCallback(cliCallbackRaw)
          ? { url: cliCallbackRaw, state: cliState }
          : undefined
      }
      onTokenObtained={setLoggedInCookie}
      extra={
        <span className="text-caption text-muted-foreground">
          {t(($) => $.web.prefer_desktop)}{" "}
          <Link
            href="/download"
            className="font-medium text-foreground underline decoration-foreground/30 underline-offset-4 hover:decoration-foreground/70"
          >
            {t(($) => $.web.download)}
          </Link>
        </span>
      }
    />
  );
}

function LoginPageContent() {
  const useSySso = useConfigStore((state) => state.useSySso);
  const configError = useConfigStore((state) => state.authConfigError);
  const loadConfig = useConfigStore((state) => state.loadConfig);

  if (useSySso === null) {
    return (
      <AuthModeStatus
        error={configError}
        onRetry={() => {
          void loadConfig(() => api.getConfig()).catch(() => {});
        }}
      />
    );
  }
  return useSySso ? <SSOLoginContent /> : <LegacyLoginContent />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <LoginPageContent />
    </Suspense>
  );
}
