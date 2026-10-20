package handler

// Test plan and execution-run HTTP surface.
//
// Every handler here is registered in server/cmd/server/router.go. Agent
// dispatch lives in test_run_dispatch.go, because it has to resolve execution
// capabilities before anything is queued.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// TestPlanResponse is the outbound representation of a test_plan row.
type TestPlanResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	// Selector is the plan's rule for picking cases beside its list; the
	// zero value is no rule.
	Selector  TestPlanSelector `json:"selector"`
	CreatedBy *string          `json:"created_by"`
	CreatedAt string           `json:"created_at"`
	UpdatedAt string           `json:"updated_at"`
}

// TestPlanCaseResponse is the outbound representation of a test_plan_case row.
type TestPlanCaseResponse struct {
	PlanID     string `json:"plan_id"`
	TestCaseID string `json:"test_case_id"`
	Position   int32  `json:"position"`
	CreatedAt  string `json:"created_at"`
}

// TestRunResponse is the outbound representation of a test_run row. The
// ExecutionStatus and ResultCounts fields are only populated on the single-item
// GET — list calls omit them to keep the payload small.
type TestRunResponse struct {
	ID                string         `json:"id"`
	WorkspaceID       string         `json:"workspace_id"`
	ProjectID         string         `json:"project_id"`
	PlanID            *string        `json:"plan_id"`
	Title             string         `json:"title"`
	ExecutorType      string         `json:"executor_type"`
	ExecutorID        string         `json:"executor_id"`
	AgentTaskID       *string        `json:"agent_task_id"`
	Environment       string         `json:"environment"`
	BuildRef          string         `json:"build_ref"`
	CapabilityBinding map[string]any `json:"capability_binding"`
	// Parallelism caps how many case tasks a dispatched round keeps queued or
	// running at once; nil = every case at once (M4 "release one as one completes").
	Parallelism     *int32                                    `json:"parallelism"`
	Status          string                                    `json:"status"`
	SourceRunID     *string                                   `json:"source_run_id"`
	RetryScope      *string                                   `json:"retry_scope"`
	Error           *string                                   `json:"error"`
	StartedAt       *string                                   `json:"started_at"`
	CompletedAt     *string                                   `json:"completed_at"`
	CreatedBy       *string                                   `json:"created_by"`
	CreatedAt       string                                    `json:"created_at"`
	UpdatedAt       string                                    `json:"updated_at"`
	ExecutionStatus *DesignRestoreTaskExecutionStatusResponse `json:"execution_status,omitempty"`
	ResultCounts    map[string]int64                          `json:"result_counts,omitempty"`
}

