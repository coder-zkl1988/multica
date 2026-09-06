"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";
import { TEST_CAPABILITY_KINDS } from "@multica/core/testing";
import type { TestCapabilityKind, TestCapabilityRequirement } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { useT } from "../../i18n";
import { formatCapabilityMatch, parseCapabilityMatch } from "./capability-match";

interface TestCaseCapabilitiesFieldProps {
  value: TestCapabilityRequirement[];
  onChange: (next: TestCapabilityRequirement[]) => void;
  disabled?: boolean;
}

/**
 * Edits `required_capabilities`: which kinds of browser or device a case needs
 * a round to be bound to. Only the kind is mandatory; `match` narrows the
 * device and `optional` keeps a missing kind from blocking the run.
 */
export function TestCaseCapabilitiesField({
  value,
  onChange,
  disabled = false,
}: TestCaseCapabilitiesFieldProps) {
  const { t } = useT("testing");
  // The match line is kept as typed until it parses, so a half-written
  // `os_version=` does not vanish from the input on every keystroke.
  const [matchDrafts, setMatchDrafts] = useState<Record<number, string>>({});

  function patchRequirement(position: number, patch: Partial<TestCapabilityRequirement>) {
    onChange(
      value.map((requirement, index) =>
        index === position ? { ...requirement, ...patch } : requirement,
      ),
    );
  }

  function removeRequirement(position: number) {
    onChange(value.filter((_, index) => index !== position));
    setMatchDrafts({});
  }

  function addRequirement() {
    onChange([...value, { kind: "browser" }]);
  }

  return (
    <div className="flex flex-col gap-2">
      {value.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.capabilities.empty)}</p>
      ) : null}

      {value.map((requirement, position) => (
        <div
          key={`${requirement.kind}-${position}`}
          className="flex items-start gap-2 rounded-md border border-border p-2"
        >
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <NativeSelect
              aria-label={t(($) => $.capabilities.kind)}
              value={requirement.kind}
              disabled={disabled}
              onChange={(e) =>
                patchRequirement(position, { kind: e.target.value as TestCapabilityKind })
              }
            >
              {TEST_CAPABILITY_KINDS.map((kind) => (
                <option key={kind} value={kind}>
                  {t(($) => $.capabilities.kinds[kind])}
                </option>
              ))}
            </NativeSelect>
            <Input
              aria-label={t(($) => $.capabilities.match)}
              value={matchDrafts[position] ?? formatCapabilityMatch(requirement.match)}
              disabled={disabled}
              placeholder={t(($) => $.capabilities.matchPlaceholder)}
              onChange={(e) => {
                const text = e.target.value;
                setMatchDrafts((previous) => ({ ...previous, [position]: text }));
                const next = { ...requirement };
                const parsed = parseCapabilityMatch(text);
                if (parsed) next.match = parsed;
                else delete next.match;
                onChange(value.map((entry, index) => (index === position ? next : entry)));
              }}
            />
            <label className="inline-flex items-center gap-2 text-caption text-muted-foreground">
              <input
                type="checkbox"
                className="size-3.5 accent-primary"
                checked={requirement.optional === true}
                disabled={disabled}
                onChange={(e) =>
                  patchRequirement(position, { optional: e.target.checked ? true : undefined })
                }
              />
              {t(($) => $.capabilities.optional)}
            </label>
          </div>
          <Button
            size="icon"
            variant="ghost"
            className="size-7 shrink-0"
            disabled={disabled}
            aria-label={t(($) => $.capabilities.remove)}
            onClick={() => removeRequirement(position)}
          >
            <X className="size-3.5" />
          </Button>
        </div>
      ))}

      <p className="text-micro text-muted-foreground">{t(($) => $.capabilities.hint)}</p>

      <Button
        size="sm"
        variant="outline"
        disabled={disabled}
        onClick={addRequirement}
        className="self-start"
      >
        <Plus className="size-3.5" />
        {t(($) => $.capabilities.add)}
      </Button>
    </div>
  );
}
