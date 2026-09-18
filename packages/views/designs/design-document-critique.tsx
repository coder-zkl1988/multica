"use client";

import { CircleAlert, CircleCheck } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

/**
 * The critique report a design document revision may carry (DC-050): the
 * agent's review loop through Open Design's five lenses. The platform only
 * validated its shape, so this panel renders it as the agent's own account —
 * never as a verdict, and never as what made the draft.
 */

export const CRITIQUE_LENSES: ReadonlyArray<{ id: string; label: string }> = [
  { id: "designer", label: "设计师" },
  { id: "critic", label: "评审" },
  { id: "brand", label: "品牌" },
  { id: "a11y", label: "无障碍" },
  { id: "copy", label: "文案" },
];

export interface CritiqueFinding {
  lens: string;
  severity: "must_fix" | "should_fix" | "note";
  summary: string;
  resolved: boolean;
}

export interface CritiqueRound {
  index: number;
  scores: Record<string, number>;
  findings: CritiqueFinding[];
}

export interface Critique {
  threshold: number;
  max_rounds: number;
  outcome: "passed" | "stopped_at_max_rounds" | "not_run";
  rounds: CritiqueRound[];
}

const SEVERITIES = new Set(["must_fix", "should_fix", "note"]);
const OUTCOMES = new Set(["passed", "stopped_at_max_rounds", "not_run"]);

/**
 * Reads a critique out of the untyped revision field. Anything that is not a
 * recognisable report yields null so the panel simply does not render; the
 * server already validated packages it accepted, so this guards against old
 * revisions and drift, not against the agent.
 */
export function parseCritique(value: unknown): Critique | null {
  if (!value || typeof value !== "object") return null;
  const record = value as Record<string, unknown>;
  if (!Array.isArray(record.rounds) || record.rounds.length === 0) return null;
  const rounds: CritiqueRound[] = [];
  for (const raw of record.rounds) {
    if (!raw || typeof raw !== "object") return null;
    const round = raw as Record<string, unknown>;
    const scores: Record<string, number> = {};
    if (round.scores && typeof round.scores === "object") {
      for (const [lens, score] of Object.entries(round.scores as Record<string, unknown>)) {
        if (typeof score === "number" && Number.isFinite(score)) scores[lens] = score;
      }
    }
    const findings: CritiqueFinding[] = [];
    if (Array.isArray(round.findings)) {
      for (const item of round.findings) {
        if (!item || typeof item !== "object") continue;
        const finding = item as Record<string, unknown>;
        if (typeof finding.summary !== "string" || !SEVERITIES.has(String(finding.severity))) continue;
        findings.push({
          lens: typeof finding.lens === "string" ? finding.lens : "",
          severity: finding.severity as CritiqueFinding["severity"],
          summary: finding.summary,
          resolved: finding.resolved === true,
        });
      }
    }
    rounds.push({ index: typeof round.index === "number" ? round.index : rounds.length + 1, scores, findings });
  }
  const outcome = OUTCOMES.has(String(record.outcome)) ? (record.outcome as Critique["outcome"]) : "not_run";
  return {
    threshold: typeof record.threshold === "number" ? record.threshold : 0,
    max_rounds: typeof record.max_rounds === "number" ? record.max_rounds : rounds.length,
    outcome,
    rounds,
  };
}

export function critiqueOutcomeLabel(outcome: Critique["outcome"]): string {
  switch (outcome) {
    case "passed":
      return "全部达标";
    case "stopped_at_max_rounds":
      return "达到轮数上限";
    default:
      return "未执行";
  }
}

function lensLabel(lens: string): string {
  return CRITIQUE_LENSES.find((item) => item.id === lens)?.label ?? lens;
}

function severityLabel(severity: CritiqueFinding["severity"]): string {
  if (severity === "must_fix") return "必须修";
  if (severity === "should_fix") return "建议修";
  return "备注";
}

/**
 * Findings still open at the end of the loop: what the agent flagged and did
 * not resolve, must-fix first. Resolved findings from earlier rounds are
 * history, not a to-do list.
 */
export function openFindings(critique: Critique): CritiqueFinding[] {
  const last = critique.rounds[critique.rounds.length - 1];
  if (!last) return [];
  const order = { must_fix: 0, should_fix: 1, note: 2 } as const;
  return last.findings
    .filter((finding) => !finding.resolved)
    .sort((a, b) => order[a.severity] - order[b.severity]);
}

export function DesignDocumentCritique({ critique }: { critique: Critique }) {
  const last = critique.rounds[critique.rounds.length - 1];
  if (!last) return null;
  const open = openFindings(critique);
  return (
    <section className="rounded-lg border bg-card p-3" aria-label="设计评审">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-caption font-medium text-foreground">设计评审</h2>
        <span className="flex items-center gap-1 text-caption text-muted-foreground">
          {critique.outcome === "passed" ? <CircleCheck className="h-3.5 w-3.5 text-primary" /> : <CircleAlert className="h-3.5 w-3.5" />}
          {critiqueOutcomeLabel(critique.outcome)} · {critique.rounds.length} 轮
        </span>
      </div>
      <dl className="mt-2.5 space-y-1.5">
        {CRITIQUE_LENSES.map((lens) => {
          const score = last.scores[lens.id];
          const known = typeof score === "number";
          const reached = known && score >= critique.threshold;
          return (
            <div key={lens.id} className="flex items-center gap-2 text-caption">
              <dt className="w-12 shrink-0 text-muted-foreground">{lens.label}</dt>
              <dd className="flex min-w-0 flex-1 items-center gap-2">
                <span className="relative h-1.5 flex-1 overflow-hidden rounded-full bg-muted" aria-hidden="true">
                  {known ? (
                    <span
                      className={cn("absolute inset-y-0 left-0 rounded-full", reached ? "bg-primary" : "bg-muted-foreground/60")}
                      style={{ width: `${Math.max(0, Math.min(10, score)) * 10}%` }}
                    />
                  ) : null}
                </span>
                <span className="w-8 shrink-0 text-right font-mono text-micro text-muted-foreground">{known ? score : "–"}</span>
              </dd>
            </div>
          );
        })}
      </dl>
      <p className="mt-2 text-micro text-muted-foreground">阈值 {critique.threshold} / 10 · 这是智能体自己的评审记录，不决定草稿是否成立。</p>
      {open.length > 0 ? (
        <ul className="mt-2.5 space-y-1.5 border-t pt-2.5">
          {open.map((finding, index) => (
            <li key={`${finding.lens}-${index}`} className="text-caption leading-5">
              <span className={cn("mr-1.5 rounded-sm px-1 text-micro", finding.severity === "must_fix" ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground")}>
                {severityLabel(finding.severity)}
              </span>
              <span className="text-muted-foreground">{lensLabel(finding.lens)} · </span>
              {finding.summary}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}
