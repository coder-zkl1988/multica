import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  getState: vi.fn(),
  installUpdate: vi.fn(),
  openExternal: vi.fn(),
}));

const translations = {
  desktop: {
    updates: {
      download_title: "Downloading update",
      download_version: "v{{version}} is downloading in the background.",
      download_progress_aria: "Update download {{percent}} percent complete",
      ready_title: "Update ready",
      ready_description: "v{{version}} will be applied on next launch.",
      see_changelog: "See changelog",
      restart_now: "Restart now",
      dismiss: "Dismiss update notification",
      download_failed: "Update download failed",
    },
  },
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (
      selector: (resources: typeof translations) => string,
      values?: Record<string, string>,
    ) => {
      const template = selector(translations);
      return Object.entries(values ?? {}).reduce(
        (result, [key, value]) => result.replace(`{{${key}}}`, value),
        template,
      );
    },
  }),
}));

import { UpdateNotification } from "./update-notification";
import type { UpdaterState } from "../../../shared/updater-types";

describe("UpdateNotification", () => {
  let stateChanged: (state: UpdaterState) => void;

  beforeEach(() => {
    mocks.getState.mockReset().mockResolvedValue({ status: "idle" });
    mocks.installUpdate.mockReset().mockResolvedValue(undefined);
    mocks.openExternal.mockReset().mockResolvedValue(undefined);

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: { openExternal: mocks.openExternal },
    });
    Object.defineProperty(window, "updater", {
      configurable: true,
      value: {
        getState: mocks.getState,
        onStateChange: (listener: (state: UpdaterState) => void) => {
          stateChanged = listener;
          return vi.fn();
        },
        installUpdate: mocks.installUpdate,
      },
    });
  });

  it("shows live download progress from updater state events", () => {
    render(<UpdateNotification />);

    act(() =>
      stateChanged({ status: "downloading", version: "0.4.38", percent: 42.4 }),
    );
    expect(screen.getByText("Downloading update")).toHaveAttribute(
      "role",
      "status",
    );
    expect(
      screen.getByText("v0.4.38 is downloading in the background."),
    ).toBeInTheDocument();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "42");
    expect(screen.getByText("42%")).toBeInTheDocument();
  });

  it("stays dismissed during the same download and reopens when ready", () => {
    render(<UpdateNotification />);
    act(() =>
      stateChanged({ status: "downloading", version: "0.4.38", percent: 20 }),
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Dismiss update notification" }),
    );
    act(() =>
      stateChanged({ status: "downloading", version: "0.4.38", percent: 30 }),
    );
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();

    act(() => stateChanged({ status: "ready", version: "0.4.38" }));
    expect(screen.getByText("Update ready")).toBeInTheDocument();
  });

  it("rehydrates an active download after the renderer reloads", async () => {
    mocks.getState.mockResolvedValue({
      status: "downloading",
      version: "0.4.38",
      percent: 61,
    });

    render(<UpdateNotification />);

    await waitFor(() => expect(screen.getByText("61%")).toBeInTheDocument());
  });

  it("exits the progress state when the download fails", () => {
    render(<UpdateNotification />);

    act(() =>
      stateChanged({
        status: "error",
        version: "0.4.38",
        message: "network timeout",
      }),
    );

    expect(screen.getByText("Update download failed")).toBeInTheDocument();
    expect(screen.getByText("network timeout")).toBeInTheDocument();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("opens the downloaded version's changelog from the ready prompt", () => {
    render(<UpdateNotification />);
    act(() => stateChanged({ status: "ready", version: "0.4.38" }));

    fireEvent.click(screen.getByRole("button", { name: "See changelog" }));

    expect(mocks.openExternal).toHaveBeenCalledWith(
      "https://multica.ai/changelog#release-0-4-38",
    );
  });

  it("installs the update immediately from the primary action", () => {
    render(<UpdateNotification />);
    act(() => stateChanged({ status: "ready", version: "0.4.38" }));

    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));

    expect(mocks.installUpdate).toHaveBeenCalledOnce();
  });
});
