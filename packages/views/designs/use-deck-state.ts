import { useCallback, useEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import { DECK_PROTOCOL_VERSION } from "./deck-protocol";

/** Navigation verbs the deck runtime accepts from the host. */
export type DeckSlideAction = "next" | "prev" | "first" | "last" | "go";

export interface UseDeckStateOptions {
  /** Listen only while presentation mode is on; defaults to true. */
  enabled?: boolean;
  /**
   * Reset the projected state when the document behind the frame changes.
   * The iframe itself is never remounted by this hook — a deck is one
   * document and one runtime.
   */
  resetKey?: string;
  /** How long to wait for the first protocol message before flagging it. */
  timeoutMs?: number;
  /**
   * Called for `od:present-escape`, which the deck runtime posts when Escape
   * is pressed inside the frame. The host owns leaving presentation mode.
   */
  onPresentEscape?: () => void;
}

export interface DeckController {
  iframeRef: RefObject<HTMLIFrameElement | null>;
  /** 0-based active slide as the deck last reported it. */
  active: number;
  /** Slide count as the deck last reported it. */
  count: number;
  /** True once the deck answered `od:deck-ready` with protocol version 1. */
  ready: boolean;
  /** True when no protocol message arrived within the timeout. */
  timedOut: boolean;
  /** Post an `od:slide` navigation message into the frame. */
  send: (action: DeckSlideAction, index?: number) => void;
}

interface DeckProjection {
  active: number;
  count: number;
  ready: boolean;
  timedOut: boolean;
}

const INITIAL: DeckProjection = { active: 0, count: 0, ready: false, timedOut: false };

/**
 * Follow one deck iframe's protocol v1 state.
 *
 * Only messages whose `event.source` is this frame's `contentWindow` are
 * trusted: the workbench shares its window with other frames and embedded
 * previews, and any of them could post the same shapes.
 *
 * `od:deck-ready` is strict — it must carry protocol version 1.
 * `od:slide-state` tolerates a missing version (older decks) but a present
 * version still has to be 1, and both `active` and `count` must be finite
 * numbers. Everything else is ignored.
 */
export function useDeckState(options: UseDeckStateOptions = {}): DeckController {
  const enabled = options.enabled ?? true;
  const resetKey = options.resetKey ?? "";
  const timeoutMs = options.timeoutMs ?? 1500;
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [state, setState] = useState<DeckProjection>(INITIAL);

  // The escape callback is a render-local closure; keep the listener effect
  // independent of its identity.
  const escapeRef = useRef(options.onPresentEscape);
  useEffect(() => {
    escapeRef.current = options.onPresentEscape;
  }, [options.onPresentEscape]);

  useEffect(() => {
    setState(INITIAL);
    if (!enabled) return;

    const timer = window.setTimeout(() => {
      setState((current) => (current.ready || current.count > 0 ? current : { ...current, timedOut: true }));
    }, timeoutMs);

    const onMessage = (event: MessageEvent) => {
      const frame = iframeRef.current;
      if (!frame || event.source !== frame.contentWindow) return;
      const data: unknown = event.data;
      if (!data || typeof data !== "object") return;
      const message = data as Record<string, unknown>;

      if (message.type === "od:present-escape") {
        escapeRef.current?.();
        return;
      }

      if (message.type === "od:deck-ready") {
        if (message.protocolVersion !== DECK_PROTOCOL_VERSION) return;
        window.clearTimeout(timer);
        setState((current) => ({ ...current, ready: true, timedOut: false }));
        return;
      }

      if (message.type === "od:slide-state") {
        if (message.protocolVersion != null && message.protocolVersion !== DECK_PROTOCOL_VERSION) return;
        const active = message.active;
        const count = message.count;
        if (typeof active !== "number" || !Number.isFinite(active)) return;
        if (typeof count !== "number" || !Number.isFinite(count)) return;
        window.clearTimeout(timer);
        setState((current) => ({
          ...current,
          active: Math.max(0, Math.floor(active)),
          count: Math.max(0, Math.floor(count)),
          timedOut: false,
        }));
      }
    };

    window.addEventListener("message", onMessage);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("message", onMessage);
    };
  }, [enabled, resetKey, timeoutMs]);

  const send = useCallback((action: DeckSlideAction, index?: number) => {
    const target = iframeRef.current?.contentWindow;
    if (!target) return;
    const message: Record<string, unknown> = { type: "od:slide", action };
    if (index !== undefined && Number.isFinite(index)) message.index = Math.floor(index);
    target.postMessage(message, "*");
  }, []);

  return { iframeRef, active: state.active, count: state.count, ready: state.ready, timedOut: state.timedOut, send };
}
