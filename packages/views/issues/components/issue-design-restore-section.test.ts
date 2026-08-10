import { describe, expect, it } from "vitest";
import { createIssueDesignDeliveryScope, createIssueDesignRestoreTaskInput, createRawDesignFallbackScope, defaultDeliveryTargetId, deliveryActorName, deliveryCancelActorName, deliveryFileTitle, deliveryHandoffSource, deliveryScopeItemLabel, deliveryScopeItems, deliveryScopeTitle, deliveryStatusCopy, deliveryTargetCandidates, isRawDesignFallbackDelivery, issueDesignScopeOptions, latestInactiveTargetDelivery, restoreAgentUnavailableCopy, restoreDispatchPrompt, selectDeliveryRestoreTask, selectIssueRestoreTask, sortDesignDeliveryHistory } from "./issue-design-restore-section";
import type { DesignDelivery, DesignFile, DesignFrame, DesignRestoreTask, Issue, MemberWithUser } from "@multica/core/types";

function task(overrides: Partial<DesignRestoreTask>): DesignRestoreTask {
  return {
    id: overrides.id ?? "task",
    workspace_id: "workspace",
    file_id: "file",
    revision_id: overrides.revision_id ?? "rev-current",
    issue_id: overrides.issue_id ?? "issue-1",
    delivery_id: overrides.delivery_id ?? null,
    agent_task_id: overrides.agent_task_id ?? null,
    status: overrides.status ?? "queued",
    input: {},
    result: overrides.result ?? {},
    error: null,
    created_by: null,
    created_at: "2026-06-29T00:00:00Z",
    updated_at: "2026-06-29T00:00:00Z",
    execution_status: overrides.execution_status ?? null,
  };
}

function delivery(overrides: Partial<DesignDelivery>): DesignDelivery {
  return {
    id: overrides.id ?? "delivery",
    workspace_id: "workspace",
    project_id: null,
    source_issue_id: "ui-issue",
    target_issue_id: overrides.target_issue_id ?? "frontend-issue",
    file_id: overrides.file_id ?? "file",
    revision_id: "revision",
    scope: overrides.scope ?? { items: [] },
    status: overrides.status ?? "active",
    delivered_by: overrides.delivered_by ?? null,
    delivered_at: overrides.delivered_at ?? "2026-06-29T00:00:00Z",
    cancelled_by: overrides.cancelled_by ?? null,
    cancelled_at: overrides.cancelled_at ?? null,
    cancel_reason: overrides.cancel_reason ?? null,
    audit_metadata: overrides.audit_metadata ?? {},
    created_at: overrides.created_at ?? "2026-06-29T00:00:00Z",
    updated_at: overrides.updated_at ?? "2026-06-29T00:00:00Z",
  };
}

function issue(overrides: Partial<Issue>): Issue {
  return {
    id: overrides.id ?? "issue",
    workspace_id: "workspace",
    number: overrides.number ?? 1,
    identifier: overrides.identifier ?? "AMC-1",
    title: overrides.title ?? "Issue",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user",
    parent_issue_id: overrides.parent_issue_id ?? "parent",
    project_id: null,
    position: overrides.position ?? 0,
    start_date: null,
    due_date: null,
    metadata: overrides.metadata ?? {},
    created_at: "2026-06-29T00:00:00Z",
    updated_at: "2026-06-29T00:00:00Z",
    ...overrides,
    stage: overrides.stage ?? null,
    properties: overrides.properties ?? {},
  };
}

describe("selectIssueRestoreTask", () => {
  it("ignores completed restore tasks from stale revisions", () => {
    const selected = selectIssueRestoreTask([
      task({ id: "old-completed", revision_id: "rev-old", status: "completed", agent_task_id: "agent-task" }),
      task({ id: "new-queued", revision_id: "rev-current", status: "queued" }),
    ], "issue-1", "rev-current");

    expect(selected?.id).toBe("new-queued");
  });

  it("does not fall back to queued tasks from stale revisions", () => {
    const selected = selectIssueRestoreTask([
      task({ id: "old-queued", revision_id: "rev-old", status: "queued" }),
    ], "issue-1", "rev-current");

    expect(selected).toBeNull();
  });

  it("selects the task bound to the active delivery", () => {
    const selected = selectIssueRestoreTask([
      task({ id: "manual-task", delivery_id: null }),
      task({ id: "delivery-task", delivery_id: "delivery-1" }),
    ], "issue-1", "rev-current", "delivery-1");

    expect(selected?.id).toBe("delivery-task");
  });

  it("does not use delivery-bound tasks for manual issue restore", () => {
    const selected = selectIssueRestoreTask([
      task({ id: "delivery-task", delivery_id: "delivery-1" }),
    ], "issue-1", "rev-current");

    expect(selected).toBeNull();
  });
});

