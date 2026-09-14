# Design Center Active Plan Index

> **Navigation index only.** The authoritative current handoff is `docs/superpowers/handoffs/2026-09-01-design-center-end-to-end-mvp-handoff.md`. Read that handoff first. If this index, a Plan, a Spec, or historical chat conflicts with the handoff, the handoff wins. Do not scan the plans/specs directories broadly.
>
> Updated: 2026-09-04

## Active execution priority

0. **Authoritative current state — read first**
   - `docs/superpowers/handoffs/2026-09-01-design-center-end-to-end-mvp-handoff.md`
   - MVP Task 1–14 已完成并进入 `codex/design-center-end-to-end-mvp@95ad8fd63`。没有 Task 15。

1. **Current completion matrix and m-next acceptance replay**
   - `docs/superpowers/plans/2026-09-04-design-center-end-to-end-mvp-completion-plan.md`
   - 记录 Task 1–14 完成对照，并定义新的 `m-next` 真实目标复验；它不解冻 Post-MVP。

2. **Completed MVP roadmap — historical execution plan**
   - `docs/superpowers/plans/2026-08-31-design-center-end-to-end-mvp-roadmap.md`
   - Goal: repository-specific design system → real Multica designs → unified Multica/Figma restore → Issue design/restore loop → one real end-to-end acceptance.

## Completed foundation

3. **M1 Slice 1 repository association foundation — completed**
   - `docs/superpowers/plans/2026-08-27-design-file-repository-scope.md`

4. **M1 Slice 1 closure + Slice 2A read projection — completed**
   - `docs/superpowers/plans/2026-08-27-design-center-repository-read-projection.md`
   - Validation evidence in the integration branch:
     `docs/product/design-center/m1-slice-2a-validation.md`

## Deferred until after MVP acceptance

5. **Full Finder / Repository Workspace polish plan — DEFERRED**
   - `docs/superpowers/plans/2026-08-31-design-center-finder-repository-workspaces.md`
   - Do not execute before the end-to-end MVP is accepted by the user.
   - Its full multi-open tabs, isolated searches, batch association, realtime polish, and full workspace migration are Post-MVP refinement work.

## Product authority used by the active roadmap

Read only the relevant section named by the active Task:

- Confirmed Design Center memory:
  `docs/product/design-center/README.md`
- Overall priority/dependency overview:
  `docs/superpowers/specs/2026-08-27-multica-design-center-master-plan.md`
- Project/repository and repository design-system product contract:
  `docs/superpowers/specs/2026-08-26-design-center-project-repository-views-m1-design.md`
- Unified restore / Implementation Context contract:
  `docs/superpowers/specs/2026-08-26-unified-design-asset-implementation-design.md`
- Issue creation/restore automation contract:
  `docs/superpowers/specs/2026-08-27-issue-design-automation-design.md`

## Reading rule

For every Task:

1. Read the authoritative handoff.
2. Read `docs/product/design-center/README.md`.
3. Use this index to locate the active roadmap.
4. Read the active roadmap's relevant Task and constraints.
5. Read only the cited section of one product Spec.
6. Inspect only the Task's bounded code read set.
7. Do not recursively read unrelated historical plans/specs.
