export type DesignDocumentProvenanceScope = "repository" | "project" | "workspace" | "builtin" | "none";

export interface DesignDocumentSystemProvenance {
  source: string;
  scope: DesignDocumentProvenanceScope;
  projectId: string;
  repositoryId: string | null;
  systemId: string;
  packageId: string | null;
  name: string;
  digest: string;
}

export interface DesignDocumentReplayRequest {
  agentId: string;
  repositoryId: string;
  issueId: string;
  designSystemId: string;
  builtinDesignSystem: string;
  platform: string;
  recipe: string;
  brief: string;
  attachments: ReadonlyArray<{ attachmentId: string }>;
}

export interface DesignDocumentProvenance {
  valid: boolean;
  system: DesignDocumentSystemProvenance | null;
  associatedRepositoryId: string | null;
  repositoryGrounded: boolean;
  request: DesignDocumentReplayRequest | null;
}

interface DesignDocumentProvenanceInput {
  inputSnapshot: unknown;
  repositoryGrounded: boolean;
}

function object(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function string(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function nonEmptyString(value: unknown): string | null {
  const result = string(value);
  return result && result.trim() ? result : null;
}

function attachments(value: unknown): Array<{ attachmentId: string }> | null {
  if (!Array.isArray(value)) return [];
  const result: Array<{ attachmentId: string }> = [];
  for (const item of value) {
    const record = object(item);
    const attachmentId = string(record?.attachment_id);
    if (!attachmentId) return null;
    result.push({ attachmentId });
  }
  return result;
}

function systemProvenance(context: Record<string, unknown>): DesignDocumentSystemProvenance | null {
  const source = nonEmptyString(context.source) ?? "unknown";
  const projectId = string(context.project_id) ?? "";
  const digest = string(context.digest) ?? "";

  if (source === "none") {
    return { source, scope: "none", projectId, repositoryId: null, systemId: "", packageId: null, name: "", digest };
  }

  if (source === "builtin_catalogue_design_system") {
    const builtin = object(context.builtin);
    if (!builtin) return null;
    const slug = nonEmptyString(builtin.slug);
    const name = nonEmptyString(builtin.name) ?? slug;
    if (!slug || !name) return null;
    return { source, scope: "builtin", projectId, repositoryId: null, systemId: slug, packageId: null, name, digest };
  }

  const saved = object(context.package);
  if (!saved) return null;
  const rawScope = string(saved.scope);
  const scope = rawScope === "repository" || rawScope === "project" || rawScope === "workspace" ? rawScope : null;
  const systemId = nonEmptyString(saved.design_system_id);
  const packageId = nonEmptyString(saved.saved_package_id);
  const name = nonEmptyString(saved.name);
  if (!scope || !systemId || !packageId || !name) return null;
  const repositoryId = scope === "repository" ? nonEmptyString(saved.project_resource_id) : null;
  if (scope === "repository" && !repositoryId) return null;
  if (!digest) return null;

  return { source, scope, projectId, repositoryId, systemId, packageId, name, digest };
}

function replayRequest(snapshot: Record<string, unknown>): DesignDocumentReplayRequest | null {
  const agentId = nonEmptyString(snapshot.agent_id);
  const brief = nonEmptyString(snapshot.brief);
  const platform = nonEmptyString(snapshot.platform);
  if (!agentId || !brief || !platform) return null;
  const frozenAttachments = attachments(snapshot.attachments);
  if (!frozenAttachments) return null;
  return {
    agentId,
    repositoryId: string(snapshot.project_resource_id) ?? "",
    issueId: string(snapshot.issue_id) ?? "",
    designSystemId: string(snapshot.design_system_id) ?? "",
    builtinDesignSystem: string(snapshot.builtin_design_system) ?? "",
    platform,
    recipe: string(snapshot.recipe) ?? "",
    brief,
    attachments: frozenAttachments,
  };
}

/** Parses the server-frozen provenance defensively. Unknown or malformed data yields a bounded state. */
export function parseDesignDocumentProvenance({ inputSnapshot, repositoryGrounded }: DesignDocumentProvenanceInput): DesignDocumentProvenance {
  const snapshot = object(inputSnapshot);
  const context = object(snapshot?.resolved_design_context);
  const system = context ? systemProvenance(context) : null;
  return {
    valid: !!system,
    system,
    associatedRepositoryId: system?.repositoryId ?? (nonEmptyString(snapshot?.project_resource_id) ?? null),
    repositoryGrounded: repositoryGrounded === true,
    request: snapshot ? replayRequest(snapshot) : null,
  };
}
