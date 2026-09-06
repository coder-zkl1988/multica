package handler

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

// completeTestRunTask closes what a finished task owned. A per-case task
// settles its case (the CLI-recorded result stands; otherwise the closing
// marker, otherwise `blocked`) and completes the round once no case is left
// pending or running. A whole-round task keeps the pre-M2 behaviour: only the
// agent's declared status decides — scanning the transcript for the word
// "blocked" is the design_restore mistake.
func (h *Handler) completeTestRunTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, output string) error {
	run, isRun, err := h.testRunForTaskWith(ctx, q, task)
	if !isRun {
		return err
	}
	rc, isCase, err := h.testRunCaseForTask(ctx, q, task, run)
	if err != nil {
		return err
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
		_, err = q.UpdateTestRun(ctx, params)
		return err
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
		if _, err := q.UpdateTestRunCaseResult(ctx, db.UpdateTestRunCaseResultParams{
			ID:             rc.ID,
			WorkspaceID:    run.WorkspaceID,
			Result:         pgtype.Text{String: result, Valid: true},
			Notes:          pgtype.Text{String: notes, Valid: true},
			ExecutedByType: pgtype.Text{String: "agent", Valid: true},
			ExecutedByID:   run.ExecutorID,
			ExecutedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			return err
		}
	}
	return h.convergeTestRun(ctx, q, run)
}

// convergeTestRun completes the round once every case is terminal. A round
// where every case legitimately failed is still a completed round; the
// per-case results carry the verdict.
func (h *Handler) convergeTestRun(ctx context.Context, q *db.Queries, run db.TestRun) error {
	pending, err := q.CountPendingTestRunCases(ctx, db.CountPendingTestRunCasesParams{RunID: run.ID, WorkspaceID: run.WorkspaceID})
	if err != nil {
		return err
	}
	if pending > 0 || run.Status == "completed" || run.Status == "aborted" {
		return nil
	}
	_, err = q.UpdateTestRun(ctx, db.UpdateTestRunParams{
		ID:          run.ID,
		WorkspaceID: run.WorkspaceID,
		Status:      pgtype.Text{String: "completed", Valid: true},
		CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	return err
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
		_, err = h.Queries.UpdateTestRun(ctx, db.UpdateTestRunParams{
			ID:          run.ID,
			WorkspaceID: run.WorkspaceID,
			Status:      pgtype.Text{String: "aborted", Valid: true},
			CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			Error:       pgtype.Text{String: req.Error, Valid: req.Error != ""},
		})
		return err
	}
	if !isTerminalRunCaseResult(rc.Result) {
		notes := "the agent task failed before recording a result"
		if strings.TrimSpace(req.Error) != "" {
			notes += ": " + strings.TrimSpace(req.Error)
		}
		if _, err := h.Queries.UpdateTestRunCaseResult(ctx, db.UpdateTestRunCaseResultParams{
			ID:             rc.ID,
			WorkspaceID:    run.WorkspaceID,
			Result:         pgtype.Text{String: "blocked", Valid: true},
			Notes:          pgtype.Text{String: notes, Valid: true},
			ExecutedByType: pgtype.Text{String: "agent", Valid: true},
			ExecutedByID:   run.ExecutorID,
			ExecutedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			return err
		}
	}
	return h.convergeTestRun(ctx, h.Queries, run)
}