// TestRunCaseResponse is the outbound representation of a test_run_case row.
type TestRunCaseResponse struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	RunID          string         `json:"run_id"`
	TestCaseID     string         `json:"test_case_id"`
	CaseSnapshot   map[string]any `json:"case_snapshot"`
	Position       int32          `json:"position"`
	Result         string         `json:"result"`
	Notes          string         `json:"notes"`
	Evidence       []any          `json:"evidence"`
	StepResults    []any          `json:"step_results"`
	DurationMs     *int32         `json:"duration_ms"`
	ExecutedByType *string        `json:"executed_by_type"`
	ExecutedByID   *string        `json:"executed_by_id"`
	ExecutedAt     *string        `json:"executed_at"`
	DefectIssueID  *string        `json:"defect_issue_id"`
	AgentTaskID    *string        `json:"agent_task_id"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

// TestCaseResultTimelineEntryResponse is one entry in the per-case regression
// history — the query joins test_run_case with test_run to include run metadata.
type TestCaseResultTimelineEntryResponse struct {
	ID             string  `json:"id"`
	RunID          string  `json:"run_id"`
	RunTitle       string  `json:"run_title"`
	Environment    string  `json:"environment"`
	BuildRef       string  `json:"build_ref"`
	Result         string  `json:"result"`
	ExecutedAt     *string `json:"executed_at"`
	ExecutedByType *string `json:"executed_by_type"`
	ExecutedByID   *string `json:"executed_by_id"`
	DefectIssueID  *string `json:"defect_issue_id"`
	RunCreatedAt   string  `json:"run_created_at"`
}

// ---------------------------------------------------------------------------
// Converter helpers
// ---------------------------------------------------------------------------

func testPlanToResponse(plan db.TestPlan) TestPlanResponse {
	return TestPlanResponse{
		ID:          uuidToString(plan.ID),
		WorkspaceID: uuidToString(plan.WorkspaceID),
		ProjectID:   uuidToString(plan.ProjectID),
		Title:       plan.Title,
		Description: plan.Description,
		Status:      plan.Status,
		Selector:    decodeTestPlanSelector(plan.Selector),
		CreatedBy:   uuidToPtr(plan.CreatedBy),
		CreatedAt:   timestampToString(plan.CreatedAt),
		UpdatedAt:   timestampToString(plan.UpdatedAt),
	}
}

func testPlanCaseToResponse(pc db.TestPlanCase) TestPlanCaseResponse {
	return TestPlanCaseResponse{
		PlanID:     uuidToString(pc.PlanID),
		TestCaseID: uuidToString(pc.TestCaseID),
		Position:   pc.Position,
		CreatedAt:  timestampToString(pc.CreatedAt),
	}
}

// decodeRunJSONColumn is the run-side equivalent of decodeJSONColumn: it
// unmarshals a JSONB column into dst and logs on failure without aborting.
func decodeRunJSONColumn(raw []byte, dst any, field, runID string) {
	if len(raw) == 0 {
		return
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		slog.Warn("test run column failed to decode",
			"field", field, "run_id", runID, "error", err)
	}
}

func testRunToResponse(run db.TestRun) TestRunResponse {
	binding := map[string]any{}
	decodeRunJSONColumn(run.CapabilityBinding, &binding, "capability_binding", uuidToString(run.ID))
	return TestRunResponse{
		ID:                uuidToString(run.ID),
		WorkspaceID:       uuidToString(run.WorkspaceID),
		ProjectID:         uuidToString(run.ProjectID),
		PlanID:            uuidToPtr(run.PlanID),
		Title:             run.Title,
		ExecutorType:      run.ExecutorType,
		ExecutorID:        uuidToString(run.ExecutorID),
		AgentTaskID:       uuidToPtr(run.AgentTaskID),
		Environment:       run.Environment,
		BuildRef:          run.BuildRef,
		CapabilityBinding: binding,
		Parallelism:       int4ToPtr(run.Parallelism),
		Status:            run.Status,
		SourceRunID:       uuidToPtr(run.SourceRunID),
		RetryScope:        textToPtr(run.RetryScope),
		Error:             textToPtr(run.Error),
		StartedAt:         timestampToPtr(run.StartedAt),
		CompletedAt:       timestampToPtr(run.CompletedAt),
		CreatedBy:         uuidToPtr(run.CreatedBy),
		CreatedAt:         timestampToString(run.CreatedAt),
		UpdatedAt:         timestampToString(run.UpdatedAt),
	}
}

func testRunCaseToResponse(rc db.TestRunCase) TestRunCaseResponse {
	snapshot := map[string]any{}
	evidence := []any{}
	stepResults := []any{}
	id := uuidToString(rc.ID)
	decodeRunJSONColumn(rc.CaseSnapshot, &snapshot, "case_snapshot", id)
	decodeRunJSONColumn(rc.Evidence, &evidence, "evidence", id)
	decodeRunJSONColumn(rc.StepResults, &stepResults, "step_results", id)
	return TestRunCaseResponse{
		ID:             id,
		WorkspaceID:    uuidToString(rc.WorkspaceID),
		RunID:          uuidToString(rc.RunID),
		TestCaseID:     uuidToString(rc.TestCaseID),
		CaseSnapshot:   snapshot,
		Position:       rc.Position,
		Result:         rc.Result,
		Notes:          rc.Notes,
		Evidence:       evidence,
		StepResults:    stepResults,
		DurationMs:     int4ToPtr(rc.DurationMs),
		ExecutedByType: textToPtr(rc.ExecutedByType),
		ExecutedByID:   uuidToPtr(rc.ExecutedByID),
		ExecutedAt:     timestampToPtr(rc.ExecutedAt),
		DefectIssueID:  uuidToPtr(rc.DefectIssueID),
		AgentTaskID:    uuidToPtr(rc.AgentTaskID),
		CreatedAt:      timestampToString(rc.CreatedAt),
		UpdatedAt:      timestampToString(rc.UpdatedAt),
	}
}

func testCaseResultTimelineRowToResponse(row db.ListTestCaseResultTimelineRow) TestCaseResultTimelineEntryResponse {
	return TestCaseResultTimelineEntryResponse{
		ID:             uuidToString(row.ID),
		RunID:          uuidToString(row.RunID),
		RunTitle:       row.RunTitle,
		Environment:    row.Environment,
		BuildRef:       row.BuildRef,
		Result:         row.Result,
		ExecutedAt:     timestampToPtr(row.ExecutedAt),
		ExecutedByType: textToPtr(row.ExecutedByType),
		ExecutedByID:   uuidToPtr(row.ExecutedByID),
		DefectIssueID:  uuidToPtr(row.DefectIssueID),
		RunCreatedAt:   timestampToString(row.RunCreatedAt),
	}
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

var (
	validTestPlanStatuses   = []string{"draft", "active", "archived"}
	validTestRunRetryScopes = []string{"all", "failed_only", "selected"}
	validTestRunCaseResults = []string{"pending", "running", "passed", "failed", "blocked", "skipped"}
)

// ---------------------------------------------------------------------------
// Loaders
// ---------------------------------------------------------------------------

// loadTestPlanForUser resolves the {id} path param into the caller's workspace
// plan, writing a 404 if the plan is not found.
func (h *Handler) loadTestPlanForUser(w http.ResponseWriter, r *http.Request) (db.TestPlan, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.TestPlan{}, pgtype.UUID{}, false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "test plan id")
	if !ok {
		return db.TestPlan{}, pgtype.UUID{}, false
	}
	plan, err := h.Queries.GetTestPlanInWorkspace(r.Context(), db.GetTestPlanInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "test plan not found")
		return db.TestPlan{}, pgtype.UUID{}, false
	}
	return plan, wsUUID, true
}

// loadTestRunForUser resolves the {id} path param into the caller's workspace
// run, writing a 404 if the run is not found.
func (h *Handler) loadTestRunForUser(w http.ResponseWriter, r *http.Request) (db.TestRun, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.TestRun{}, pgtype.UUID{}, false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "test run id")
	if !ok {
		return db.TestRun{}, pgtype.UUID{}, false
	}
	run, err := h.Queries.GetTestRunInWorkspace(r.Context(), db.GetTestRunInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "test run not found")
		return db.TestRun{}, pgtype.UUID{}, false
	}
	return run, wsUUID, true
}

// loadTestRunCaseForUser resolves the {id} path param for a test_run_case row.
func (h *Handler) loadTestRunCaseForUser(w http.ResponseWriter, r *http.Request) (db.TestRunCase, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.TestRunCase{}, pgtype.UUID{}, false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "test run case id")
	if !ok {
		return db.TestRunCase{}, pgtype.UUID{}, false
	}
	rc, err := h.Queries.GetTestRunCaseInWorkspace(r.Context(), db.GetTestRunCaseInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "test run case not found")
		return db.TestRunCase{}, pgtype.UUID{}, false
	}
	return rc, wsUUID, true
}

// requireTestRunTaskToken is the write-gate for agent callers on a run. It
// enforces the three-way check described in P3-A2:
//   - X-Actor-Source must equal "task_token"
//   - X-Task-ID must match the task that owns the work: the run's task, or —
//     since per-case dispatch (TS-021) — the task of the case being written
//   - run-case must belong to this run (enforced by the caller via run_id check)
func (h *Handler) requireTestRunTaskToken(w http.ResponseWriter, r *http.Request, run db.TestRun, rc *db.TestRunCase) bool {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "updating a test run case is only available from within an agent task")
		return false
	}
	boundTaskID := strings.TrimSpace(r.Header.Get("X-Task-ID"))
	if boundTaskID == "" {
		writeError(w, http.StatusForbidden, "this request carries no task token")
		return false
	}
	if !run.AgentTaskID.Valid {
		writeError(w, http.StatusConflict, "this test run has not been dispatched to an agent")
		return false
	}
	if strings.EqualFold(boundTaskID, uuidToString(run.AgentTaskID)) {
		return true
	}
	if rc != nil && rc.AgentTaskID.Valid && strings.EqualFold(boundTaskID, uuidToString(rc.AgentTaskID)) {
		return true
	}
	writeError(w, http.StatusForbidden, "this task token does not own the test run")
	return false
}

// buildRunExecutionStatus follows the same pattern as
// designRestoreTaskToResponseWithExecution: it loads the agent task, runtime
// and latest message, then calls the shared designRestoreExecutionStatusToResponse
// helper (same package) to derive phase/reason/severity. The
// RestoreTaskID field of the snapshot struct is set to the run ID even though
// the field name is design-specific — it is unused by the helper.
func (h *Handler) buildRunExecutionStatus(ctx context.Context, run db.TestRun) *DesignRestoreTaskExecutionStatusResponse {
	if !run.AgentTaskID.Valid {
		return nil
	}
	snapshot := designRestoreExecutionStatusSnapshot{
		RestoreTaskID: run.ID,
		AgentTaskID:   run.AgentTaskID,
	}
	agentTask, err := h.Queries.GetAgentTask(ctx, run.AgentTaskID)
	if err != nil && err != pgx.ErrNoRows {
		slog.Warn("test run execution status: failed to load agent task",
			"run_id", uuidToString(run.ID),
			"agent_task_id", uuidToString(run.AgentTaskID),
			"error", err)
		return nil
	}
	if err == nil {
		snapshot.AgentTaskStatus = pgtype.Text{String: agentTask.Status, Valid: true}
		snapshot.RuntimeID = agentTask.RuntimeID
		snapshot.AgentTaskDispatchedAt = agentTask.DispatchedAt
		snapshot.AgentTaskStartedAt = agentTask.StartedAt
		snapshot.AgentTaskCompletedAt = agentTask.CompletedAt
		snapshot.AgentTaskCreatedAt = agentTask.CreatedAt
		snapshot.AgentTaskError = agentTask.Error
		snapshot.AgentTaskWaitReason = agentTask.WaitReason

		if agentTask.RuntimeID.Valid {
			runtime, runtimeErr := h.runtimeLookup(obsmetrics.RuntimeLookupSourceTestCapability).Get(ctx, agentTask.RuntimeID)
			if runtimeErr != nil && runtimeErr != pgx.ErrNoRows {
				slog.Warn("test run execution status: failed to load runtime",
					"run_id", uuidToString(run.ID),
					"runtime_id", uuidToString(agentTask.RuntimeID),
					"error", runtimeErr)
				return nil
			}
			if runtimeErr == nil {
				snapshot.RuntimeStatus = pgtype.Text{String: runtime.Status, Valid: true}
				snapshot.RuntimeLastSeenAt = runtime.LastSeenAt
			}
		}

		latestMessage, messageErr := h.Queries.GetLatestTaskMessageForTask(ctx, agentTask.ID)
		if messageErr != nil && messageErr != pgx.ErrNoRows {
			slog.Warn("test run execution status: failed to load latest task message",
				"run_id", uuidToString(run.ID),
				"agent_task_id", uuidToString(agentTask.ID),
				"error", messageErr)
			return nil
		}
		if messageErr == nil {
			snapshot.LastMessageSeq = latestMessage.Seq
			snapshot.LastMessageAt = latestMessage.CreatedAt
		}
	}
	return designRestoreExecutionStatusToResponse(snapshot, time.Now())
}

// buildResultCounts turns the CountTestRunResults rows into a map keyed by
// result bucket. Callers receive a non-nil map even when no cases exist yet.
func buildResultCounts(rows []db.CountTestRunResultsRow) map[string]int64 {
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.Result] = row.ResultCount
	}
	return counts
}

// snapshotForCase marshals testCaseToResponse into the JSON blob that
// test_run_case.case_snapshot permanently records. The snapshot is intentionally
// taken from a fresh DB row and its repos so future edits do not rewrite it.
func snapshotForCase(testCase db.TestCase, repos []db.TestCaseRepo) []byte {
	return marshalJSONColumn(testCaseToResponse(testCase, repos), "{}")
}

// fetchCasesForIDs resolves a slice of UUID strings into their TestCase rows
// and repos. Every case must exist in the workspace; the first missing one
// causes an early 404. All cases must share the same project_id — a mismatch
// causes a 400. Returns the project UUID derived from the cases.
func (h *Handler) fetchCasesForIDs(
	ctx context.Context,
	w http.ResponseWriter,
	wsUUID pgtype.UUID,
	rawIDs []string,
) ([]db.TestCase, map[string][]db.TestCaseRepo, pgtype.UUID, bool) {
	cases, reposByCase, projectUUID, err := h.loadCasesForRun(ctx, wsUUID, rawIDs)
	if err != nil {
		var rce *runCasesError
		if errors.As(err, &rce) {
			writeError(w, rce.status, rce.msg)
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load test cases")
		}
		return nil, nil, pgtype.UUID{}, false
	}
	return cases, reposByCase, projectUUID, true
}

// runCasesError is a caller's mistake while resolving a run's cases, with
// the HTTP status the request handlers answer.
type runCasesError struct {
	status int
	msg    string
}

func (e *runCasesError) Error() string { return e.msg }

// loadCasesForRun resolves the cases a run is built from — every id must be
// a case of this workspace and all of one project — with their repo
// bindings batched in one query.
func (h *Handler) loadCasesForRun(ctx context.Context, wsUUID pgtype.UUID, rawIDs []string) ([]db.TestCase, map[string][]db.TestCaseRepo, pgtype.UUID, error) {
	if len(rawIDs) == 0 {
		return nil, nil, pgtype.UUID{}, &runCasesError{status: http.StatusBadRequest, msg: "test_case_ids must not be empty"}
	}
	cases := make([]db.TestCase, 0, len(rawIDs))
	var projectUUID pgtype.UUID
	for i, raw := range rawIDs {
		id, err := util.ParseUUID(raw)
		if err != nil {
			return nil, nil, pgtype.UUID{}, &runCasesError{status: http.StatusBadRequest, msg: fmt.Sprintf("invalid test_case_ids[%d]", i)}
		}
		tc, err := h.Queries.GetTestCaseInWorkspace(ctx, db.GetTestCaseInWorkspaceParams{
			ID:          id,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			return nil, nil, pgtype.UUID{}, &runCasesError{status: http.StatusNotFound, msg: "test case not found: " + raw}
		}
		if i == 0 {
			projectUUID = tc.ProjectID
		} else if tc.ProjectID != projectUUID {
			return nil, nil, pgtype.UUID{}, &runCasesError{status: http.StatusBadRequest, msg: "all test cases must belong to the same project"}
		}
		// A deprecated case describes behaviour that no longer exists, so a
		// round that executes it reports on nothing. It is left out rather
		// than refused: a plan keeps its list when a member is deprecated, and
		// a scheduled regression must not stop running because of it. Drafts
		// stay runnable — a reviewer trying one out is legitimate — and are
		// kept out of coverage and verification instead.
		if tc.Status == "deprecated" {
			continue
		}
		cases = append(cases, tc)
	}
	if len(cases) == 0 {
		return nil, nil, pgtype.UUID{}, &runCasesError{status: http.StatusBadRequest, msg: "every selected test case is deprecated; there is nothing to execute"}
	}

	// Batch-fetch repos for all cases.
	caseIDs := make([]pgtype.UUID, len(cases))
	for i, tc := range cases {
		caseIDs[i] = tc.ID
	}
	repoRows, err := h.Queries.ListTestCaseReposForCases(ctx, caseIDs)
	if err != nil {
		slog.Warn("fetch repos for test cases failed", "error", err)
		repoRows = nil
	}
	reposByCase := map[string][]db.TestCaseRepo{}
	for _, repo := range repoRows {
		key := uuidToString(repo.TestCaseID)
		reposByCase[key] = append(reposByCase[key], repo)
	}
	return cases, reposByCase, projectUUID, nil
}

// createTestRunWithCases inserts the run and one run case per test case, each
// with a frozen snapshot, in one transaction.
func (h *Handler) createTestRunWithCases(ctx context.Context, params db.CreateTestRunParams, cases []db.TestCase, reposByCase map[string][]db.TestCaseRepo) (db.TestRun, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.TestRun{}, fmt.Errorf("start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	run, err := qtx.CreateTestRun(ctx, params)
	if err != nil {
		return db.TestRun{}, fmt.Errorf("create test run: %w", err)
	}
	for i, tc := range cases {
		snapshot := snapshotForCase(tc, reposByCase[uuidToString(tc.ID)])
		if _, err := qtx.CreateTestRunCase(ctx, db.CreateTestRunCaseParams{
			WorkspaceID:  params.WorkspaceID,
			RunID:        run.ID,
			TestCaseID:   tc.ID,
			CaseSnapshot: snapshot,
			Position:     int32(i),
			Result:       "pending",
		}); err != nil {
			return db.TestRun{}, fmt.Errorf("create test run case: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.TestRun{}, fmt.Errorf("commit test run create: %w", err)
	}
	return run, nil
}

// ---------------------------------------------------------------------------
// Test plan limits
// ---------------------------------------------------------------------------

const (
	testRunListDefaultLimit = 50
	testRunListMaxLimit     = 200
	testCaseTimelineLimit   = 50
	testCaseTimelineMax     = 200
)

// ---------------------------------------------------------------------------
// Test plan handlers
// ---------------------------------------------------------------------------

// ListTestPlans returns plans scoped to the caller's workspace, filtered by
// project_id and status when the corresponding query params are present.
func (h *Handler) ListTestPlans(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	params := db.ListTestPlansParams{WorkspaceID: wsUUID}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return
		}
		params.ProjectID = id
	}
	if raw := r.URL.Query().Get("status"); raw != "" {
		params.Status = pgtype.Text{String: raw, Valid: true}
	}
	plans, err := h.Queries.ListTestPlans(r.Context(), params)
	if err != nil {
		slog.Error("list test plans failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test plans")
		return
	}
	resp := make([]TestPlanResponse, len(plans))
	for i, p := range plans {
		resp[i] = testPlanToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"test_plans": resp, "total": len(resp)})
}

type CreateTestPlanRequest struct {
	ProjectID   string            `json:"project_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Selector    *TestPlanSelector `json:"selector"`
}

