# Running tests — source map

Every claim in `SKILL.md` traces to a line below. Re-derive against the current
tree before trusting any line number; the behavior is the contract, the line is
a pointer.

## Capability discovery

| Behavior | Source |
| --- | --- |
| `GET /api/test-runs/{id}/capabilities` returns `capability_binding` with `daemon_id`, `runtime_id`, and `resolved` map | `server/internal/handler/test_capability.go` (`ListTestRunCapabilities`) |
| Capability binding is frozen at dispatch time in `test_run.capability_binding` | `server/internal/handler/test_run_dispatch.go:182` (`UpdateTestRun` after `CreateQuickCreateTask`) |
| The binding is resolved only among capabilities reported by the executing agent's own daemon | `server/internal/handler/test_capability.go` (`resolveRunCapabilities`, `daemonID` parameter) |
| Daemons report capabilities on `POST /api/daemon/runtimes/{id}/capabilities`, unsolicited after registration and when a heartbeat carries `pending_capability_scan` | `server/internal/daemon/daemon.go` (`reportRuntimeCapabilities`), `server/internal/handler/daemon.go` (`processHeartbeat`) |
| Resolved map shape: `{kind → capability_key}` | `server/internal/handler/test_capability.go` (`TestRunCapabilityBindingResponse`) |
| `multica test capability list --run <run-id>` calls `GET /api/test-runs/{id}/capabilities` | `server/cmd/multica/cmd_testrun.go` (`runTestCapList`) |
| `multica test run get <run-id>` calls `GET /api/test-runs/{id}` | `server/cmd/multica/cmd_testrun.go` (`runTestRunGet`) |

## Result recording

| Behavior | Source |
| --- | --- |
| `PUT /api/test-run-cases/{id}/result` with `{result, notes, step_results, evidence, duration_ms}` | `server/internal/handler/test_run.go` (`UpdateTestRunCaseResultRequest`, `UpdateTestRunCaseResult`) |
| Valid result values: `passed`, `failed`, `blocked`, `skipped` | `server/internal/handler/test_run.go` (`validTestRunCaseResults`) |
| `multica test result set <run-case-id> --result …` calls `PUT /api/test-run-cases/{id}/result` | `server/cmd/multica/cmd_testrun.go` (`runTestResultSet`) |
| `--note` is serialized to the `notes` JSON field (not `note`) | `server/cmd/multica/cmd_testrun.go` (`runTestResultSet`) |
| `--step-results` is validated as JSON client-side before sending | `server/cmd/multica/cmd_testrun.go` (`runTestResultSet`) |

## Evidence upload

| Behavior | Source |
| --- | --- |
| Evidence upload gate: `X-Actor-Source == "task_token"`, `X-Task-ID` must equal run's `agent_task_id`, authenticated agent must be run's executor | `server/internal/handler/file.go` (`UploadTestEvidence`) |
| Multipart form fields: `file`, `test_run_case_id`, `kind` | `server/internal/handler/file.go` (`UploadTestEvidence`) |
| `multica test evidence add <run-case-id> --file … --kind …` posts multipart to `/api/upload-file` | `server/cmd/multica/cmd_testrun.go` (`runTestEvidenceAdd`) |

## Defect creation

| Behavior | Source |
| --- | --- |
| `POST /api/test-run-cases/{id}/defect` with `{title, note}` creates a linked issue | `server/internal/handler/test_run.go` (`OpenTestRunCaseDefectRequest`, `OpenTestRunCaseDefect`) |
| `multica test defect open <run-case-id> --title …` calls the defect endpoint | `server/cmd/multica/cmd_testrun.go` (`runTestDefectOpen`) |

## Run lifecycle

| Behavior | Source |
| --- | --- |
| `POST /api/test-runs/{id}/start` transitions `pending` → `running` | `server/internal/handler/test_run.go` (`StartTestRun`) |
| `multica test run start <run-id>` calls the start endpoint | `server/cmd/multica/cmd_testrun.go` (`runTestRunStart`) |
| Run status values: `pending`, `running`, `passed`, `failed`, `blocked`, `aborted` | `server/internal/handler/test_run.go` (`testRunToResponse`) |

## Agent prompt contract

| Behavior | Source |
| --- | --- |
| Agent is told to call `multica test run get` then `multica test capability list` before executing cases | `server/internal/daemon/prompt.go` (`buildTestRunPrompt`) |
| Agent is told to use only the capability keys returned by `capability list`, never probe for adb/browser | `server/internal/daemon/prompt.go` (`buildTestRunPrompt`) |
| `blocked` and `failed` are distinguished in the prompt | `server/internal/daemon/prompt.go` (`buildTestRunPrompt`) |

## Test plans (informational)

| Behavior | Source |
| --- | --- |
| `GET /api/test-plans` returns list of plans | `server/cmd/server/router.go` (`/api/test-plans` group) |
| `GET /api/test-plans/{id}` returns single plan | `server/cmd/server/router.go` (`/api/test-plans/{id}` group) |
| `multica test plan list` and `multica test plan get <id>` call those endpoints | `server/cmd/multica/cmd_testrun.go` (`runTestPlanList`, `runTestPlanGet`) |

## Verification command

```bash
cd server && rg -n \
  'runTestRunGet|runTestRunStart|runTestResultSet|runTestEvidenceAdd|runTestDefectOpen|runTestCapList|runTestPlanList|runTestPlanGet|testRunAPIPath|testRunCaseAPIPath|testPlanAPIPath|uploadTestRunCaseEvidence' \
  cmd/multica/cmd_testrun.go
rg -n 'UpdateTestRunCaseResult|OpenTestRunCaseDefect|StartTestRun|ListTestRunCapabilities|UploadTestEvidence' \
  internal/handler/test_run.go internal/handler/test_capability.go internal/handler/file.go
rg -n 'buildTestRunPrompt' internal/daemon/prompt.go
```
