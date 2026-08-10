import type { GalleryNativeJson } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { analyzeFrameFidelity } from "../native-renderer/fidelity";

export function designQualitySummary(nativeJson: GalleryNativeJson | undefined) {
  if (!nativeJson?.frames.length) return null;
  const frameReports = nativeJson.frames.map((frame) => ({ frame, report: analyzeFrameFidelity(nativeJson, frame) }));
  const average = Math.round(frameReports.reduce((sum, item) => sum + item.report.renderQualityPercent, 0) / frameReports.length);
  const lowest = [...frameReports].sort((a, b) => a.report.renderQualityPercent - b.report.renderQualityPercent).slice(0, 3);
  return { average, frameReports, lowest };
}

export function DesignQualitySummary({ nativeJson }: { nativeJson: GalleryNativeJson | undefined }) {
  const summary = designQualitySummary(nativeJson);
  if (!summary) return null;

  return (
    <section className="rounded-2xl border bg-background/95 p-4 shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-body font-medium">导入质量</div>
          <div className="mt-1 text-caption text-muted-foreground">按真实图层、局部兜底和缺失情况计算</div>
        </div>
        <Badge variant="outline" className="h-8 shrink-0 px-2 text-caption">平均还原度 {summary.average}%</Badge>
      </div>
      <div className="mt-3 grid gap-2 text-caption sm:grid-cols-3">
        {summary.lowest.map(({ frame, report }) => (
          <div key={frame.id} className="flex min-w-0 items-center justify-between gap-2 rounded-xl bg-muted/45 px-3 py-2">
            <span className="truncate text-muted-foreground">{frame.name}</span>
            <span className="shrink-0 font-mono text-foreground">{report.renderQualityPercent}%</span>
          </div>
        ))}
      </div>
    </section>
  );
}
