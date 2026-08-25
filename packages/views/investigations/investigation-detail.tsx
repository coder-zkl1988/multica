"use client";

import { useState } from "react";
import { ArrowLeft, Check, FileImage, FlaskConical, FolderPlus, ImagePlus, Link2, Loader2, MessageSquare, RefreshCw, Server, Star, UserRoundCog, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { investigationDetailOptions, useAddInvestigationComment, useChangeInvestigationAgent, useConfirmInvestigation, useCreateInvestigationProject, useInvestigationFeedback, useLinkInvestigationProject, useRetryInvestigation } from "@multica/core/investigations";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../navigation";
import { useT } from "../i18n";

type FeedbackCheckpoint = "diagnosis_confirmed" | "project_converted";

function FeedbackPanel({ investigationId, checkpoint }: { investigationId: string; checkpoint: FeedbackCheckpoint }) {
  const { t } = useT("investigations");
  const feedback = useInvestigationFeedback();
  const [score, setScore] = useState(0);
  const [attribution, setAttribution] = useState("");
  const [comment, setComment] = useState("");
  const chooseScore = (value: number) => {
    setScore(value);
    if (value > 3) feedback.mutate({ id: investigationId, checkpoint, score: value });
  };
  return <div className="space-y-3 border-b py-4">
    <div className="flex flex-wrap items-center gap-3">
      <span className="text-sm text-muted-foreground">{checkpoint === "diagnosis_confirmed" ? t(($) => $.diagnosis_feedback) : t(($) => $.project_feedback)}</span>
      <div className="flex gap-1">{[1, 2, 3, 4, 5].map((value) => <button type="button" key={value} aria-label={t(($) => $.score, { value })} className="flex size-8 items-center justify-center rounded border hover:bg-muted" onClick={() => chooseScore(value)}><Star className={`size-4 ${score >= value ? "fill-amber-400 text-amber-500" : "text-muted-foreground"}`} /></button>)}</div>
    </div>
    {score > 0 && score <= 3 ? <div className="flex flex-wrap gap-2">
      <select className="h-9 rounded-md border bg-background px-2 text-sm" value={attribution} onChange={(event) => setAttribution(event.target.value)}><option value="">{t(($) => $.attribution_optional)}</option><option value="capability">{t(($) => $.attribution_capability)}</option><option value="platform">{t(($) => $.attribution_platform)}</option><option value="both">{t(($) => $.attribution_both)}</option><option value="uncertain">{t(($) => $.attribution_uncertain)}</option></select>
      <input className="h-9 min-w-52 flex-1 rounded-md border bg-background px-3 text-sm" value={comment} onChange={(event) => setComment(event.target.value)} placeholder={t(($) => $.feedback_comment)} />
      <Button size="sm" disabled={feedback.isPending} onClick={() => feedback.mutate({ id: investigationId, checkpoint, score, attribution: attribution || undefined, comment: comment || undefined })}>{t(($) => $.save_feedback)}</Button>
    </div> : null}
  </div>;
}

export function InvestigationDetail({ investigationId }: { investigationId: string }) {
  const { t } = useT("investigations");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const user = useAuthStore((state) => state.user);
  const { data, isPending } = useQuery(investigationDetailOptions(wsId, investigationId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const addComment = useAddInvestigationComment();
  const confirm = useConfirmInvestigation();
  const retry = useRetryInvestigation();
  const changeAgent = useChangeInvestigationAgent();
  const createProject = useCreateInvestigationProject();
  const linkProject = useLinkInvestigationProject();
  const [comment, setComment] = useState("");
  const [projectId, setProjectId] = useState("");
  const [agentId, setAgentId] = useState("");
  const [commentAttachments, setCommentAttachments] = useState<{ id: string; filename: string }[]>([]);
  const { upload, uploading } = useFileUpload(api);
  if (isPending || !data) return <div className="p-6 text-sm text-muted-foreground">{t(($) => $.loading)}</div>;

  const role = members.find((member) => member.user_id === user?.id)?.role;
  const canManage = data.created_by === user?.id || role === "owner" || role === "admin";
  const canChangeAgent = canManage && data.status !== "completed" && (data.status === "needs_input" || data.tasks[0]?.status === "failed");
  const availableAgents = agents.filter((agent) => !agent.archived_at && agent.runtime_id);
  const addImages = async (files: FileList | null) => {
    const images = Array.from(files ?? []).filter((file) => file.type.startsWith("image/"));
    const uploaded = await Promise.all(images.map((file) => upload(file)));
    setCommentAttachments((current) => [...current, ...uploaded.filter((item): item is NonNullable<typeof item> => item !== null).map((item) => ({ id: item.id, filename: item.filename }))]);
  };

  return <div className="h-full overflow-auto">
    <header className="sticky top-0 z-10 flex min-h-12 items-center justify-between border-b bg-background/95 px-5 backdrop-blur">
      <AppLink href={p.investigations()} className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />{t(($) => $.back)}</AppLink>
      <div className="flex items-center gap-2">{canManage && data.status === "awaiting_confirmation" ? <Button size="sm" onClick={() => confirm.mutate(data.id)}><Check className="size-4" />{t(($) => $.confirm)}</Button> : null}{canManage && data.status !== "completed" && data.tasks[0]?.status === "failed" ? <Button size="sm" variant="outline" onClick={() => retry.mutate(data.id)}><RefreshCw className="size-4" />{t(($) => $.retry)}</Button> : null}</div>
    </header>
    <main className="mx-auto w-full max-w-5xl px-5 py-6">
      <div className="flex flex-wrap items-start justify-between gap-4 border-b pb-6"><div className="min-w-0"><h1 className="text-xl font-semibold">{data.title}</h1><p className="mt-2 max-w-3xl whitespace-pre-wrap text-sm leading-6 text-muted-foreground">{data.description}</p></div><span className="flex items-center gap-1.5 rounded border px-2 py-1 text-xs">{data.environment === "production" ? <Server className="size-3.5" /> : <FlaskConical className="size-3.5" />}{t(($) => $[data.environment])}</span></div>
      {data.attachments.length > 0 ? <section className="border-b py-5"><h2 className="mb-3 flex items-center gap-2 text-sm font-semibold"><FileImage className="size-4" />{t(($) => $.images)}</h2><div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">{data.attachments.map((attachment) => <a key={attachment.id} href={attachment.download_url} target="_blank" rel="noreferrer" className="overflow-hidden rounded border bg-muted/30"><img className="aspect-video w-full object-cover" src={attachment.download_url} alt={attachment.filename} /><span className="block truncate px-2 py-1.5 text-xs">{attachment.filename}</span></a>)}</div></section> : null}
      {(data.root_cause || data.status === "awaiting_confirmation" || data.status === "completed") ? <section className="grid gap-6 border-b py-6 md:grid-cols-2"><div><h2 className="text-sm font-semibold">{t(($) => $.root_cause)}</h2><p className="mt-2 whitespace-pre-wrap text-sm leading-6">{data.root_cause ?? "-"}</p></div><div><h2 className="text-sm font-semibold">{t(($) => $.recommendations)}</h2><ul className="mt-2 space-y-2 text-sm">{data.recommendations.map((item) => <li key={item}>{item}</li>)}</ul></div><div><h2 className="text-sm font-semibold">{t(($) => $.evidence)}</h2><pre className="mt-2 overflow-auto whitespace-pre-wrap text-xs text-muted-foreground">{JSON.stringify(data.evidence, null, 2)}</pre></div><div><h2 className="text-sm font-semibold">{t(($) => $.open_questions)}</h2><ul className="mt-2 space-y-2 text-sm">{data.open_questions.map((item) => <li key={item}>{item}</li>)}</ul></div></section> : null}
      {canChangeAgent ? <section className="flex flex-wrap items-center gap-2 border-b py-4"><UserRoundCog className="size-4 text-muted-foreground" /><select className="h-9 min-w-52 rounded-md border bg-background px-2 text-sm" value={agentId || data.agent_id} onChange={(event) => setAgentId(event.target.value)}>{availableAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select><Button size="sm" variant="outline" disabled={!agentId || agentId === data.agent_id || changeAgent.isPending} onClick={() => changeAgent.mutate({ id: data.id, agentId })}>{t(($) => $.change_agent)}</Button></section> : null}
      {canManage && data.status === "completed" && !data.project_id ? <section className="flex flex-wrap items-center gap-2 border-b py-4"><Button variant="outline" onClick={() => createProject.mutate({ id: data.id })}><FolderPlus className="size-4" />{t(($) => $.convert)}</Button><select className="h-9 min-w-48 rounded-md border bg-background px-2 text-sm" value={projectId} onChange={(event) => setProjectId(event.target.value)}><option value="">{t(($) => $.link)}</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}</select><Button size="sm" variant="ghost" disabled={!projectId} onClick={() => linkProject.mutate({ id: data.id, projectId })}><Link2 className="size-4" />{t(($) => $.link)}</Button></section> : null}
      {data.project_id ? <section className="border-b py-4"><AppLink href={p.projectDetail(data.project_id)} className="inline-flex items-center gap-2 text-sm font-medium hover:underline"><Link2 className="size-4" />{t(($) => $.linked_project)}</AppLink></section> : null}
      {data.confirmed_at ? <FeedbackPanel investigationId={data.id} checkpoint="diagnosis_confirmed" /> : null}
      {data.converted_at ? <FeedbackPanel investigationId={data.id} checkpoint="project_converted" /> : null}
      <section className="grid gap-8 py-6 lg:grid-cols-[minmax(0,1fr)_16rem]">
        <div><h2 className="mb-3 flex items-center gap-2 text-sm font-semibold"><MessageSquare className="size-4" />{t(($) => $.activity)}</h2><div className="space-y-4">{data.comments.map((entry) => <div key={entry.id} className="border-l-2 pl-3"><div className="text-xs text-muted-foreground">{entry.author_type} · {new Date(entry.created_at).toLocaleString()}</div><p className="mt-1 whitespace-pre-wrap text-sm leading-6">{entry.content}</p></div>)}</div>
          <div className="mt-5 space-y-2"><textarea aria-label={t(($) => $.reply)} className="min-h-20 w-full resize-y rounded-md border bg-background p-3 text-sm" placeholder={t(($) => $.reply)} value={comment} onChange={(event) => setComment(event.target.value)} /><div className="flex flex-wrap items-center gap-2">{commentAttachments.map((item) => <span key={item.id} className="inline-flex max-w-48 items-center gap-1 rounded border px-2 py-1 text-xs"><span className="truncate">{item.filename}</span><button type="button" aria-label={t(($) => $.remove_image)} onClick={() => setCommentAttachments((current) => current.filter((entry) => entry.id !== item.id))}><X className="size-3" /></button></span>)}<label className="inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-md border px-3 text-sm hover:bg-muted">{uploading ? <Loader2 className="size-4 animate-spin" /> : <ImagePlus className="size-4" />}{t(($) => uploading ? $.uploading : $.add_images)}<input className="sr-only" type="file" accept="image/*" multiple onChange={(event) => { void addImages(event.target.files); event.currentTarget.value = ""; }} /></label><Button className="ml-auto" disabled={!comment.trim() || uploading || addComment.isPending} onClick={() => addComment.mutate({ id: data.id, content: comment, attachmentIds: commentAttachments.map((item) => item.id) }, { onSuccess: () => { setComment(""); setCommentAttachments([]); } })}>{t(($) => $.send)}</Button></div></div>
        </div>
        <aside><h2 className="mb-3 text-sm font-semibold">{t(($) => $.runs)}</h2><div className="space-y-2">{data.tasks.map((task) => <div key={task.id} className="rounded border p-3 text-xs"><div className="flex justify-between"><span className="font-medium">{task.status}</span><span className="text-muted-foreground">#{task.attempt}</span></div>{task.failure_reason ? <p className="mt-2 text-destructive">{task.failure_reason}</p> : null}</div>)}</div></aside>
      </section>
    </main>
  </div>;
}
