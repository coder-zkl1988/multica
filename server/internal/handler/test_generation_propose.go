package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// maxTestCaseProposalItems caps one writeback batch. A generation run that
// wants to emit more should call propose repeatedly; an unbounded batch would
// hold the workspace counter lock for an arbitrary time.
const maxTestCaseProposalItems = 200

var validTestCaseProposalKinds = []string{"new", "update", "obsolete"}

// TestCaseProposalCasePayload is the writable surface of a case as proposed by
// a generation run. project_id is deliberately absent: it comes from the job,
// so a run cannot write cases into a project it was not scoped to.
type TestCaseProposalCasePayload struct {
	Title                string                `json:"title"`
	Module               string                `json:"module"`
	Preconditions        string                `json:"preconditions"`
	Steps                []TestCaseStep        `json:"steps"`
	ExpectedResult       string                `json:"expected_result"`
	TestData             map[string]any        `json:"test_data"`
	Priority             string                `json:"priority"`
	CaseType             string                `json:"case_type"`
	Scope                string                `json:"scope"`
	ExecutionMode        string                `json:"execution_mode"`
	RequiredCapabilities []map[string]any      `json:"required_capabilities"`
	BusinessRulesRef     []string              `json:"business_rules_ref"`
	Repos                []TestCaseRepoPayload `json:"repos"`
	SourceRefs           map[string]any        `json:"source_refs"`
}

type TestCaseProposalItem struct {
	Kind      string                       `json:"kind"`
	Target    string                       `json:"target"`
	Case      *TestCaseProposalCasePayload `json:"case"`
	Rationale string                       `json:"rationale"`
}

type ProposeTestCasesRequest struct {
	Items []TestCaseProposalItem `json:"items"`
}

// requireTestGenerationTaskToken is the authoritative writeback gate. The
// pattern mirrors the task-token boundary in file.go: X-Task-ID is only
// trustworthy when the auth middleware set it from a task-scoped token, and
// that path is the only one that stamps X-Actor-Source=task_token and strips a
// client-forged X-Task-ID. Pinning the job's own agent_task_id on top means a
// run authorized for job A cannot write cases into job B.
func (h *Handler) requireTestGenerationTaskToken(w http.ResponseWriter, r *http.Request, job db.TestGenerationJob) bool {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "proposing test cases is only available from within an agent task")
		return false
	}
	boundTaskID := strings.TrimSpace(r.Header.Get("X-Task-ID"))
	if boundTaskID == "" {
		writeError(w, http.StatusForbidden, "this request carries no task token")
		return false
	}
	if !job.AgentTaskID.Valid {
		writeError(w, http.StatusConflict, "this generation job has not been dispatched")
		return false
	}
	if !strings.EqualFold(boundTaskID, uuidToString(job.AgentTaskID)) {
		writeError(w, http.StatusForbidden, "this task token does not own the generation job")
		return false
	}
	return true
}

