import { describe, expect, it } from "vitest";
import { repositoryName } from "./project-repository";

describe("repositoryName", () => {
  it("prefers an explicit label and otherwise derives the repository basename", () => {
    expect(repositoryName("m-next label", "https://gitlab.example/fe/m-next")).toBe("m-next label");
    expect(repositoryName("", "https://gitlab.example/fe/m-next")).toBe("m-next");
    expect(repositoryName(null, "git@gitlab.example:fe/m-next.git")).toBe("m-next");
    expect(repositoryName("", "", "Current project")).toBe("Current project");
  });
});
