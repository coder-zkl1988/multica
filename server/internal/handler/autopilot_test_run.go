package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// autopilotTestRunLauncher is the handler-side half of the autopilot
// test_run execution mode (testing-center M6, TS-033): the service admits
// the agent and resolves the run's attribution; this builds a round from
// the autopilot's plan and pushes it through the same dispatch core the
// run page uses, so capability resolution, the test-host gate, the
// parallelism cap and the live-frame lease tags all apply unchanged.
type autopilotTestRunLauncher struct{ h *Handler }

func (l autopilotTestRunLauncher) LaunchAutopilotTestRun(ctx context.Context, in service.AutopilotTestRunInput) (service.AutopilotTestRunResult, error) {
	h := l.h
	ap := in.Autopilot
	wsUUID := ap.WorkspaceID
	workspaceID := uuidToString(wsUUID)

	plan, err := h.Queries.GetTestPlanInWorkspace(ctx, db.GetTestPlanInWorkspaceParams{ID: ap.TestPlanID, WorkspaceID: wsUUID})
	if err != nil {
		return service.AutopilotTestRunResult{}, fmt.Errorf("the autopilot's test plan no longer exists")
	}
	// The same selection a person's round of this plan gets: the listed
	// cases, then every active case its rule matches today — so a nightly
	// regression picks up cases written since the plan was built.
	sources, err := h.planRoundCases(ctx, plan)
	if err != nil {
		return service.AutopilotTestRunResult{}, err
	}
	if len(sources) == 0 {
		return service.AutopilotTestRunResult{}, errors.New("the test plan selects no cases")
	}
	cases, reposByCase, _, err := h.loadCasesForRun(ctx, wsUUID, planRoundCaseIDs(sources))
	if err != nil {
		return service.AutopilotTestRunResult{}, err
	}

	// The requester travels in every case task's context and is parsed back
	// by the follow-up dispatch of a capped round, so it must be a member id:
	// the accountable human, else the originator, else the autopilot's creator.
	requester := firstValidUUID(in.Attribution.AccountableUserID, in.Attribution.UserID, ap.CreatedByID)
	run, err := h.createTestRunWithCases(ctx, db.CreateTestRunParams{
		WorkspaceID:       wsUUID,
		ProjectID:         plan.ProjectID,
		PlanID:            plan.ID,
		Title:             fmt.Sprintf("%s · %s", ap.Title, time.Now().UTC().Format("2006-01-02 15:04 UTC")),
		ExecutorType:      "agent",
		ExecutorID:        in.Agent.ID,
		CapabilityBinding: []byte("{}"),
		Status:            "pending",
		CreatedBy:         requester,
		Parallelism:       ap.TestRunParallelism,
	}, cases, reposByCase)
	if err != nil {
		return service.AutopilotTestRunResult{}, fmt.Errorf("create the test run: %w", err)
	}
	h.publish(protocol.EventTestRunUpdated, workspaceID, "system", "", map[string]any{"test_run": testRunToResponse(run)})

	source, evidenceKind, evidenceRef := service.AttributionTaskParams(in.Attribution)
	outcome, err := h.dispatchTestRunCases(ctx, testRunDispatchInput{
		run:          run,
		agent:        in.Agent,
		wsUUID:       wsUUID,
		workspaceID:  workspaceID,
		originator:   in.Attribution.UserID,
		accountable:  in.Attribution.AccountableUserID,
		source:       source,
		evidenceKind: evidenceKind,
		evidenceRef:  evidenceRef,
		requesterID:  uuidToString(requester),
		prompt:       ap.Description.String,
		actorType:    "system",
	})
	if err != nil {
		var undispatchable *errTestRunUndispatchable
		if errors.As(err, &undispatchable) {
			// The round exists; park it with the reason rather than leaving a
			// pending round nobody will start.
			parked, perr := h.parkTestRun(ctx, testRunDispatchInput{run: run, wsUUID: wsUUID, workspaceID: workspaceID, actorType: "system"}, "", undispatchable.reason)
			if perr == nil {
				return service.AutopilotTestRunResult{TestRunID: parked.run.ID, Cases: len(cases), Blocked: true, Reason: undispatchable.reason}, nil
			}
		}
		return service.AutopilotTestRunResult{TestRunID: run.ID}, err
	}
	return service.AutopilotTestRunResult{
		TestRunID:   outcome.run.ID,
		FirstTaskID: outcome.firstTaskID,
		Cases:       outcome.cases,
		Blocked:     outcome.blocked,
		Reason:      outcome.reason,
	}, nil
}

func firstValidUUID(candidates ...pgtype.UUID) pgtype.UUID {
	for _, c := range candidates {
		if c.Valid {
			return c
		}
	}
	return pgtype.UUID{}
}

// settleAutopilotTestRun tells the autopilot service a round is terminal so
// the run that launched it (if any) closes with the round's result counts.
func (h *Handler) settleAutopilotTestRun(ctx context.Context, q *db.Queries, run db.TestRun) {
	if h.AutopilotService == nil {
		return
	}
	results := map[string]int64{}
	if rows, err := q.CountTestRunResults(ctx, db.CountTestRunResultsParams{RunID: run.ID, WorkspaceID: run.WorkspaceID}); err == nil {
		results = buildResultCounts(rows)
	} else {
		slog.Warn("count test run results for autopilot settle failed", "run_id", uuidToString(run.ID), "error", err)
	}
	h.AutopilotService.SettleAutopilotTestRun(ctx, run, results)
}

// resolveAutopilotTestRunConfig validates the test_run mode's configuration:
// the plan must exist in the workspace, and the autopilot's project, when
// set, must be the plan's — otherwise the plan's project is adopted so the
// round's tasks get the project's repositories. The other modes store
// neither field.
func (h *Handler) resolveAutopilotTestRunConfig(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, mode string, planRef *string, parallelism *int32, projectID *pgtype.UUID) (pgtype.UUID, pgtype.Int4, bool) {
	if mode != "test_run" {
		return pgtype.UUID{}, pgtype.Int4{}, true
	}
	if planRef == nil || strings.TrimSpace(*planRef) == "" {
		writeError(w, http.StatusBadRequest, "test_plan_id is required for the test_run execution mode")
		return pgtype.UUID{}, pgtype.Int4{}, false
	}
	planUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(*planRef), "test_plan_id")
	if !ok {
		return pgtype.UUID{}, pgtype.Int4{}, false
	}
	plan, err := h.Queries.GetTestPlanInWorkspace(r.Context(), db.GetTestPlanInWorkspaceParams{ID: planUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "test plan not found")
		return pgtype.UUID{}, pgtype.Int4{}, false
	}
	if projectID.Valid && *projectID != plan.ProjectID {
		writeError(w, http.StatusBadRequest, "the test plan belongs to another project than the autopilot")
		return pgtype.UUID{}, pgtype.Int4{}, false
	}
	*projectID = plan.ProjectID
	var cap pgtype.Int4
	if parallelism != nil {
		if *parallelism < 1 || *parallelism > maxTestRunParallelism {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("test_run_parallelism must be between 1 and %d", maxTestRunParallelism))
			return pgtype.UUID{}, pgtype.Int4{}, false
		}
		cap = pgtype.Int4{Int32: *parallelism, Valid: true}
	}
	return plan.ID, cap, true
}

func isValidAutopilotExecutionMode(mode string) bool {
	switch mode {
	case "create_issue", "run_only", "test_run":
		return true
	}
	return false
}