// ProposeTestCases is the agent's writeback endpoint. New cases land straight
// in the library as drafts; update and obsolete suggestions against an already
// reviewed case go to test_case_proposal so a human decides, because silently
// rewriting an approved case would make "human review" a lie.
func (h *Handler) ProposeTestCases(w http.ResponseWriter, r *http.Request) {
	job, wsUUID, ok := h.loadTestGenerationJobForUser(w, r)
	if !ok {
		return
	}
	if !h.requireTestGenerationTaskToken(w, r, job) {
		return
	}

	var req ProposeTestCasesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items must not be empty")
		return
	}
	if len(req.Items) > maxTestCaseProposalItems {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("items holds %d entries; at most %d per call", len(req.Items), maxTestCaseProposalItems))
		return
	}

	// Validate the whole batch before opening the transaction so a malformed
	// entry at index 40 cannot leave the first 39 written.
	type resolvedItem struct {
		item   TestCaseProposalItem
		target db.TestCase
		repos  []db.CreateTestCaseRepoParams
	}
	resolved := make([]resolvedItem, 0, len(req.Items))
	for i, item := range req.Items {
		prefix := fmt.Sprintf("items[%d]: ", i)
		if !validateTestCaseEnum(w, prefix+"kind", item.Kind, validTestCaseProposalKinds) {
			return
		}
		entry := resolvedItem{item: item}

		if item.Kind == "obsolete" || item.Kind == "update" {
			target, found := h.lookupTestCaseByRef(r.Context(), wsUUID, item.Target)
			if !found {
				writeError(w, http.StatusBadRequest, prefix+"target test case not found: "+item.Target)
				return
			}
			if target.ProjectID != job.ProjectID {
				writeError(w, http.StatusBadRequest, prefix+"target test case belongs to a different project")
				return
			}
			entry.target = target
		}
		if item.Kind == "new" || item.Kind == "update" {
			if item.Case == nil {
				writeError(w, http.StatusBadRequest, prefix+"case is required for kind "+item.Kind)
				return
			}
			if strings.TrimSpace(item.Case.Title) == "" {
				writeError(w, http.StatusBadRequest, prefix+"case.title is required")
				return
			}
			if !h.validateProposalCaseEnums(w, prefix, item.Case) {
				return
			}
			repos, reposOK := h.validateTestCaseRepos(r.Context(), w, wsUUID, job.ProjectID, item.Case.Repos)
			if !reposOK {
				return
			}
			entry.repos = repos
		}
		resolved = append(resolved, entry)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	stats := map[string]int{"new": 0, "updated": 0, "obsolete": 0, "proposed": 0}
	createdCases := make([]TestCaseResponse, 0, len(resolved))
	createdProposals := make([]TestCaseProposalResponse, 0, len(resolved))

	for _, entry := range resolved {
		switch entry.item.Kind {
		case "new":
			created, repos, createErr := h.insertProposedTestCase(r, qtx, job, wsUUID, entry.item.Case, entry.repos)
			if createErr != nil {
				if writeIssueWindowViolation(w, createErr) {
					return
				}
				h.writeTestGenerationWriteError(w, r, createErr, "create")
				return
			}
			stats["new"]++
			createdCases = append(createdCases, testCaseToResponse(created, repos))

		case "update":
			// A draft case has not been reviewed by anyone yet, so a later run
			// may refine it directly. Piling proposals onto drafts would make
			// every re-run add review work instead of removing it.
			if entry.target.Status == "draft" {
				updated, repos, updateErr := h.rewriteDraftTestCase(r, qtx, wsUUID, entry.target, entry.item.Case, entry.repos)
				if updateErr != nil {
					h.writeTestGenerationWriteError(w, r, updateErr, "update")
					return
				}
				stats["updated"]++
				createdCases = append(createdCases, testCaseToResponse(updated, repos))
				continue
			}
			proposal, proposalErr := qtx.CreateTestCaseProposal(r.Context(), db.CreateTestCaseProposalParams{
				WorkspaceID:  wsUUID,
				JobID:        job.ID,
				TargetCaseID: entry.target.ID,
				Kind:         "update",
				Payload:      marshalJSONColumn(entry.item.Case, "{}"),
				Rationale:    entry.item.Rationale,
			})
			if proposalErr != nil {
				h.writeTestGenerationWriteError(w, r, proposalErr, "create")
				return
			}
			stats["proposed"]++
			createdProposals = append(createdProposals, testCaseProposalToResponse(proposal))

		case "obsolete":
			if entry.target.Status == "draft" {
				// Nobody has approved it, so retiring it needs no review.
				if _, delErr := qtx.UpdateTestCase(r.Context(), db.UpdateTestCaseParams{
					ID:          entry.target.ID,
					WorkspaceID: wsUUID,
					Status:      pgtype.Text{String: "deprecated", Valid: true},
				}); delErr != nil {
					h.writeTestGenerationWriteError(w, r, delErr, "update")
					return
				}
				stats["obsolete"]++
				continue
			}
			proposal, proposalErr := qtx.CreateTestCaseProposal(r.Context(), db.CreateTestCaseProposalParams{
				WorkspaceID:  wsUUID,
				JobID:        job.ID,
				TargetCaseID: entry.target.ID,
				Kind:         "obsolete",
				Payload:      []byte("{}"),
				Rationale:    entry.item.Rationale,
			})
			if proposalErr != nil {
				h.writeTestGenerationWriteError(w, r, proposalErr, "create")
				return
			}
			stats["proposed"]++
			createdProposals = append(createdProposals, testCaseProposalToResponse(proposal))
		}
	}

	result := unmarshalJSONObject(job.Result)
	previous, _ := result["stats"].(map[string]any)
	result["stats"] = mergeProposalStats(previous, stats)
	if _, err := qtx.UpdateTestGenerationJob(r.Context(), db.UpdateTestGenerationJobParams{
		ID:          job.ID,
		WorkspaceID: wsUUID,
		Result:      marshalJSONColumn(result, "{}"),
	}); err != nil {
		h.writeTestGenerationWriteError(w, r, err, "update")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit test case proposal batch failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to write the proposed test cases")
		return
	}

	workspaceID := uuidToString(wsUUID)
	actorID := uuidToString(job.AgentID)
	for _, testCase := range createdCases {
		h.publish(protocol.EventTestCaseCreated, workspaceID, "agent", actorID, map[string]any{"test_case": testCase})
	}
	for _, proposal := range createdProposals {
		h.publish(protocol.EventTestCaseProposalCreated, workspaceID, "agent", actorID, map[string]any{"proposal": proposal})
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"stats":      stats,
		"test_cases": createdCases,
		"proposals":  createdProposals,
	})
}

