import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import { useConfigStore } from "@multica/core/config";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { LogIn, Loader2, RefreshCw } from "lucide-react";

function requireRuntimeAppUrl(): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      "Invariant violated: DesktopLoginPage rendered before App accepted runtime config",
    );
  }
  return runtimeConfig.config.appUrl;
}

function LegacyDesktopLogin() {
  const webUrl = requireRuntimeAppUrl();
  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => {}}
        onGoogleLogin={() => {
          void window.desktopAPI.openExternal(
            `${webUrl}/login?platform=desktop`,
          );
        }}
      />
    </div>
  );
}

function SSODesktopLogin() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => window.desktopAPI.onAuthError(setError), []);

  const signIn = async () => {
    setLoading(true);
    setError("");
    try {
      await window.desktopAPI.startSSO();
    } catch (err) {
      setError(err instanceof Error ? err.message : "SSO sign-in failed");
      setLoading(false);
    }
  };

  return (
    <main className="flex min-h-screen items-center justify-center px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle>Multica</CardTitle>
          <CardDescription>
            {error || "Sign in with your company account"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button className="w-full" onClick={signIn} disabled={loading}>
            {loading ? <Loader2 className="animate-spin" /> : <LogIn />}
            Continue with SSO
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}

function ConfigStatus({ error }: { error: string | null }) {
  const loadConfig = useConfigStore((state) => state.loadConfig);
  return (
    <main className="flex min-h-screen items-center justify-center px-4">
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
            <Button
              onClick={() => {
                void loadConfig(() => api.getConfig()).catch(() => {});
              }}
            >
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

export function DesktopLoginPage() {
  const useSySso = useConfigStore((state) => state.useSySso);
  const configError = useConfigStore((state) => state.authConfigError);
  if (useSySso === null) return <ConfigStatus error={configError} />;
  return useSySso ? <SSODesktopLogin /> : <LegacyDesktopLogin />;
}
