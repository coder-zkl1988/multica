/**
 * Mobile-local mirror of packages/core/runtimes/hooks.ts's runtimeNeedsUpdate.
 * The exported hooks built on it
 * (useMyRuntimesNeedUpdate, useUpdatableRuntimeIds) internally call
 * packages/core's OWN runtimeListOptions/latestCliVersionOptions, binding
 * to a different QueryClient/key-factory instance than mobile owns. Same
 * hazard apps/mobile/CLAUDE.md's "Mobile-owned updaters" section documents
 * for realtime WS updaters — mirror the comparison logic instead of trying
 * to reuse the hooks. readRuntimeCliVersion IS imported below since it's
 * actually exported and purely reads a field off `metadata`.
 */
import type { RuntimeDevice } from "@multica/core/types";
import {
  isNewerCliReleaseVersion,
  readRuntimeCliVersion,
} from "@multica/core/runtimes";

/**
 * Whether to show a static "update available" badge for this runtime.
 * Mirrors desktop's exact gating (packages/core/runtimes/hooks.ts's
 * runtimeNeedsUpdate): local runtimes only, only for the signed-in owner,
 * never for desktop-launched runtimes (Desktop has its own auto-updater).
 */
export function runtimeNeedsUpdate(
  runtime: RuntimeDevice,
  latestVersion: string | null | undefined,
  userId: string | null | undefined,
): boolean {
  if (!latestVersion || !userId) return false;
  if (runtime.runtime_mode !== "local") return false;
  if (runtime.owner_id !== userId) return false;
  if (runtime.metadata && runtime.metadata.launched_by === "desktop") {
    return false;
  }
  const cliVersion = readRuntimeCliVersion(runtime.metadata);
  if (!cliVersion) return false;
  return isNewerCliReleaseVersion(latestVersion, cliVersion);
}
