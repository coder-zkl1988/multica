import { useCallback, useEffect, useState } from "react";
import { KeyboardAvoidingView, Platform, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import {
  makeRedirectUri,
  ResponseType,
  useAuthRequest,
} from "expo-auth-session";
import * as WebBrowser from "expo-web-browser";
import * as Haptics from "expo-haptics";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { api } from "@/data/api";
import { useAuthStore } from "@/data/auth-store";
import { mapAuthError } from "@/lib/auth-error";

WebBrowser.maybeCompleteAuthSession();

const API_URL = process.env.EXPO_PUBLIC_API_URL?.replace(/\/+$/, "");
if (!API_URL) throw new Error("EXPO_PUBLIC_API_URL is not set");

const redirectUri = makeRedirectUri({
  native: "multica://auth/mobile-callback",
  scheme: "multica",
  path: "auth/mobile-callback",
});

export default function Login() {
  const sendCode = useAuthStore((state) => state.sendCode);
  const { t } = useTranslation("auth");
  const loginWithSSO = useAuthStore((state) => state.loginWithSSO);
  const [useSySso, setUseSySso] = useState<boolean | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [request, response, promptAsync] = useAuthRequest(
    {
      clientId: "mobile",
      redirectUri,
      responseType: ResponseType.Code,
      usePKCE: true,
    },
    {
      authorizationEndpoint: `${API_URL}/auth/sso/authorize`,
      tokenEndpoint: `${API_URL}/auth/sso/token`,
    },
  );

  const loadConfig = useCallback(async () => {
    setUseSySso(null);
    setConfigError(null);
    try {
      const config = await api.getConfig();
      setUseSySso(config.use_sy_sso === true);
    } catch (err) {
      setConfigError(
        mapAuthError(err, "Couldn't load sign-in configuration."),
      );
    }
  }, []);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  useEffect(() => {
    if (useSySso !== true || !response) return;
    if (response.type !== "success") {
      if (response.type !== "dismiss" && response.type !== "cancel") {
        setError("SSO sign-in was not completed");
      }
      setSubmitting(false);
      return;
    }
    const code = response.params.code;
    const verifier = request?.codeVerifier;
    if (!code || !verifier) {
      setError("SSO callback was incomplete");
      setSubmitting(false);
      return;
    }
    void loginWithSSO(code, verifier, redirectUri)
      .then(() => {
        void Haptics.notificationAsync(
          Haptics.NotificationFeedbackType.Success,
        );
        router.replace("/");
      })
      .catch((err) => {
        void Haptics.notificationAsync(
          Haptics.NotificationFeedbackType.Error,
        );
        setError(mapAuthError(err, "Couldn't complete SSO sign-in."));
        setSubmitting(false);
      });
  }, [loginWithSSO, request, response, useSySso]);

  const sendEmailCode = async () => {
    const trimmed = email.trim();
    if (!trimmed || useSySso !== false) return;
    void Haptics.selectionAsync();
    setSubmitting(true);
    setError(null);
    try {
      await sendCode(trimmed);
      router.push({ pathname: "/verify", params: { email: trimmed } });
    } catch (err) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError(mapAuthError(err, t("login.error_fallback")));
    } finally {
      setSubmitting(false);
    }
  };

  const signInWithSSO = () => {
    if (useSySso !== true) return;
    setError(null);
    setSubmitting(true);
    void Haptics.selectionAsync();
    void promptAsync().catch((err) => {
      setError(mapAuthError(err, "Couldn't open SSO sign-in."));
      setSubmitting(false);
    });
  };

  if (useSySso === null) {
    return (
      <SafeAreaView className="flex-1 bg-background">
        <View className="flex-1 justify-center px-6 gap-4 items-center">
          <MulticaLogo size={32} />
          <Text className="text-lg font-semibold text-foreground">
            {configError
              ? "Unable to load sign-in"
              : "Loading sign-in configuration"}
          </Text>
          {configError ? (
            <>
              <Text className="text-sm text-destructive text-center">
                {configError}
              </Text>
              <Button size="lg" onPress={() => void loadConfig()}>
                <Text>Retry</Text>
              </Button>
            </>
          ) : null}
        </View>
      </SafeAreaView>
    );
  }

  if (useSySso === false) {
    return (
      <SafeAreaView className="flex-1 bg-background">
        <KeyboardAvoidingView
          className="flex-1"
          behavior={Platform.OS === "ios" ? "padding" : undefined}
        >
          <View className="flex-1 justify-center px-6 gap-6">
            <View className="items-center gap-3">
              <MulticaLogo size={32} />
              <View className="gap-1 items-center">
                <Text className="text-2xl font-semibold text-foreground">
                  {t("login.title")}
                </Text>
                <Text className="text-sm text-muted-foreground text-center">
                  {t("login.subtitle")}
                </Text>
              </View>
            </View>
            <View className="gap-3">
              <TextField
                autoCapitalize="none"
                autoComplete="email"
                autoFocus
                keyboardType="email-address"
                placeholder={t("login.email_placeholder")}
                value={email}
                onChangeText={setEmail}
                onSubmitEditing={sendEmailCode}
                returnKeyType="send"
                editable={!submitting}
                invalid={!!error}
              />
              {error ? (
                <Text className="text-sm text-destructive">{error}</Text>
              ) : null}
            </View>
            <Button
              size="lg"
              disabled={submitting || !email.trim()}
              onPress={sendEmailCode}
            >
              <Text>{submitting ? t("login.sending") : t("login.send_code")}</Text>
            </Button>
          </View>
        </KeyboardAvoidingView>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView className="flex-1 bg-background">
      <View className="flex-1 justify-center px-6 gap-6">
        <View className="items-center gap-3">
          <MulticaLogo size={32} />
          <View className="gap-1 items-center">
            <Text className="text-2xl font-semibold text-foreground">Multica</Text>
            <Text className="text-sm text-muted-foreground text-center">
              Sign in with your company account
            </Text>
          </View>
        </View>
        {error ? (
          <Text className="text-sm text-destructive text-center">{error}</Text>
        ) : null}
        <Button
          size="lg"
          disabled={!request || submitting}
          onPress={signInWithSSO}
        >
          <Text>{submitting ? "Signing in..." : "Continue with SSO"}</Text>
        </Button>
      </View>
    </SafeAreaView>
  );
}
