import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { TestCapabilityRequirement } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { TestCaseCapabilitiesField } from "./test-case-capabilities-field";

// The match-line parser has its own matrix in capability-match.test.ts; this
// suite covers the wiring: add, remove, kind change and the optional flag.

const REQUIREMENTS: TestCapabilityRequirement[] = [
  { kind: "browser", match: { browser: "chromium" } },
  { kind: "android_device", optional: true },
];

describe("TestCaseCapabilitiesField", () => {
  it("shows the empty hint and appends a browser requirement", () => {
    const onChange = vi.fn();
    renderWithI18n(<TestCaseCapabilitiesField value={[]} onChange={onChange} />);

    expect(screen.getByText("This case needs no browser or device.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add capability" }));

    expect(onChange).toHaveBeenCalledWith([{ kind: "browser" }]);
  });

  it("renders the match line and the optional flag of existing requirements", () => {
    renderWithI18n(<TestCaseCapabilitiesField value={REQUIREMENTS} onChange={vi.fn()} />);

    const matchInputs = screen.getAllByLabelText("Match") as HTMLInputElement[];
    expect(matchInputs[0]?.value).toBe("browser=chromium");
    expect(matchInputs[1]?.value).toBe("");
    const optional = screen.getAllByRole("checkbox") as HTMLInputElement[];
    expect(optional[0]?.checked).toBe(false);
    expect(optional[1]?.checked).toBe(true);
  });

  it("changes a kind, rewrites the match and removes a requirement", () => {
    const onChange = vi.fn();
    renderWithI18n(<TestCaseCapabilitiesField value={REQUIREMENTS} onChange={onChange} />);

    fireEvent.change(screen.getAllByLabelText("Kind")[1]!, { target: { value: "ios_device" } });
    expect(onChange).toHaveBeenLastCalledWith([
      REQUIREMENTS[0],
      { kind: "ios_device", optional: true },
    ]);

    fireEvent.change(screen.getAllByLabelText("Match")[0]!, {
      target: { value: "os_version=>=13" },
    });
    expect(onChange).toHaveBeenLastCalledWith([
      { kind: "browser", match: { os_version: ">=13" } },
      REQUIREMENTS[1],
    ]);

    fireEvent.click(screen.getAllByRole("button", { name: "Remove this capability" })[0]!);
    expect(onChange).toHaveBeenLastCalledWith([REQUIREMENTS[1]]);
  });
});