// CreateTestPlan creates a new test plan inside the caller's workspace. The
// project must exist in the same workspace (no FK, verified here).
func (h *Handler) CreateTestPlan(w http.ResponseWriter, r *http.Request) {
	var req CreateTestPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	// Verify project belongs to this workspace (no FK).
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectUUID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "project not found")
		return
	}
	status := req.Status
	if status == "" {
		status = "draft"
	}
	if !validateTestCaseEnum(w, "status", status, validTestPlanStatuses) {
		return
	}
	var selector TestPlanSelector
	if req.Selector != nil {
		normalized, problem := h.normalizeTestPlanSelector(r, wsUUID, *req.Selector)
		if problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		selector = normalized
	}
	plan, err := h.Queries.CreateTestPlan(r.Context(), db.CreateTestPlanParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectUUID,
		Title:       strings.TrimSpace(req.Title),
		Description: req.Description,
		Status:      status,
		CreatedBy:   userUUID,
		Selector:    marshalJSONColumn(selector, "{}"),
	})
	if err != nil {
		slog.Error("create test plan failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create test plan")
		return
	}
	resp := testPlanToResponse(plan)
	h.publish(protocol.EventTestPlanCreated, workspaceID, "member", userID, map[string]any{"test_plan": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// GetTestPlan returns a single plan by UUID.
func (h *Handler) GetTestPlan(w http.ResponseWriter, r *http.Request) {
	plan, _, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, testPlanToResponse(plan))
}

type UpdateTestPlanRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	// Selector replaces the rule; `{}` clears it, omitted keeps it.
	Selector *TestPlanSelector `json:"selector"`
}

