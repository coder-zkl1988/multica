# Design Center End-to-End MVP Validation

Validated on 2026-09-03 against local branch
`codex/design-center-end-to-end-mvp-task-14`, based on
`0f27dfa0f8b2656523c5501eb3ffaba8a675c2ec`.

## Scenario

The final MVP gate used one isolated PostgreSQL database and real local API,
Web, daemon, Chrome, Agent, and repository checkouts. Product mutations were
performed through the real API or UI. HTTP status alone was never treated as
acceptance evidence.

| Artifact | Exact identity |
| --- | --- |
| Workspace | `982328ab-b92f-4a66-a79f-958834b9e179` (`Task 11 Figma`) |
| Project | `1e7e9ef6-c499-4c4a-9550-baf1a4ef3462` |
| Repository association | `a1383069-8333-45bd-a3a5-b57502becafb` (`alisafyj/multica`) |
| Agent | `41de03ef-d13a-4be5-99fd-b3f704eb640b` (`Task 14 Design Gate Agent`) |
| Repository design system | `51ecf581-fca1-42b8-aa95-b1c1ad766691` |
| Design A | `65447a09-4451-4ad5-8ee0-2b6a5ba2b61f` |
| Design B | `7c398ff5-541d-4df5-9553-d3eb9f510c47` |
| Issue-initiated design | `b83514f0-63e2-4bf7-b205-9c75ec70f5a4` |
| Figma file | `505cbfd9-d4ad-4843-83c8-4d6e1fa46112` (`task11`) |
| Figma frozen revision | `30afa5fa-6b67-4242-a36f-abd767b7b5f0` |
| Existing Issue | `TASK-1` / `01a0628c-f9ab-717e-a872-82ae5c6bad31` |

No mock product data was used. Iterative checks used one minimal sample and
mocked network/IO where applicable. One complete real scan is run only after
the product changes and real acceptance chain are final.

## Phase A Evidence

The repository analysis produced a repository-specific saved design system for
the selected project resource. Its saved package is
`multica.project-design-system/v2`, integrity digest
`a528225d8cad8c2a34b153a63bfdd292bd3d51d81f68d8764ebbfe7e6e04c1fa`,
with validation and render status both passing.

Two Multica designs were generated from that fixed system and saved manually:

| Design | Saved revision | Revision | Audit | Preview |
| --- | --- | ---: | --- | --- |
| Task 14 Design A - Repository Operations | `44cc85d6-4bda-4ceb-ae62-ab5e1ea41cb8` | 2 | passed | passed |
| Task 14 Design B - Design Delivery Detail | `8587679a-1fb4-4ba1-9081-898fb23be60b` | 1 | passed, zero diagnostics | passed |

Design A was adjusted before its second saved revision. Design B's validated
content digest is
`sha256:994ac9d8f3be6fae13651c9af7f194d2d70eba2a33ce863c97f8f2763e06e722`.
Its real Chrome preview ran at 1280x900, rendered a visible body with 209 DOM
elements, and reported zero console errors, failed resources, or outbound
requests.

The real uploaded Figma file remained valid at the frozen revision with four
frames, 972 layers, and 141 assets. Source badges for Multica Design and Figma
remained visible together in the Issue selector.

## Restore Evidence

From `TASK-1`, the operator selected Design B, its `设计交付详情` page, and the
`alisafyj/multica` target repository. The UI generated the implementation
prompt, it was edited in the real Tiptap composer, and it was sent manually.
The send created Agent task `01a0669d-cdb4-7105-be9e-37630fad80c8`; the Issue
status remained `todo`.

The Multica restore completed and the daemon persisted a
`multica.design-implementation-receipt/v1` with result digest
`sha256:139df8a3e3b872e35569fecd7699c2fe865987d2d76497c77f0b4257c44cc873`.
The matching result is `completed`: one frame mapping, two target files, six
passed commands, one passed Preview evidence record, and zero blockers. The
target checkout began at `078425fff474f7faf9155ce2f1ccb1bc136dd14c`.

