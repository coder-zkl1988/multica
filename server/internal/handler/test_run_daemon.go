package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// testRunResultMarker prefixes the agent's closing summary on a whole-round
// task (pre-M2 shape). Per-case results are written through the authenticated
// CLI, so a missing marker costs a status line and never a result.
const testRunResultMarker = "TEST_RUN_RESULT_JSON:"

// testRunCaseResultMarker prefixes the closing line of a per-case task. The
// CLI write is authoritative; the marker is the fallback that keeps a case
// from ending as "running" when the agent reported but forgot the CLI call.
const testRunCaseResultMarker = "TEST_RUN_CASE_RESULT_JSON:"

type testRunResultSummary struct {
	Status   string   `json:"status"`
	Summary  string   `json:"summary"`
	Blockers []string `json:"blockers"`
}

type testRunCaseResultSummary struct {
	Result  string `json:"result"`
	Summary string `json:"summary"`
}

func parseMarkerJSON(output, marker string, out any) bool {
	idx := strings.LastIndex(output, marker)
	if idx < 0 {
		return false
	}
	tail := strings.TrimSpace(output[idx+len(marker):])
	tail = strings.TrimPrefix(tail, "```json")
	tail = strings.TrimPrefix(tail, "```")
	if fence := strings.Index(tail, "```"); fence >= 0 {
		tail = tail[:fence]
	}
	return json.Unmarshal([]byte(strings.TrimSpace(tail)), out) == nil
}

func parseTestRunResultSummary(output string) (testRunResultSummary, bool) {
	var summary testRunResultSummary
	if !parseMarkerJSON(output, testRunResultMarker, &summary) {
		return testRunResultSummary{}, false
	}
	return summary, true
}

func parseTestRunCaseResultSummary(output string) (testRunCaseResultSummary, bool) {
	var summary testRunCaseResultSummary
	if !parseMarkerJSON(output, testRunCaseResultMarker, &summary) {
		return testRunCaseResultSummary{}, false
	}
	switch summary.Result {
	case "passed", "failed", "blocked", "skipped":
		return summary, true
	default:
		return testRunCaseResultSummary{}, false
	}
}

// testRunContextForTask decodes a task's context; ok is false for every task
// that is not a test round.
func testRunContextForTask(task db.AgentTaskQueue) (service.TestRunContext, bool) {
	var runCtx service.TestRunContext
	if err := json.Unmarshal(task.Context, &runCtx); err != nil || runCtx.Type != service.TestRunContextType {
		return service.TestRunContext{}, false
	}
	return runCtx, true
}

// testRunForTask resolves the round a task belongs to. Per-case tasks name
// the run in their context; the pre-M2 whole-round task is found through
// test_run.agent_task_id.
func (h *Handler) testRunForTask(ctx context.Context, task db.AgentTaskQueue) (db.TestRun, bool, error) {
	return h.testRunForTaskWith(ctx, h.Queries, task)
}

func (h *Handler) testRunForTaskWith(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (db.TestRun, bool, error) {
	runCtx, ok := testRunContextForTask(task)
	if !ok {
		return db.TestRun{}, false, nil
	}
	wsUUID, err := util.ParseUUID(runCtx.WorkspaceID)
	if err != nil {
		return db.TestRun{}, false, err
	}
	if runCtx.RunID != "" {
		runUUID, err := util.ParseUUID(runCtx.RunID)
		if err != nil {
			return db.TestRun{}, false, err
		}
		run, err := q.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: runUUID, WorkspaceID: wsUUID})
		if err != nil {
			return db.TestRun{}, false, err
		}
		return run, true, nil
	}
	run, err := q.GetTestRunByAgentTask(ctx, db.GetTestRunByAgentTaskParams{AgentTaskID: task.ID, WorkspaceID: wsUUID})
	if err != nil {
		return db.TestRun{}, false, err
	}
	return run, true, nil
}

// testRunCaseForTask resolves the case a per-case task executes; ok is false
// for whole-round tasks.
func (h *Handler) testRunCaseForTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, run db.TestRun) (db.TestRunCase, bool, error) {
	runCtx, ok := testRunContextForTask(task)
	if !ok || runCtx.RunCaseID == "" {
		return db.TestRunCase{}, false, nil
	}
	rc, err := q.GetTestRunCaseByAgentTask(ctx, db.GetTestRunCaseByAgentTaskParams{AgentTaskID: task.ID, WorkspaceID: run.WorkspaceID})
	if err != nil {
		return db.TestRunCase{}, false, err
	}
	return rc, true, nil
}

