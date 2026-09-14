"use client";

import { Folder, GitBranch } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

export type DesignMvpViewMode = "project" | "repository";

const modes = [
  { value: "project", label: "按项目", tooltip: "项目", icon: Folder },
  { value: "repository", label: "按仓库", tooltip: "仓库", icon: GitBranch },
] as const;

export function DesignMvpViewSwitcher({
  mode,
  onModeChange,
}: {
  mode: DesignMvpViewMode;
  onModeChange: (mode: DesignMvpViewMode) => void;
}) {
  return (
    <div
      role="group"
      aria-label="设计中心视角"
      className="inline-flex items-center gap-0.5 rounded-lg bg-muted p-1"
    >
      {modes.map(({ value, label, tooltip, icon: Icon }) => {
        const selected = mode === value;
        return (
          <Tooltip key={value}>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={label}
                  aria-pressed={selected}
                  title={tooltip}
                  className={selected ? "bg-background text-foreground shadow-sm hover:bg-background" : "text-muted-foreground"}
                  onClick={() => onModeChange(value)}
                >
                  <Icon aria-hidden="true" className="size-4" />
                </Button>
              }
            />
            <TooltipContent>
              {tooltip}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}