describe("deliveryTargetCandidates", () => {
  it("orders explicit frontend issues before title fallback and ordinary siblings", () => {
    const current = issue({ id: "ui", title: "UI设计", metadata: { design_role: "ui_design" } });
    const explicitFrontend = issue({ id: "explicit", title: "工程实现", number: 3, metadata: { design_role: "frontend_dev" } });
    const titleFrontend = issue({ id: "title", title: "前端开发", number: 1, metadata: {} });
    const ordinary = issue({ id: "ordinary", title: "接口联调", number: 2, metadata: {} });
    const anotherUi = issue({ id: "another-ui", title: "视觉设计", number: 4, metadata: { design_role: "ui_design" } });

    const candidates = deliveryTargetCandidates(current, [ordinary, titleFrontend, explicitFrontend, anotherUi, current]);

    expect(candidates.map((candidate) => candidate.issue.id)).toEqual(["explicit", "title", "ordinary"]);
    expect(candidates.map((candidate) => candidate.badge)).toEqual(["前端开发", "前端开发", "待选择"]);
    expect(candidates.map((candidate) => candidate.hint)).toEqual(["可作为前端交付目标", "可作为前端交付目标", "同级子 Issue，可手动选择为交付目标"]);
  });

  it("defaults only for a single candidate or an existing active delivery target", () => {
    const current = issue({ id: "ui", title: "UI设计" });
    const one = deliveryTargetCandidates(current, [issue({ id: "frontend", title: "前端开发" })]);
    const multiple = deliveryTargetCandidates(current, [
      issue({ id: "frontend-a", title: "前端开发 A", number: 1 }),
      issue({ id: "frontend-b", title: "前端开发 B", number: 2 }),
    ]);

    expect(defaultDeliveryTargetId(one)).toBe("frontend");
    expect(defaultDeliveryTargetId(multiple)).toBe("");
    expect(defaultDeliveryTargetId(multiple, "frontend-b")).toBe("frontend-b");
  });
});

describe("selectDeliveryRestoreTask", () => {
  it("prefers running tasks for the same delivery", () => {
    const selected = selectDeliveryRestoreTask([
      task({ id: "queued", delivery_id: "delivery-1", status: "queued" }),
      task({ id: "running", delivery_id: "delivery-1", status: "running" }),
      task({ id: "other", delivery_id: "delivery-2", status: "running" }),
    ], "delivery-1");

    expect(selected?.id).toBe("running");
  });

  it("returns null without a matching delivery task", () => {
    const selected = selectDeliveryRestoreTask([
      task({ id: "manual", delivery_id: null }),
    ], "delivery-1");

    expect(selected).toBeNull();
  });
});

describe("sortDesignDeliveryHistory", () => {
  it("keeps active deliveries first, then superseded, then cancelled by latest update", () => {
    const sorted = sortDesignDeliveryHistory([
      delivery({ id: "cancelled-new", status: "cancelled", updated_at: "2026-07-01T00:00:00Z" }),
      delivery({ id: "superseded-old", status: "superseded", updated_at: "2026-06-29T00:00:00Z" }),
      delivery({ id: "active", status: "active", updated_at: "2026-06-28T00:00:00Z" }),
      delivery({ id: "superseded-new", status: "superseded", updated_at: "2026-06-30T00:00:00Z" }),
    ]);

    expect(sorted.map((item) => item.id)).toEqual(["active", "superseded-new", "superseded-old", "cancelled-new"]);
  });
});

describe("latestInactiveTargetDelivery", () => {
  it("returns the newest non-active delivery received by an issue", () => {
    const selected = latestInactiveTargetDelivery([
      delivery({ id: "active", status: "active", target_issue_id: "frontend-issue", updated_at: "2026-07-02T00:00:00Z" }),
      delivery({ id: "old-superseded", status: "superseded", target_issue_id: "frontend-issue", updated_at: "2026-06-30T00:00:00Z" }),
      delivery({ id: "new-cancelled", status: "cancelled", target_issue_id: "frontend-issue", updated_at: "2026-07-01T00:00:00Z" }),
      delivery({ id: "other-target", status: "cancelled", target_issue_id: "other-issue", updated_at: "2026-07-03T00:00:00Z" }),
    ], "frontend-issue");

    expect(selected?.id).toBe("new-cancelled");
  });
});

