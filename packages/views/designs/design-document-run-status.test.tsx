import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DesignDocumentRunStatus } from "./design-document-run-status";
import { parseDesignDocumentProvenance } from "./design-document-provenance";

const savedContext = {
  version: "multica.design-context/v1",
  project_id: "project-1",
  source: "cloud_saved_repository_design_system",
  digest: "sha256:" + "a".repeat(64),
  package: {
    scope: "repository",
    project_id: "project-1",
    project_resource_id: "repository-1",
    design_system_id: "system-1",
    saved_package_id: "package-1",
    archive_object_key: "internal/package.zip",
    name: "订单后台体系",
  },
};

const provenance = parseDesignDocumentProvenance({
  inputSnapshot: {
    agent_id: "agent-1",
    project_resource_id: "repository-1",
    issue_id: "issue-1",
    platform: "web",
    recipe: "ui-mockup",
    brief: "做一个订单总览页",
    attachments: [{ attachment_id: "attachment-1" }],
    resolved_design_context: savedContext,
  },
  repositoryGrounded: true,
});

function runTask(overrides: Record<string, unknown> = {}) {
  return {
    id: "task-1",
    agent_id: "agent-1",
    status: "running",
    operation: "generate",
    error: null,
    failure_reason: null,
    wait_reason: null,
    created_at: "2026-08-19T00:00:00Z",
    dispatched_at: null,
    started_at: "2026-08-19T00:00:01Z",
    completed_at: null,
    ...overrides,
  };
}

function renderStatus(overrides: Record<string, unknown> = {}) {
  return render(
    <DesignDocumentRunStatus
      status="running"
      task={runTask()}
      provenance={provenance}
      audit={{ passed: true }}
      previewReceipt={{ passed: true }}
      {...overrides}
    />,
  );
}

describe("DesignDocumentRunStatus", () => {
  it.each([
    ["queued", runTask({ status: "queued", wait_reason: "等待本地目录" }), "等待智能体接单"],
    ["running", runTask({ started_at: "2026-08-19T00:00:01Z" }), "智能体执行中"],
    ["failed", runTask({ status: "failed", error: "runtime went offline", failure_reason: "Runtime 连接中断" }), "执行失败"],
  ] as const)("shows task operation and actionable reason for %s", (_name, task, label) => {
    renderStatus({ task });

    expect(screen.getByText("生成")).toBeInTheDocument();
    expect(screen.getByText(label)).toBeInTheDocument();
    if (task.status === "queued") expect(screen.getByText(/等待原因：等待本地目录/)).toBeInTheDocument();
    if (task.status === "failed") expect(screen.getByText(/Runtime 连接中断/)).toBeInTheDocument();
  });

  it("shows completion wording, elapsed time, and start metadata", () => {
    renderStatus({ status: "draft", task: runTask({ status: "completed", completed_at: "2026-08-19T00:05:00Z" }) });

    expect(screen.getByText("草稿")).toBeInTheDocument();
    expect(screen.getByText(/执行完成/)).toBeInTheDocument();
    expect(screen.getByText(/开始时间/)).toBeInTheDocument();
    expect(screen.getByText(/^\d+ 分/)).toBeInTheDocument();
  });

  it("shows repository association, evidence, frozen package identity, and never the archive key", () => {
    renderStatus();

    expect(screen.getByText("已按仓库取证")).toBeInTheDocument();
    expect(screen.queryByText(/未做仓库取证/)).not.toBeInTheDocument();
    expect(screen.getByText("订单后台体系")).toBeInTheDocument();
    expect(screen.getByText(/system-1/)).toBeInTheDocument();
    expect(screen.getByText(/package-1/)).toBeInTheDocument();
    expect(screen.getByText(/aaaaaaa/)).toBeInTheDocument();
    expect(screen.queryByText(/internal\/package\.zip/)).not.toBeInTheDocument();
  });

  it("keeps association and evidence separate when repository has no evidence", () => {
    renderStatus({ provenance: { ...provenance, repositoryGrounded: false } });

    expect(screen.getByText(/已关联仓库 repository-1/)).toBeInTheDocument();
    expect(screen.getByText("未做仓库取证")).toBeInTheDocument();
  });

  it("shows audit and preview gate results truthfully", () => {
    renderStatus({ audit: { passed: true }, previewReceipt: { verification: { passed: false, reason: "DOM 不可见" } } });
    expect(screen.getByText("Audit 通过")).toBeInTheDocument();
    expect(screen.getByText("Preview 未通过")).toBeInTheDocument();
    expect(screen.getByText("DOM 不可见")).toBeInTheDocument();

    renderStatus({ audit: null, previewReceipt: null });
    expect(screen.getAllByText(/暂无结果/)).toHaveLength(2);
  });
});
