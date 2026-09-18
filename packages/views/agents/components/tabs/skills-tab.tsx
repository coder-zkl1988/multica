"use client";

import { useEffect, useState } from "react";
import {
  Loader2,
  Plus,
  RefreshCw,
  RotateCcw,
  Server,
  Trash2,
} from "lucide-react";
import { SkillIcon } from "../../../skills/lib/skill-icon";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  DisabledRuntimeSkill,
  RuntimeLocalSkillSummary,
} from "@multica/core/types";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  isRuntimeUsableForUser,
  runtimeCapabilitiesOptions,
} from "@multica/core/runtimes";
import {
  skillDetailOptions,
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { SkillAddDialog } from "../skill-add-dialog";
import { useT } from "../../../i18n";

type SelectedSkill =
  | { kind: "workspace"; id: string }
  | { kind: "runtime"; skill: RuntimeLocalSkillSummary }
  | null;

export function SkillsTab({
  agent,
  runtime,
  currentUserId,
  canEdit = true,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  currentUserId?: string | null;
  canEdit?: boolean;
}) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const canReadRuntime =
    runtime != null && isRuntimeUsableForUser(runtime, currentUserId ?? null);
  const runtimeId =
    runtime?.runtime_mode === "local" &&
    runtime.status === "online" &&
    canReadRuntime
      ? runtime.id
      : null;
  const runtimeQuery = useQuery(runtimeCapabilitiesOptions(runtimeId));
  const [busyId, setBusyId] = useState<string | null>(null);
  const [bulkBusy, setBulkBusy] = useState(false);
  const [bulkFailed, setBulkFailed] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [selected, setSelected] = useState<SelectedSkill>(null);
  const selectedWorkspaceId = selected?.kind === "workspace" ? selected.id : "";
  const detailQuery = useQuery(skillDetailOptions(wsId, selectedWorkspaceId));

  useEffect(() => {
    setBulkFailed(false);
  }, [runtimeId]);

  const refreshAgent = async () => {
    await qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

  const handleRemove = async (skillId: string) => {
    setBusyId(skillId);
    try {
      await api.removeAgentSkill(agent.id, skillId);
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.remove_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  const handleToggle = async (skillId: string, enabled: boolean) => {
    setBusyId(skillId);
    try {
      await api.setAgentSkillEnabled(agent.id, skillId, enabled);
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.toggle_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  const handleRuntimeToggle = async (
    skill: RuntimeLocalSkillSummary,
    enabled: boolean,
  ) => {
    if (!runtime || !skill.root || bulkBusy) return;
    const busyKey = runtimeSkillIdentity(skill);
    setBusyId(busyKey);
    try {
      await api.setAgentRuntimeSkillEnabled(agent.id, {
        runtime_id: runtime.id,
        root: skill.root,
        key: skill.key,
        name: skill.name,
        plugin: skill.plugin,
        enabled,
      });
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.runtime_toggle_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  const runtimeSkills = runtimeQuery.data?.skills ?? [];
  const missingDisabledRuntimeSkills =
    runtimeQuery.isSuccess &&
    runtimeQuery.data.supported === true &&
    runtime != null
      ? (agent.disabled_runtime_skills ?? []).filter(
          (disabled) =>
            disabled.runtime_id === runtime.id &&
            disabled.provider === runtime.provider &&
            !runtimeSkills.some((skill) =>
              isRuntimeSkillDisabled([disabled], runtime.id, skill),
            ),
        )
      : [];
  const controllableRuntimeSkills = runtimeSkills.filter(
    (skill) => skill.can_disable === true && Boolean(skill.root),
  );
  const disabledRuntimeSkillCount = controllableRuntimeSkills.filter((skill) =>
    isRuntimeSkillDisabled(agent.disabled_runtime_skills, runtimeId ?? undefined, skill),
  ).length;
  const allRuntimeSkillsEnabled =
    controllableRuntimeSkills.length > 0 && disabledRuntimeSkillCount === 0;
  const allRuntimeSkillsDisabled =
    controllableRuntimeSkills.length > 0 &&
    disabledRuntimeSkillCount === controllableRuntimeSkills.length;

  const handleRuntimeBulkToggle = async (enabled: boolean) => {
    if (!runtime || bulkBusy) return;
    const targetSkills = controllableRuntimeSkills.filter(
      (skill) =>
        isRuntimeSkillDisabled(
          agent.disabled_runtime_skills,
          runtime.id,
          skill,
        ) === enabled,
    );
    setBulkBusy(true);
    setBulkFailed(false);
    let failed = false;
    let firstFailureMessage: string | undefined;
    for (const skill of targetSkills) {
      try {
        await api.setAgentRuntimeSkillEnabled(agent.id, {
          runtime_id: runtime.id,
          root: skill.root!,
          key: skill.key,
          name: skill.name,
          plugin: skill.plugin,
          enabled,
        });
      } catch (error) {
        failed = true;
        if (firstFailureMessage === undefined && error instanceof Error) {
          firstFailureMessage = error.message;
        }
      }
    }
    try {
      await refreshAgent();
    } catch (error) {
      failed = true;
      if (firstFailureMessage === undefined && error instanceof Error) {
        firstFailureMessage = error.message;
      }
    }
    setBulkFailed(failed);
    setBulkBusy(false);
    if (failed) {
      toast.error(
        firstFailureMessage ??
          t(($) => $.tab_body.skills.runtime_toggle_failed_toast),
      );
    }
  };

  const handleClearMissingRuntimeSkill = async (
    disabled: DisabledRuntimeSkill,
  ) => {
    if (
      !runtime ||
      disabled.runtime_id !== runtime.id ||
      disabled.provider !== runtime.provider ||
      bulkBusy
    ) {
      return;
    }
    const busyKey = missingRuntimeSkillIdentity(disabled);
    setBusyId(busyKey);
    try {
      await api.setAgentRuntimeSkillEnabled(agent.id, {
        runtime_id: disabled.runtime_id,
        root: disabled.root,
        key: disabled.key,
        name: disabled.name ?? disabled.key,
        plugin: disabled.plugin,
        enabled: true,
      });
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.runtime_clear_saved_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="space-y-8">

      <CapabilitySection
        title={t(($) => $.tab_body.skills.assigned_title)}
        description={t(($) => $.tab_body.skills.assigned_hint)}
        action={
          canEdit ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowAdd(true)}
              disabled={workspaceSkills.length === 0}
            >
              <Plus className="h-3.5 w-3.5" />
              {t(($) => $.tab_body.skills.add_action)}
            </Button>
          ) : null
        }
      >
        {agent.skills.length === 0 ? (
          <EmptyState
            icon={<SkillIcon className="h-6 w-6" />}
            title={t(($) => $.tab_body.skills.empty_title)}
          />
        ) : (
          <ul className="divide-y rounded-lg border bg-surface-raised/40">
            {agent.skills.map((skill) => {
              const enabled = skill.enabled !== false;
              const busy = busyId === skill.id;
              return (
                <li key={skill.id} className="flex items-center gap-3 p-3">
                  <button
                    type="button"
                    onClick={() => setSelected({ kind: "workspace", id: skill.id })}
                    className="flex min-w-0 flex-1 items-center gap-3 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span
                      className={cn(
                        "flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
                        !enabled && "text-faint-foreground",
                      )}
                    >
                      <SkillIcon className="h-4 w-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className={cn("block text-body font-medium", !enabled && "text-muted-foreground")}>
                        {skill.name}
                      </span>
                      <span className="block truncate text-caption text-muted-foreground">
                        {skill.description || t(($) => $.tab_body.skills.no_description)}
                      </span>
                    </span>
                  </button>
                  {canEdit && (
                    <>
                      {busy ? (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground motion-reduce:animate-none" />
                      ) : (
                        <Switch
                          checked={enabled}
                          onCheckedChange={(checked) => handleToggle(skill.id, checked)}
                          aria-label={t(($) => $.tab_body.skills.toggle_aria, {
                            name: skill.name,
                          })}
                        />
                      )}
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => handleRemove(skill.id)}
                        disabled={busyId !== null}
                        aria-label={t(($) => $.tab_body.skills.remove_aria, {
                          name: skill.name,
                        })}
                        className="text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CapabilitySection>

      <CapabilitySection
        title={t(($) => $.tab_body.skills.runtime_title)}
        action={
          runtimeId ? (
            <div className="flex items-center gap-3">
              {canEdit &&
                runtimeQuery.data?.supported === true &&
                controllableRuntimeSkills.length > 0 && (
                  <span className="flex items-center gap-2">
                    <Checkbox
                      checked={allRuntimeSkillsEnabled}
                      indeterminate={
                        !allRuntimeSkillsEnabled && !allRuntimeSkillsDisabled
                      }
                      onCheckedChange={(checked) =>
                        handleRuntimeBulkToggle(checked === true)
                      }
                      disabled={bulkBusy || busyId !== null}
                      aria-invalid={bulkFailed || undefined}
                      aria-label={t(
                        ($) => $.tab_body.skills.runtime_bulk_toggle_aria,
                      )}
                    />
                    {bulkBusy && (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground motion-reduce:animate-none" />
                    )}
                  </span>
                )}
              <Button
                variant="ghost"
                size="sm"
                onClick={() => runtimeQuery.refetch()}
                disabled={runtimeQuery.isFetching || bulkBusy}
              >
                <RefreshCw
                  className={cn(
                    "h-3.5 w-3.5",
                    runtimeQuery.isFetching && "animate-spin motion-reduce:animate-none",
                  )}
                />
                {t(($) => $.tab_body.skills.refresh_action)}
              </Button>
            </div>
          ) : null
        }
      >
        {!runtime ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_missing)} />
        ) : !canReadRuntime ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_forbidden)} />
        ) : runtime.status !== "online" ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_offline)} />
        ) : runtimeQuery.isLoading ? (
          <RuntimeNotice
            loading
            text={t(($) => $.tab_body.skills.runtime_discovering)}
          />
        ) : runtimeQuery.isError ? (
          <RuntimeNotice
            text={
              runtimeQuery.error instanceof ApiError &&
              runtimeQuery.error.status === 403
                ? t(($) => $.tab_body.skills.runtime_forbidden)
                : t(($) => $.tab_body.skills.runtime_failed)
            }
          />
        ) : runtimeQuery.data?.supported !== true ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_unsupported)} />
        ) : runtimeSkills.length === 0 && missingDisabledRuntimeSkills.length === 0 ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_empty)} />
        ) : (
          <ul className="divide-y rounded-lg border bg-surface-raised/40">
            {runtimeSkills.map((skill) => {
              const savedDisabled = (agent.disabled_runtime_skills ?? []).find(
                (disabled) =>
                  disabled.runtime_id === runtime?.id &&
                  disabled.provider === runtime?.provider &&
                  isRuntimeSkillDisabled([disabled], runtime?.id, skill),
              );
              const disabled = isRuntimeSkillDisabled(
                agent.disabled_runtime_skills,
                runtime?.id,
                skill,
              );
              const busyKey = runtimeSkillIdentity(skill);
              const busy = busyId === busyKey;
              return (
                <li
                  key={`${skill.root ?? "unknown"}:${skill.key}`}
                  className="flex items-center gap-3 p-3"
                >
                  <button
                    type="button"
                    onClick={() => setSelected({ kind: "runtime", skill })}
                    className="flex min-w-0 flex-1 items-center gap-3 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span
                      className={cn(
                        "flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
                        disabled && "opacity-50",
                      )}
                    >
                      <Server className="h-4 w-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span
                        className={cn(
                          "block text-body font-medium",
                          disabled && "text-muted-foreground",
                        )}
                      >
                        {skill.name}
                      </span>
                      <span className="block truncate text-caption text-muted-foreground">
                        {skill.description || skill.source_path}
                      </span>
                    </span>
                  </button>
                  {canEdit && skill.root && skill.can_disable === true &&
                    (busy ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground motion-reduce:animate-none" />
                    ) : (
                      <Switch
                        checked={!disabled}
                        disabled={bulkBusy}
                        onCheckedChange={(checked) =>
                          handleRuntimeToggle(skill, checked)
                        }
                        aria-label={t(
                          ($) => $.tab_body.skills.runtime_toggle_aria,
                          { name: skill.name },
                        )}
                      />
                    ))}
                  {canEdit &&
                    skill.can_disable !== true &&
                    savedDisabled && (
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() =>
                                handleClearMissingRuntimeSkill(savedDisabled)
                              }
                              disabled={busyId !== null || bulkBusy}
                              aria-label={t(
                                ($) => $.tab_body.skills.runtime_clear_saved_aria,
                                { name: savedDisabled.name ?? skill.name },
                              )}
                            >
                              {busyId === missingRuntimeSkillIdentity(savedDisabled) ? (
                                <Loader2 className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />
                              ) : (
                                <RotateCcw className="h-3.5 w-3.5" />
                              )}
                            </Button>
                          }
                        />
                        <TooltipContent>
                          {t(
                            ($) => $.tab_body.skills.runtime_clear_saved_aria,
                            { name: savedDisabled.name ?? skill.name },
                          )}
                        </TooltipContent>
                      </Tooltip>
                    )}
                </li>
              );
            })}
            {missingDisabledRuntimeSkills.map((disabled) => {
              const name = disabled.name ?? disabled.key;
              const busyKey = missingRuntimeSkillIdentity(disabled);
              const busy = busyId === busyKey;
              const clearLabel = t(
                ($) => $.tab_body.skills.runtime_clear_saved_aria,
                { name },
              );
              return (
                <li
                  key={busyKey}
                  className="flex items-center gap-3 p-3"
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                    <Server className="h-4 w-4" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-body font-medium">{name}</span>
                    <span className="mt-1 flex flex-wrap items-center gap-1.5">
                      <Badge variant="outline">
                        {t(($) => $.tab_body.skills.runtime_saved_badge)}
                      </Badge>
                      <span className="text-caption text-muted-foreground">
                        {t(($) => $.tab_body.skills.runtime_not_found_badge)}
                      </span>
                    </span>
                  </span>
                  {canEdit && (
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() =>
                              handleClearMissingRuntimeSkill(disabled)
                            }
                            disabled={busyId !== null || bulkBusy}
                            aria-label={clearLabel}
                          >
                            {busy ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />
                            ) : (
                              <RotateCcw className="h-3.5 w-3.5" />
                            )}
                          </Button>
                        }
                      />
                      <TooltipContent>{clearLabel}</TooltipContent>
                    </Tooltip>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CapabilitySection>

      <SkillAddDialog agent={agent} open={showAdd} onOpenChange={setShowAdd} />
      <SkillDetailDialog
        selected={selected}
        onOpenChange={(open) => !open && setSelected(null)}
        workspaceSkill={detailQuery.data}
        loading={selected?.kind === "workspace" && detailQuery.isLoading}
      />
    </div>
  );
}

function runtimeSkillIdentity(skill: RuntimeLocalSkillSummary): string {
  return `runtime:${skill.root ?? "unknown"}:${skill.key}:${skill.plugin ?? ""}`;
}

function missingRuntimeSkillIdentity(skill: DisabledRuntimeSkill): string {
  return `missing-runtime:${skill.runtime_id}:${skill.provider}:${skill.root}:${skill.key}:${skill.plugin ?? ""}`;
}

function isRuntimeSkillDisabled(
  disabledSkills: DisabledRuntimeSkill[] | undefined,
  runtimeId: string | undefined,
  skill: RuntimeLocalSkillSummary,
): boolean {
  if (!runtimeId || !skill.root) return false;
  return (disabledSkills ?? []).some(
    (disabled) =>
      disabled.runtime_id === runtimeId &&
      disabled.provider === skill.provider &&
      disabled.root === skill.root &&
      disabled.key === skill.key &&
      (disabled.plugin ?? "") === (skill.plugin ?? ""),
  );
}

function CapabilitySection({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-body font-medium">{title}</h3>
          {description && (
            <p className="mt-1 text-caption leading-5 text-muted-foreground">
              {description}
            </p>
          )}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function EmptyState({ icon, title, hint }: { icon: React.ReactNode; title: string; hint?: string }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10 text-muted-foreground">
      <span className="opacity-50">{icon}</span>
      <p className="mt-3 text-body">{title}</p>
      {hint && <p className="mt-1 max-w-sm text-center text-caption">{hint}</p>}
    </div>
  );
}

function RuntimeNotice({ text, loading = false }: { text: string; loading?: boolean }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-dashed px-4 py-6 text-caption text-muted-foreground">
      {loading ? (
        <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" />
      ) : (
        <Server className="h-4 w-4" />
      )}
      {text}
    </div>
  );
}

function SkillDetailDialog({
  selected,
  onOpenChange,
  workspaceSkill,
  loading,
}: {
  selected: SelectedSkill;
  onOpenChange: (open: boolean) => void;
  workspaceSkill?: Awaited<ReturnType<typeof api.getSkill>>;
  loading: boolean;
}) {
  const { t } = useT("agents");
  const runtimeSkill = selected?.kind === "runtime" ? selected.skill : null;
  const title = runtimeSkill?.name || workspaceSkill?.name || t(($) => $.tab_body.skills.detail_title);
  const description = runtimeSkill?.description || workspaceSkill?.description;

  return (
    <Dialog open={selected !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <div className="flex items-center gap-2 pr-8">
            <DialogTitle>{title}</DialogTitle>
            <Badge variant={runtimeSkill ? "secondary" : "outline"}>
              {runtimeSkill
                ? t(($) => $.tab_body.skills.inherited_badge)
                : t(($) => $.tab_body.skills.workspace_badge)}
            </Badge>
          </div>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        {loading ? (
          <div className="flex items-center gap-2 py-8 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" />
            {t(($) => $.tab_body.skills.detail_loading)}
          </div>
        ) : runtimeSkill ? (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 rounded-lg border p-4 text-caption">
            <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_source)}</dt>
            <dd className="break-all">{runtimeSkill.source_path}</dd>
            <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_provider)}</dt>
            <dd>{runtimeSkill.provider}</dd>
            {runtimeSkill.plugin && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_plugin)}</dt>
                <dd>{runtimeSkill.plugin}</dd>
              </>
            )}
            <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_files)}</dt>
            <dd>
              {runtimeSkill.can_import === false
                ? t(($) => $.tab_body.skills.detail_files_unavailable)
                : runtimeSkill.file_count}
            </dd>
          </dl>
        ) : workspaceSkill ? (
          <div className="space-y-4">
            <div className="max-h-96 overflow-auto rounded-lg border bg-muted/30 p-4">
              <pre className="whitespace-pre-wrap break-words font-mono text-caption leading-5">
                {workspaceSkill.content}
              </pre>
            </div>
            {(workspaceSkill.files ?? []).length > 0 && (
              <div>
                <h4 className="text-caption font-medium">{t(($) => $.tab_body.skills.detail_supporting_files)}</h4>
                <div className="mt-2 flex flex-wrap gap-2">
                  {(workspaceSkill.files ?? []).map((file) => (
                    <Badge key={file.id} variant="outline">{file.path}</Badge>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
