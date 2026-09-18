"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Unlink } from "lucide-react";
import { toast } from "sonner";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  testCaseIssuesOptions,
  useLinkTestCaseIssues,
  useUnlinkTestCaseIssue,
} from "@multica/core/testing";
import type { TestCaseIssueLink } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useT } from "../../i18n";

/**
 * The requirements one test case claims to verify.
 *
 * This is the case side of the coverage relation. Until it existed the only
 * structured path between a case and an issue ran the other way — a failed
 * execution opening a defect — so nothing recorded what a case was written FOR,
 * and an AI-generated case lost its provenance the moment its job finished.
 */
export function CaseIssueLinks({ wsId, caseRef }: { wsId: string; caseRef: string }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const [pickerOpen, setPickerOpen] = useState(false);

  const { data: links = [] } = useQuery(testCaseIssuesOptions(wsId, caseRef));
  const linkIssues = useLinkTestCaseIssues();
  const unlinkIssue = useUnlinkTestCaseIssue();

  const busy = linkIssues.isPending || unlinkIssue.isPending;

  return (
    <div className="flex flex-col gap-1.5">
      {links.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.coverage.caseEmpty)}
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {links.map((link) => (
            <LinkRow
              key={link.issue_id}
              link={link}
              href={paths.issueDetail(link.issue_id)}
              disabled={busy}
              onUnlink={() =>
                unlinkIssue.mutate(
                  { ref: caseRef, issueId: link.issue_id },
                  {
                    onError: () => toast.error(t(($) => $.coverage.unlinkFailed)),
                  },
                )
              }
            />
          ))}
        </ul>
      )}

      <Button
        size="sm"
        variant="outline"
        className="w-fit"
        disabled={busy}
        onClick={() => setPickerOpen(true)}
      >
        <Plus className="size-3.5" />
        {t(($) => $.coverage.linkIssue)}
      </Button>

      <IssuePickerModal
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        title={t(($) => $.coverage.pickerTitle)}
        description={t(($) => $.coverage.pickerDescription)}
        excludeIds={links.map((link) => link.issue_id)}
        onSelect={(issue) =>
          linkIssues.mutate(
            { ref: caseRef, issueIds: [issue.id] },
            {
              onSuccess: () => toast.success(t(($) => $.coverage.linked)),
              onError: (err) =>
                toast.error(
                  err instanceof Error && err.message
                    ? err.message
                    : t(($) => $.coverage.linkFailed),
                ),
            },
          )
        }
      />
    </div>
  );
}

function LinkRow({
  link,
  href,
  disabled,
  onUnlink,
}: {
  link: TestCaseIssueLink;
  href: string;
  disabled: boolean;
  onUnlink: () => void;
}) {
  const { t } = useT("testing");

  return (
    <li className="group/link flex items-center gap-1.5 text-caption">
      <AppLink
        href={href}
        className="shrink-0 text-muted-foreground tabular-nums hover:text-foreground hover:underline"
      >
        {link.issue_identifier}
      </AppLink>
      <AppLink href={href} className="min-w-0 flex-1 truncate hover:underline" title={link.issue_title}>
        {link.issue_title}
      </AppLink>
      {/* An AI-asserted coverage claim is exactly what a reviewer needs to see
          flagged; a hand-drawn link needs no badge. */}
      {link.origin === "ai" ? (
        <span className="shrink-0 rounded-sm bg-muted px-1 text-micro text-muted-foreground">
          {t(($) => $.origin.ai)}
        </span>
      ) : null}
      <button
        type="button"
        disabled={disabled}
        aria-label={t(($) => $.coverage.unlink)}
        title={t(($) => $.coverage.unlink)}
        onClick={onUnlink}
        className="shrink-0 rounded-sm p-0.5 text-muted-foreground opacity-0 transition-opacity hover:text-destructive focus-visible:opacity-100 group-hover/link:opacity-100 disabled:opacity-50 [@media(hover:none)]:opacity-100"
      >
        <Unlink className="size-3" />
      </button>
    </li>
  );
}
