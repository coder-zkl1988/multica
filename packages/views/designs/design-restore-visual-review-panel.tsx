import { ExternalLink, GitCompareArrows, ImageIcon, Route } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import type { ReactNode } from "react";
import type { DesignRestoreVisualReview } from "./design-restore-result";

function scoreBadge(review: DesignRestoreVisualReview) {
  return review.score === null ? "未评分" : `${review.score}/100`;
}

function EvidenceRow({ label, value, icon }: { label: string; value: string; icon: ReactNode }) {
  if (!value) return null;
  return (
    <div className="grid gap-1">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <div className="break-all rounded-md bg-muted px-2 py-1 font-mono text-foreground">{value}</div>
    </div>
  );
}

export function DesignRestoreVisualReviewPanel({ review }: { review: DesignRestoreVisualReview | null }) {
  if (!review) return null;
  return (
    <section className="rounded-lg border bg-background p-3">
      <div className="flex items-center justify-between gap-2">
        <div className="text-body font-medium">视觉验收</div>
        <Badge variant={review.score !== null && review.score >= 80 ? "secondary" : "outline"}>{scoreBadge(review)}</Badge>
      </div>
      <div className="mt-3 space-y-3 text-caption">
        <EvidenceRow label="实现路由" value={review.implementedRoute} icon={<Route className="h-3.5 w-3.5" />} />
        <EvidenceRow label="设计截图" value={review.designScreenshot} icon={<ImageIcon className="h-3.5 w-3.5" />} />
        <EvidenceRow label="实现截图" value={review.implementationScreenshot} icon={<ExternalLink className="h-3.5 w-3.5" />} />
        <EvidenceRow label="对比截图" value={review.comparisonScreenshot} icon={<GitCompareArrows className="h-3.5 w-3.5" />} />
        {review.remainingDiffs.length ? (
          <div>
            <div className="mb-1 font-medium">遗留差异</div>
            <ul className="space-y-1 text-muted-foreground">
              {review.remainingDiffs.map((diff) => <li key={diff}>{diff}</li>)}
            </ul>
          </div>
        ) : null}
        {review.notes ? (
          <div>
            <div className="mb-1 font-medium">备注</div>
            <p className="leading-relaxed text-muted-foreground">{review.notes}</p>
          </div>
        ) : null}
      </div>
    </section>
  );
}
