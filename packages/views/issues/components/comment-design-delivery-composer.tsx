"use client";

import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { projectResourcesOptions } from "@multica/core/projects";
import { projectDesignSystemCatalogueOptions } from "@multica/core/designs/queries";
import type { Agent, CommentDesignRequest, Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { AgentSetting, DesignSystemSetting, RepositorySetting } from "../../designs/design-task-composer";
import { useT } from "../../i18n";
import { IssueDesignRestoreSection } from "./issue-design-restore-section";

export function CommentDesignDeliveryComposer({ issue, agents, request, disabled, onChange, onPrepared, onValidityChange }: {
  issue: Issue;
  agents: Agent[];
  request: CommentDesignRequest;
  disabled: boolean;
  onChange: (request: CommentDesignRequest | undefined) => void;
  onPrepared: (prompt: string, request: CommentDesignRequest, sourceRequestId: string) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const resourcesQuery = useQuery({ ...projectResourcesOptions(wsId, issue.project_id ?? ""), enabled: !!issue.project_id });
  const systemsQuery = useQuery({ ...projectDesignSystemCatalogueOptions(wsId), enabled: request.operation === "design" });
  const repositories = useMemo(() => (resourcesQuery.data ?? []).filter((resource) => resource.resource_type === "github_repo"), [resourcesQuery.data]);
  const selectedAgent = agents.find((agent) => agent.id === request.agent_id && !agent.archived_at && isAgentRuntimeBound(agent));
  const valid = !!issue.project_id && !!selectedAgent && repositories.some((resource) => resource.id === request.project_resource_id)
    && (request.operation === "implement"
      ? !!request.design_ref && !!request.revision_id && !!request.frame_refs?.length
      : !request.design_system_id || (systemsQuery.data ?? []).some((system) => system.id === request.design_system_id));
  useEffect(() => onValidityChange(valid), [onValidityChange, valid]);
  const change = (patch: Partial<CommentDesignRequest>) => onChange({
    ...request,
    ...patch,
    request_id: crypto.randomUUID(),
    ...(request.operation === "implement" ? { revision_id: undefined } : {}),
  });

  return (
    <fieldset disabled={disabled} className="m-2 min-w-0 space-y-3 rounded-md border bg-muted/20 p-3" aria-label={t(($) => $.design_delivery.title)}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="mr-auto text-caption font-medium">{t(($) => $.design_delivery.title)}</span>
        <Button type="button" variant={request.operation === "design" ? "secondary" : "ghost"} size="sm" aria-pressed={request.operation === "design"}
          onClick={() => onChange({ request_id: crypto.randomUUID(), operation: "design", agent_id: request.agent_id, project_resource_id: request.project_resource_id })}>{t(($) => $.design_delivery.design)}</Button>
        <Button type="button" variant={request.operation === "implement" ? "secondary" : "ghost"} size="sm" aria-pressed={request.operation === "implement"}
          onClick={() => onChange({ request_id: crypto.randomUUID(), operation: "implement", agent_id: request.agent_id, project_resource_id: request.project_resource_id })}>{t(($) => $.design_delivery.implement)}</Button>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={t(($) => $.design_delivery.remove)} onClick={() => onChange(undefined)}><X className="size-3.5" /></Button>
      </div>
      <div className="flex flex-wrap gap-2">
        <RepositorySetting repositories={repositories} repositoryId={request.project_resource_id} disabled={!issue.project_id || disabled} required onChange={(id) => change({ project_resource_id: id })} />
        <AgentSetting agents={agents} agentId={request.agent_id} onChange={(id) => change({ agent_id: id })} />
        {request.operation === "design" ? <DesignSystemSetting workspaceSystems={systemsQuery.data ?? []} builtinSystems={[]} designSystemId={request.design_system_id ?? ""} builtinSlug=""
          onChange={({ designSystemId }) => change({ design_system_id: designSystemId || undefined })} /> : null}
      </div>
      {!issue.project_id ? <p className="text-caption text-muted-foreground">{t(($) => $.design_delivery.project_required)}</p> : null}
      {resourcesQuery.isError || systemsQuery.isError ? <p role="alert" className="text-caption text-destructive">{t(($) => $.design_delivery.settings_failed)}</p> : null}
      {request.operation === "design" ? <p className="text-caption text-muted-foreground">{t(($) => $.design_delivery.design_hint)}</p>
        : <IssueDesignRestoreSection issue={issue} request={request} onChange={onChange} onPrepared={onPrepared} />}
      <p className="text-caption text-muted-foreground">{t(($) => $.design_delivery.send_hint)}</p>
    </fieldset>
  );
}
