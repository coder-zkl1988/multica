import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DesignDocumentTitleEditor } from "./design-document-title";

// The interaction matrix for the inline rename editor (DC-066) lives here;
// design-document-page.test.tsx keeps the wiring that connects it to the
// mutation, the toast and the cache.
function renderEditor(overrides: {
  title?: string;
  onRename?: (title: string) => Promise<unknown>;
  disabled?: boolean;
} = {}) {
  const onRename = overrides.onRename ?? vi.fn(async () => undefined);
  render(
    <DesignDocumentTitleEditor
      title={overrides.title ?? "订单总览"}
      disabled={overrides.disabled}
      onRename={onRename}
    />,
  );
  return { onRename };
}

async function openEditor() {
  await userEvent.click(screen.getByRole("button", { name: "重命名设计稿" }));
  return screen.getByRole("textbox", { name: "设计稿标题" });
}

describe("DesignDocumentTitleEditor", () => {
  it("shows the stored title and opens an input prefilled with it", async () => {
    renderEditor();
    expect(screen.getByText("订单总览")).toBeInTheDocument();

    const input = await openEditor();
    expect(input).toHaveValue("订单总览");
  });

  it("shows the 设计稿 placeholder for an untitled document", () => {
    renderEditor({ title: "   " });
    expect(screen.getByText("设计稿")).toBeInTheDocument();
  });

  it("saves on Enter with the trimmed title and closes", async () => {
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.type(input, "  新版订单总览  ");
    await userEvent.keyboard("{Enter}");

    expect(onRename).toHaveBeenCalledTimes(1);
    expect(onRename).toHaveBeenCalledWith("新版订单总览");
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "设计稿标题" })).not.toBeInTheDocument());
  });

  it("saves from the 保存 button as well as Enter", async () => {
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.type(input, "另一个名字");
    await userEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(onRename).toHaveBeenCalledWith("另一个名字");
  });

  it("cancels on Escape without a request and restores the stored title", async () => {
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.type(input, "不该保存的名字");
    await userEvent.keyboard("{Escape}");

    expect(onRename).not.toHaveBeenCalled();
    expect(screen.getByText("订单总览")).toBeInTheDocument();

    // Reopening shows the stored title, not the abandoned draft.
    expect(await openEditor()).toHaveValue("订单总览");
  });

  it("cancels from the 取消 button without a request", async () => {
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.type(input, "被放弃");
    await userEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(onRename).not.toHaveBeenCalled();
    expect(screen.getByText("订单总览")).toBeInTheDocument();
  });

  it("rejects an empty or whitespace-only title without a request", async () => {
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.keyboard("{Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent("标题不能为空");
    expect(onRename).not.toHaveBeenCalled();

    await userEvent.type(input, "   ");
    await userEvent.keyboard("{Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent("标题不能为空");
    expect(onRename).not.toHaveBeenCalled();
    // The editor stays open so the user can fix the value in place.
    expect(screen.getByRole("textbox", { name: "设计稿标题" })).toBeInTheDocument();
  });

  it("rejects a title past the server's byte cap without a request", async () => {
    // 200 CJK characters pass maxLength (UTF-16 units) but are 600 bytes,
    // which the server would refuse — the editor says so up front.
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.click(input);
    await userEvent.paste("订".repeat(201));
    await userEvent.keyboard("{Enter}");

    expect(await screen.findByRole("alert")).toHaveTextContent("标题过长");
    expect(onRename).not.toHaveBeenCalled();
  });

  it("closes as a no-op when the trimmed title is unchanged", async () => {
    const { onRename } = renderEditor();
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.type(input, " 订单总览 ");
    await userEvent.keyboard("{Enter}");

    expect(onRename).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "设计稿标题" })).not.toBeInTheDocument());
  });

  it("stays open with the draft intact when the save is refused", async () => {
    // The page shows the server's message; the editor only has to keep the
    // user's work so a retry does not need retyping.
    const onRename = vi.fn(async () => {
      throw new Error("network down");
    });
    renderEditor({ onRename });
    const input = await openEditor();

    await userEvent.clear(input);
    await userEvent.type(input, "重试中的名字");
    await userEvent.keyboard("{Enter}");

    expect(onRename).toHaveBeenCalledWith("重试中的名字");
    expect(await screen.findByRole("alert")).toHaveTextContent("重命名失败");
    expect(screen.getByRole("textbox", { name: "设计稿标题" })).toHaveValue("重试中的名字");
    // The header keeps showing the editor, not a title the server never stored.
    expect(screen.queryByText("订单总览")).not.toBeInTheDocument();
  });

  it("cannot start editing while disabled", async () => {
    renderEditor({ disabled: true });
    expect(screen.getByRole("button", { name: "重命名设计稿" })).toBeDisabled();
  });
});
