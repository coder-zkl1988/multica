/**
 * Empty-state surface shown when the active session has no messages.
 *
 * Two modes mirror web (packages/views/chat/components/chat-window.tsx
 * `EmptyState`):
 *
 *   - first-time (the workspace has never started a chat) → educate and
 *     offer conversation starters so the composer is not a blank dead end.
 *   - returning (at least one prior session exists) → lead with conversation
 *     starters. Tapping prefills the draft so the user can edit before sending.
 *
 * Copy mirrors the web `chat.json` namespace 1:1, and goes through mobile's
 * own i18n — the swap upstream's version anticipated. The fallback prompts
 * are built inside the component because they need `t`.
 */
import { View } from "react-native";
import { useTranslation } from "react-i18next";
import type { Agent, AgentConversationStarter } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";

interface Props {
  hasSessions: boolean;
  agent: Agent | null;
  onPickPrompt: (text: string) => void;
}

export function ChatEmptyState({ hasSessions, agent, onPickPrompt }: Props) {
  const { t } = useTranslation("chat");
  const fallbackPrompts: AgentConversationStarter[] = [
    "capabilities",
    "first_task",
    "recommend",
  ].map((id) => ({
    label: t(`empty_state.fallback_prompts.${id}.label`),
    prompt: t(`empty_state.fallback_prompts.${id}.prompt`),
  }));
  const title = agent
    ? t("empty_state.returning.title_named", { name: agent.name })
    : t("empty_state.first_time.title");
  const configured = (agent?.conversation_starters ?? []).filter(
    (item) => item.label.trim() && item.prompt.trim(),
  );
  const starters = configured.length > 0 ? configured : fallbackPrompts;
  return (
    <View className="flex-1 items-center justify-center px-6 py-8 gap-5">
      <View className="items-center gap-1">
        <Text className="text-base font-semibold text-foreground text-center">
          {title}
        </Text>
        {agent?.description ? (
          <Text className="text-sm text-muted-foreground text-center">
            {agent.description}
          </Text>
        ) : null}
        {!hasSessions ? (
          <Text className="text-sm text-muted-foreground text-center">
            {t("empty_state.pick_example")}
          </Text>
        ) : null}
      </View>
      {agent ? (
        <View className="w-full max-w-xs gap-2">
          {starters.map((item, index) => (
            <Button
              key={index}
              variant="outline"
              onPress={() => onPickPrompt(item.prompt)}
              className="h-auto justify-start px-3 py-2.5"
              accessibilityLabel={item.label}
            >
              <Text className="text-sm text-foreground">{item.label}</Text>
            </Button>
          ))}
        </View>
      ) : null}
    </View>
  );
}
