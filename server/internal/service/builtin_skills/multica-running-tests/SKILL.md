---
name: multica-running-tests
description: "Use when an agent is assigned to execute a Multica test run — discovering capabilities, recording results, uploading evidence, and opening defects. Not for writing, reviewing, or generating test cases."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Running Tests

This skill covers what a Multica test run is, how to drive it from the CLI,
and what the platform enforces. When behavior differs from this document, the
CLI's own `--help` and the run's JSON output are the authority.

## 1. Discover capabilities first, always

Before touching any test case result, run:

    multica test run get <run-id> --output json
    multica test capability list --run <run-id> --output json

`run get` returns the run's title, status, and the frozen case list.
`capability list` returns the `capability_binding` the platform resolved at
dispatch time. That binding looks like:

```json
{
  "run_id": "<run-id>",
  "capability_binding": {
    "daemon_id": "<daemon-id>",
    "runtime_id": "<runtime-id>",
    "resolved": {
      "android": "pixel-9-key",
      "browser": "chrome-desktop-key"
    }
  }
}
```

**Only the `capability_key` values in `resolved` are valid for this run.**
The platform freezes the binding at dispatch time — it is not re-evaluated at
runtime. Do not use any capability the binding did not return.

## 2. Never probe the host for devices or browsers

Do not run `adb devices`, `xcrun simctl list`, `google-chrome --version`,
`which chromium`, or any other tool that looks for capabilities on the host.
The platform already resolved which device or browser is bound to this run. If
the capability is not in the `resolved` map, that kind is unavailable for this
run — use `blocked` as the result, not `failed`.

Probing the host finds ambient devices that are NOT bound to your run, produces
false positives, and violates the capability-isolation contract that lets
multiple runs share one daemon safely.

## 3. `blocked` is not `failed`

| Result | Meaning |
| --- | --- |
| `passed` | Case executed; acceptance criterion met. |
| `failed` | Case executed; criterion not met. |
| `blocked` | Case could not be executed (missing device, missing credential, environment not ready). |
| `skipped` | Case excluded from this run (not applicable). |

Set `blocked` when the required capability is absent or the prerequisite
environment is not available. Never set `failed` when the test could not run.

## 4. Execute the frozen snapshot

A run executes the frozen case snapshot captured at dispatch, not the live case
record. The snapshot includes the `steps` array and the `required_capabilities`
list. Treat the snapshot as immutable; do not look up the live case mid-run.

## 5. Record results as you go

After each test case completes, record the result immediately — do not batch at
the end.

### Set result

    multica test result set <run-case-id> --result passed|failed|blocked|skipped [--note "…"] [--step-results <json>]

`--step-results` is an optional JSON array of per-step outcomes:

```json
[{"index": 1, "result": "passed"}, {"index": 2, "result": "failed", "note": "Button not found"}]
```

`--note` maps to the `notes` field — use it for a short failure summary or
blocking reason.

### Upload evidence

Upload evidence for every `failed` or `blocked` result:

    multica test evidence add <run-case-id> --file ./path/to/screenshot.png --kind screenshot

`--kind` values: `screenshot`, `video`, `log`, `other`.

Evidence upload uses multipart form with fields `file`, `test_run_case_id`, and
`kind`. The agent's task token authenticates the request automatically.

### Open a defect

When a case `failed` and the failure represents a product defect:

    multica test defect open <run-case-id> --title "Short reproduction title" [--note "…"]

This creates a linked issue in the workspace and attaches it to the test run
case. One defect per reproduction scenario; do not open a defect for `blocked`
results.

## 6. Start a pending run

If the run is in `pending` status, start it before recording results:

    multica test run start <run-id>

This transitions the run from `pending` to `running`. The agent normally
receives a run that has already been dispatched to `running` status; call
`run start` only if status is `pending`.

## 7. One task, one case

Since per-case dispatch a round is executed as one agent task per case. Your
task's context JSON names it:

A round may carry a parallelism cap: the server queues only that many case tasks at once and releases the next one when yours settles, so finishing (or blocking) your case promptly is what lets the round advance.

```json
{"type": "test_run", "run_id": "…", "run_case_id": "…", "case_key": "TC-42",
 "case_snapshot": {"steps": [...], "preconditions": "…", "expected_result": "…"},
 "capability_binding": {"resolved": {"android_device": "android:…"}},
 "assigned_capabilities": {"android_device": "android:<your phone's serial>"}}
```

- Execute `case_snapshot` and nothing else. Sibling cases run in their own
  tasks, possibly on other phones, at the same time.
- Record against `run_case_id` with `multica test result set`. The round
  completes by itself once every case is terminal; you never close it.
- End with `TEST_RUN_CASE_RESULT_JSON:{"result":"passed|failed|blocked|skipped","summary":"…"}`.
  The CLI write is the record; the line only settles the case if the write
  never happened.

## 8. Driving a phone

A case bound to a phone mounts one phone server, chosen by the kind. The task
context names your phone in `assigned_capabilities` (`android:<serial>` for an
Android phone).

