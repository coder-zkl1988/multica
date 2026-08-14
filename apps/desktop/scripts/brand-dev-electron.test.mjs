import { describe, expect, it } from "vitest";

import {
  desiredDevElectronBranding,
  launchServicesRegistration,
} from "./brand-dev-electron-lib.mjs";

describe("development Electron branding", () => {
  it("declares a unique macOS bundle and the Multica deep-link scheme", () => {
    expect(desiredDevElectronBranding("iworker-5174")).toEqual({
      bundleIdentifier: "ai.multica.desktop.dev.iworker-5174",
      name: "Multica Canary iworker-5174",
      protocolName: "Multica Development",
      protocolScheme: "multica",
    });
  });

  it("uses stable defaults without a worktree suffix", () => {
    expect(desiredDevElectronBranding()).toEqual({
      bundleIdentifier: "ai.multica.desktop.dev",
      name: "Multica Canary",
      protocolName: "Multica Development",
      protocolScheme: "multica",
    });
  });

  it("forces LaunchServices to refresh the modified development app", () => {
    expect(launchServicesRegistration("/tmp/Multica Canary.app")).toEqual({
      args: ["-f", "/tmp/Multica Canary.app"],
      executable:
        "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
    });
  });
});