describe("deliveryStatusCopy", () => {
  it("labels all delivery statuses for issue-side history", () => {
    expect(deliveryStatusCopy("active").label).toBe("进行中");
    expect(deliveryStatusCopy("superseded").label).toBe("已覆盖");
    expect(deliveryStatusCopy("cancelled").label).toBe("已撤回");
  });
});

describe("delivery detail helpers", () => {
  it("extracts object scope items and labels mixed frame/layer items", () => {
    const scoped = delivery({
      scope: {
        items: [
          { frameId: "frame-1", frameName: "服务记录", source: "frame" },
          null,
          { layerId: "layer-1", layerName: "筛选栏", source: "selected_layers" },
        ],
      },
    });

    const items = deliveryScopeItems(scoped);
    expect(items).toHaveLength(2);
    expect(deliveryScopeItemLabel(items[0]!, 0)).toEqual({ name: "服务记录", source: "frame", id: "frame-1" });
    expect(deliveryScopeItemLabel(items[1]!, 1)).toEqual({ name: "筛选栏", source: "selected_layers", id: "layer-1" });
  });

  it("resolves delivery file title and actor name with short-id fallbacks", () => {
    const d = delivery({ file_id: "file-123456789", delivered_by: "user-123456789" });
    const files = [{ id: "file-123456789", title: "服务记录设计稿" } as DesignFile];
    const members = [{ user_id: "user-123456789", name: "A 同学" } as MemberWithUser];

    expect(deliveryFileTitle(d, files)).toBe("服务记录设计稿");
    expect(deliveryActorName(d, members)).toBe("A 同学");
    expect(deliveryFileTitle({ ...d, file_id: "unknown-file-id" }, files)).toBe("unknown-");
    expect(deliveryActorName({ ...d, delivered_by: "unknown-user-id" }, members)).toBe("unknown-");
    expect(deliveryActorName({ ...d, delivered_by: null }, members)).toBe("系统");
    expect(deliveryCancelActorName({ ...d, cancelled_by: "user-123456789" }, members)).toBe("A 同学");
    expect(deliveryCancelActorName({ ...d, cancelled_by: null }, members)).toBe("系统");
  });
});

describe("delivery handoff source helpers", () => {
  it("detects raw design fallback deliveries from scope metadata", () => {
    const raw = delivery({
      scope: {
        source_type: "raw_design_revision",
        fallback_policy: "frontend_full_restore_fallback",
        items: [],
      },
    });

    expect(deliveryHandoffSource(raw)).toBe("raw_design_revision");
    expect(isRawDesignFallbackDelivery(raw)).toBe(true);
  });

  it("detects UI restore artifact deliveries from scope metadata", () => {
    const artifact = delivery({
      scope: {
        source_type: "ui_restore_artifact",
        artifact_id: "artifact-1",
        items: [],
      },
    });

    expect(deliveryHandoffSource(artifact)).toBe("ui_restore_artifact");
    expect(isRawDesignFallbackDelivery(artifact)).toBe(false);
  });

  it("creates raw design fallback scope metadata without exposing a user-facing policy", () => {
    const scope = createRawDesignFallbackScope({
      projectId: "project-1",
      sourceIssueId: "ui-issue",
      targetIssueId: "frontend-issue",
      designFileId: "file-1",
      revisionId: "revision-1",
      frameId: "frame-1",
      frameName: "服务记录",
    });

    expect(scope).toEqual({
      version: "1.0",
      source: "issue_delivery",
      source_type: "raw_design_revision",
      fallback_policy: "frontend_full_restore_fallback",
      projectId: "project-1",
      sourceIssueId: "ui-issue",
      targetIssueId: "frontend-issue",
      items: [{
        itemId: "delivery-ui-issue-frame-1",
        order: 1,
        designFileId: "file-1",
        revisionId: "revision-1",
        frameId: "frame-1",
        frameName: "服务记录",
        source: "frame",
        note: "Internal fallback: raw design source handed to frontend for full restore.",
      }],
    });
  });
});