| Kind | MCP server | Who reads the screen and acts |
| --- | --- | --- |
| `android_device` | `artemis` | Artemis's own agent, on a task you write |
| `ios_device` | `multica-device` | you, frame by frame |

Never type into a password field, complete a payment, install from outside
the store, or change system settings the case does not ask for — and never ask
Artemis to.

### Android: hand the steps to Artemis

`artemis` is [Artemis](https://github.com/google/artemis) on the test host,
behind a proxy that pins every call to your phone: `device_serial` is fixed
whatever you pass, and `mobile_diagnose` cannot restart adb or boot an
emulator.

| Tool | Use |
| --- | --- |
| `mobile_get_device_state` | `view_type: "screenshot"` returns a screenshot file path — read the image; `"hierarchy"` returns the element list Artemis sees |
| `mobile_run_task` | start Artemis on a task; returns a `trace_id` at once |
| `mobile_manage_task` | `status` (poll about once a minute), `inject_instruction`, `stop` |
| `mobile_inspect_trace` | `view_summary`, `search`, `view_step_screenshots`, `view_step_details` of a task |
| `mobile_diagnose` | once, when a tool errors or a task never starts |

1. Look first: take a screenshot.
2. One `mobile_run_task` per case. `task_desc` must stand on its own — Artemis
   never sees the case JSON: preconditions, every step's action in order, test
   data, and the screen to stop on. Set `locked_app_package` when the case
   names its app. A case is a known path, so `model: "Flash"` (seconds per
   step) is the default; `"Pro"` (tens of seconds per step, with
   `verification_level` and `expected_output_desc`) is for branches, long
   waits, polling or logs from the phone.
3. Poll `status` until `completed`, `failed` or `cancelled`. Steer with
   `inject_instruction`, or `stop` the task, if it leaves the case's path.
4. **Artemis finishing is not a pass.** Its `test_summary` is evidence, not
   the verdict. Judge every step against its `expected` from
   `mobile_inspect_trace` and a final screenshot. A step Artemis could not do
   is `failed` when the product stopped it and `blocked` when the environment
   did.
5. Copy the screenshot files you relied on into `./evidence/` and upload them
   with `multica test evidence add`.

A phone that is gone or unauthorized, or Artemis without model credentials,
is `blocked`: record what `mobile_diagnose` said and stop. Your phone may still
be finishing another case's Artemis task; yours then waits in Artemis's queue.

### iPhone: drive it through `multica-device`

`multica-device` leases an iPhone on the test host's device hub, driven by
PulsePhone on that Mac.

| Tool | Use |
| --- | --- |
| `device_info` | first call: model, iOS version, screen, serving track |
| `screenshot` | before every decision; coordinates you send afterwards are pixels of THIS frame |
| `tap` `double_tap` `long_press` `swipe` `scroll` | gestures; `scroll` takes the direction you want to see |
| `type_text` | after tapping the field; refused on password fields |
| `press_key` `launch_app` `wait` | `launch_app` needs the bundle id in `package` |
| `a11y_tree` | optional cross-check when the frame does not settle a label or a control's exact bounds; bounds are physical pixels — divide by `scale_factor` |
| `save_screenshot` | write the last frame to a file, then `multica test evidence add` |

**You are the one who reads the screen.** The frame comes back as an image on
the tool result: look at it and decide the next action from what you see.
Nothing else recognises the UI for you, so never block a case because
`a11y_tree` came back thin — on an iPhone it is on-device recognition of the
visible viewport (`cls` `text` or `controlCandidate`, often `degraded`), not an
accessibility tree. `screenshot` takes `full_res: true` when the default
728-pixel-wide frame is too small to read.

Every action returns `effect`: `changed` (read the new frame), `unchanged`
(the action did nothing visible — try one other target; three unchanged
actions on one screen means stuck: stop and report), `unknown` (screenshot).
Budget about 30 actions per case. There is no back key: tap the app's own Back
or Close control, or `press_key` `home` to leave an unknown state; `stop_app`
and `open_url` are unavailable; touch needs iOS 17+. `no_device`,
`device_offline`, `approval_denied`, `approval_timeout`,
`approval_requires_app` mean the phone is not available: record `blocked` with
the code and stop.

## 9. Test plans (informational)

Test plans group cases into a named release scope. You can read them but you
do not modify plans during a run:

    multica test plan list --output json
    multica test plan get <plan-id> --output json

## Hard rules (never violate)

- **Capability boundary**: only use capability keys from `capability list`. Do
  not use `adb`, `xcrun simctl`, `which chromium`, or any host probe.
- **Blocked ≠ failed**: if you cannot run a case, set `blocked` with a note
  explaining why. `failed` means the test ran but the product did not behave
  correctly.
- **Record immediately**: set the result before moving to the next case. Do not
  accumulate results and flush them at the end.
- **Evidence on failure**: upload at least one screenshot or log for every
  `failed` or `blocked` case where the environment permits it.
- **Frozen snapshot is authoritative**: do not re-read the live test case
  record mid-run. The run has the snapshot it was dispatched with.
- **One task, one case**: record only against your own `run_case_id`; the
  round completes on its own.
- **One defect per scenario**: do not open duplicate defect issues for the
  same reproduction path.