func mergeProposalStats(previous map[string]any, delta map[string]int) map[string]any {
	merged := map[string]any{}
	for key, value := range previous {
		merged[key] = value
	}
	for key, increment := range delta {
		existing := 0
		switch typed := merged[key].(type) {
		case float64:
			existing = int(typed)
		case int:
			existing = typed
		}
		merged[key] = existing + increment
	}
	return merged
}

// lookupTestCaseByRef resolves a TC-<n> key or a UUID without writing an HTTP
// error, so batch validation can report the offending index instead.
func (h *Handler) lookupTestCaseByRef(ctx context.Context, wsUUID pgtype.UUID, ref string) (db.TestCase, bool) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return db.TestCase{}, false
	}
	if number, isKey := parseTestCaseNumber(trimmed); isKey {
		testCase, err := h.Queries.GetTestCaseByNumber(ctx, db.GetTestCaseByNumberParams{
			WorkspaceID: wsUUID,
			CaseNumber:  number,
		})
		return testCase, err == nil
	}
	idUUID, err := util.ParseUUID(trimmed)
	if err != nil {
		return db.TestCase{}, false
	}
	testCase, queryErr := h.Queries.GetTestCaseInWorkspace(ctx, db.GetTestCaseInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	return testCase, queryErr == nil
}

func (h *Handler) validateProposalCaseEnums(w http.ResponseWriter, prefix string, payload *TestCaseProposalCasePayload) bool {
	if payload.Priority != "" && !validateTestCaseEnum(w, prefix+"case.priority", payload.Priority, validTestCasePriorities) {
		return false
	}
	if payload.CaseType != "" && !validateTestCaseEnum(w, prefix+"case.case_type", payload.CaseType, validTestCaseTypes) {
		return false
	}
	if payload.Scope != "" && !validateTestCaseEnum(w, prefix+"case.scope", payload.Scope, validTestCaseScopes) {
		return false
	}
	if payload.ExecutionMode != "" && !validateTestCaseEnum(w, prefix+"case.execution_mode", payload.ExecutionMode, validTestCaseExecutionModes) {
		return false
	}
	// A cross-repo case that names fewer than two roles is a mislabelled
	// single-repo case; the executing agent would have nothing to correlate.
	if payload.Scope == "cross_repo" {
		roles := map[string]struct{}{}
		for _, repo := range payload.Repos {
			role := repo.Role
			if role == "" {
				role = "under_test"
			}
			roles[role] = struct{}{}
		}
		if len(roles) < 2 {
			writeError(w, http.StatusBadRequest,
				prefix+"a cross_repo case needs at least two related repositories with different roles")
			return false
		}
	}
	return true
}

func proposalCaseDefaults(payload *TestCaseProposalCasePayload) (priority, caseType, scope, executionMode string) {
	priority = payload.Priority
	if priority == "" {
		priority = "p2"
	}
	caseType = payload.CaseType
	if caseType == "" {
		caseType = "functional"
	}
	scope = payload.Scope
	if scope == "" {
		scope = "single_repo"
	}
	executionMode = payload.ExecutionMode
	if executionMode == "" {
		executionMode = "manual"
	}
	return priority, caseType, scope, executionMode
}

