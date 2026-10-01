import { describe, expect, it } from "vitest";
import { resources as RESOURCES } from "./i18n/resources";
import {
  PROJECT_PRIORITIES,
  PROJECT_STATUSES,
  type ProjectTranslate,
  projectPriorityLabel,
  projectStatusLabel,
} from "./project-status";

/** Project-side twin of lib/issue-status.test.ts — same drift hazard. */
const LOCALES = ["en", "zh-Hans"] as const;

function projectsBundle(locale: (typeof LOCALES)[number]) {
  return RESOURCES[locale].projects as unknown as {
    status?: Record<string, string>;
    priority?: Record<string, string>;
  };
}

function makeT(dict: Record<string, string> = {}): ProjectTranslate {
  return (key, opts) =>
    dict[key] ?? ((opts?.defaultValue as string | undefined) ?? key);
}

describe("project status / priority label coverage", () => {
  for (const locale of LOCALES) {
    it(`${locale}: every project status has a label`, () => {
      const status = projectsBundle(locale).status ?? {};
      const missing = PROJECT_STATUSES.filter((s) => !status[s]);
      expect(missing).toEqual([]);
    });

    it(`${locale}: every project priority has a label`, () => {
      const priority = projectsBundle(locale).priority ?? {};
      const missing = PROJECT_PRIORITIES.filter((p) => !priority[p]);
      expect(missing).toEqual([]);
    });
  }

  // Unlike issues, `cancelled` IS a rendered project status (see the file
  // header in project-status.ts) — guard against it being dropped.
  it("keeps cancelled in the rendered project statuses", () => {
    expect(PROJECT_STATUSES).toContain("cancelled");
  });
});

describe("projectStatusLabel / projectPriorityLabel", () => {
  it("resolves a known status through the projects namespace", () => {
    const t = makeT({ "status.paused": "已暂停" });
    expect(projectStatusLabel(t, "paused")).toBe("已暂停");
  });

  it("falls back to the raw value for an unknown status", () => {
    expect(projectStatusLabel(makeT(), "archived")).toBe("archived");
  });

  it("falls back to the raw value for an unknown priority", () => {
    expect(projectPriorityLabel(makeT(), "blocker")).toBe("blocker");
  });
});
