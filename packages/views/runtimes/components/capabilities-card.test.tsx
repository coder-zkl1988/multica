// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  AgentRuntime,
  TestCapability,
  TestCapabilityKind,
  TestCapabilityStatus,
} from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { CapabilitiesCard } from "./capabilities-card";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const mocks = vi.hoisted(() => ({
  capabilities: [] as unknown[],
  scan: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mocks.capabilities, isLoading: false }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/testing", () => ({
  TEST_CAPABILITY_KINDS: ["browser", "android_device", "ios_device", "computer_use"],
  testCapabilityListOptions: () => ({ queryKey: ["test-capabilities", "ws-1"] }),
  useRequestRuntimeCapabilityScan: () => ({ mutate: mocks.scan, isPending: false }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const RUNTIME = {
  id: "rt-1",
  daemon_id: "daemon-1",
  name: "mac-mini",
  runtime_mode: "local",
  provider: "claude",
  status: "online",
} as unknown as AgentRuntime;

function capability(over: Partial<TestCapability>): TestCapability {
  return {
    id: "cap-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    runtime_id: "rt-1",
    kind: "browser",
    capability_key: "browser:playwright",
    target: { provider: "playwright" },
    status: "available",
    last_probe_at: null,
    created_at: "2026-09-06T00:00:00Z",
    ...over,
  };
}

function renderCard(canScan = true) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CapabilitiesCard runtime={RUNTIME} canScan={canScan} />
    </I18nProvider>,
  );
}

afterEach(() => {
  cleanup();
  mocks.capabilities = [];
  mocks.scan.mockReset();
});

describe("CapabilitiesCard", () => {
  it("explains an empty inventory and lets the reader ask for a scan", () => {
    renderCard();

    expect(screen.getByText("No browsers or devices reported yet.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Scan again" }));
    expect(mocks.scan).toHaveBeenCalledWith("rt-1", expect.any(Object));
  });

  it("lists only this runtime's capabilities with their kind and status", () => {
    mocks.capabilities = [
      capability({}),
      capability({ id: "cap-2", runtime_id: "rt-other", capability_key: "browser:elsewhere" }),
      capability({
        id: "cap-3",
        kind: "android_device",
        capability_key: "android:pixel-9",
        status: "busy",
      }),
      // A kind the server may add later renders as its raw name, not a crash.
      capability({
        id: "cap-4",
        kind: "vr_headset" as TestCapabilityKind,
        capability_key: "vr:quest",
        status: "weird" as TestCapabilityStatus,
      }),
    ];
    renderCard();

    expect(screen.getByText("Browser")).toBeTruthy();
    expect(screen.getByText("browser:playwright")).toBeTruthy();
    expect(screen.queryByText("browser:elsewhere")).toBeNull();
    expect(screen.getByText("Android device")).toBeTruthy();
    expect(screen.getByText("Busy")).toBeTruthy();
    expect(screen.getByText("vr_headset")).toBeTruthy();
    expect(screen.getByText("Unknown")).toBeTruthy();
  });

  it("hides the scan button when the reader may not scan", () => {
    renderCard(false);
    expect(screen.queryByRole("button", { name: "Scan again" })).toBeNull();
  });
});