func (h *Handler) insertProposedTestCase(
	r *http.Request,
	qtx *db.Queries,
	job db.TestGenerationJob,
	wsUUID pgtype.UUID,
	payload *TestCaseProposalCasePayload,
	repoParams []db.CreateTestCaseRepoParams,
) (db.TestCase, []db.TestCaseRepo, error) {
	caseNumber, err := qtx.IncrementTestCaseCounter(r.Context(), wsUUID)
	if err != nil {
		return db.TestCase{}, nil, fmt.Errorf("increment test case counter: %w", err)
	}
	priority, caseType, scope, executionMode := proposalCaseDefaults(payload)
	sourceRefs := payload.SourceRefs
	if sourceRefs == nil {
		sourceRefs = map[string]any{}
	}
	sourceRefs["generation_job_id"] = uuidToString(job.ID)

	testCase, err := qtx.CreateTestCase(r.Context(), db.CreateTestCaseParams{
		WorkspaceID:          wsUUID,
		ProjectID:            job.ProjectID,
		CaseNumber:           caseNumber,
		Title:                strings.TrimSpace(payload.Title),
		Module:               payload.Module,
		Preconditions:        payload.Preconditions,
		Steps:                marshalJSONColumn(normalizeTestCaseSteps(payload.Steps), "[]"),
		ExpectedResult:       payload.ExpectedResult,
		TestData:             marshalJSONColumn(defaultMap(payload.TestData), "{}"),
		Priority:             priority,
		CaseType:             caseType,
		Scope:                scope,
		ExecutionMode:        executionMode,
		RequiredCapabilities: marshalJSONColumn(defaultMapSlice(payload.RequiredCapabilities), "[]"),
		BusinessRulesRef:     marshalJSONColumn(defaultStringSlice(payload.BusinessRulesRef), "[]"),
		// Generated cases are unreviewed by definition.
		Status:          "draft",
		Origin:          "ai",
		SourceRefs:      marshalJSONColumn(sourceRefs, "{}"),
		GenerationJobID: job.ID,
		CreatedBy:       job.CreatedBy,
		UpdatedBy:       job.CreatedBy,
	})
	if err != nil {
		return db.TestCase{}, nil, err
	}
	repos := make([]db.TestCaseRepo, 0, len(repoParams))
	for _, params := range repoParams {
		params.TestCaseID = testCase.ID
		repo, repoErr := qtx.CreateTestCaseRepo(r.Context(), params)
		if repoErr != nil {
			return db.TestCase{}, nil, repoErr
		}
		repos = append(repos, repo)
	}
	// Carry the approved plan's issue scope onto the case. Without this the
	// provenance dies with the job and the generated case can never say what it
	// was written for.
	if err := h.linkGeneratedCaseIssues(
		r, qtx, testCase.ID, wsUUID, uuidToString(wsUUID), job.CreatedBy,
		h.testGenerationScopeIssueRefs(r, job, wsUUID),
	); err != nil {
		return db.TestCase{}, nil, err
	}
	return testCase, repos, nil
}

// testGenerationScopeIssueRefs reads the issue scope the agent actually worked
// under. The approved plan wins because a reviewer can edit the scope there
// before approving; the job's original input is only the fallback for a job
// whose plan row is missing.
func (h *Handler) testGenerationScopeIssueRefs(
	r *http.Request,
	job db.TestGenerationJob,
	wsUUID pgtype.UUID,
) []string {
	plan, err := h.Queries.GetTestGenerationPlanByJob(r.Context(), db.GetTestGenerationPlanByJobParams{
		JobID:       job.ID,
		WorkspaceID: wsUUID,
	})
	if err == nil {
		if payload := unmarshalJSONObject(plan.Plan); payload != nil {
			return stringsFromAny(payload["issues"])
		}
	}
	if input := unmarshalJSONObject(job.Input); input != nil {
		return stringsFromAny(input["issue_ids"])
	}
	return nil
}