describe("delivery scope title", () => {
  it("uses the Figma group as the title for multi-frame delivery", () => {
    const title = deliveryScopeTitle(delivery({
      id: "delivery-1",
      scope: {
        source_type: "raw_design_revision",
        items: [
          { frameId: "frame-1", frameName: "钱包首页", source: "frame", groupId: "group-43", groupName: "Group 43" },
          { frameId: "frame-2", frameName: "提现流程", source: "frame", groupId: "group-43", groupName: "Group 43" },
        ],
      },
    }));

    expect(title).toBe("Group 43 · 2 个画板");
  });

  it("falls back to the first item name for non-group multi-item delivery", () => {
    const title = deliveryScopeTitle(delivery({
      id: "delivery-1",
      scope: {
        source_type: "raw_design_revision",
        items: [
          { frameId: "frame-1", frameName: "钱包首页", source: "frame" },
          { frameId: "frame-2", frameName: "提现流程", source: "frame" },
        ],
      },
    }));

    expect(title).toBe("钱包首页 等 2 个对象");
  });
});

describe("issue restore task input helpers", () => {
  it("creates UI generation input for all frames in a selected Figma group", () => {
    const input = createIssueDesignRestoreTaskInput({
      issueId: "ui-issue-12345678",
      projectId: "project-1",
      restoreFileId: "file-1",
      restoreRevisionId: "revision-1",
      restoreFrameId: "frame-1",
      restoreFrameName: "Group 43",
      restoreItems: [
        { frameId: "frame-1", frameName: "钱包首页", groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] },
        { frameId: "frame-2", frameName: "提现流程", groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] },
      ],
      receivedDesignDelivery: null,
    });

    expect(input.purpose).toBe("ui_generation");
    expect(input.items).toHaveLength(2);
    expect(input.items.map((item) => item.frameId)).toEqual(["frame-1", "frame-2"]);
    expect(input.items[0]?.note).toContain("Figma 分组 Group 43");
  });

  it("creates UI generation input for UI-owned restore work", () => {
    const input = createIssueDesignRestoreTaskInput({
      issueId: "ui-issue-12345678",
      projectId: "project-1",
      restoreFileId: "file-1",
      restoreRevisionId: "revision-1",
      restoreFrameId: "frame-1",
      restoreFrameName: "服务记录",
      receivedDesignDelivery: null,
    });

    expect(input.purpose).toBe("ui_generation");
    expect(input.items[0]?.note).toBe("Issue 内触发：UI Agent 进行页面所见还原。");
    expect(restoreDispatchPrompt(false)).toContain("UI 页面所见还原");
    expect(restoreAgentUnavailableCopy(false)).toBe("暂无可用 UI Agent");
  });

  it("creates frontend restore input from every frame in the received delivery scope", () => {
    const input = createIssueDesignRestoreTaskInput({
      issueId: "frontend-issue-12345678",
      projectId: "project-1",
      restoreFileId: "file-1",
      restoreRevisionId: "revision-1",
      restoreFrameId: "frame-1",
      restoreFrameName: "Group 43",
      receivedDesignDelivery: delivery({
        id: "delivery-1",
        scope: {
          source_type: "raw_design_revision",
          items: [
            { frameId: "frame-1", frameName: "钱包首页", source: "frame", groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] },
            { frameId: "frame-2", frameName: "提现流程", source: "frame", groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] },
          ],
        },
      }),
    });

    expect(input.purpose).toBe("frontend_restore");
    expect(input.items.map((item) => item.frameId)).toEqual(["frame-1", "frame-2"]);
    expect(input.items[0]?.note).toContain("收到的设计交付");
  });

  it("keeps frontend restore input for frontend work from a received delivery", () => {
    const input = createIssueDesignRestoreTaskInput({
      issueId: "frontend-issue-12345678",
      projectId: "project-1",
      restoreFileId: "file-1",
      restoreRevisionId: "revision-1",
      restoreFrameId: "frame-1",
      restoreFrameName: "服务记录",
      receivedDesignDelivery: delivery({ id: "delivery-1" }),
    });

    expect(input.purpose).toBe("frontend_restore");
    expect(input.items[0]?.note).toBe("Issue 内触发：基于收到的设计交付进行前端整页还原。");
    expect(restoreDispatchPrompt(true)).toContain("前端整页还原");
    expect(restoreAgentUnavailableCopy(true)).toBe("暂无可用前端 Agent");
  });

  it("passes UI restore artifact document path into frontend restore input", () => {
    const input = createIssueDesignRestoreTaskInput({
      issueId: "frontend-issue-12345678",
      projectId: "project-1",
      restoreFileId: "file-1",
      restoreRevisionId: "revision-1",
      restoreFrameId: "frame-1",
      restoreFrameName: "服务记录",
      receivedDesignDelivery: delivery({
        id: "delivery-1",
        scope: {
          source_type: "ui_restore_artifact",
          artifactDocPath: "docs/multica/ui-restore/restore-task-1.md",
          restoreTaskId: "restore-task-1",
          items: [{ frameId: "frame-1", frameName: "服务记录", source: "ui_restore_task" }],
        },
      }),
    });

    expect(input.purpose).toBe("frontend_restore");
    expect(input.artifactDocPath).toBe("docs/multica/ui-restore/restore-task-1.md");
    expect(input.items[0]?.note).toContain("docs/multica/ui-restore/restore-task-1.md");
  });
});

