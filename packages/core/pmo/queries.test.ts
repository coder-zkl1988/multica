import { describe, expect, it } from "vitest";
import { pmoKeys, pmoRunsOptions } from "./queries";
import type { PMORun } from "../types";

// The query keys every PMO list/detail hook uses. Workspace id is always
// in the key so a workspace switch invalidates nothing across tenants and
// broad invalidation can target `pmoKeys.all(wsId)`.
describe("pmoKeys", () => {
  it("includes the workspace id in every key shape", () => {
    expect(pmoKeys.all("ws-1")).toEqual(["pmo", "ws-1"]);
  });

  it("builds the configs list key", () => {
    expect(pmoKeys.configs("ws-1")).toEqual(["pmo", "ws-1", "configs"]);
  });

  it("builds the runs key scoped by config", () => {
    expect(pmoKeys.runs("ws-1", "cfg-1")).toEqual([
      "pmo",
      "ws-1",
      "runs",
      "cfg-1",
    ]);
  });

  it("builds the single-run key", () => {
    expect(pmoKeys.run("ws-1", "run-1")).toEqual(["pmo", "ws-1", "run", "run-1"]);
  });

  it("keeps run and runs key prefixes distinct", () => {
    const run = pmoKeys.run("ws-1", "run-1");
    const runs = pmoKeys.runs("ws-1", "run-1");
    expect(run).not.toEqual(runs);
    // Neither is a prefix of the other so invalidating one never hits the
    // other accidentally.
    expect(run.slice(0, runs.length)).not.toEqual(runs);
    expect(runs.slice(0, run.length)).not.toEqual(run);
  });
});

// Compile-time sanity: the exported status/trigger literals cover the full
// committed backend enum. This type assertion fails the build if a status
// union member is dropped or renamed.
const _statusExhaustive: Array<PMORun["status"]> = [
  "queued",
  "running",
  "preview_ready",
  "applied",
  "applied_with_review",
  "failed",
];
void _statusExhaustive;

describe("pmoRunsOptions", () => {
  it("polls only while a run is queued or running", () => {
    const options = pmoRunsOptions("ws-1", "cfg-1");
    const refetchInterval = options.refetchInterval as unknown as (query: {
      state: { data?: { runs: Array<Pick<PMORun, "status">> } };
    }) => number | false;

    expect(
      refetchInterval({ state: { data: { runs: [{ status: "queued" }] } } }),
    ).toBe(2000);
    expect(
      refetchInterval({ state: { data: { runs: [{ status: "running" }] } } }),
    ).toBe(2000);
    expect(
      refetchInterval({
        state: { data: { runs: [{ status: "failed" }, { status: "applied" }] } },
      }),
    ).toBe(false);
    expect(refetchInterval({ state: {} })).toBe(false);
  });
});
