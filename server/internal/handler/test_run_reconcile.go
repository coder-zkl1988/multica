package handler

import (
	"context"
	"log/slog"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// testRunReconcileGrace is how long an ended task's case is left to the
// daemon's /fail hook before the reconciler settles it instead.
const testRunReconcileGrace = time.Minute

// ReconcileTestRunCasesWithEndedTasks settles open run cases whose agent task
// failed or was cancelled without the /fail hook seeing it — the runtime
// sweeper, orphan recovery and cancellations end a task directly. Without it
// such a case stays "running" and its round never completes, which also
// strands a regression autopilot run. Each case is blocked (no result was
// recorded) and its round released and converged like the hook does. It
// returns how many cases it settled.
func (h *Handler) ReconcileTestRunCasesWithEndedTasks(ctx context.Context, limit int32) (int, error) {
	rows, err := h.Queries.ListTestRunCasesWithEndedTask(ctx, db.ListTestRunCasesWithEndedTaskParams{
		GraceSeconds: testRunReconcileGrace.Seconds(),
		RowLimit:     limit,
	})
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return settled, ctx.Err()
		}
		run, err := h.Queries.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: row.RunID, WorkspaceID: row.WorkspaceID})
		if err != nil {
			slog.Warn("test run reconcile: load round failed", "run_id", uuidToString(row.RunID), "error", err)
			continue
		}
		rc, err := h.Queries.GetTestRunCaseInWorkspace(ctx, db.GetTestRunCaseInWorkspaceParams{ID: row.RunCaseID, WorkspaceID: row.WorkspaceID})
		if err != nil {
			slog.Warn("test run reconcile: load case failed", "run_case_id", uuidToString(row.RunCaseID), "error", err)
			continue
		}
		if err := h.blockTestRunCaseAndAdvance(ctx, run, rc, endedTaskNotes(row)); err != nil {
			slog.Warn("test run reconcile: settle case failed", "run_case_id", uuidToString(row.RunCaseID), "error", err)
			continue
		}
		settled++
	}
	return settled, nil
}

func endedTaskNotes(row db.ListTestRunCasesWithEndedTaskRow) string {
	notes := "the agent task failed before recording a result"
	if row.TaskStatus == "cancelled" {
		notes = "the agent task was cancelled before recording a result"
	}
	reason := ""
	if row.TaskError.Valid {
		reason = strings.TrimSpace(row.TaskError.String)
	}
	if reason == "" && row.TaskFailureReason.Valid {
		reason = strings.TrimSpace(row.TaskFailureReason.String)
	}
	if reason != "" {
		notes += ": " + reason
	}
	return notes
}