// UpdateTestPlan applies a partial update to a plan. Any omitted field is
// preserved by COALESCE in the query.
func (h *Handler) UpdateTestPlan(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	var req UpdateTestPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	params := db.UpdateTestPlanParams{ID: plan.ID, WorkspaceID: wsUUID}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		params.Title = pgtype.Text{String: strings.TrimSpace(*req.Title), Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		if !validateTestCaseEnum(w, "status", *req.Status, validTestPlanStatuses) {
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Selector != nil {
		normalized, problem := h.normalizeTestPlanSelector(r, wsUUID, *req.Selector)
		if problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		params.Selector = marshalJSONColumn(normalized, "{}")
	}
	updated, err := h.Queries.UpdateTestPlan(r.Context(), params)
	if err != nil {
		slog.Error("update test plan failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update test plan")
		return
	}
	resp := testPlanToResponse(updated)
	h.publish(protocol.EventTestPlanUpdated, workspaceID, "member", userID, map[string]any{"test_plan": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteTestPlan sweeps plan_case rows first (no FK), then deletes the plan,
// all in one transaction so a partial sweep cannot leave orphaned rows.
func (h *Handler) DeleteTestPlan(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteTestPlanCases(r.Context(), db.DeleteTestPlanCasesParams{
		PlanID: plan.ID, WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("delete test plan cases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete test plan")
		return
	}
	if err := qtx.DeleteTestPlan(r.Context(), db.DeleteTestPlanParams{
		ID: plan.ID, WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("delete test plan failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete test plan")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit test plan delete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete test plan")
		return
	}
	h.publish(protocol.EventTestPlanDeleted, workspaceID, "member", userID,
		map[string]any{"test_plan_id": uuidToString(plan.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ListTestPlanCases returns the cases attached to a plan in position order.
func (h *Handler) ListTestPlanCases(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	pcs, err := h.Queries.ListTestPlanCases(r.Context(), db.ListTestPlanCasesParams{
		PlanID: plan.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("list test plan cases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test plan cases")
		return
	}
	resp := make([]TestPlanCaseResponse, len(pcs))
	for i, pc := range pcs {
		resp[i] = testPlanCaseToResponse(pc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": resp, "total": len(resp)})
}

type AddTestPlanCasesRequest struct {
	Cases []struct {
		TestCaseID string `json:"test_case_id"`
		Position   int32  `json:"position"`
	} `json:"cases"`
}

// AddTestPlanCases upserts one or more test cases into a plan. Sending the same
// case_id twice with different positions is handled by ON CONFLICT DO UPDATE.
func (h *Handler) AddTestPlanCases(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	var req AddTestPlanCasesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Cases) == 0 {
		writeError(w, http.StatusBadRequest, "cases must not be empty")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	added := make([]TestPlanCaseResponse, 0, len(req.Cases))
	for i, item := range req.Cases {
		caseUUID, ok := parseUUIDOrBadRequest(w, item.TestCaseID, fmt.Sprintf("cases[%d].test_case_id", i))
		if !ok {
			return
		}
		// Verify the case exists in this workspace (no FK).
		tc, err := h.Queries.GetTestCaseInWorkspace(r.Context(), db.GetTestCaseInWorkspaceParams{
			ID: caseUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "test case not found: "+item.TestCaseID)
			return
		}
		// The case must belong to the same project as the plan.
		if tc.ProjectID != plan.ProjectID {
			writeError(w, http.StatusBadRequest,
				"test case "+item.TestCaseID+" belongs to a different project")
			return
		}
		pc, err := h.Queries.AddTestPlanCase(r.Context(), db.AddTestPlanCaseParams{
			PlanID:      plan.ID,
			WorkspaceID: wsUUID,
			TestCaseID:  caseUUID,
			Position:    item.Position,
		})
		if err != nil {
			slog.Error("add test plan case failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to add test plan case")
			return
		}
		added = append(added, testPlanCaseToResponse(pc))
	}
	h.publish(protocol.EventTestPlanUpdated, workspaceID, "member", userID,
		map[string]any{"test_plan_id": uuidToString(plan.ID), "added_cases": len(added)})
	writeJSON(w, http.StatusOK, map[string]any{"cases": added})
}

// RemoveTestPlanCase removes a single case binding from a plan.
func (h *Handler) RemoveTestPlanCase(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	caseUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "caseId"), "caseId")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.RemoveTestPlanCase(r.Context(), db.RemoveTestPlanCaseParams{
		PlanID: plan.ID, WorkspaceID: wsUUID, TestCaseID: caseUUID,
	}); err != nil {
		slog.Error("remove test plan case failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove test plan case")
		return
	}
	h.publish(protocol.EventTestPlanUpdated, workspaceID, "member", userID,
		map[string]any{"test_plan_id": uuidToString(plan.ID), "removed_case_id": uuidToString(caseUUID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// Test run handlers
// ---------------------------------------------------------------------------

// ListTestRuns returns runs scoped to the caller's workspace.
func (h *Handler) ListTestRuns(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	limit := int32(testRunListDefaultLimit)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > testRunListMaxLimit {
			parsed = testRunListMaxLimit
		}
		limit = int32(parsed)
	}
	params := db.ListTestRunsParams{WorkspaceID: wsUUID, Limit: limit}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return
		}
		params.ProjectID = id
	}
	if raw := r.URL.Query().Get("plan_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "plan_id")
		if !ok {
			return
		}
		params.PlanID = id
	}
	if raw := r.URL.Query().Get("status"); raw != "" {
		params.Status = pgtype.Text{String: raw, Valid: true}
	}
	runs, err := h.Queries.ListTestRuns(r.Context(), params)
	if err != nil {
		slog.Error("list test runs failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test runs")
		return
	}
	resp := make([]TestRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = testRunToResponse(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"test_runs": resp, "total": len(resp)})
}

type CreateTestRunRequest struct {
	// PlanID, when set, selects all cases in the named plan.
	PlanID string `json:"plan_id"`
	// TestCaseIDs selects an explicit list; ignored when PlanID is set.
	TestCaseIDs []string `json:"test_case_ids"`
	Title       string   `json:"title"`
	Environment string   `json:"environment"`
	BuildRef    string   `json:"build_ref"`
	// Parallelism caps concurrently dispatched case tasks (1..maxTestRunParallelism);
	// omitted or null means no cap.
	Parallelism *int32 `json:"parallelism"`
}

// maxTestRunParallelism bounds the per-round cap: more concurrent phones than
// this is not a lab, it is a typo.
const maxTestRunParallelism = 64

// CreateTestRun creates an execution round for a plan or an explicit list of
// test cases. In one transaction it creates the run row and a test_run_case row
// per case, each carrying case_snapshot — the JSON of testCaseToResponse at
// creation time. Editing the case later must not change what a past round
// recorded.
//
// executor_type is always "member" here; agent dispatch is wired separately
// (DispatchTestRun, which returns 501 at this stage).
func (h *Handler) CreateTestRun(w http.ResponseWriter, r *http.Request) {
	var req CreateTestRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.PlanID == "" && len(req.TestCaseIDs) == 0 {
		writeError(w, http.StatusBadRequest, "either plan_id or test_case_ids is required")
		return
	}
	var parallelism pgtype.Int4
	if req.Parallelism != nil {
		if *req.Parallelism < 1 || *req.Parallelism > maxTestRunParallelism {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("parallelism must be between 1 and %d", maxTestRunParallelism))
			return
		}
		parallelism = pgtype.Int4{Int32: *req.Parallelism, Valid: true}
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	// Resolve the test cases to run and the project they belong to.
	var cases []db.TestCase
	var reposByCase map[string][]db.TestCaseRepo
	var projectUUID pgtype.UUID
	var planUUID pgtype.UUID // may stay zero-valued (null) for ad-hoc runs

	if req.PlanID != "" {
		planID, ok := parseUUIDOrBadRequest(w, req.PlanID, "plan_id")
		if !ok {
			return
		}
		plan, err := h.Queries.GetTestPlanInWorkspace(r.Context(), db.GetTestPlanInWorkspaceParams{
			ID: planID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "test plan not found")
			return
		}
		planUUID = plan.ID
		projectUUID = plan.ProjectID

		sources, err := h.planRoundCases(r.Context(), plan)
		if err != nil {
			slog.Error("list test plan cases for run failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to list plan cases")
			return
		}
		if len(sources) == 0 {
			writeError(w, http.StatusBadRequest, "the test plan selects no cases: add cases to it, or give it a rule that matches active cases")
			return
		}
		cases, reposByCase, _, ok = h.fetchCasesForIDs(r.Context(), w, wsUUID, planRoundCaseIDs(sources))
		if !ok {
			return
		}
	} else {
		cases, reposByCase, projectUUID, ok = h.fetchCasesForIDs(r.Context(), w, wsUUID, req.TestCaseIDs)
		if !ok {
			return
		}
	}

	run, err := h.createTestRunWithCases(r.Context(), db.CreateTestRunParams{
		WorkspaceID:       wsUUID,
		ProjectID:         projectUUID,
		PlanID:            planUUID,
		Title:             strings.TrimSpace(req.Title),
		ExecutorType:      "member",
		ExecutorID:        userUUID,
		Environment:       req.Environment,
		BuildRef:          req.BuildRef,
		CapabilityBinding: []byte("{}"),
		Status:            "pending",
		CreatedBy:         userUUID,
		Parallelism:       parallelism,
	}, cases, reposByCase)
	if err != nil {
		slog.Error("create test run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create test run")
		return
	}

	resp := testRunToResponse(run)
	h.publish(protocol.EventTestRunUpdated, workspaceID, "member", userID, map[string]any{"test_run": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// GetTestRun returns a single run enriched with execution_status (agent runs
// only) and per-result counts across all its cases.
func (h *Handler) GetTestRun(w http.ResponseWriter, r *http.Request) {
	run, wsUUID, ok := h.loadTestRunForUser(w, r)
	if !ok {
		return
	}
	resp := testRunToResponse(run)

	// Derive agent execution status if the run was dispatched.
	if run.AgentTaskID.Valid {
		resp.ExecutionStatus = h.buildRunExecutionStatus(r.Context(), run)
	}

	// Per-result counts for the pass-rate summary.
	countRows, err := h.Queries.CountTestRunResults(r.Context(), db.CountTestRunResultsParams{
		RunID: run.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Warn("count test run results failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	resp.ResultCounts = buildResultCounts(countRows)

	writeJSON(w, http.StatusOK, resp)
}

// StartTestRun transitions a run from pending to running and stamps started_at.
func (h *Handler) StartTestRun(w http.ResponseWriter, r *http.Request) {
	run, wsUUID, ok := h.loadTestRunForUser(w, r)
	if !ok {
		return
	}
	if run.Status != "pending" {
		writeError(w, http.StatusConflict, "only a pending run can be started")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	updated, err := h.Queries.UpdateTestRun(r.Context(), db.UpdateTestRunParams{
		ID:          run.ID,
		WorkspaceID: wsUUID,
		Status:      pgtype.Text{String: "running", Valid: true},
		StartedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		slog.Error("start test run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start test run")
		return
	}
	resp := testRunToResponse(updated)
	h.publish(protocol.EventTestRunUpdated, workspaceID, "member", userID, map[string]any{"test_run": resp})
	writeJSON(w, http.StatusOK, resp)
}

// AbortTestRun stops a round that will never finish — a run whose agent died,
// or one a human abandoned. Without it "aborted" would be a status the CHECK
// constraint allows and no code path can ever write, which is exactly the dead
// enum design_restore left behind.
//
// The cases keep whatever results they already had: aborting ends the round, it
// does not erase what the round observed.
func (h *Handler) AbortTestRun(w http.ResponseWriter, r *http.Request) {
	run, wsUUID, ok := h.loadTestRunForUser(w, r)
	if !ok {
		return
	}
	if run.Status == "completed" || run.Status == "aborted" {
		writeError(w, http.StatusConflict, "this run is already "+run.Status)
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	// A body is optional; an abort with no stated reason is still a valid abort.
	_ = json.NewDecoder(r.Body).Decode(&req)

	params := db.UpdateTestRunParams{
		ID:          run.ID,
		WorkspaceID: wsUUID,
		Status:      pgtype.Text{String: "aborted", Valid: true},
		CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	if strings.TrimSpace(req.Reason) != "" {
		params.Error = pgtype.Text{String: req.Reason, Valid: true}
	}
	updated, err := h.Queries.UpdateTestRun(r.Context(), params)
	if err != nil {
		slog.Error("abort test run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to abort test run")
		return
	}
	// Cases a parallelism cap was still holding back have no task to fail
	// them; without this they would sit pending forever under an aborted
	// round. Only a capped agent round has such cases: a member-executed
	// round keeps its unresolved cases pending, as it always did.
	if updated.ExecutorType == "agent" && updated.Parallelism.Valid {
		skipped, err := h.Queries.SkipUndispatchedTestRunCases(r.Context(), db.SkipUndispatchedTestRunCasesParams{
			RunID:       run.ID,
			WorkspaceID: wsUUID,
			Notes:       "skipped: the round was aborted before this case was dispatched",
		})
		if err != nil {
			slog.Warn("skip undispatched test run cases failed", append(logger.RequestAttrs(r), "error", err)...)
		}
		for _, rc := range skipped {
			h.publish(protocol.EventTestRunCaseUpdated, workspaceID, "member", userID,
				map[string]any{"test_run_case": testRunCaseToResponse(rc), "run_id": uuidToString(run.ID)})
		}
	}
	h.settleAutopilotTestRun(r.Context(), h.Queries, updated)
	resp := testRunToResponse(updated)
	h.publish(protocol.EventTestRunUpdated, workspaceID, "member", userID, map[string]any{"test_run": resp})
	writeJSON(w, http.StatusOK, resp)
}

type RetryTestRunRequest struct {
	// Scope controls which cases from the source run are carried forward.
	// "all" retries every case, "failed_only" retries failed/blocked/skipped,
	// "selected" retries the explicit case_ids list.
	Scope   string   `json:"scope"`
	CaseIDs []string `json:"case_ids"` // only for scope="selected"
	Title   string   `json:"title"`    // optional; auto-generated if empty
}

// RetryTestRun creates a NEW run that points at the original via source_run_id.
// It NEVER writes to the original run — the original run's result rows are
// immutable history. Snapshots are taken fresh from the current case
// definitions because a rerun targets today's behaviour, not the original
// execution's definition.
func (h *Handler) RetryTestRun(w http.ResponseWriter, r *http.Request) {
	sourceRun, wsUUID, ok := h.loadTestRunForUser(w, r)
	if !ok {
		return
	}
	var req RetryTestRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateTestCaseEnum(w, "scope", req.Scope, validTestRunRetryScopes) {
		return
	}
	if req.Scope == "selected" && len(req.CaseIDs) == 0 {
		writeError(w, http.StatusBadRequest, "case_ids is required when scope is \"selected\"")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	// Determine which source run-cases to carry forward.
	var sourceCases []db.TestRunCase
	switch req.Scope {
	case "all":
		sourceCases, _ = h.Queries.ListTestRunCases(r.Context(), db.ListTestRunCasesParams{
			RunID: sourceRun.ID, WorkspaceID: wsUUID,
		})
	case "failed_only":
		sourceCases, _ = h.Queries.ListTestRunCasesByResult(r.Context(), db.ListTestRunCasesByResultParams{
			RunID:       sourceRun.ID,
			WorkspaceID: wsUUID,
			Results:     []string{"failed", "blocked", "skipped"},
		})
	case "selected":
		for i, raw := range req.CaseIDs {
			caseID, ok := parseUUIDOrBadRequest(w, raw, fmt.Sprintf("case_ids[%d]", i))
			if !ok {
				return
			}
			rc, err := h.Queries.GetTestRunCaseInWorkspace(r.Context(), db.GetTestRunCaseInWorkspaceParams{
				ID: caseID, WorkspaceID: wsUUID,
			})
			if err != nil {
				writeError(w, http.StatusBadRequest, "test run case not found: "+raw)
				return
			}
			if rc.RunID != sourceRun.ID {
				writeError(w, http.StatusBadRequest,
					"case "+raw+" does not belong to run "+uuidToString(sourceRun.ID))
				return
			}
			sourceCases = append(sourceCases, rc)
		}
	}
	if len(sourceCases) == 0 {
		writeError(w, http.StatusBadRequest, "no cases to retry with the given scope")
		return
	}

	// Build the title for the new run.
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = fmt.Sprintf("Retry: %s", sourceRun.Title)
	}

	// Fetch fresh snapshots for all cases to be retried.
	rawIDs := make([]string, len(sourceCases))
	for i, sc := range sourceCases {
		rawIDs[i] = uuidToString(sc.TestCaseID)
	}
	freshCases, freshRepos, _, ok := h.fetchCasesForIDs(r.Context(), w, wsUUID, rawIDs)
	if !ok {
		return
	}

	// Create the new run and its cases in one transaction. The original run is
	// NOT touched — its rows remain as immutable history.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	newRun, err := qtx.CreateTestRun(r.Context(), db.CreateTestRunParams{
		WorkspaceID:       wsUUID,
		ProjectID:         sourceRun.ProjectID,
		PlanID:            sourceRun.PlanID,
		Title:             title,
		ExecutorType:      "member",
		ExecutorID:        userUUID,
		Environment:       sourceRun.Environment,
		BuildRef:          sourceRun.BuildRef,
		CapabilityBinding: []byte("{}"),
		Status:            "pending",
		SourceRunID:       sourceRun.ID,
		RetryScope:        pgtype.Text{String: req.Scope, Valid: true},
		CreatedBy:         userUUID,
		// A rerun targets the same lab, so it keeps the cap.
		Parallelism: sourceRun.Parallelism,
	})
	if err != nil {
		slog.Error("create retry test run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create retry run")
		return
	}

	for i, tc := range freshCases {
		repos := freshRepos[uuidToString(tc.ID)]
		snapshot := snapshotForCase(tc, repos)
		if _, err := qtx.CreateTestRunCase(r.Context(), db.CreateTestRunCaseParams{
			WorkspaceID:  wsUUID,
			RunID:        newRun.ID,
			TestCaseID:   tc.ID,
			CaseSnapshot: snapshot,
			Position:     int32(i),
			Result:       "pending",
		}); err != nil {
			slog.Error("create retry test run case failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create retry run case")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit retry test run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create retry run")
		return
	}

	resp := testRunToResponse(newRun)
	h.publish(protocol.EventTestRunUpdated, workspaceID, "member", userID, map[string]any{"test_run": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// ListTestRunCases returns the cases attached to a run in position order.
func (h *Handler) ListTestRunCases(w http.ResponseWriter, r *http.Request) {
	run, wsUUID, ok := h.loadTestRunForUser(w, r)
	if !ok {
		return
	}
	rcs, err := h.Queries.ListTestRunCases(r.Context(), db.ListTestRunCasesParams{
		RunID: run.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("list test run cases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test run cases")
		return
	}
	resp := make([]TestRunCaseResponse, len(rcs))
	for i, rc := range rcs {
		resp[i] = testRunCaseToResponse(rc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": resp, "total": len(resp)})
}

// ---------------------------------------------------------------------------
// Test run case handlers
// ---------------------------------------------------------------------------

type UpdateTestRunCaseResultRequest struct {
	Result      string               `json:"result"`
	Notes       *string              `json:"notes"`
	Evidence    []any                `json:"evidence"`
	StepResults *[]TestRunStepResult `json:"step_results"`
	DurationMs  *int32               `json:"duration_ms"`
}

// UpdateTestRunCaseResult writes the outcome of one executed case. It accepts
// both a workspace member (normal auth) and the run's own agent via task token.
//
// For the agent path: X-Actor-Source must equal "task_token", X-Task-ID must
// match run.agent_task_id, and the run-case must belong to that run.
//
// After writing, if CountPendingTestRunCases returns 0, the run is flipped to
// "completed" and completed_at is set.
func (h *Handler) UpdateTestRunCaseResult(w http.ResponseWriter, r *http.Request) {
	rc, wsUUID, ok := h.loadTestRunCaseForUser(w, r)
	if !ok {
		return
	}

	// Load the parent run for the task-token gate and for auto-completion.
	run, err := h.Queries.GetTestRunInWorkspace(r.Context(), db.GetTestRunInWorkspaceParams{
		ID: rc.RunID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("get test run for case update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load test run")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)

	// Auth: either a normal member or the run's dispatched agent.
	var executedByType string
	var executedByID pgtype.UUID
	isAgentCall := r.Header.Get("X-Actor-Source") == "task_token"
	if isAgentCall {
		if !h.requireTestRunTaskToken(w, r, run, &rc) {
			return
		}
		// The agent executor is the run's executor (the agent UUID set at dispatch).
		executedByType = "agent"
		executedByID = run.ExecutorID
	} else {
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		uid, ok := parseUUIDOrBadRequest(w, userID, "user id")
		if !ok {
			return
		}
		executedByType = "member"
		executedByID = uid
	}

	var req UpdateTestRunCaseResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateTestCaseEnum(w, "result", req.Result, validTestRunCaseResults) {
		return
	}
	if accepted, reason := runAcceptsResultWrite(run.Status, isAgentCall); !accepted {
		writeError(w, http.StatusConflict, reason)
		return
	}
	// An agent records verdicts; pending and running are the platform's to
	// set when it dispatches and starts the case task.
	if isAgentCall && !isTerminalRunCaseResult(req.Result) {
		writeError(w, http.StatusBadRequest, "an agent can only record passed, failed, blocked or skipped")
		return
	}
	var stepResults []TestRunStepResult
	if req.StepResults != nil {
		stepResults = *req.StepResults
	}
	requireComplete := isAgentCall && agentVerdictNeedsEvidence(req.Result)
	if problem := stepResultsProblem(req.Result, stepResults, snapshotStepIndexes(rc.CaseSnapshot), requireComplete); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	// An agent's pass or fail rests on what it saw; without a single captured
	// frame or trace nobody can check it. Evidence is uploaded first
	// (`multica test evidence add`), then the verdict is recorded.
	if requireComplete {
		files, err := h.Queries.ListAttachmentsByTestRunCase(r.Context(), db.ListAttachmentsByTestRunCaseParams{
			TestRunCaseID: rc.ID, WorkspaceID: wsUUID,
		})
		if err != nil {
			slog.Error("list run case evidence failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to check evidence")
			return
		}
		if len(files) == 0 {
			writeError(w, http.StatusConflict, "a "+req.Result+" verdict needs evidence: upload what you observed with `multica test evidence add` first, then record the result")
			return
		}
	}

	params := db.UpdateTestRunCaseResultParams{
		ID:             rc.ID,
		WorkspaceID:    wsUUID,
		Result:         pgtype.Text{String: req.Result, Valid: true},
		ExecutedByType: pgtype.Text{String: executedByType, Valid: true},
		ExecutedByID:   executedByID,
		ExecutedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	if req.Notes != nil {
		params.Notes = pgtype.Text{String: *req.Notes, Valid: true}
	}
	if req.Evidence != nil {
		params.Evidence = marshalJSONColumn(req.Evidence, "[]")
	}
	if req.StepResults != nil {
		params.StepResults = marshalJSONColumn(stepResults, "[]")
	}
	if req.DurationMs != nil {
		params.DurationMs = pgtype.Int4{Int32: *req.DurationMs, Valid: true}
	}

	updated, err := h.Queries.UpdateTestRunCaseResult(r.Context(), params)
	if err != nil {
		slog.Error("update test run case result failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update test run case result")
		return
	}

	// Publish the case-level event before checking run completion so clients
	// can render progress incrementally.
	caseResp := testRunCaseToResponse(updated)
	actorType := "member"
	actorID := uuidToString(executedByID)
	if isAgentCall {
		actorType = "agent"
	}
	h.publish(protocol.EventTestRunCaseUpdated, workspaceID, actorType, actorID,
		map[string]any{"test_run_case": caseResp, "run_id": uuidToString(run.ID)})

	// Complete the run once no case is pending or running. This goes through
	// the same convergence as a finished case task: it never revives an
	// aborted round, and it settles the autopilot run that launched the round.
	// The CLI write is the normal last write of a round, so completing it here
	// without that settle left the autopilot run "running" for good — the
	// task hook that follows finds the round already completed and stops.
	if completedRun, convergeErr := h.convergeTestRun(r.Context(), h.Queries, run); convergeErr != nil {
		slog.Error("auto-complete test run failed", append(logger.RequestAttrs(r), "error", convergeErr)...)
	} else if completedRun != nil {
		h.publish(protocol.EventTestRunUpdated, workspaceID, actorType, actorID,
			map[string]any{"test_run": testRunToResponse(*completedRun)})
	}

	writeJSON(w, http.StatusOK, caseResp)
}

// ListTestRunCaseEvidence — GET /api/test-run-cases/{id}/evidence. The files
// an executor uploaded for one run case: what a reviewer checks the recorded
// verdict against.
func (h *Handler) ListTestRunCaseEvidence(w http.ResponseWriter, r *http.Request) {
	rc, wsUUID, ok := h.loadTestRunCaseForUser(w, r)
	if !ok {
		return
	}
	files, err := h.Queries.ListAttachmentsByTestRunCase(r.Context(), db.ListAttachmentsByTestRunCaseParams{
		TestRunCaseID: rc.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("list run case evidence failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list evidence")
		return
	}
	mode := attachmentURLModeFromRequest(r)
	resp := make([]AttachmentResponse, len(files))
	for i, file := range files {
		resp[i] = h.attachmentToResponse(file, mode)
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": resp})
}

type OpenTestRunCaseDefectRequest struct {
	// Title is an optional override; when empty the handler derives the title
	// from the case key and title stored in the snapshot.
	Title string `json:"title"`
	// Note is appended to the auto-generated description.
	Note string `json:"note"`
}

// OpenTestRunCaseDefect creates a defect issue whose title carries the case key
// and whose description embeds the case snapshot and a back-link to the run.
// After the issue is created, defect_issue_id is written back to the run case.
func (h *Handler) OpenTestRunCaseDefect(w http.ResponseWriter, r *http.Request) {
	rc, wsUUID, ok := h.loadTestRunCaseForUser(w, r)
	if !ok {
		return
	}

	// Load the parent run for project context and the back-link.
	run, err := h.Queries.GetTestRunInWorkspace(r.Context(), db.GetTestRunInWorkspaceParams{
		ID: rc.RunID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("get test run for defect failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load test run")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	var req OpenTestRunCaseDefectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Parse the snapshot to extract the case key and title for the issue.
	var snapshot map[string]any
	if len(rc.CaseSnapshot) > 0 {
		_ = json.Unmarshal(rc.CaseSnapshot, &snapshot)
	}
	caseKey, _ := snapshot["key"].(string)
	caseTitle, _ := snapshot["title"].(string)

	// Build the issue title.
	issueTitle := strings.TrimSpace(req.Title)
	if issueTitle == "" {
		if caseKey != "" && caseTitle != "" {
			issueTitle = fmt.Sprintf("[%s] %s", caseKey, caseTitle)
		} else if caseKey != "" {
			issueTitle = fmt.Sprintf("[%s] defect", caseKey)
		} else {
			issueTitle = fmt.Sprintf("Defect from run %s", uuidToString(run.ID))
		}
	}

	// Build the issue description.
	var descParts []string
	descParts = append(descParts, fmt.Sprintf("**Test run:** %s (`%s`)", run.Title, uuidToString(run.ID)))
	if caseKey != "" {
		descParts = append(descParts, fmt.Sprintf("**Test case:** %s", caseKey))
	}
	descParts = append(descParts, fmt.Sprintf("**Result:** %s", rc.Result))
	if req.Note != "" {
		descParts = append(descParts, "", req.Note)
	}
	if len(rc.CaseSnapshot) > 0 {
		pretty, jsonErr := json.MarshalIndent(snapshot, "", "  ")
		if jsonErr == nil {
			descParts = append(descParts, "", "**Case snapshot:**", "```json", string(pretty), "```")
		}
	}
	description := strings.Join(descParts, "\n")

	// Create the defect issue using the service to get all the standard
	// side-effects: numbering, analytics, event broadcast.
	result, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID: wsUUID,
		Title:       issueTitle,
		Description: pgtype.Text{String: description, Valid: true},
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   userUUID,
		ProjectID:   run.ProjectID,
	}, service.IssueCreateOpts{
		ActorID: userID,
	})
	if err != nil {
		if writeIssueWindowViolation(w, err) {
			return
		}
		slog.Error("create defect issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create defect issue")
		return
	}

	// The evidence that made the tester open this defect travels with it
	// (09-02 §7.5): one more attachment row per file, over the same stored
	// object, filed under the new issue. Best effort — a defect without its
	// screenshots is still a defect.
	h.copyRunCaseEvidenceToIssue(r.Context(), rc, result.Issue.ID, userUUID)

	// Write defect_issue_id back to the run case.
	updated, err := h.Queries.UpdateTestRunCaseResult(r.Context(), db.UpdateTestRunCaseResultParams{
		ID:            rc.ID,
		WorkspaceID:   wsUUID,
		DefectIssueID: result.Issue.ID,
	})
	if err != nil {
		slog.Error("write defect_issue_id failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record defect issue on run case")
		return
	}

	caseResp := testRunCaseToResponse(updated)
	h.publish(protocol.EventTestRunCaseUpdated, workspaceID, "member", userID,
		map[string]any{"test_run_case": caseResp, "run_id": uuidToString(run.ID)})

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	issueResp := issueToResponse(result.Issue, prefix)
	writeJSON(w, http.StatusCreated, map[string]any{
		"test_run_case": caseResp,
		"issue":         issueResp,
	})
}

// ListTestCaseResultTimeline serves the per-case regression history across all
// runs the case has appeared in. The {ref} path param is resolved with the
// same loader used by GetTestCase (accepts both "TC-42" and a UUID).
func (h *Handler) ListTestCaseResultTimeline(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	limit := int32(testCaseTimelineLimit)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > testCaseTimelineMax {
			parsed = testCaseTimelineMax
		}
		limit = int32(parsed)
	}
	rows, err := h.Queries.ListTestCaseResultTimeline(r.Context(), db.ListTestCaseResultTimelineParams{
		WorkspaceID: testCase.WorkspaceID,
		TestCaseID:  testCase.ID,
		Limit:       limit,
	})
	if err != nil {
		slog.Error("list test case result timeline failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test case result timeline")
		return
	}
	resp := make([]TestCaseResultTimelineEntryResponse, len(rows))
	for i, row := range rows {
		resp[i] = testCaseResultTimelineRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"timeline": resp, "total": len(resp)})
}

// notImplemented is a temporary scaffold while the handlers in this file are
// being filled in. DispatchTestRun and the capability handlers in
// test_capability.go still use it.
func notImplemented(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "not implemented yet")
}

// copyRunCaseEvidenceToIssue files every evidence attachment of a run case
// under an issue as a second row over the same object. DeleteAttachment keeps
// the object while any row still references it.
func (h *Handler) copyRunCaseEvidenceToIssue(ctx context.Context, rc db.TestRunCase, issueID pgtype.UUID, userUUID pgtype.UUID) int {
	evidence, err := h.Queries.ListAttachmentsByTestRunCase(ctx, db.ListAttachmentsByTestRunCaseParams{
		TestRunCaseID: rc.ID,
		WorkspaceID:   rc.WorkspaceID,
	})
	if err != nil {
		slog.Warn("copy evidence to defect: list failed", "run_case_id", uuidToString(rc.ID), "error", err)
		return 0
	}
	copied := 0
	for _, att := range evidence {
		if _, err := h.Queries.CreateAttachmentCopyForIssue(ctx, db.CreateAttachmentCopyForIssueParams{
			ID:           dbid.NewV7(),
			IssueID:      issueID,
			UploaderType: "member",
			UploaderID:   userUUID,
			ID_2:         att.ID,
			WorkspaceID:  rc.WorkspaceID,
		}); err != nil {
			slog.Warn("copy evidence to defect: copy failed", "attachment_id", uuidToString(att.ID), "error", err)
			continue
		}
		copied++
	}
	return copied
}
