"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleStop, Clock3, FilePlus2, Loader2, Paperclip, Play, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { designDocumentTaskListOptions } from "@multica/core/designs/queries";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent, DesignDocumentAgentTask, Issue, Project } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { Textarea } from "@multica/ui/components/ui/textarea";

type UploadedInput = { id: string; filename: string };

export function DesignDocumentTaskPanel({
  projects,
  agents,
  projectId,
  onTaskCreated,
}: {
  projects: Project[];
  agents: Agent[];
  projectId?: string;
  onTaskCreated?: (projectId: string) => void;
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [selectedProjectId, setSelectedProjectId] = useState(projectId ?? "");
  const [agentId, setAgentId] = useState("");
  const [issueId, setIssueId] = useState("");
  const [requirement, setRequirement] = useState("");
  const [platform, setPlatform] = useState<"" | "web" | "mobile" | "cross_platform">("");
  const [attachments, setAttachments] = useState<UploadedInput[]>([]);
  const [submitError, setSubmitError] = useState("");
  const effectiveProjectId = projectId ?? selectedProjectId;
  const selectedProject = projects.find((project) => project.id === effectiveProjectId);
  const availableAgents = useMemo(() => agents.filter((agent) => !agent.archived_at && agent.runtime_id), [agents]);

  const { data: taskRows = [], isLoading: tasksLoading } = useQuery(designDocumentTaskListOptions(wsId, projectId));
  const { data: projectIssues = [] } = useQuery({
    queryKey: ["design-document-task-issues", wsId, effectiveProjectId],
    queryFn: async () => (await api.listIssues({ project_id: effectiveProjectId, limit: 100 })).issues,
    enabled: Boolean(effectiveProjectId),
  });
  const { upload, uploading } = useFileUpload(api, (error) => toast.error(error.message));

  const createTask = useMutation({
    mutationFn: () => api.createDesignDocumentAgentTask({
      project_id: effectiveProjectId,
      agent_id: agentId,
      requirement: requirement.trim(),
      ...(issueId ? { issue_id: issueId } : {}),
      ...(platform ? { target_platform: platform } : {}),
      ...(attachments.length ? { attachment_ids: attachments.map((item) => item.id) } : {}),
    }),
    onSuccess: async (task) => {
      await queryClient.invalidateQueries({ queryKey: designKeys.documentTasks(wsId) });
	  setRequirement("");
	  setIssueId("");
	  setAttachments([]);
	  setSubmitError("");
      toast.success("设计任务已创建");
      onTaskCreated?.(task.project_id);
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "创建设计任务失败";
      setSubmitError(message);
      toast.error(message);
    },
  });

  const cancelTask = useMutation({
    mutationFn: (taskId: string) => api.cancelTaskById(taskId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: designKeys.documentTasks(wsId) });
      toast.success("任务已停止");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "停止任务失败"),
  });

  const handleFiles = async (files: FileList | null) => {
    if (!files) return;
    for (const file of Array.from(files)) {
      const result = await upload(file);
      if (result) setAttachments((current) => [...current, { id: result.id, filename: result.filename }]);
    }
  };

  const removeAttachment = async (attachment: UploadedInput) => {
    try {
      await api.deleteAttachment(attachment.id);
      setAttachments((current) => current.filter((item) => item.id !== attachment.id));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除附件失败");
    }
  };

  const canSubmit = Boolean(effectiveProjectId && agentId && requirement.trim() && !uploading && !createTask.isPending);

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-4 py-5 sm:px-6">
      <section aria-labelledby="design-task-form-heading" className="space-y-4 border-b pb-6">
        <div>
          <h2 id="design-task-form-heading" className="text-base font-semibold">开始设计</h2>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="design-task-project">项目</Label>
            {projectId ? (
              <div className="flex h-8 items-center rounded-md border bg-muted/20 px-2.5 text-body">{selectedProject?.title ?? "项目"}</div>
            ) : (
              <NativeSelect id="design-task-project" aria-label="项目" className="w-full" value={selectedProjectId} onChange={(event) => { setSelectedProjectId(event.target.value); setIssueId(""); }}>
                <NativeSelectOption value="">选择项目</NativeSelectOption>
                {projects.map((project) => <NativeSelectOption key={project.id} value={project.id}>{project.title}</NativeSelectOption>)}
              </NativeSelect>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="design-task-agent">Agent</Label>
            <NativeSelect id="design-task-agent" aria-label="Agent" className="w-full" value={agentId} onChange={(event) => setAgentId(event.target.value)}>
              <NativeSelectOption value="">选择 Agent</NativeSelectOption>
              {availableAgents.map((agent) => <NativeSelectOption key={agent.id} value={agent.id}>{agent.name}</NativeSelectOption>)}
            </NativeSelect>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="design-task-issue">Issue（可选）</Label>
            <NativeSelect id="design-task-issue" className="w-full" value={issueId} disabled={!effectiveProjectId} onChange={(event) => setIssueId(event.target.value)}>
              <NativeSelectOption value="">不关联 Issue</NativeSelectOption>
              {projectIssues.map((issue: Issue) => <NativeSelectOption key={issue.id} value={issue.id}>{issue.identifier} · {issue.title}</NativeSelectOption>)}
            </NativeSelect>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="design-task-platform">目标平台（可选）</Label>
            <NativeSelect id="design-task-platform" className="w-full" value={platform} onChange={(event) => setPlatform(event.target.value as typeof platform)}>
              <NativeSelectOption value="">未指定</NativeSelectOption>
              <NativeSelectOption value="web">Web</NativeSelectOption>
              <NativeSelectOption value="mobile">移动端</NativeSelectOption>
              <NativeSelectOption value="cross_platform">跨平台</NativeSelectOption>
            </NativeSelect>
          </div>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="design-task-requirement">设计需求</Label>
          <Textarea id="design-task-requirement" aria-label="设计需求" value={requirement} onChange={(event) => setRequirement(event.target.value)} maxLength={32768} rows={5} placeholder="描述页面、状态、流程和关键约束" />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-md border px-3 text-body font-medium hover:bg-accent">
            {uploading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FilePlus2 className="h-3.5 w-3.5" />}
            {uploading ? "上传中" : "添加附件"}
            <input type="file" multiple className="sr-only" disabled={uploading || attachments.length >= 10} onChange={(event) => void handleFiles(event.target.files)} />
          </label>
          {attachments.map((attachment) => (
            <span key={attachment.id} className="inline-flex h-8 max-w-64 items-center gap-1.5 rounded-md border bg-muted/20 px-2 text-caption">
              <Paperclip className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate">{attachment.filename}</span>
              <button type="button" className="rounded p-0.5 text-muted-foreground hover:text-foreground" aria-label={`移除 ${attachment.filename}`} onClick={() => void removeAttachment(attachment)}><X className="h-3.5 w-3.5" /></button>
            </span>
          ))}
          <Button className="ml-auto" disabled={!canSubmit} onClick={() => createTask.mutate()}>
            {createTask.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
            开始设计
          </Button>
        </div>
        {submitError ? <p role="alert" className="text-caption text-destructive">{submitError}</p> : null}
      </section>

      <section aria-labelledby="design-task-list-heading" className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <h2 id="design-task-list-heading" className="text-body font-semibold">设计草稿任务</h2>
          <span className="font-mono text-caption text-muted-foreground">{taskRows.length}</span>
        </div>
        {tasksLoading ? (
          <div className="py-8 text-center text-body text-muted-foreground">加载中…</div>
        ) : taskRows.length === 0 ? (
          <div className="rounded-md border border-dashed px-4 py-8 text-center text-body text-muted-foreground">暂无设计任务</div>
        ) : (
          <div className="divide-y rounded-md border">
            {taskRows.map((task) => <DesignTaskRow key={task.id} task={task} stopping={cancelTask.isPending && cancelTask.variables === task.id} onStop={() => cancelTask.mutate(task.id)} />)}
          </div>
        )}
      </section>
    </div>
  );
}

function DesignTaskRow({ task, stopping, onStop }: { task: DesignDocumentAgentTask; stopping: boolean; onStop: () => void }) {
  const active = ["queued", "dispatched", "running", "waiting_local_directory", "deferred"].includes(task.status);
  return (
    <article className="grid min-h-24 gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-body font-medium">{task.requirement}</span>
          <Badge variant="secondary">{taskStatusLabel(task.status)}</Badge>
        </div>
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-caption text-muted-foreground">
          <span>{task.project_title}</span>
          <span>{task.agent_name}</span>
          {task.issue_number ? <span>#{task.issue_number} {task.issue_title}</span> : null}
          {task.target_platform ? <span>{platformLabel(task.target_platform)}</span> : null}
        </div>
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-caption text-muted-foreground">
          <span className="inline-flex items-center gap-1"><Clock3 className="h-3.5 w-3.5" />创建 {formatTaskTime(task.created_at)}</span>
          <span>开始 {task.started_at ? formatTaskTime(task.started_at) : "未开始"}</span>
          <span>耗时 {task.started_at ? elapsedLabel(task.started_at, task.completed_at) : "—"}</span>
          <span>最近活动 {formatTaskTime(task.last_activity_at)}</span>
        </div>
        {task.failure_reason || task.error || task.wait_reason ? <p className="mt-2 text-caption text-muted-foreground">{task.failure_reason || task.error || task.wait_reason}</p> : null}
      </div>
      {active ? <Button size="sm" variant="outline" disabled={stopping} onClick={onStop}>{stopping ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CircleStop className="h-3.5 w-3.5" />}停止</Button> : null}
    </article>
  );
}

function taskStatusLabel(status: string) {
  const labels: Record<string, string> = { deferred: "等待中", queued: "排队中", dispatched: "已派发", running: "进行中", waiting_local_directory: "等待目录", completed: "已完成", failed: "失败", cancelled: "已停止" };
  return labels[status] ?? status;
}

function platformLabel(platform: string) {
  return ({ web: "Web", mobile: "移动端", cross_platform: "跨平台" } as Record<string, string>)[platform] ?? platform;
}

function formatTaskTime(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date);
}

function elapsedLabel(start: string, end?: string) {
  const startTime = new Date(start).getTime();
  const endTime = end ? new Date(end).getTime() : Date.now();
  if (!Number.isFinite(startTime) || !Number.isFinite(endTime) || endTime < startTime) return "—";
  const seconds = Math.floor((endTime - startTime) / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟`;
  return `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分钟`;
}
