#!/usr/bin/env node
// Rebrand the bundled Electron.app's Info.plist so `pnpm dev:desktop`
// shows "Multica Canary" in the menu bar, Cmd+Tab switcher, and
// Activity Monitor. On macOS these titles come from CFBundleName at
// launch time — `app.setName()` cannot override them at runtime, so
// patching the plist in node_modules is the only working fix.
//
// Plist writes are idempotent. LaunchServices is refreshed on every run so a
// modified development app reliably claims multica:// before Electron starts.
// The patch is isolated to this worktree's node_modules — we unlink the file
// before rewriting so we never mutate a pnpm-store inode shared with another
// project.
//
// In a worktree, scripts/dev.mjs sets DESKTOP_APP_SUFFIX so the name becomes
// "Multica Canary <suffix>" — distinguishable in Cmd+Tab and matching the app
// name src/main/index.ts derives from the same env var.

import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  desiredDevElectronBranding,
  launchServicesRegistration,
} from "./brand-dev-electron-lib.mjs";

if (process.platform !== "darwin") process.exit(0);

const branding = desiredDevElectronBranding(process.env.DESKTOP_APP_SUFFIX);

const require = createRequire(import.meta.url);
// `require('electron')` returns the path to the executable
// (.../Electron.app/Contents/MacOS/Electron). Walk up to Contents/Info.plist.
const electronBin = require("electron");
const plistPath = resolve(electronBin, "../../Info.plist");
const appPath = resolve(electronBin, "../../..");

function plistGet(key) {
  try {
    return execFileSync(
      "/usr/libexec/PlistBuddy",
      ["-c", `Print :${key}`, plistPath],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    ).trim();
  } catch {
    return "";
  }
}

function plistSet(key, value) {
  try {
    execFileSync("/usr/libexec/PlistBuddy", [
      "-c",
      `Set :${key} ${value}`,
      plistPath,
    ]);
  } catch {
    execFileSync("/usr/libexec/PlistBuddy", [
      "-c",
      `Add :${key} string ${value}`,
      plistPath,
    ]);
  }
}

function plistDelete(key) {
  try {
    execFileSync("/usr/libexec/PlistBuddy", [
      "-c",
      `Delete :${key}`,
      plistPath,
    ], { stdio: "ignore" });
  } catch {
    // Missing keys are already in the desired state.
  }
}

function plistAdd(key, type, value) {
  const command = value === undefined
    ? `Add :${key} ${type}`
    : `Add :${key} ${type} ${value}`;
  execFileSync("/usr/libexec/PlistBuddy", ["-c", command, plistPath]);
}

const plistMatches =
  plistGet("CFBundleName") === branding.name &&
  plistGet("CFBundleDisplayName") === branding.name &&
  plistGet("CFBundleIdentifier") === branding.bundleIdentifier &&
  plistGet("CFBundleURLTypes:0:CFBundleURLName") === branding.protocolName &&
  plistGet("CFBundleURLTypes:0:CFBundleURLSchemes:0") === branding.protocolScheme;

if (!plistMatches) {
  // Break any pnpm hardlink to the global store: read, unlink, rewrite.
  // PlistBuddy would otherwise write through the hardlink and mutate the
  // shared store file (and every other project's Electron.app with it).
  const original = readFileSync(plistPath);
  unlinkSync(plistPath);
  writeFileSync(plistPath, original);

  plistSet("CFBundleName", branding.name);
  plistSet("CFBundleDisplayName", branding.name);
  plistSet("CFBundleIdentifier", branding.bundleIdentifier);

  plistDelete("CFBundleURLTypes");
  plistAdd("CFBundleURLTypes", "array");
  plistAdd("CFBundleURLTypes:0", "dict");
  plistAdd("CFBundleURLTypes:0:CFBundleURLName", "string", branding.protocolName);
  plistAdd("CFBundleURLTypes:0:CFBundleURLSchemes", "array");
  plistAdd(
    "CFBundleURLTypes:0:CFBundleURLSchemes:0",
    "string",
    branding.protocolScheme,
  );
}

const launchServices = launchServicesRegistration(appPath);
execFileSync(launchServices.executable, launchServices.args);

console.log(
  `[brand-dev-electron] ${plistPath} → ` +
    `CFBundleName="${branding.name}", ` +
    `CFBundleIdentifier="${branding.bundleIdentifier}", ` +
    `${branding.protocolScheme}://`,
);
