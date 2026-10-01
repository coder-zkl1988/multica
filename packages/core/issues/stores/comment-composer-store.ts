import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * What a reply does by default when it reaches a running agent that would
 * otherwise take it in its current turn: `steer` adds it to that turn,
 * `after_run` starts a new run once the current one ends. Either way the
 * composer's recipient chip can change it for a single message.
 */
export type RunningAgentReply = "steer" | "after_run";

/**
 * Personal preferences for the issue-detail comment composer.
 *
 * `sticky` pins the bottom comment bar to the scroll viewport so it stays
 * reachable while reading a long timeline. It's a personal reading-ergonomics
 * preference (like theme), so it persists globally via `defaultStorage`
 * rather than per-workspace storage.
 *
 * `concise` opts agent runs triggered by this member's comments into the
 * lightweight task prompt. It is a per-member execution preference shared by
 * the top-level composer and every thread reply box, so a conversation keeps
 * one mode instead of re-answering per box (mirrors the chat store's
 * per-session concise selection, SY-326).
 */
interface CommentComposerStore {
  sticky: boolean;
  runningAgentReply: RunningAgentReply;
  toggleSticky: () => void;
  concise: boolean;
  setConcise: (v: boolean) => void;
  setRunningAgentReply: (value: RunningAgentReply) => void;
}

export const useCommentComposerStore = create<CommentComposerStore>()(
  persist(
    (set) => ({
      sticky: true,
      runningAgentReply: "steer",
      toggleSticky: () => set((s) => ({ sticky: !s.sticky })),
      concise: false,
      setConcise: (v) => set({ concise: v }),
      setRunningAgentReply: (runningAgentReply) => set({ runningAgentReply }),
    }),
    {
      name: "multica_comment_composer",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
