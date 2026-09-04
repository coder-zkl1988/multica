// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: () => "Plain text" }),
}));

import { CodeBlock } from "@multica/ui/markdown/CodeBlock";

describe("focused desktop syntax-highlighting bundle", () => {
  it("highlights a supported language with the focused Shiki bundle", async () => {
    const { container } = render(
      <CodeBlock code="const answer: number = 42" language="typescript" mode="minimal" />,
    );

    await waitFor(() => expect(container.querySelector(".shiki")).not.toBeNull());
    expect(screen.getByText("const")).toBeInTheDocument();
  });

  it.each(["c++", "cts", "mts", "kts", "zsh"])(
    "keeps the %s alias highlighted",
    async (language) => {
      const { container } = render(
        <CodeBlock code="value" language={language} mode="minimal" />,
      );

      await waitFor(() => expect(container.querySelector(".shiki")).not.toBeNull());
    },
  );

  it("renders unsupported languages as escaped plain text", async () => {
    const code = "<script>alert('safe')</script>";
    const { container } = render(
      <CodeBlock code={code} language="unsupported-grammar" mode="minimal" />,
    );

    await waitFor(() => expect(screen.getByText(code)).toBeInTheDocument());
    expect(container.querySelector(".shiki")).toBeNull();
    expect(container.querySelector("script")).toBeNull();
  });
});