describe("issue design delivery scope helpers", () => {
  const baseInput = {
    projectId: "project-1",
    sourceIssueId: "ui-issue",
    targetIssueId: "frontend-issue",
    designFileId: "file-1",
    revisionId: "revision-1",
    frameId: "frame-1",
    frameName: "服务记录",
  };

  it("creates UI restore artifact scope after UI restore completes", () => {
    const scope = createIssueDesignDeliveryScope({
      ...baseInput,
      activeRestoreTask: task({
        id: "restore-task-1",
        status: "completed",
        result: {
          summary: {
            artifactDocPath: "docs/multica/ui-restore/restore-task-1.md",
          },
        },
      }),
    });

    expect(scope).toEqual({
      version: "1.0",
      source: "issue_delivery",
      source_type: "ui_restore_artifact",
      artifact_id: "restore-task-1",
      restoreTaskId: "restore-task-1",
      artifactDocPath: "docs/multica/ui-restore/restore-task-1.md",
      projectId: "project-1",
      sourceIssueId: "ui-issue",
      targetIssueId: "frontend-issue",
      items: [{
        itemId: "artifact-restore-task-1-frame-1",
        order: 1,
        designFileId: "file-1",
        revisionId: "revision-1",
        frameId: "frame-1",
        frameName: "服务记录",
        source: "ui_restore_task",
        restoreTaskId: "restore-task-1",
        note: "UI restore artifact handed to frontend for implementation.",
      }],
    });
  });

  it("keeps all selected Figma group frames in raw design fallback scope", () => {
    const scope = createIssueDesignDeliveryScope({
      ...baseInput,
      frameName: "Group 43",
      items: [
        { frameId: "frame-1", frameName: "钱包首页", groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] },
        { frameId: "frame-2", frameName: "提现流程", groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] },
      ],
      activeRestoreTask: task({ id: "queued-task", status: "queued" }),
    });

    const items = scope.items as Array<Record<string, unknown>>;
    expect(scope.source_type).toBe("raw_design_revision");
    expect(items).toHaveLength(2);
    expect(items.map((item) => item.frameId)).toEqual(["frame-1", "frame-2"]);
    expect(items[0]?.groupName).toBe("Group 43");
  });

  it("keeps raw design fallback scope when no completed UI restore exists", () => {
    const scope = createIssueDesignDeliveryScope({
      ...baseInput,
      activeRestoreTask: task({ id: "queued-task", status: "queued" }),
    });

    expect(scope.source_type).toBe("raw_design_revision");
    expect(scope.fallback_policy).toBe("frontend_full_restore_fallback");
  });
});

describe("issue design scope options", () => {
  function frame(input: Partial<DesignFrame> & Pick<DesignFrame, "id" | "name">): DesignFrame {
    return {
      rootLayerId: `${input.id}-root`,
      width: 390,
      height: 844,
      ...input,
    };
  }

  it("keeps both Figma group options and individual frame options", () => {
    const options = issueDesignScopeOptions([
      frame({ id: "frame-1", name: "钱包首页", source: { groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] } }),
      frame({ id: "frame-2", name: "提现流程", source: { groupId: "group-43", groupName: "Group 43", groupPath: ["Group 43"] } }),
      frame({ id: "frame-3", name: "未分组页面" }),
    ]);

    expect(options.map((option) => ({ id: option.id, label: option.label, kind: option.kind, count: option.items.length }))).toEqual([
      { id: "group:group-43", label: "Group 43", kind: "figma_group", count: 2 },
      { id: "frame:frame-1", label: "钱包首页", kind: "frame", count: 1 },
      { id: "frame:frame-2", label: "提现流程", kind: "frame", count: 1 },
      { id: "frame:frame-3", label: "未分组页面", kind: "frame", count: 1 },
    ]);
    expect(options.find((option) => option.id === "frame:frame-1")?.items[0]?.groupName).toBe("Group 43");
  });
});