// rewriteDraftTestCase applies a later run's refinement straight onto an
// unreviewed draft, leaving a revision snapshot so the change is still visible.
func (h *Handler) rewriteDraftTestCase(
	r *http.Request,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	current db.TestCase,
	payload *TestCaseProposalCasePayload,
	repoParams []db.CreateTestCaseRepoParams,
) (db.TestCase, []db.TestCaseRepo, error) {
	snapshot, err := json.Marshal(testCaseToResponse(current, nil))
	if err != nil {
		snapshot = []byte("{}")
	}
	if _, err := qtx.CreateTestCaseRevision(r.Context(), db.CreateTestCaseRevisionParams{
		WorkspaceID:   wsUUID,
		TestCaseID:    current.ID,
		Version:       current.Version,
		Snapshot:      snapshot,
		ChangeKind:    "proposal_accepted",
		ChangedBy:     current.CreatedBy,
		ChangedByType: "agent",
		Note:          "refined by a later generation run while still in draft",
	}); err != nil {
		return db.TestCase{}, nil, err
	}

	priority, caseType, scope, executionMode := proposalCaseDefaults(payload)
	updated, err := qtx.UpdateTestCase(r.Context(), db.UpdateTestCaseParams{
		ID:                   current.ID,
		WorkspaceID:          wsUUID,
		Title:                pgtype.Text{String: strings.TrimSpace(payload.Title), Valid: true},
		Module:               pgtype.Text{String: payload.Module, Valid: true},
		Preconditions:        pgtype.Text{String: payload.Preconditions, Valid: true},
		Steps:                marshalJSONColumn(normalizeTestCaseSteps(payload.Steps), "[]"),
		ExpectedResult:       pgtype.Text{String: payload.ExpectedResult, Valid: true},
		TestData:             marshalJSONColumn(defaultMap(payload.TestData), "{}"),
		Priority:             pgtype.Text{String: priority, Valid: true},
		CaseType:             pgtype.Text{String: caseType, Valid: true},
		Scope:                pgtype.Text{String: scope, Valid: true},
		ExecutionMode:        pgtype.Text{String: executionMode, Valid: true},
		RequiredCapabilities: marshalJSONColumn(defaultMapSlice(payload.RequiredCapabilities), "[]"),
		BusinessRulesRef:     marshalJSONColumn(defaultStringSlice(payload.BusinessRulesRef), "[]"),
	})
	if err != nil {
		return db.TestCase{}, nil, err
	}

	if err := qtx.DeleteTestCaseRepos(r.Context(), db.DeleteTestCaseReposParams{
		TestCaseID:  current.ID,
		WorkspaceID: wsUUID,
	}); err != nil {
		return db.TestCase{}, nil, err
	}
	repos := make([]db.TestCaseRepo, 0, len(repoParams))
	for _, params := range repoParams {
		params.TestCaseID = current.ID
		repo, repoErr := qtx.CreateTestCaseRepo(r.Context(), params)
		if repoErr != nil {
			return db.TestCase{}, nil, repoErr
		}
		repos = append(repos, repo)
	}
	return updated, repos, nil
}

func defaultMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func defaultMapSlice(value []map[string]any) []map[string]any {
	if value == nil {
		return []map[string]any{}
	}
	return value
}

// ListTestCaseProposals returns the review queue for one case.
func (h *Handler) ListTestCaseProposals(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	var statusFilter pgtype.Text
	if s := strings.TrimSpace(r.URL.Query().Get("status")); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	proposals, err := h.Queries.ListTestCaseProposalsForCase(r.Context(), db.ListTestCaseProposalsForCaseParams{
		WorkspaceID:  testCase.WorkspaceID,
		TargetCaseID: testCase.ID,
		Status:       statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list proposals")
		return
	}
	resp := make([]TestCaseProposalResponse, len(proposals))
	for i, proposal := range proposals {
		resp[i] = testCaseProposalToResponse(proposal)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": resp, "total": len(resp)})
}

func (h *Handler) AcceptTestCaseProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewTestCaseProposal(w, r, "accepted")
}

func (h *Handler) RejectTestCaseProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewTestCaseProposal(w, r, "rejected")
}

