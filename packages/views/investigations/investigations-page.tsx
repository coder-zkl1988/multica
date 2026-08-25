"use client";

import { useMemo, useState } from "react";
import { SearchCheck, Plus, Server, FlaskConical, ChevronRight, ImagePlus, Loader2, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { investigationListOptions, useCreateInvestigation, type InvestigationEnvironment } from "@multica/core/investigations";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { AppLink } from "../navigation";
import { CollectionPageHeader, CollectionPageHeaderAction, CollectionPageState } from "../layout/collection-page";
import { useT } from "../i18n";

const statusTone = {
  investigating: "bg-blue-500/10 text-blue-700 dark:text-blue-300",
  needs_input: "bg-amber-500/12 text-amber-700 dark:text-amber-300",
  awaiting_confirmation: "bg-violet-500/10 text-violet-700 dark:text-violet-300",
  completed: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
} as const;

export function InvestigationsPage() {
  const { t } = useT("investigations");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data = [], isPending } = useQuery(investigationListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const create = useCreateInvestigation();
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState("all");
  const [environment, setEnvironment] = useState("all");
  const [description, setDescription] = useState("");
  const [agentId, setAgentId] = useState("");
  const [attachments, setAttachments] = useState<{ id: string; filename: string }[]>([]);
  const [selectedEnvironment, setSelectedEnvironment] = useState<InvestigationEnvironment>("production");
  const { upload, uploading } = useFileUpload(api);
  const visible = useMemo(() => data.filter((row) => (status === "all" || row.status === status) && (environment === "all" || row.environment === environment)), [data, environment, status]);

  const submit = () => create.mutate({ description, environment: selectedEnvironment, agent_id: agentId, attachment_ids: attachments.map((item) => item.id) }, {
    onSuccess: () => { setOpen(false); setDescription(""); setAgentId(""); setAttachments([]); },
  });

  const addImages = async (files: FileList | null) => {
    const images = Array.from(files ?? []).filter((file) => file.type.startsWith("image/"));
    const uploaded = await Promise.all(images.map((file) => upload(file)));
    setAttachments((current) => [...current, ...uploaded.filter((item): item is NonNullable<typeof item> => item !== null).map((item) => ({ id: item.id, filename: item.filename }))]);
  };

  return <div className="flex h-full min-h-0 flex-col">
    <CollectionPageHeader icon={SearchCheck} title={t(($) => $.title)} count={data.length} actions={<CollectionPageHeaderAction icon={Plus} label={t(($) => $.new)} onClick={() => setOpen(true)} />} />
    <div className="flex items-center gap-2 border-b px-5 py-2">
      <select aria-label="status" className="h-8 rounded-md border bg-background px-2 text-sm" value={status} onChange={(event) => setStatus(event.target.value)}>
        <option value="all">{t(($) => $.all_status)}</option><option value="investigating">{t(($) => $.investigating)}</option><option value="needs_input">{t(($) => $.needs_input)}</option><option value="awaiting_confirmation">{t(($) => $.awaiting_confirmation)}</option><option value="completed">{t(($) => $.completed)}</option>
      </select>
      <select aria-label={t(($) => $.environment)} className="h-8 rounded-md border bg-background px-2 text-sm" value={environment} onChange={(event) => setEnvironment(event.target.value)}>
        <option value="all">{t(($) => $.environment)}</option><option value="production">{t(($) => $.production)}</option><option value="test">{t(($) => $.test)}</option>
      </select>
    </div>
    {isPending ? <div className="p-6 text-sm text-muted-foreground">{t(($) => $.loading)}</div> : visible.length === 0 ? <CollectionPageState icon={SearchCheck} title={t(($) => $.empty)} /> : <div className="min-h-0 flex-1 overflow-auto">
      <div className="divide-y">
        {visible.map((row) => <AppLink key={row.id} href={p.investigationDetail(row.id)} className="group grid min-h-16 grid-cols-[minmax(0,1fr)_auto_auto_1rem] items-center gap-4 px-5 py-3 hover:bg-muted/40">
          <div className="min-w-0"><div className="truncate text-sm font-medium">{row.title}</div><div className="mt-0.5 truncate text-xs text-muted-foreground">{row.description}</div></div>
          <span className={`rounded px-2 py-1 text-xs ${statusTone[row.status]}`}>{t(($) => $[row.status])}</span>
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">{row.environment === "production" ? <Server className="size-3.5" /> : <FlaskConical className="size-3.5" />}{t(($) => $[row.environment])}</span>
          <ChevronRight className="size-4 text-muted-foreground opacity-0 group-hover:opacity-100" />
        </AppLink>)}
      </div>
    </div>}
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="sm:max-w-lg"><DialogHeader><DialogTitle>{t(($) => $.new)}</DialogTitle></DialogHeader>
      <div className="space-y-4">
        <div><label className="mb-1.5 block text-sm font-medium">{t(($) => $.environment)}</label><div className="grid grid-cols-2 gap-1 rounded-md bg-muted p-1">{(["production", "test"] as const).map((value) => <button key={value} type="button" onClick={() => setSelectedEnvironment(value)} className={`flex h-9 items-center justify-center gap-2 rounded text-sm ${selectedEnvironment === value ? "bg-background shadow-sm" : "text-muted-foreground"}`}>{value === "production" ? <Server className="size-4" /> : <FlaskConical className="size-4" />}{t(($) => $[value])}</button>)}</div></div>
        <div><label className="mb-1.5 block text-sm font-medium">{t(($) => $.description)}</label><textarea className="min-h-32 w-full resize-y rounded-md border bg-background p-3 text-sm outline-none focus:ring-2 focus:ring-ring" value={description} onChange={(event) => setDescription(event.target.value)} /></div>
        <div><label className="mb-1.5 block text-sm font-medium">{t(($) => $.images)}</label><div className="flex flex-wrap items-center gap-2">{attachments.map((item) => <span key={item.id} className="inline-flex max-w-full items-center gap-1 rounded border px-2 py-1 text-xs"><span className="truncate">{item.filename}</span><button type="button" aria-label={t(($) => $.remove_image)} onClick={() => setAttachments((current) => current.filter((entry) => entry.id !== item.id))}><X className="size-3" /></button></span>)}<label className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md border px-2 text-xs hover:bg-muted">{uploading ? <Loader2 className="size-3.5 animate-spin" /> : <ImagePlus className="size-3.5" />}{t(($) => uploading ? $.uploading : $.add_images)}<input className="sr-only" type="file" accept="image/*" multiple onChange={(event) => { void addImages(event.target.files); event.currentTarget.value = ""; }} /></label></div></div>
        <div><label className="mb-1.5 block text-sm font-medium">{t(($) => $.agent)}</label><select className="h-10 w-full rounded-md border bg-background px-3 text-sm" value={agentId} onChange={(event) => setAgentId(event.target.value)}><option value="" disabled>{t(($) => $.agent)}</option>{agents.filter((agent) => !agent.archived_at && agent.runtime_id).map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select></div>
      </div>
      <DialogFooter><Button variant="ghost" onClick={() => setOpen(false)}>{t(($) => $.cancel)}</Button><Button disabled={!description.trim() || !agentId || create.isPending || uploading} onClick={submit}>{t(($) => $.start)}</Button></DialogFooter>
    </DialogContent></Dialog>
  </div>;
}
