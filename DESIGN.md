# Design Center Phase A2

## Source Of Truth

This file applies the confirmed Phase A specifications to the A2 implementation. It does not replace:

- `docs/superpowers/specs/2026-08-12-native-design-phase-a-design-document-design.md`
- `docs/superpowers/specs/2026-08-12-design-home-public-systems-community-templates-design.md`
- `docs/product/design-center/README.md`

## Product Goal

Let a workspace member start a real project-bound page-design task from the fixed Design Center home tab, then inspect and stop that same server task in the project's `设计草稿` tab.

The product is a quiet operational workspace. It should optimize repeated task creation and status scanning, not read like a landing page or a visual showcase.

## Users And Jobs

- A product or design owner describes a page requirement and supplies optional references.
- They bind the request to a required project and Agent, plus an optional Issue and target platform.
- They need immediate confirmation that the server accepted one durable task and fixed its inputs.
- They need to see truthful lifecycle evidence and stop active work without an invented percentage or an empty document.

## Information Architecture

### Home

1. Natural-language requirement input.
2. Optional attachment picker with visible upload states.
3. Compact project and Agent selectors; optional Issue and platform selectors.
4. One primary create command.
5. Recent page-design tasks below the composer, ordered by server activity.

The home does not expose shared design systems, templates, a marketing hero, or a second Project model in A2.

### Project / 设计草稿

1. In-progress page-design tasks that have not formed a first valid Design Document.
2. Existing historical PageSpec drafts remain readable below during the incremental migration.

Task rows show requirement summary, Agent, optional Issue, start time, elapsed time, latest activity, truthful state, and stop action. A task row is not a document card.

## Submission Contract

- Requirement, project, and Agent are required.
- Issue choices are scoped to the selected project.
- Attachments must finish uploading before submission.
- The server validates every selected entity and fixes a pre-grounding task input; A3 turns it into the immutable input snapshot after repository evidence is available.
- Only a successful server response opens/focuses the project tab and selects `设计草稿`.
- Failure preserves requirement, selections, and uploaded attachments and does not navigate.
- No optimistic task or empty Design Document is rendered.

## Visual Language

- Reuse existing Multica tokens, typography, inputs, buttons, badges, tabs, and menus.
- Use unframed page bands and borders for hierarchy; do not nest cards or add decorative surfaces.
- Keep the form compact: the requirement field carries the visual weight, while selectors stay dense.
- Use familiar Lucide icons for upload, submit, status, and stop actions.
- Cards, where needed for repeated task items, use the existing radius and restrained shadow/border treatment.
- Status colors are semantic and secondary to readable labels.

## Responsive Behavior

- Desktop: composer and context controls share one constrained content column; recent work follows below.
- Mobile: controls stack in reading order and the primary action remains full-width and reachable.
- Task metadata wraps into stable grid tracks without overlapping actions or changing row geometry when status text changes.

## Accessibility

- Every control has a visible label; icon-only controls have accessible names and tooltips where unfamiliar.
- Validation and request failures use `role="alert"` and do not clear user input.
- Uploading, pending, and stopping states are exposed through text, not color alone.
- Keyboard order follows project, Agent, Issue, platform, requirement, attachments, submit.
- Motion is limited to existing loading indicators and respects existing reduced-motion behavior.

## States

- Empty: focused composer plus a quiet recent-work empty message.
- Uploading: individual file progress/state and disabled submit.
- Submitting: stable layout, disabled submit, explicit text.
- Created: project tab opens only after the API response; the same task appears from the server query.
- Waiting for execution support: truthful server state for A2 tasks until A3 adds grounding and the generation prompt.
- Active: queued/dispatched/running/waiting states backed by task data.
- Failed/cancelled: retained as task history; no Design Document is created.
- Loading/error: scoped skeleton or inline retry without replacing the whole Design Center shell.

## A2 Boundaries

- No repository grounding, generation prompt, or persistent document workspace; those are A3.
- No package completion, browser Preview, or first revision formation; those are A4.
- No document adjustment, save, discard, or multi-document workspace; those are A5.
- No new template/shared-system controls and no old PageSpec default execution path.