// reviewTestCaseProposal applies or discards one suggestion. Accepting writes a
// revision snapshot first, so a wrong call is reversible.
func (h *Handler) reviewTestCaseProposal(w http.ResponseWriter, r *http.Request, decision string) {
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
	proposalUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "proposal id")
	if !ok {
		return
	}
	proposal, err := h.Queries.GetTestCaseProposalInWorkspace(r.Context(), db.GetTestCaseProposalInWorkspaceParams{
		ID:          proposalUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}
	if proposal.Status != "pending" {
		writeError(w, http.StatusConflict, "this proposal has already been "+proposal.Status)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	var updatedCase *TestCaseResponse
	if decision == "accepted" {
		current, caseErr := qtx.GetTestCaseInWorkspace(r.Context(), db.GetTestCaseInWorkspaceParams{
			ID:          proposal.TargetCaseID,
			WorkspaceID: wsUUID,
		})
		if caseErr != nil {
			writeError(w, http.StatusConflict, "the target test case no longer exists")
			return
		}
		snapshot, marshalErr := json.Marshal(testCaseToResponse(current, nil))
		if marshalErr != nil {
			snapshot = []byte("{}")
		}
		if _, revErr := qtx.CreateTestCaseRevision(r.Context(), db.CreateTestCaseRevisionParams{
			WorkspaceID:   wsUUID,
			TestCaseID:    current.ID,
			Version:       current.Version,
			Snapshot:      snapshot,
			ChangeKind:    "proposal_accepted",
			ChangedBy:     userUUID,
			ChangedByType: "member",
			Note:          proposal.Rationale,
		}); revErr != nil {
			h.writeTestGenerationWriteError(w, r, revErr, "update")
			return
		}

		params := db.UpdateTestCaseParams{ID: current.ID, WorkspaceID: wsUUID, UpdatedBy: userUUID}
		if proposal.Kind == "obsolete" {
			params.Status = pgtype.Text{String: "deprecated", Valid: true}
		} else {
			var payload TestCaseProposalCasePayload
			if unmarshalErr := json.Unmarshal(proposal.Payload, &payload); unmarshalErr != nil {
				writeError(w, http.StatusConflict, "the proposal payload is not readable; reject it instead")
				return
			}
			priority, caseType, scope, executionMode := proposalCaseDefaults(&payload)
			params.Title = pgtype.Text{String: strings.TrimSpace(payload.Title), Valid: true}
			params.Module = pgtype.Text{String: payload.Module, Valid: true}
			params.Preconditions = pgtype.Text{String: payload.Preconditions, Valid: true}
			params.Steps = marshalJSONColumn(normalizeTestCaseSteps(payload.Steps), "[]")
			params.ExpectedResult = pgtype.Text{String: payload.ExpectedResult, Valid: true}
			params.TestData = marshalJSONColumn(defaultMap(payload.TestData), "{}")
			params.Priority = pgtype.Text{String: priority, Valid: true}
			params.CaseType = pgtype.Text{String: caseType, Valid: true}
			params.Scope = pgtype.Text{String: scope, Valid: true}
			params.ExecutionMode = pgtype.Text{String: executionMode, Valid: true}
			params.RequiredCapabilities = marshalJSONColumn(defaultMapSlice(payload.RequiredCapabilities), "[]")
			params.BusinessRulesRef = marshalJSONColumn(defaultStringSlice(payload.BusinessRulesRef), "[]")
		}
		applied, updateErr := qtx.UpdateTestCase(r.Context(), params)
		if updateErr != nil {
			h.writeTestGenerationWriteError(w, r, updateErr, "update")
			return
		}
		repos, reposErr := qtx.ListTestCaseRepos(r.Context(), applied.ID)
		if reposErr != nil {
			repos = []db.TestCaseRepo{}
		}
		resp := testCaseToResponse(applied, repos)
		updatedCase = &resp
	}

	reviewed, err := qtx.UpdateTestCaseProposalStatus(r.Context(), db.UpdateTestCaseProposalStatusParams{
		ID:          proposal.ID,
		WorkspaceID: wsUUID,
		Status:      decision,
		ReviewedBy:  userUUID,
	})
	if err != nil {
		h.writeTestGenerationWriteError(w, r, err, "update")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit proposal review failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record the review")
		return
	}

	resp := testCaseProposalToResponse(reviewed)
	h.publish(protocol.EventTestCaseProposalUpdated, workspaceID, "member", userID, map[string]any{"proposal": resp})
	payload := map[string]any{"proposal": resp}
	if updatedCase != nil {
		h.publish(protocol.EventTestCaseUpdated, workspaceID, "member", userID, map[string]any{"test_case": *updatedCase})
		payload["test_case"] = *updatedCase
	}
	writeJSON(w, http.StatusOK, payload)
}