Its focused target validation comprised 32 Design Document page tests,
TypeScript, changed-file ESLint with zero errors, `git diff --check`, and a
real Chromium render. Chromium loaded 566 resources without 4xx/5xx responses,
reported zero console errors, retained the 360/1080 desktop split and 365px
confirmation dialog, and had no horizontal overflow at 390px. The Agent found
and fixed a real flex-item minimum-width defect during that render.

The first Figma restore, Agent task
`01a066b3-d68a-702e-9fe8-f2479ec4329c`, correctly returned `partial` because
its nine asset requests reached an unavailable prior local endpoint. That run
also executed 32 files / 256 tests despite the one-minimal-sample instruction;
the over-broad run is recorded here and was not repeated.

The frozen package's original binaries were then served from the retained
validated asset store. A first retry, `01a06726`, still returned `partial`
because it treated design asset IDs as attachment IDs. The corrected retry,
`01a06735`, read the authorized `assets.*.url` values from
`figma-restore-pack.json`, downloaded the nine selected-frame images into the
target checkout, and replaced the temporary local substitute. Its daemon
receipt is `completed`: one mapping, 13 target files, six passed checks, one
passed Preview evidence record, and zero blockers. Real Chrome at 375x812
confirmed all nine files were non-empty, returned HTTP 200, decoded, and
rendered without horizontal overflow.

The Issue entry point created Design Document
`b83514f0-63e2-4bf7-b205-9c75ec70f5a4` as Agent task
`01a066c6-1828-7ce0-a509-113aa3d3d8f9`, locked to `TASK-1`, the target
repository, and the saved repository design system. Its saved revision is
`2603f6c0-eeec-4fd6-99fc-c563a77be10c`, with content digest
`sha256:fd6a70677c86b1a518d0aebafbad0ef47d8e1c516d7f5503f76e692f26651bd1`.
The authoritative collector reports Audit and nested Preview verification both
passing, six indexed artifacts, two 9/10 critique rounds, and repository
grounding. It was saved manually in the real UI.

The Issue then generated the exact implementation prompt for that frozen
revision and frame. The main composer now exposes a stable scope, so the prompt
was manually edited with the Task 14 test and side-effect constraints and sent
through the real UI. Agent task `01a0671d-d378-797d-ab90-79827514a533`
completed, and the UI read the daemon receipt back as `验收通过`: one mapping,
four target files, five passed checks, one passed Preview evidence record, and
zero blockers.

No target checkout was committed, pushed, merged, or attached to a pull
request.

## Defect Found

The daemon stored Design Document preview evidence under
`preview.verification.passed`, while the run-status UI read only
`preview.passed`. A valid real preview therefore rendered as “Preview 暂无结果”.
The owning Views component now reads the nested verification receipt for
Preview while retaining the existing top-level format for the other gates.
The focused component suite covers the real stored shape.

The Design Document runtime also lacked a reliable Agent-facing command for
full package diagnostics. A hidden `multica design-document validate` command
now binds to the daemon-provided task context, emits the complete audit report,
and exits non-zero on failure. The daemon prompt requires that validator before
completion instead of leaving an Agent with only the first post-exit diagnostic.

Three delivery-path defects found by the real reruns were fixed at their shared
owners: the main Issue composer has a stable selector; package materialization
reuses an existing ordinary file only when its content is exactly identical;
and implementation-result collection resolves both normal checkout placement
and the existing `repositories/<checkout>` placement used by reused Design
Document workdirs.

The final repository scan also found one pre-existing disabled Issue action
using text opacity. It now uses the existing `text-faint-foreground` token, so
the product's solid-color text hierarchy and contrast gate remain intact.

## Validation

Completed checks before the final full scan:

- `go test ./cmd/multica -run '^TestValidateDesignDocumentPackageReturnsEveryAuditDiagnostic$' -count=1`: passed.
- `pnpm --filter @multica/views exec vitest run designs/design-document-run-status.test.tsx`: 1 file, 7 tests passed in 1.63s.
- Main composer scope test: 1 passed, 41 skipped.
- Exact package reuse test: passed.
- Reused Design Document checkout resolution tests: passed.
- Web production build: passed; only existing `::highlight` optimizer warnings.
- Design B package validator: zero diagnostics.
- Design B browser preview: passed real DOM, layout, interaction, resource, console, and outbound-request checks.
- Issue-created Design restore: `completed`, zero blockers.
- Figma restore with nine original assets: `completed`, zero blockers.

The final repository scan produced these results:

- TypeScript typecheck: all 9 packages passed.
- TypeScript unit tests: all 9 packages passed. The text-contrast regression
  found during this scan was fixed with the existing solid-color token; its
  focused suite then passed all 23 tests.
- Go tests: all packages passed after isolating the agent runtime's task marker
  and using the same local PostgreSQL database over TCP. The default wrapper's
  failures were environmental: CLI tests discovered the outer daemon marker,
  while two database helpers misparsed the Unix-socket query as a database
  path. `cmd/multica`, including the new Design Document validator test, passed
  from an equivalent marker-free package path.
- Playwright entered real Chromium with the required SSO test mode. The login
  page and self-contained iframe cases passed, but the suite remained blocked
  by existing environment/baseline failures: the Agent MCP tab assertions no
  longer match the current tab structure, and authenticated app tests are
  redirected into the SSO session page despite successful fixture setup and
  200 responses from `/api/me`. The run was stopped after the same shared
  failure exceeded three cases. No failing E2E file is in the Task 14 diff.

The final GitNexus comparison indexed 89,508 nodes, 288,999 edges, and 290
flows. It mapped the diff to 25 symbols in 14 files, found zero affected
processes, and rated the change risk `low`.

No screenshots, screen recordings, or traces are retained or attached. The
Issue-initiated design Agent briefly created one browser screenshot during its
own QA despite the Task 14 boundary, then deleted it before completion; the
final existence check confirms it is absent. The retained visual acceptance
evidence uses DOM, accessibility, computed-layout, resource, and interaction
checks.

## Interventions

- The first Design B Agent run exposed the missing Agent-facing full validator;
  the command and prompt contract were fixed in the owning CLI/daemon code.
- One Design B rerun failed after the Agent used an older `multica` from PATH;
  the authoritative task runtime binary was made explicit and the next run
  passed.
- Chrome login briefly hit the one-minute local verification-code cooldown;
  the same browser session was retained after the cooldown.
- Direct SQL aggregation of Figma native JSON was stopped after its shape did
  not match the assumed arrays. Product API/UI evidence and the previously
  validated typed summary remain authoritative. A future schema should expose
  stable first-class counts rather than require native JSON inspection.
- The first Figma retry used the wrong retrieval primitive for design assets.
  The corrected retry consumed the package URLs and passed without re-uploading
  or creating another design sample.
- The Issue composer ambiguity was removed with a stable main-composer scope;
  the same generated draft then dispatched and completed normally.
- The final full check inherited the Multica task marker and a Unix-socket
  database URL into tests that explicitly assume an ordinary local shell.
  Running the same Go packages with those runtime-only inputs isolated passed.
- Playwright's SSO fixture setup succeeded, but existing browser baseline
  failures remained after the shared environment was corrected; the repeated
  non-Task14 failures were not patched as part of this gate.

## Not Implemented

Post-MVP Finder work, multi-project tabs, visual refinement, automatic Issue
status changes, automatic repository commits, Push, PR creation, merge, and
release are explicitly outside this gate. The frozen Post-MVP items 1-7 remain
outside Task 14.

## User Acceptance

Status: Task 14 product chain passed and the implementation is ready for
review on the Task 14 branch. The final engineering scan has no Task 14 code
failure; its remaining Playwright blockers are the unrelated baseline issues
recorded above.

Acceptance means the exact scenario above is approved as the Design Center
End-to-End MVP. It does not authorize any Post-MVP refinement.
