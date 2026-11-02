package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AutopilotTestRunLauncher is what the test_run execution mode needs from the
// test-run domain, which lives in the handler package: build a round from the
// autopilot's plan and dispatch it to the admitted agent. The handler that
// owns both wires it; without one the mode fails its runs with a reason.
type AutopilotTestRunLauncher interface {
	LaunchAutopilotTestRun(ctx context.Context, in AutopilotTestRunInput) (AutopilotTestRunResult, error)
}

type AutopilotTestRunInput struct {
	Autopilot   db.Autopilot
	Run         db.AutopilotRun
	Agent       db.Agent
	Attribution attribution.Result
}

type AutopilotTestRunResult struct {
	TestRunID   pgtype.UUID
	FirstTaskID pgtype.UUID
	Cases       int
	// Blocked: the round exists but was parked (no test host, no runtime
	// with the capability); Reason says what to fix.
	Blocked bool
	Reason  string
}

// AttributionTaskParams exposes the task-row attribution columns derived from
// a resolved attribution, for the task rows the handler package inserts on an
// autopilot's behalf (the cases of a test round).
func AttributionTaskParams(attr attribution.Result) (source pgtype.Text, evidenceKind pgtype.Text, evidenceRef pgtype.UUID) {
	source, _, evidenceKind, evidenceRef = attributionCreateParams(attr)
	return source, evidenceKind, evidenceRef
}

// dispatchTestRun is the test_run execution mode: the same admission and
// attribution as run_only, then a test round from the autopilot's plan
// instead of a single task. The round's per-case tasks carry the run's
// attribution; the run turns running with the round's id and completes when
// the round converges (SettleAutopilotTestRun).
func (s *AutopilotService) dispatchTestRun(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, actorUserID pgtype.UUID) error {
	if s.TestRuns == nil {
		return errors.New("the test_run execution mode is not available on this server")
	}
	if !ap.TestPlanID.Valid {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, "no test plan configured"), code: dispatch.ReasonTargetUnavailable}
	}
	agent, autopilotAttr, err := s.admitAutopilotAgent(ctx, ap, run, actorUserID)
	if err != nil {
		return err
	}
	result, err := s.TestRuns.LaunchAutopilotTestRun(ctx, AutopilotTestRunInput{Autopilot: ap, Run: *run, Agent: agent, Attribution: autopilotAttr})
	if err != nil {
		return fmt.Errorf("launch test run: %w", err)
	}
	if result.Blocked {
		// The parked round stays attached to the run so the reason is one
		// click away; the run itself is a skip, not a failure of the agent.
		if _, err := s.Queries.SetAutopilotRunTestRun(ctx, db.SetAutopilotRunTestRunParams{ID: run.ID, TestRunID: result.TestRunID}); err != nil {
			slog.Warn("failed to attach the blocked test run to the autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
		}
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, result.Reason), code: dispatch.ReasonTargetUnavailable}
	}
	updated, err := s.Queries.UpdateAutopilotRunTestRunRunning(ctx, db.UpdateAutopilotRunTestRunRunningParams{
		ID:        run.ID,
		TestRunID: result.TestRunID,
		TaskID:    result.FirstTaskID,
	})
	if err != nil {
		slog.Warn("failed to update run with test_run_id", "run_id", util.UUIDToString(run.ID), "error", err)
	} else {
		*run = updated
	}
	slog.Info("autopilot dispatched (test_run)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"test_run_id", util.UUIDToString(result.TestRunID),
		"cases", result.Cases,
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

// SettleAutopilotTestRun closes the autopilot run that launched a test round
// once the round is terminal: completed rounds complete the run with the
// result counts (a round where every case failed is still a completed round;
// the counts carry the verdict), aborted and blocked rounds fail it with the
// round's error. A round no autopilot launched is a no-op.
func (s *AutopilotService) SettleAutopilotTestRun(ctx context.Context, testRun db.TestRun, results map[string]int64) {
	apRun, err := s.Queries.GetAutopilotRunByTestRun(ctx, testRun.ID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("lookup autopilot run for test run failed", "test_run_id", util.UUIDToString(testRun.ID), "error", err)
		}
		return
	}
	switch apRun.Status {
	case "completed", "failed", "skipped":
		return
	}
	ap, err := s.Queries.GetAutopilot(ctx, apRun.AutopilotID)
	if err != nil {
		return
	}
	workspaceID := util.UUIDToString(ap.WorkspaceID)
	switch testRun.Status {
	case "completed":
		resultJSON, _ := json.Marshal(map[string]any{
			"test_run_id": util.UUIDToString(testRun.ID),
			"status":      testRun.Status,
			"results":     results,
		})
		updated, err := s.completeAutopilotRun(ctx, autopilotRunCompletion{ID: apRun.ID, Result: resultJSON})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("failed to complete autopilot run from test run", "run_id", util.UUIDToString(apRun.ID), "error", err)
			}
			return
		}
		s.publishRunDone(workspaceID, updated, "completed")
	case "aborted", "blocked":
		reason := "the test round was " + testRun.Status
		if testRun.Error.Valid && testRun.Error.String != "" {
			reason += ": " + testRun.Error.String
		}
		code := dispatch.ReasonInternalError
		if testRun.Status == "blocked" {
			code = dispatch.ReasonTargetUnavailable
		}
		updated, err := s.failAutopilotRun(ctx, autopilotRunFailure{
			ID:            apRun.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
			ReasonCode:    pgtype.Text{String: string(code), Valid: true},
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("failed to fail autopilot run from test run", "run_id", util.UUIDToString(apRun.ID), "error", err)
			}
			return
		}
		s.publishRunDone(workspaceID, updated, "failed")
		s.captureAutopilotRunFailed(ap, updated, apRun.Source, reason)
	}
}