func isTerminalRunCaseResult(result string) bool {
	switch result {
	case "passed", "failed", "blocked", "skipped":
		return true
	default:
		return false
	}
}

// markTestRunRunning flips the round to running when its first case task
// starts, and the case itself to running. Later case tasks find the round
// already running and only touch their own case.
func (h *Handler) markTestRunRunning(ctx context.Context, task db.AgentTaskQueue) error {
	run, isRun, err := h.testRunForTask(ctx, task)
	if !isRun {
		return err
	}
	if run.Status == "pending" {
		if _, err := h.Queries.UpdateTestRun(ctx, db.UpdateTestRunParams{
			ID:          run.ID,
			WorkspaceID: run.WorkspaceID,
			Status:      pgtype.Text{String: "running", Valid: true},
			StartedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			return err
		}
	}
	rc, isCase, err := h.testRunCaseForTask(ctx, h.Queries, task, run)
	if !isCase || err != nil {
		return err
	}
	if rc.Result != "pending" {
		return nil
	}
	_, err = h.Queries.UpdateTestRunCaseResult(ctx, db.UpdateTestRunCaseResultParams{
		ID:             rc.ID,
		WorkspaceID:    run.WorkspaceID,
		Result:         pgtype.Text{String: "running", Valid: true},
		ExecutedByType: pgtype.Text{String: "agent", Valid: true},
		ExecutedByID:   run.ExecutorID,
	})
	return err
}

// testRunSettlement is what a task hook changed: the case it settled and the
// round it completed, either possibly nil. The hooks run inside the task's
// completion transaction, so the caller publishes them once it commits.
type testRunSettlement struct {
	runCase *db.TestRunCase
	run     *db.TestRun
}

// publishTestRunSettlement tells open round pages and issue test blocks what a
// hook settled; without it a case finished by its task stays "running" on
// screen until a manual refresh.
func (h *Handler) publishTestRunSettlement(workspaceID string, settled testRunSettlement) {
	if settled.runCase != nil {
		h.publish(protocol.EventTestRunCaseUpdated, workspaceID, "system", "", map[string]any{
			"test_run_case": testRunCaseToResponse(*settled.runCase),
			"run_id":        uuidToString(settled.runCase.RunID),
		})
	}
	if settled.run != nil {
		h.publish(protocol.EventTestRunUpdated, workspaceID, "system", "", map[string]any{"test_run": testRunToResponse(*settled.run)})
	}
}

// settleOpenTestRunCase is the hooks' fallback settle. It only writes a case
// nobody recorded yet — an agent's CLI verdict that committed after the hook
// read the case is never overwritten — and, in an aborted round, leaves
// executed_at unset so the settle never becomes the case's latest result.
func settleOpenTestRunCase(ctx context.Context, q *db.Queries, run db.TestRun, rc db.TestRunCase, result, notes string) (*db.TestRunCase, error) {
	params := db.SettleOpenTestRunCaseParams{
		ID:             rc.ID,
		WorkspaceID:    run.WorkspaceID,
		Result:         result,
		Notes:          notes,
		ExecutedByType: pgtype.Text{String: "agent", Valid: true},
		ExecutedByID:   run.ExecutorID,
	}
	if run.Status != "aborted" {
		params.ExecutedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	settled, err := q.SettleOpenTestRunCase(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &settled, nil
}

// completeTestRunTask closes what a finished task owned. A per-case task
// settles its case (the CLI-recorded result stands; otherwise the closing
// marker, otherwise `blocked`) and completes the round once no case is left
// pending or running. A whole-round task keeps the pre-M2 behaviour: only the
// agent's declared status decides — scanning the transcript for the word
// "blocked" is the design_restore mistake.
func (h *Handler) completeTestRunTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, output string) (testRunSettlement, error) {
	var settled testRunSettlement
	run, isRun, err := h.testRunForTaskWith(ctx, q, task)
	if !isRun {
		return settled, err
	}
	rc, isCase, err := h.testRunCaseForTask(ctx, q, task, run)
	if err != nil {
		return settled, err
	}
	if !isCase {
		summary, _ := parseTestRunResultSummary(output)
		status := "completed"
		if summary.Status == "blocked" {
			status = "blocked"
		}
		params := db.UpdateTestRunParams{
			ID:          run.ID,
			WorkspaceID: run.WorkspaceID,
			Status:      pgtype.Text{String: status, Valid: true},
			CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}
		if len(summary.Blockers) > 0 {
			params.Error = pgtype.Text{String: strings.Join(summary.Blockers, "; "), Valid: true}
		}
		updated, err := q.UpdateTestRun(ctx, params)
		if err == nil {
			settled.run = &updated
		}
		return settled, err
	}
	if !isTerminalRunCaseResult(rc.Result) {
		result := "blocked"
		notes := "the agent finished without recording a result"
		if summary, ok := parseTestRunCaseResultSummary(output); ok {
			result = summary.Result
			if strings.TrimSpace(summary.Summary) != "" {
				notes = summary.Summary
			}
		}
		// The closing marker settles a case the agent never recorded. That
		// is fine for a failure or a blocker — they never overstate — but a
		// pass declared only in prose carries no step verdicts and no checked
		// evidence: the unverifiable pass the verdict rules exist to stop.
		// It settles as blocked so the case is retried, not counted.
		if result == "passed" {
			result = "blocked"
			notes = "the agent declared the case passed but never recorded the verdict with step results and evidence (`multica test result set`), so the pass is not accepted. Agent summary: " + notes
		}
		if settled.runCase, err = settleOpenTestRunCase(ctx, q, run, rc, result, notes); err != nil {
			return settled, err
		}
	}
	if err := h.dispatchNextTestRunCases(ctx, q, run); err != nil {
		// The settled case must not be lost because the next one could not be
		// queued; the round stays running and a later hook retries.
		slog.Warn("dispatch next test run cases failed", "run_id", uuidToString(run.ID), "error", err)
	}
	settled.run, err = h.convergeTestRun(ctx, q, run)
	return settled, err
}

// convergeTestRun completes the round once every case is terminal. A round
// where every case legitimately failed is still a completed round; the
// per-case results carry the verdict. The completion is conditional on the
// round still being open, so of two racing convergers (an HTTP write and a
// case task's hook) only one completes it, and an abort that committed in
// between is never undone. It returns the completed round when this call
// completed it, nil otherwise — so exactly one caller settles the autopilot
// run and publishes the change.
func (h *Handler) convergeTestRun(ctx context.Context, q *db.Queries, run db.TestRun) (*db.TestRun, error) {
	if run.Status == "completed" || run.Status == "aborted" {
		return nil, nil
	}
	pending, err := q.CountPendingTestRunCases(ctx, db.CountPendingTestRunCasesParams{RunID: run.ID, WorkspaceID: run.WorkspaceID})
	if err != nil {
		return nil, err
	}
	if pending > 0 {
		return nil, nil
	}
	completed, err := q.CompleteOpenTestRun(ctx, db.CompleteOpenTestRunParams{ID: run.ID, WorkspaceID: run.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.settleAutopilotTestRun(ctx, q, completed)
	return &completed, nil
}

// updateTestRunFromAgentFailure records an agent task that died. A per-case
// task blocks its case (the agent never got to record anything) and lets the
// round converge, so one crashed case does not abort the others; a whole-round
// task aborts the round as before.
func (h *Handler) updateTestRunFromAgentFailure(ctx context.Context, task db.AgentTaskQueue, req TaskFailRequest) error {
	run, isRun, err := h.testRunForTask(ctx, task)
	if !isRun {
		return err
	}
	rc, isCase, err := h.testRunCaseForTask(ctx, h.Queries, task, run)
	if err != nil {
		return err
	}
	if !isCase {
		aborted, err := h.Queries.UpdateTestRun(ctx, db.UpdateTestRunParams{
			ID:          run.ID,
			WorkspaceID: run.WorkspaceID,
			Status:      pgtype.Text{String: "aborted", Valid: true},
			CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			Error:       pgtype.Text{String: req.Error, Valid: req.Error != ""},
		})
		if err != nil {
			return err
		}
		h.settleAutopilotTestRun(ctx, h.Queries, aborted)
		return nil
	}
	var settled testRunSettlement
	if !isTerminalRunCaseResult(rc.Result) {
		notes := "the agent task failed before recording a result"
		if strings.TrimSpace(req.Error) != "" {
			notes += ": " + strings.TrimSpace(req.Error)
		}
		if settled.runCase, err = settleOpenTestRunCase(ctx, h.Queries, run, rc, "blocked", notes); err != nil {
			return err
		}
	}
	if err := h.dispatchNextTestRunCases(ctx, h.Queries, run); err != nil {
		slog.Warn("dispatch next test run cases failed", "run_id", uuidToString(run.ID), "error", err)
	}
	settled.run, err = h.convergeTestRun(ctx, h.Queries, run)
	h.publishTestRunSettlement(uuidToString(run.WorkspaceID), settled)
	return err
}
