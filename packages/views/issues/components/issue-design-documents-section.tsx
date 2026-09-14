"use client";

import { useQuery } from "@tanstack/react-query";
import { LoaderCircle, Palette, Plus } from "lucide-react";
import { issueDesignDocumentsOptions } from "@multica/core/designs/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DesignDocument, Issue } from "@multica/core/types";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { designDocumentStatusLabel } from "../../designs/design-document-card";

/**
 * The designs being made for this task.
 *
 * A design run linked to an issue used to be invisible from it: the card
 * showed whatever had last touched it — in the case that prompted this, a run
 * cancelled twenty minutes earlier — while an agent was actively designing
 * for it. This is the issue's side of that link: what exists, what state it is
 * in, and a way into it.
 *
 * Deliberately not a copy of the run's transcript. A document that goes
 * through eight revisions would bury everything else on the card, and the same
 * messages rendered in two places drift apart. The design page owns the
 * conversation; the issue owns knowing that it is happening.
 */
export function IssueDesignDocumentsSection({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const { data: documents = [], isPending } = useQuery(
    issueDesignDocumentsOptions(issue.workspace_id, issue.id),
  );
  const params = new URLSearchParams({ create_issue_id: issue.id });
  if (issue.project_id) params.set("create_project_id", issue.project_id);
  if (issue.assignee_type === "agent" && issue.assignee_id) {
    params.set("create_agent_id", issue.assignee_id);
  }
  const createHref = `${paths.designs()}?${params.toString()}`;

  return (
    <div>
      <div className="mb-2 flex items-center gap-1 px-2 py-1 text-caption font-medium text-muted-foreground">
        <Palette className="!size-3 shrink-0 stroke-[2.5]" />
        {t(($) => $.detail.section_design_documents)}
      </div>
      <div className="pl-2">
        {issue.project_id ? (
          <AppLink
            href={createHref}
            className="-mx-2 flex items-center gap-1.5 rounded-md px-2 py-1.5 text-caption font-medium text-primary transition-colors hover:bg-accent/50"
          >
            <Plus className="size-3.5 shrink-0" />
            <span>{t(($) => $.detail.create_multica_design)}</span>
          </AppLink>
        ) : (
          <button
            type="button"
            disabled
            title={t(($) => $.detail.design_requires_project)}
            className="-mx-2 flex items-center gap-1.5 px-2 py-1.5 text-caption text-faint-foreground"
          >
            <Plus className="size-3.5 shrink-0" />
            <span>{t(($) => $.detail.create_multica_design)}</span>
          </button>
        )}
        {isPending ? (
          <div className="-mx-2 flex items-center gap-1.5 px-2 py-1.5 text-caption text-muted-foreground">
            <LoaderCircle className="size-3.5 shrink-0 animate-spin" />
            <span>{t(($) => $.detail.design_documents_loading)}</span>
          </div>
        ) : documents.map((document) => (
          <AppLink
            key={document.id}
            href={paths.designDocumentDetail(document.id)}
            className="-mx-2 flex items-center gap-1.5 rounded-md px-2 py-1.5 text-caption transition-colors hover:bg-accent/50"
          >
            <DocumentIcon document={document} />
            <span className="min-w-0 flex-1 truncate">{document.title}</span>
            <span className="shrink-0 text-muted-foreground">
              {designDocumentStatusLabel(document.status) ?? ""}
            </span>
          </AppLink>
        ))}
      </div>
    </div>
  );
}

/** A live run spins; anything else is a document sitting still. */
function DocumentIcon({ document }: { document: DesignDocument }) {
  if (document.status === "running") {
    return <LoaderCircle className="size-3.5 shrink-0 animate-spin text-muted-foreground" />;
  }
  return <Palette className="size-3.5 shrink-0 text-muted-foreground" />;
}
