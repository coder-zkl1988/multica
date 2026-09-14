import type { ProjectResource } from "@multica/core/types";

/**
 * Repository identity helpers shared by the design-centre surfaces that let a
 * user pick one repository out of a project (DC-052). Repository names repeat
 * across hosts and get truncated in narrow columns, so callers pair the label
 * with the URL as a title.
 */
export function repositoryUrl(resource: ProjectResource): string {
  const ref = resource.resource_ref as { url?: unknown } | undefined;
  return typeof ref?.url === "string" ? ref.url.trim() : "";
}

export function repositoryName(label: string | null | undefined, url: string, fallback = "未命名仓库"): string {
  const named = label?.trim();
  if (named) return named;
  const normalized = url.trim().replace(/\.git$/, "").replace(/\/+$/, "");
  if (!normalized) return fallback;
  return normalized.split(/[/:]/).filter(Boolean).pop() || fallback;
}

export function repositoryLabel(resource: ProjectResource): string {
  return repositoryName(resource.label, repositoryUrl(resource));
}
