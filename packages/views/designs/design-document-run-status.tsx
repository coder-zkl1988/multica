"use client";

import type { ProjectDesignSystemTask } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import type { DesignDocumentProvenance } from "./design-document-provenance";

type RunStatus = "empty" | "running" | "failed" | "draft" | "draft_ahead_of_saved" | "saved";

function labelFor(status: RunStatus): string {
  if (status === "running") return "生成中";
  if (status === "failed") return "失败";
  if (status === "draft" || status === "draft_ahead_of_saved") return "草稿";
  if (status === "saved") return "完成";
  return "待生成";
}

function taskLabel(task: ProjectDesignSystemTask | null): string {
  const status = task?.status ?? "";
  if (status === "queued" || status === "dispatched") return "等待智能体接单";
  if (status === "running") return "智能体执行中";
  if (status === "waiting_local_directory") return "等待本地目录";
  if (status === "completed") return "执行完成，正在校验产物";
  if (status === "failed") return "执行失败";
  if (status === "cancelled") return "任务已停止";
  return "任务状态待确认";
}

function operationLabel(operation: string): string {
  if (operation === "adjust") return "调整";
  if (operation === "regenerate") return "重新生成";
  if (operation === "manual_edit") return "手动修改";
  return "生成";
}

function time(value: string | null | undefined): number | null {
  if (!value) return null;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? null : parsed;
}

function duration(milliseconds: number): string {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000));
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分 ${seconds % 60} 秒`;
  return `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分`;
}

function shortDigest(value: string): string {
  return value ? value.replace(/^sha256:/, "").slice(0, 8) : "—";
}

function gate(value: unknown, kind: "Audit" | "Preview"): { label: string; detail: string | null; passed: boolean | null } {
  if (!value || typeof value !== "object" || Array.isArray(value)) return { label: `${kind} 暂无结果`, detail: null, passed: null };
  const record = value as Record<string, unknown>;
  const verification = kind === "Preview" && record.verification && typeof record.verification === "object" && !Array.isArray(record.verification)
    ? record.verification as Record<string, unknown>
    : record;
  if (typeof verification.passed !== "boolean") return { label: `${kind} 暂无结果`, detail: null, passed: null };
  const reason = typeof verification.reason === "string" && verification.reason.trim() ? verification.reason
    : typeof verification.message === "string" && verification.message.trim() ? verification.message
      : null;
  return { label: verification.passed ? `${kind} 通过` : `${kind} 未通过`, detail: reason, passed: verification.passed };
}

function Metadata({ label, value }: { label: string; value: string }) {
  return <div className="flex min-w-0 justify-between gap-2"><dt className="shrink-0 text-muted-foreground">{label}</dt><dd className="truncate font-medium">{value}</dd></div>;
}

export function DesignDocumentRunStatus({
  status,
  task,
  provenance,
  audit,
  previewReceipt,
}: {
  status: RunStatus;
  task: ProjectDesignSystemTask | null;
  provenance: DesignDocumentProvenance;
  audit: unknown;
  previewReceipt: unknown;
}) {
  const started = time(task?.started_at);
  const completed = time(task?.completed_at);
  const elapsed = started !== null && completed !== null ? duration(completed - started) : null;
  const failureReason = task?.failure_reason?.trim() || task?.error?.trim() || null;
  const system = provenance.system;
  const auditGate = gate(audit, "Audit");
  const previewGate = gate(previewReceipt, "Preview");

  return (
    <section aria-label="运行状态与来源" className="space-y-3 text-caption leading-5">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={status === "failed" ? "destructive" : status === "running" ? "default" : "secondary"}>{labelFor(status)}</Badge>
        <span>{task ? operationLabel(task.operation) : "生成"}</span>
        <span className="text-muted-foreground">{taskLabel(task)}</span>
      </div>
      {task?.wait_reason ? <p className="text-muted-foreground">等待原因：{task.wait_reason}</p> : null}
      {status === "failed" || task?.status === "failed" ? (
        <p data-testid="run-status-alert" className="text-destructive">失败原因：{failureReason ?? "任务未返回可操作原因"}</p>
      ) : null}

      <dl className="grid gap-1">
        {started !== null ? <Metadata label="开始时间" value={new Date(started).toISOString()} /> : null}
        {elapsed !== null ? <Metadata label="执行时长" value={elapsed} /> : null}
      </dl>

      <div className="flex flex-wrap gap-2">
        {provenance.associatedRepositoryId ? <Badge variant="outline">已关联仓库 {provenance.associatedRepositoryId}</Badge> : null}
        <Badge variant={provenance.repositoryGrounded ? "outline" : "secondary"}>
          {provenance.repositoryGrounded ? "已按仓库取证" : "未做仓库取证"}
        </Badge>
      </div>

      {system ? (
        <dl className="grid gap-1">
          <Metadata label="设计体系" value={system.name || "未指定"} />
          <Metadata label="来源" value={system.source} />
          <Metadata label="体系 ID" value={system.systemId || "—"} />
          {system.packageId ? <Metadata label="保存包 ID" value={system.packageId} /> : null}
          <Metadata label="内容摘要" value={shortDigest(system.digest)} />
        </dl>
      ) : (
        <p className="text-muted-foreground">设计来源快照缺失或格式无法识别；本页不会臆测体系来源。</p>
      )}

      <div className="grid gap-1">
        {[auditGate, previewGate].map((item) => (
          <div key={item.label} className="flex items-center justify-between gap-2">
            <span className={cn(item.passed === false && "text-destructive", item.passed === true && "text-foreground", item.passed === null && "text-muted-foreground")}>{item.label}</span>
            {item.detail ? <span className="truncate text-muted-foreground">{item.detail}</span> : null}
          </div>
        ))}
      </div>
    </section>
  );
}
