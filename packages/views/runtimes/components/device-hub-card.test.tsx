// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime, RuntimeDeviceHub } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { DeviceHubCard } from "./device-hub-card";

// Canonical parsing for the report shape lives in
// packages/core/api/runtime-device-hub-schema.test.ts; this suite keeps the
// wiring: what the owner sees, what a reader sees, what is missing, and the
// switch.

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const mocks = vi.hoisted(() => ({
  hub: null as unknown,
  mutate: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mocks.hub, isLoading: false }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeDeviceHubOptions: (id: string) => ({ queryKey: ["runtimes", "device-hub", id] }),
  useUpdateRuntime: () => ({ mutate: mocks.mutate, isPending: false }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const RUNTIME = {
  id: "rt-1",
  daemon_id: "daemon-1",
  name: "mac-mini",
  runtime_mode: "local",
  provider: "claude",
  status: "online",
  test_host_enabled: false,
} as unknown as AgentRuntime;

function report(over: Partial<RuntimeDeviceHub> = {}): RuntimeDeviceHub {
  return {
    reachable: true,
    url: "http://127.0.0.1:18801",
    version: "0.1.0",
    iphones: 1,
    leases: 0,
    artemis: { installed: true, home: "/opt/artemis", adb: true, phones: 5, unauthorized: 0 },
    reported_at: new Date().toISOString(),
    ...over,
  };
}

function renderCard(canEdit: boolean, runtime: AgentRuntime = RUNTIME) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <DeviceHubCard runtime={runtime} canEdit={canEdit} />
    </I18nProvider>,
  );
}

afterEach(() => {
  cleanup();
  mocks.hub = null;
  mocks.mutate.mockReset();
});

describe("DeviceHubCard", () => {
  it("shows both halves and the checkout path to an editor", () => {
    mocks.hub = report();
    renderCard(true);
    expect(screen.getByText("Artemis ready · 5 phones")).toBeTruthy();
    expect(screen.getByText("Checkout: /opt/artemis")).toBeTruthy();
    expect(screen.getByText("Hub 0.1.0 online")).toBeTruthy();
    expect(screen.getByText("1 iPhones · 0 leases")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /pairing/i })).toBeNull();
  });

  it("hides the checkout path from a reader and disables the test-host switch", () => {
    mocks.hub = report({ artemis: { installed: true, home: "/opt/artemis", adb: true, phones: 5, unauthorized: 0 } });
    renderCard(false);
    expect(screen.queryByText(/Checkout:/)).toBeNull();
    const toggle = screen.getByRole("switch", { name: "Test host" });
    // Base UI marks a disabled switch with aria-disabled / data-disabled rather
    // than the native attribute on every render path; accept any of them.
    const disabled =
      toggle.hasAttribute("disabled") ||
      toggle.getAttribute("aria-disabled") === "true" ||
      toggle.hasAttribute("data-disabled");
    expect(disabled).toBe(true);
  });

  it("names what is missing: Artemis, adb, an authorization, the hub", () => {
    mocks.hub = report({
      reachable: false,
      artemis: { installed: false, home: null, adb: true, phones: 5, unauthorized: 0 },
      reported_at: null,
    });
    renderCard(true);
    expect(screen.getByText("Artemis is not set up on this machine")).toBeTruthy();
    expect(screen.getByText("No device hub on this machine")).toBeTruthy();
    cleanup();

    mocks.hub = report({ artemis: { installed: true, home: null, adb: false, phones: 0, unauthorized: 0 } });
    renderCard(true);
    expect(screen.getByText("adb not found")).toBeTruthy();
    cleanup();

    mocks.hub = report({ artemis: { installed: true, home: null, adb: true, phones: 4, unauthorized: 1 } });
    renderCard(true);
    expect(screen.getByText("Artemis ready · 4 phones")).toBeTruthy();
    expect(screen.getByText(/1 phone is attached but not authorized/)).toBeTruthy();
  });

  it("patches test_host_enabled when the owner flips the switch", () => {
    mocks.hub = report();
    renderCard(true);
    fireEvent.click(screen.getByRole("switch", { name: "Test host" }));
    expect(mocks.mutate).toHaveBeenCalledWith(
      { runtimeId: "rt-1", patch: { test_host_enabled: true } },
      expect.anything(),
    );
  });
});
