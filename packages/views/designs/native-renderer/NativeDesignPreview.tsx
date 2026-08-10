import { useMemo, useState } from "react";
import type { GalleryNativeJson } from "@multica/core/types";
import { NativeFramePreview } from "./NativeFramePreview";

export function NativeDesignPreview({ nativeJson, className }: { nativeJson: GalleryNativeJson | undefined; className?: string }) {
  const frames = useMemo(() => nativeJson?.frames ?? [], [nativeJson]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const safeIndex = Math.min(selectedIndex, Math.max(frames.length - 1, 0));
  const frame = frames[safeIndex];
  if (!nativeJson || !frame) return <div className={className}>暂无可预览设计数据</div>;
  const scale = Math.min(1, 760 / Math.max(frame.width, 1), 520 / Math.max(frame.height, 1));
  return (
    <div className={className}>
      <div className="mb-2 flex items-center justify-between text-caption text-muted-foreground">
        <span className="truncate">{frame.name}</span>
        <span className="font-mono">{Math.round(frame.width)}×{Math.round(frame.height)}</span>
      </div>
      {frames.length > 1 ? (
        <div className="mb-3 flex gap-2 overflow-auto pb-1">
          {frames.map((item, index) => (
            <button
              key={item.id}
              type="button"
              onClick={() => setSelectedIndex(index)}
              className={`shrink-0 rounded-full border px-3 py-1 text-caption ${index === safeIndex ? "border-primary bg-primary text-primary-foreground" : "bg-background text-muted-foreground hover:bg-accent hover:text-foreground"}`}
            >
              {item.name}
            </button>
          ))}
        </div>
      ) : null}
      <div className="overflow-auto rounded-lg border bg-muted/30 p-3">
        <div className="mx-auto" style={{ width: frame.width * scale, height: frame.height * scale }}>
          <div className="relative overflow-hidden bg-background shadow-sm" style={{ width: frame.width, height: frame.height, transform: `scale(${scale})`, transformOrigin: "top left" }}>
            <NativeFramePreview nativeJson={nativeJson} frame={frame} className="absolute inset-0" />
          </div>
        </div>
      </div>
    </div>
  );
}
