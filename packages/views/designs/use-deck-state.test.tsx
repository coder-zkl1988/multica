import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useDeckState } from "./use-deck-state";

function mountFrame(): HTMLIFrameElement {
  const frame = document.createElement("iframe");
  document.body.appendChild(frame);
  return frame;
}

function post(source: Window | null, data: unknown) {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source: source as MessageEventSource }));
  });
}

function controller(timeoutMs = 1500) {
  const result = renderHook(() => useDeckState({ timeoutMs }));
  const frame = mountFrame();
  result.result.current.iframeRef.current = frame;
  return { ...result, frame };
}

afterEach(() => {
  document.body.innerHTML = "";
  vi.useRealTimers();
});

describe("useDeckState protocol messages", () => {
  it("accepts od:deck-ready only with protocol version 1", () => {
    const { result, frame } = controller();
    post(frame.contentWindow, { type: "od:deck-ready" });
    expect(result.current.ready).toBe(false);
    post(frame.contentWindow, { type: "od:deck-ready", protocolVersion: 2 });
    expect(result.current.ready).toBe(false);
    post(frame.contentWindow, { type: "od:deck-ready", protocolVersion: 1 });
    expect(result.current.ready).toBe(true);
  });

  it("accepts od:slide-state without a version but requires finite active/count", () => {
    const { result, frame } = controller();
    post(frame.contentWindow, { type: "od:slide-state", active: 1, count: 4 });
    expect(result.current.active).toBe(1);
    expect(result.current.count).toBe(4);

    // A present version still has to match the one we speak.
    post(frame.contentWindow, { type: "od:slide-state", protocolVersion: 2, active: 2, count: 4 });
    expect(result.current.active).toBe(1);

    post(frame.contentWindow, { type: "od:slide-state", active: Number.NaN, count: 4 });
    expect(result.current.active).toBe(1);
    post(frame.contentWindow, { type: "od:slide-state", active: 2 });
    expect(result.current.active).toBe(1);
    post(frame.contentWindow, { type: "od:slide-state", active: "2", count: 4 });
    expect(result.current.active).toBe(1);
  });

  it("ignores messages from any other source", () => {
    const { result, frame } = controller();
    const other = mountFrame();
    post(other.contentWindow, { type: "od:deck-ready", protocolVersion: 1 });
    post(other.contentWindow, { type: "od:slide-state", active: 2, count: 5 });
    expect(result.current.ready).toBe(false);
    expect(result.current.active).toBe(0);
    expect(result.current.count).toBe(0);

    post(window, { type: "od:deck-ready", protocolVersion: 1 });
    expect(result.current.ready).toBe(false);

    post(frame.contentWindow, { type: "od:slide-state", active: 2, count: 5 });
    expect(result.current.active).toBe(2);
  });

  it("ignores non-object payloads", () => {
    const { result, frame } = controller();
    post(frame.contentWindow, null);
    post(frame.contentWindow, "od:deck-ready");
    post(frame.contentWindow, 42);
    expect(result.current.ready).toBe(false);
    expect(result.current.active).toBe(0);
  });

  it("posts od:slide navigation into the frame", () => {
    const { result, frame } = controller();
    const postMessage = vi.spyOn(frame.contentWindow!, "postMessage");

    act(() => result.current.send("next"));
    expect(postMessage).toHaveBeenLastCalledWith({ type: "od:slide", action: "next" }, "*");

    act(() => result.current.send("go", 2));
    expect(postMessage).toHaveBeenLastCalledWith({ type: "od:slide", action: "go", index: 2 }, "*");

    act(() => result.current.send("prev"));
    expect(postMessage).toHaveBeenLastCalledWith({ type: "od:slide", action: "prev" }, "*");
  });

  it("does not post when no frame is attached", () => {
    const { result } = renderHook(() => useDeckState());
    expect(() => act(() => result.current.send("next"))).not.toThrow();
  });

  it("flags 1.5s of silence and clears the flag once the deck speaks", () => {
    vi.useFakeTimers();
    const { result, frame } = controller();
    act(() => vi.advanceTimersByTime(1500));
    expect(result.current.timedOut).toBe(true);

    post(frame.contentWindow, { type: "od:slide-state", active: 0, count: 2 });
    expect(result.current.timedOut).toBe(false);
  });

  it("does not time out when disabled before any message", () => {
    vi.useFakeTimers();
    const result = renderHook(() => useDeckState({ enabled: false }));
    act(() => vi.advanceTimersByTime(5000));
    expect(result.result.current.timedOut).toBe(false);
  });

  it("hands Escape back to the host", () => {
    const onPresentEscape = vi.fn();
    const result = renderHook(() => useDeckState({ onPresentEscape }));
    const frame = mountFrame();
    result.result.current.iframeRef.current = frame;
    post(frame.contentWindow, { type: "od:present-escape", protocolVersion: 1 });
    expect(onPresentEscape).toHaveBeenCalledTimes(1);
  });

  it("resets the projection when the reset key changes", () => {
    const result = renderHook(({ resetKey }) => useDeckState({ resetKey }), {
      initialProps: { resetKey: "revision-1" },
    });
    const frame = mountFrame();
    result.result.current.iframeRef.current = frame;
    post(frame.contentWindow, { type: "od:slide-state", active: 3, count: 5 });
    expect(result.result.current.active).toBe(3);

    result.rerender({ resetKey: "revision-2" });
    expect(result.result.current.active).toBe(0);
    expect(result.result.current.count).toBe(0);
    expect(result.result.current.ready).toBe(false);
  });
});
