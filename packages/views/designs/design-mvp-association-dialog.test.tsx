import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { DesignMvpAssociationDialog } from "./design-mvp-association-dialog";
import type { DesignMvpRepository } from "./design-mvp-workspace";

const repositories: DesignMvpRepository[] = [
  { id: "repo-1", projectId: "project-1", projectTitle: "CRM", label: "web", repositoryUrl: "https://github.com/example/web", defaultBranchHint: "main" },
];

describe("DesignMvpAssociationDialog", () => {
  it("requires an explicit target and confirms first association without an early mutation", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    render(
      <DesignMvpAssociationDialog
        open
        item={{ id: "file-1", kind: "design_file", projectId: "project-1", projectResourceId: null, title: "Login design", sourceLabel: "Figma" }}
        repositories={repositories}
        pending={false}
        error={null}
        onClose={vi.fn()}
        onConfirm={onConfirm}
      />,
    );
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("Figma · Login design")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "确认关联" }));
    expect(onConfirm).not.toHaveBeenCalled();
    await user.selectOptions(within(dialog).getByLabelText("选择目标仓库"), "repo-1");
    await user.click(within(dialog).getByRole("button", { name: "确认关联" }));
    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith("repo-1"));
  });

  it("offers unlink and preserves asset and target after a rejected change", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn().mockRejectedValue(new ApiError("request failed", 409, "Conflict", { code: "project_resource_project_mismatch" }));
    render(
      <DesignMvpAssociationDialog
        open
        item={{ id: "doc-1", kind: "design_document", projectId: "project-1", projectResourceId: "repo-1", title: "Settings page", sourceLabel: "Multica Design" }}
        repositories={repositories}
        pending={false}
        error={null}
        onClose={vi.fn()}
        onConfirm={onConfirm}
      />,
    );
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "取消关联" })).toBeInTheDocument();
    expect(within(dialog).getByLabelText("选择目标仓库")).toHaveValue("repo-1");
    await user.click(within(dialog).getByRole("button", { name: "确认更换" }));
    expect(await within(dialog).findByText("目标仓库与设计资产不属于同一项目，请重新选择。")).toBeInTheDocument();
    expect(within(dialog).getByText("Multica Design · Settings page")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("选择目标仓库")).toHaveValue("repo-1");
  });
  it("maps a real-shaped ApiError body for an active document task", async () => {
    const onConfirm = vi.fn().mockRejectedValue(new ApiError("request failed", 409, "Conflict", { code: "design_document_task_active" }));
    render(
      <DesignMvpAssociationDialog
        open
        item={{ id: "doc-1", kind: "design_document", projectId: "project-1", projectResourceId: null, title: "Settings page", sourceLabel: "Multica Design" }}
        repositories={repositories}
        pending={false}
        error={null}
        onClose={vi.fn()}
        onConfirm={onConfirm}
      />,
    );
    const dialog = screen.getByRole("dialog");
    await userEvent.selectOptions(within(dialog).getByLabelText("选择目标仓库"), "repo-1");
    await userEvent.click(within(dialog).getByRole("button", { name: "确认关联" }));
    expect(await within(dialog).findByText("当前设计文档任务运行中，请稍后重试。")).toBeInTheDocument();
  });

  it("routes unlink failures through the same error wrapper", async () => {
    const onConfirm = vi.fn().mockRejectedValue(new ApiError("request failed", 409, "Conflict", { code: "design_document_task_active" }));
    render(
      <DesignMvpAssociationDialog
        open
        item={{ id: "doc-1", kind: "design_document", projectId: "project-1", projectResourceId: "repo-1", title: "Settings page", sourceLabel: "Multica Design" }}
        repositories={repositories}
        pending={false}
        error={null}
        onClose={vi.fn()}
        onConfirm={onConfirm}
      />,
    );
    const dialog = screen.getByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "取消关联" }));
    expect(await within(dialog).findByText("当前设计文档任务运行中，请稍后重试。")).toBeInTheDocument();
    expect(within(dialog).getByText("Multica Design · Settings page")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("选择目标仓库")).toHaveValue("repo-1");
    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith(""));
  });
});
