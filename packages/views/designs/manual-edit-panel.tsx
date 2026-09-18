"use client";

import { X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { ElementDescriptor } from "./element-descriptor";
import {
  computedColorToHex,
  declarationOf,
  EDITABLE_GROUPS,
  type ManualEdit,
} from "./manual-edit-model";

/**
 * The properties panel: what a designer changes about the element they picked.
 *
 * Every control shows the value the canvas is actually painting — read from
 * the live computed style, not from the package source — so an untouched
 * control is never a lie about what the element looks like. Only what the
 * designer edits is recorded; the rest of the computed style belongs to the
 * design the agent wrote and must not be frozen into an override.
 */
export function ManualEditPanel({
  descriptor,
  page,
  edits,
  computed,
  onChange,
  onClear,
  onDeselect,
}: {
  descriptor: ElementDescriptor | null;
  page: string;
  edits: ReadonlyArray<ManualEdit>;
  /** Live computed style of the picked element, or null before a pick. */
  computed: CSSStyleDeclaration | null;
  onChange: (property: string, value: string) => void;
  onClear: () => void;
  onDeselect: () => void;
}) {
  if (!descriptor) {
    return (
      <p className="px-0.5 text-caption leading-5 text-muted-foreground">
        在画布上点选一个元素，就能直接改它的文字、颜色、间距和布局。
      </p>
    );
  }

  const pending = edits.find((edit) => edit.page === page && edit.selector === descriptor.selector);
  const changedCount = pending ? Object.keys(pending.declarations).length : 0;

  /** The value a control shows: the pending override, else what is painted. */
  const shownValue = (property: string): string => {
    const override = declarationOf(edits, page, descriptor.selector, property);
    if (override !== "") return override;
    // An override cleared back to "" deliberately shows the computed value
    // again, which is what the canvas will render once the edit lands.
    return computed?.getPropertyValue(property).trim() ?? "";
  };

  return (
    <div className="min-w-0">
      <div className="flex items-start gap-1.5">
        <div className="min-w-0 flex-1">
          <p className="truncate text-caption font-medium" title={descriptor.selector}>{descriptor.label}</p>
          <p className="truncate font-mono text-micro text-muted-foreground" title={descriptor.selector}>
            {descriptor.selector}
          </p>
        </div>
        <button
          type="button"
          aria-label="取消选中"
          title="取消选中"
          className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          onClick={onDeselect}
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {changedCount > 0 ? (
        <div className="mt-1.5 flex items-center justify-between gap-2 text-caption">
          <span className="text-muted-foreground">已改 {changedCount} 项</span>
          <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-caption" onClick={onClear}>
            还原这个元素
          </Button>
        </div>
      ) : null}

      <div className="mt-2 space-y-3">
        {EDITABLE_GROUPS.map((group) => (
          <section key={group.id}>
            <h3 className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{group.label}</h3>
            <div className="mt-1 space-y-1">
              {group.properties.map((control) => {
                const value = shownValue(control.property);
                const overridden = declarationOf(edits, page, descriptor.selector, control.property) !== "";
                const controlId = `manual-edit-${control.property}`;
                return (
                  <div key={control.property} className="flex items-center gap-2">
                    <label
                      htmlFor={controlId}
                      className={cn("w-16 shrink-0 text-caption", overridden ? "font-medium text-foreground" : "text-muted-foreground")}
                    >
                      {control.label}
                    </label>
                    {control.kind === "color" ? (
                      <div className="flex min-w-0 flex-1 items-center gap-1.5">
                        <input
                          id={controlId}
                          type="color"
                          aria-label={control.label}
                          value={computedColorToHex(value) || "#000000"}
                          className="size-6 shrink-0 cursor-pointer rounded-sm border bg-transparent p-0"
                          onChange={(event) => onChange(control.property, event.target.value)}
                        />
                        <input
                          type="text"
                          aria-label={`${control.label}（值）`}
                          value={value}
                          placeholder="未设置"
                          className="min-w-0 flex-1 rounded-sm border bg-background px-1.5 py-0.5 font-mono text-micro outline-none focus:border-primary/60"
                          onChange={(event) => onChange(control.property, event.target.value)}
                        />
                      </div>
                    ) : control.kind === "select" ? (
                      <select
                        id={controlId}
                        aria-label={control.label}
                        value={control.options?.some((option) => option.value === value) ? value : ""}
                        className="min-w-0 flex-1 rounded-sm border bg-background px-1.5 py-0.5 text-caption outline-none focus:border-primary/60"
                        onChange={(event) => onChange(control.property, event.target.value)}
                      >
                        <option value="">{value || "未设置"}</option>
                        {control.options?.map((option) => (
                          <option key={option.value} value={option.value}>{option.label}</option>
                        ))}
                      </select>
                    ) : (
                      <input
                        id={controlId}
                        type="text"
                        aria-label={control.label}
                        value={value}
                        placeholder="未设置"
                        className="min-w-0 flex-1 rounded-sm border bg-background px-1.5 py-0.5 font-mono text-micro outline-none focus:border-primary/60"
                        onChange={(event) => onChange(control.property, event.target.value)}
                      />
                    )}
                  </div>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
