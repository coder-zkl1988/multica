package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestCaseStep is one row of a case's structured procedure. Steps are a typed
// array rather than a markdown blob precisely so an agent can execute them:
// Repo names a test_case_repo alias when the step runs against a specific
// repository of a multi-repo project.
type TestCaseStep struct {
	Index    int32  `json:"index"`
	Action   string `json:"action"`
	Expected string `json:"expected"`
	Repo     string `json:"repo,omitempty"`
}

// TestCaseRepoResponse binds a case to one repository of its project. The
// binding is by project_resource id, not repo URL: the URL can change, the
// resource id is stable within the workspace and is already shipped to agents
// in the task claim payload.
type TestCaseRepoResponse struct {
	ProjectResourceID string   `json:"project_resource_id"`
	Alias             string   `json:"alias"`
	Role              string   `json:"role"`
	PathGlobs         []string `json:"path_globs"`
}

type TestCaseResponse struct {
	ID                   string                 `json:"id"`
	WorkspaceID          string                 `json:"workspace_id"`
	ProjectID            string                 `json:"project_id"`
	CaseNumber           int32                  `json:"case_number"`
	Key                  string                 `json:"key"`
	Title                string                 `json:"title"`
	Module               string                 `json:"module"`
	Preconditions        string                 `json:"preconditions"`
	Steps                []TestCaseStep         `json:"steps"`
	ExpectedResult       string                 `json:"expected_result"`
	TestData             map[string]any         `json:"test_data"`
	Priority             string                 `json:"priority"`
	CaseType             string                 `json:"case_type"`
	Scope                string                 `json:"scope"`
	ExecutionMode        string                 `json:"execution_mode"`
	RequiredCapabilities []map[string]any       `json:"required_capabilities"`
	BusinessRulesRef     []string               `json:"business_rules_ref"`
	Status               string                 `json:"status"`
	Origin               string                 `json:"origin"`
	SourceRefs           map[string]any         `json:"source_refs"`
	GenerationJobID      *string                `json:"generation_job_id"`
	Version              int32                  `json:"version"`
	Repos                []TestCaseRepoResponse `json:"repos"`
	CreatedBy            *string                `json:"created_by"`
	UpdatedBy            *string                `json:"updated_by"`
	ReviewedBy           *string                `json:"reviewed_by"`
	ReviewedAt           *string                `json:"reviewed_at"`
	CreatedAt            string                 `json:"created_at"`
	UpdatedAt            string                 `json:"updated_at"`
}

type TestCaseRevisionResponse struct {
	ID            string         `json:"id"`
	TestCaseID    string         `json:"test_case_id"`
	Version       int32          `json:"version"`
	Snapshot      map[string]any `json:"snapshot"`
	ChangeKind    string         `json:"change_kind"`
	ChangedBy     *string        `json:"changed_by"`
	ChangedByType string         `json:"changed_by_type"`
	Note          string         `json:"note"`
	CreatedAt     string         `json:"created_at"`
}

// decodeJSONColumn unmarshals a JSONB column into dst. A column that fails to
// parse leaves dst at its zero value and logs once: the columns all carry a
// DEFAULT and downstream UI is written defensively, so a single malformed row
// must degrade to an empty field rather than fail the whole list request.
func decodeJSONColumn(raw []byte, dst any, field string, caseID pgtype.UUID) {
	if len(raw) == 0 {
		return
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		slog.Warn("test case column failed to decode",
			"field", field, "test_case_id", uuidToString(caseID), "error", err)
	}
}

func testCaseRepoToResponse(repo db.TestCaseRepo) TestCaseRepoResponse {
	globs := []string{}
	if len(repo.PathGlobs) > 0 {
		if err := json.Unmarshal(repo.PathGlobs, &globs); err != nil {
			slog.Warn("test case repo path_globs failed to decode",
				"test_case_id", uuidToString(repo.TestCaseID), "error", err)
			globs = []string{}
		}
	}
	return TestCaseRepoResponse{
		ProjectResourceID: uuidToString(repo.ProjectResourceID),
		Alias:             repo.Alias,
		Role:              repo.Role,
		PathGlobs:         globs,
	}
}

func testCaseToResponse(testCase db.TestCase, repos []db.TestCaseRepo) TestCaseResponse {
	steps := []TestCaseStep{}
	testData := map[string]any{}
	capabilities := []map[string]any{}
	businessRules := []string{}
	sourceRefs := map[string]any{}

	decodeJSONColumn(testCase.Steps, &steps, "steps", testCase.ID)
	decodeJSONColumn(testCase.TestData, &testData, "test_data", testCase.ID)
	decodeJSONColumn(testCase.RequiredCapabilities, &capabilities, "required_capabilities", testCase.ID)
	decodeJSONColumn(testCase.BusinessRulesRef, &businessRules, "business_rules_ref", testCase.ID)
	decodeJSONColumn(testCase.SourceRefs, &sourceRefs, "source_refs", testCase.ID)

	repoResponses := make([]TestCaseRepoResponse, 0, len(repos))
	for _, repo := range repos {
		repoResponses = append(repoResponses, testCaseRepoToResponse(repo))
	}

	return TestCaseResponse{
		ID:                   uuidToString(testCase.ID),
		WorkspaceID:          uuidToString(testCase.WorkspaceID),
		ProjectID:            uuidToString(testCase.ProjectID),
		CaseNumber:           testCase.CaseNumber,
		Key:                  formatTestCaseKey(testCase.CaseNumber),
		Title:                testCase.Title,
		Module:               testCase.Module,
		Preconditions:        testCase.Preconditions,
		Steps:                steps,
		ExpectedResult:       testCase.ExpectedResult,
		TestData:             testData,
		Priority:             testCase.Priority,
		CaseType:             testCase.CaseType,
		Scope:                testCase.Scope,
		ExecutionMode:        testCase.ExecutionMode,
		RequiredCapabilities: capabilities,
		BusinessRulesRef:     businessRules,
		Status:               testCase.Status,
		Origin:               testCase.Origin,
		SourceRefs:           sourceRefs,
		GenerationJobID:      uuidToPtr(testCase.GenerationJobID),
		Version:              testCase.Version,
		Repos:                repoResponses,
		CreatedBy:            uuidToPtr(testCase.CreatedBy),
		UpdatedBy:            uuidToPtr(testCase.UpdatedBy),
		ReviewedBy:           uuidToPtr(testCase.ReviewedBy),
		ReviewedAt:           timestampToPtr(testCase.ReviewedAt),
		CreatedAt:            timestampToString(testCase.CreatedAt),
		UpdatedAt:            timestampToString(testCase.UpdatedAt),
	}
}

func testCaseRevisionToResponse(revision db.TestCaseRevision) TestCaseRevisionResponse {
	snapshot := map[string]any{}
	if len(revision.Snapshot) > 0 {
		if err := json.Unmarshal(revision.Snapshot, &snapshot); err != nil {
			slog.Warn("test case revision snapshot failed to decode",
				"revision_id", uuidToString(revision.ID), "error", err)
		}
	}
	return TestCaseRevisionResponse{
		ID:            uuidToString(revision.ID),
		TestCaseID:    uuidToString(revision.TestCaseID),
		Version:       revision.Version,
		Snapshot:      snapshot,
		ChangeKind:    revision.ChangeKind,
		ChangedBy:     uuidToPtr(revision.ChangedBy),
		ChangedByType: revision.ChangedByType,
		Note:          revision.Note,
		CreatedAt:     timestampToString(revision.CreatedAt),
	}
}

// validTestCase* mirror the CHECK constraints on test_case (migration 280).
// Pre-validating here turns a typo into a 400 that names the allowed values
// instead of surfacing the DB CHECK violation as a 500 — same reasoning as
// validProjectStatuses.
var (
	validTestCasePriorities = []string{"p0", "p1", "p2", "p3"}
	validTestCaseTypes      = []string{
		"functional", "business_flow", "api", "ui", "e2e", "regression",
		"boundary", "exception", "permission", "data_consistency",
		"compatibility", "performance", "security",
	}
	validTestCaseScopes         = []string{"single_repo", "cross_repo", "no_repo"}
	validTestCaseExecutionModes = []string{"manual", "agent", "both"}
	validTestCaseStatuses       = []string{"draft", "active", "deprecated"}
	validTestCaseRepoRoles      = []string{"under_test", "driver", "verifier", "fixture"}
)

// testCaseRepoResourceTypes are the project resource types a case may bind to.
// Both carry code: github_repo is a remote checkout, local_directory is a
// daemon-local path. Anything else (a future document resource, say) is not a
// repository and is rejected.
var testCaseRepoResourceTypes = []string{"github_repo", "local_directory"}

func validateTestCaseEnum(w http.ResponseWriter, field, value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	writeError(w, http.StatusBadRequest,
		fmt.Sprintf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", ")))
	return false
}

// writeTestCaseWriteError maps a failed INSERT/UPDATE to an HTTP response. A
// CHECK violation is a client error; pre-validation already covers every
// constrained column, so this only backstops drift. Anything else is a genuine
// server fault and is logged so transient DB failures stay diagnosable.
func (h *Handler) writeTestCaseWriteError(w http.ResponseWriter, r *http.Request, err error, action string) {
	if isCheckViolation(err) {
		writeError(w, http.StatusBadRequest, "test case "+action+" rejected: a field value failed a database constraint")
		return
	}
	slog.Error("test case "+action+" failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to "+action+" test case")
}

type TestCaseRepoPayload struct {
	ProjectResourceID string   `json:"project_resource_id"`
	Alias             string   `json:"alias"`
	Role              string   `json:"role"`
	PathGlobs         []string `json:"path_globs"`
}

type CreateTestCaseRequest struct {
	ProjectID            string                `json:"project_id"`
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
	Status               string                `json:"status"`
	Repos                []TestCaseRepoPayload `json:"repos"`
}

type UpdateTestCaseRequest struct {
	Title                *string                `json:"title"`
	Module               *string                `json:"module"`
	Preconditions        *string                `json:"preconditions"`
	Steps                *[]TestCaseStep        `json:"steps"`
	ExpectedResult       *string                `json:"expected_result"`
	TestData             *map[string]any        `json:"test_data"`
	Priority             *string                `json:"priority"`
	CaseType             *string                `json:"case_type"`
	Scope                *string                `json:"scope"`
	ExecutionMode        *string                `json:"execution_mode"`
	RequiredCapabilities *[]map[string]any      `json:"required_capabilities"`
	BusinessRulesRef     *[]string              `json:"business_rules_ref"`
	Status               *string                `json:"status"`
	Repos                *[]TestCaseRepoPayload `json:"repos"`
	Note                 string                 `json:"note"`
}

// normalizeTestCaseSteps renumbers steps 1..n. The client is free to send any
// indexes (or none); persisting a canonical sequence means an agent executing
// the case can rely on step order without re-deriving it.
func normalizeTestCaseSteps(steps []TestCaseStep) []TestCaseStep {
	normalized := make([]TestCaseStep, 0, len(steps))
	for i, step := range steps {
		step.Index = int32(i + 1)
		normalized = append(normalized, step)
	}
	return normalized
}

// testCaseStepsProblem reports why a procedure cannot be executed, or "" when
// it can. A case is the thing a tester or an agent checks the product against:
// with no step, or a step whose expected result is blank, there is nothing to
// compare, and an execution of it can only ever come back "passed". Drafts may
// be incomplete — that is what a draft is — so callers apply this when a case
// is, or is about to become, active.
func testCaseStepsProblem(steps []TestCaseStep) string {
	if len(steps) == 0 {
		return "an active test case needs at least one step"
	}
	for i, step := range steps {
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Sprintf("step %d has no action", i+1)
		}
		if strings.TrimSpace(step.Expected) == "" {
			return fmt.Sprintf("step %d has no expected result: every step needs one an executor can check", i+1)
		}
	}
	return ""
}

// decodeTestCaseSteps reads the persisted steps column. A malformed column
// decodes to no steps, which testCaseStepsProblem then reports.
func decodeTestCaseSteps(raw []byte) []TestCaseStep {
	steps := []TestCaseStep{}
	if len(raw) == 0 {
		return steps
	}
	if err := json.Unmarshal(raw, &steps); err != nil {
		return []TestCaseStep{}
	}
	return steps
}

func marshalJSONColumn(value any, fallback string) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte(fallback)
	}
	return raw
}

// validateTestCaseRepos checks every binding points at a resource of this
// project that actually carries code, and normalizes it for insertion. There
// is no foreign key, so this is the only thing standing between a typo and a
// dangling reference.
func (h *Handler) validateTestCaseRepos(
	ctx context.Context,
	w http.ResponseWriter,
	wsUUID pgtype.UUID,
	projectID pgtype.UUID,
	payloads []TestCaseRepoPayload,
) ([]db.CreateTestCaseRepoParams, bool) {
	params := make([]db.CreateTestCaseRepoParams, 0, len(payloads))
	seen := make(map[string]struct{}, len(payloads))
	for _, payload := range payloads {
		resourceUUID, ok := parseUUIDOrBadRequest(w, payload.ProjectResourceID, "project_resource_id")
		if !ok {
			return nil, false
		}
		alias := strings.TrimSpace(payload.Alias)
		if alias == "" {
			writeError(w, http.StatusBadRequest, "each related repository needs a non-empty alias")
			return nil, false
		}
		role := payload.Role
		if role == "" {
			role = "under_test"
		}
		if !validateTestCaseEnum(w, "role", role, validTestCaseRepoRoles) {
			return nil, false
		}
		dedupeKey := payload.ProjectResourceID + "\x00" + role
		if _, duplicate := seen[dedupeKey]; duplicate {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("repository %s is bound twice with role %q", payload.ProjectResourceID, role))
			return nil, false
		}
		seen[dedupeKey] = struct{}{}

		resource, err := h.Queries.GetProjectResourceInWorkspace(ctx, db.GetProjectResourceInWorkspaceParams{
			ID:          resourceUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "project resource not found: "+payload.ProjectResourceID)
			return nil, false
		}
		if resource.ProjectID != projectID {
			writeError(w, http.StatusBadRequest,
				"project resource "+payload.ProjectResourceID+" belongs to a different project")
			return nil, false
		}
		if !validateTestCaseEnum(w, "resource_type", resource.ResourceType, testCaseRepoResourceTypes) {
			return nil, false
		}

		globs := payload.PathGlobs
		if globs == nil {
			globs = []string{}
		}
		params = append(params, db.CreateTestCaseRepoParams{
			WorkspaceID:       wsUUID,
			ProjectResourceID: resourceUUID,
			Alias:             alias,
			Role:              role,
			PathGlobs:         marshalJSONColumn(globs, "[]"),
		})
	}
	return params, true
}

func (h *Handler) ListTestCases(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	params := db.ListTestCasesParams{WorkspaceID: wsUUID}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		projectUUID, ok := parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return
		}
		params.ProjectID = projectUUID
	}
	for _, filter := range []struct {
		query string
		dst   *pgtype.Text
	}{
		{"status", &params.Status},
		{"module", &params.Module},
		{"priority", &params.Priority},
		{"case_type", &params.CaseType},
		{"origin", &params.Origin},
	} {
		if value := r.URL.Query().Get(filter.query); value != "" {
			*filter.dst = pgtype.Text{String: value, Valid: true}
		}
	}

	testCases, err := h.Queries.ListTestCases(r.Context(), params)
	if err != nil {
		slog.Error("list test cases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test cases")
		return
	}

	// One batched lookup for every case's repo bindings. Resolving them per
	// case would be an N+1 on the most-visited screen in the domain.
	reposByCase := map[string][]db.TestCaseRepo{}
	if len(testCases) > 0 {
		caseIDs := make([]pgtype.UUID, len(testCases))
		for i, testCase := range testCases {
			caseIDs[i] = testCase.ID
		}
		repos, err := h.Queries.ListTestCaseReposForCases(r.Context(), caseIDs)
		if err == nil {
			for _, repo := range repos {
				key := uuidToString(repo.TestCaseID)
				reposByCase[key] = append(reposByCase[key], repo)
			}
		} else {
			slog.Warn("list test case repos failed", append(logger.RequestAttrs(r), "error", err)...)
		}
	}

	resp := make([]TestCaseResponse, len(testCases))
	for i, testCase := range testCases {
		resp[i] = testCaseToResponse(testCase, reposByCase[uuidToString(testCase.ID)])
	}
	writeJSON(w, http.StatusOK, map[string]any{"test_cases": resp, "total": len(resp)})
}

func (h *Handler) ListTestCaseModules(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("project_id"), "project_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListTestCaseModules(r.Context(), db.ListTestCaseModulesParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectUUID,
	})
	if err != nil {
		slog.Error("list test case modules failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test case modules")
		return
	}
	modules := make([]map[string]any, len(rows))
	for i, row := range rows {
		modules[i] = map[string]any{"module": row.Module, "case_count": row.CaseCount}
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": modules})
}

func (h *Handler) GetTestCase(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	repos, err := h.Queries.ListTestCaseRepos(r.Context(), testCase.ID)
	if err != nil {
		slog.Warn("list test case repos failed", append(logger.RequestAttrs(r), "error", err)...)
		repos = nil
	}
	writeJSON(w, http.StatusOK, testCaseToResponse(testCase, repos))
}

func (h *Handler) CreateTestCase(w http.ResponseWriter, r *http.Request) {
	var req CreateTestCaseRequest
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
	// There is no foreign key: the project has to be proven to live in this
	// workspace before anything references it.
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "project not found")
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = "p2"
	}
	caseType := req.CaseType
	if caseType == "" {
		caseType = "functional"
	}
	scope := req.Scope
	if scope == "" {
		scope = "single_repo"
	}
	executionMode := req.ExecutionMode
	if executionMode == "" {
		executionMode = "manual"
	}
	steps := normalizeTestCaseSteps(req.Steps)
	// A hand-authored case is live on creation once it can be executed. One
	// with nothing to check yet — the issue page's "new case" button creates
	// it from a title alone and opens the editor — lands as a draft and is
	// activated when its steps are filled in. An author who wants to park a
	// case may still pass status explicitly.
	status := req.Status
	if status == "" {
		status = "active"
		if testCaseStepsProblem(steps) != "" {
			status = "draft"
		}
	}
	if !validateTestCaseEnum(w, "priority", priority, validTestCasePriorities) ||
		!validateTestCaseEnum(w, "case_type", caseType, validTestCaseTypes) ||
		!validateTestCaseEnum(w, "scope", scope, validTestCaseScopes) ||
		!validateTestCaseEnum(w, "execution_mode", executionMode, validTestCaseExecutionModes) ||
		!validateTestCaseEnum(w, "status", status, validTestCaseStatuses) {
		return
	}
	if status == "active" {
		if problem := testCaseStepsProblem(steps); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
	}

	repoParams, ok := h.validateTestCaseRepos(r.Context(), w, wsUUID, project.ID, req.Repos)
	if !ok {
		return
	}

	testData := req.TestData
	if testData == nil {
		testData = map[string]any{}
	}
	capabilities := req.RequiredCapabilities
	if capabilities == nil {
		capabilities = []map[string]any{}
	}
	businessRules := req.BusinessRulesRef
	if businessRules == nil {
		businessRules = []string{}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Takes the workspace row lock, so two concurrent creates cannot allocate
	// the same case number. Same mechanism as issue numbering.
	caseNumber, err := qtx.IncrementTestCaseCounter(r.Context(), wsUUID)
	if err != nil {
		slog.Error("increment test case counter failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to allocate a test case number")
		return
	}

	testCase, err := qtx.CreateTestCase(r.Context(), db.CreateTestCaseParams{
		WorkspaceID:          wsUUID,
		ProjectID:            project.ID,
		CaseNumber:           caseNumber,
		Title:                strings.TrimSpace(req.Title),
		Module:               req.Module,
		Preconditions:        req.Preconditions,
		Steps:                marshalJSONColumn(steps, "[]"),
		ExpectedResult:       req.ExpectedResult,
		TestData:             marshalJSONColumn(testData, "{}"),
		Priority:             priority,
		CaseType:             caseType,
		Scope:                scope,
		ExecutionMode:        executionMode,
		RequiredCapabilities: marshalJSONColumn(capabilities, "[]"),
		BusinessRulesRef:     marshalJSONColumn(businessRules, "[]"),
		Status:               status,
		Origin:               "human",
		SourceRefs:           []byte("{}"),
		CreatedBy:            userUUID,
		UpdatedBy:            userUUID,
	})
	if err != nil {
		h.writeTestCaseWriteError(w, r, err, "create")
		return
	}

	repos := make([]db.TestCaseRepo, 0, len(repoParams))
	for _, params := range repoParams {
		params.TestCaseID = testCase.ID
		repo, err := qtx.CreateTestCaseRepo(r.Context(), params)
		if err != nil {
			h.writeTestCaseWriteError(w, r, err, "create")
			return
		}
		repos = append(repos, repo)
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit test case create failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create test case")
		return
	}

	resp := testCaseToResponse(testCase, repos)
	h.publish(protocol.EventTestCaseCreated, workspaceID, "member", userID, map[string]any{"test_case": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateTestCase(w http.ResponseWriter, r *http.Request) {
	current, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	var req UpdateTestCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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

	params := db.UpdateTestCaseParams{
		ID:          current.ID,
		WorkspaceID: current.WorkspaceID,
		UpdatedBy:   userUUID,
	}
	for _, field := range []struct {
		name    string
		value   *string
		allowed []string
		dst     *pgtype.Text
	}{
		{"priority", req.Priority, validTestCasePriorities, &params.Priority},
		{"case_type", req.CaseType, validTestCaseTypes, &params.CaseType},
		{"scope", req.Scope, validTestCaseScopes, &params.Scope},
		{"execution_mode", req.ExecutionMode, validTestCaseExecutionModes, &params.ExecutionMode},
		{"status", req.Status, validTestCaseStatuses, &params.Status},
	} {
		if field.value == nil {
			continue
		}
		if !validateTestCaseEnum(w, field.name, *field.value, field.allowed) {
			return
		}
		*field.dst = pgtype.Text{String: *field.value, Valid: true}
	}
	for _, field := range []struct {
		value *string
		dst   *pgtype.Text
	}{
		{req.Title, &params.Title},
		{req.Module, &params.Module},
		{req.Preconditions, &params.Preconditions},
		{req.ExpectedResult, &params.ExpectedResult},
	} {
		if field.value != nil {
			*field.dst = pgtype.Text{String: *field.value, Valid: true}
		}
	}
	if params.Title.Valid && strings.TrimSpace(params.Title.String) == "" {
		writeError(w, http.StatusBadRequest, "title cannot be empty")
		return
	}
	if req.Steps != nil {
		params.Steps = marshalJSONColumn(normalizeTestCaseSteps(*req.Steps), "[]")
	}
	// An active case must stay executable. Checked only when this write
	// touches the steps or turns the case active, so an unrelated edit to a
	// legacy case that predates the rule (a title fix) is not refused.
	effectiveStatus := current.Status
	if params.Status.Valid {
		effectiveStatus = params.Status.String
	}
	if effectiveStatus == "active" && (req.Steps != nil || (params.Status.Valid && current.Status != "active")) {
		effectiveSteps := decodeTestCaseSteps(current.Steps)
		if req.Steps != nil {
			effectiveSteps = normalizeTestCaseSteps(*req.Steps)
		}
		if problem := testCaseStepsProblem(effectiveSteps); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
	}
	if req.TestData != nil {
		params.TestData = marshalJSONColumn(*req.TestData, "{}")
	}
	if req.RequiredCapabilities != nil {
		params.RequiredCapabilities = marshalJSONColumn(*req.RequiredCapabilities, "[]")
	}
	if req.BusinessRulesRef != nil {
		params.BusinessRulesRef = marshalJSONColumn(*req.BusinessRulesRef, "[]")
	}

	var repoParams []db.CreateTestCaseRepoParams
	if req.Repos != nil {
		repoParams, ok = h.validateTestCaseRepos(r.Context(), w, current.WorkspaceID, current.ProjectID, *req.Repos)
		if !ok {
			return
		}
	}

	currentRepos, err := h.Queries.ListTestCaseRepos(r.Context(), current.ID)
	if err != nil {
		slog.Warn("list test case repos failed", append(logger.RequestAttrs(r), "error", err)...)
		currentRepos = nil
	}

	updated, repos, ok := h.applyTestCaseUpdate(w, r, current, currentRepos, params, repoParams, req.Repos != nil, "human_edit", req.Note, userUUID)
	if !ok {
		return
	}

	resp := testCaseToResponse(updated, repos)
	h.publish(protocol.EventTestCaseUpdated, workspaceID, "member", userID, map[string]any{"test_case": resp})
	writeJSON(w, http.StatusOK, resp)
}

// applyTestCaseUpdate snapshots the pre-change case and applies the update in
// one transaction. Snapshot-then-update has to be atomic: a crash between the
// two would leave a revision row describing a change that never landed.
func (h *Handler) applyTestCaseUpdate(
	w http.ResponseWriter,
	r *http.Request,
	current db.TestCase,
	currentRepos []db.TestCaseRepo,
	params db.UpdateTestCaseParams,
	repoParams []db.CreateTestCaseRepoParams,
	replaceRepos bool,
	changeKind string,
	note string,
	actorUUID pgtype.UUID,
) (db.TestCase, []db.TestCaseRepo, bool) {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return db.TestCase{}, nil, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	snapshot := marshalJSONColumn(testCaseToResponse(current, currentRepos), "{}")
	if _, err := qtx.CreateTestCaseRevision(r.Context(), db.CreateTestCaseRevisionParams{
		WorkspaceID:   current.WorkspaceID,
		TestCaseID:    current.ID,
		Version:       current.Version,
		Snapshot:      snapshot,
		ChangeKind:    changeKind,
		ChangedBy:     actorUUID,
		ChangedByType: "member",
		Note:          note,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "update")
		return db.TestCase{}, nil, false
	}

	updated, err := qtx.UpdateTestCase(r.Context(), params)
	if err != nil {
		h.writeTestCaseWriteError(w, r, err, "update")
		return db.TestCase{}, nil, false
	}

	repos := currentRepos
	if replaceRepos {
		if err := qtx.DeleteTestCaseRepos(r.Context(), db.DeleteTestCaseReposParams{
			TestCaseID:  current.ID,
			WorkspaceID: current.WorkspaceID,
		}); err != nil {
			h.writeTestCaseWriteError(w, r, err, "update")
			return db.TestCase{}, nil, false
		}
		repos = make([]db.TestCaseRepo, 0, len(repoParams))
		for _, repoParam := range repoParams {
			repoParam.TestCaseID = current.ID
			repo, err := qtx.CreateTestCaseRepo(r.Context(), repoParam)
			if err != nil {
				h.writeTestCaseWriteError(w, r, err, "update")
				return db.TestCase{}, nil, false
			}
			repos = append(repos, repo)
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit test case update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update test case")
		return db.TestCase{}, nil, false
	}
	return updated, repos, true
}

func (h *Handler) ApproveTestCase(w http.ResponseWriter, r *http.Request) {
	current, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	if current.Status != "draft" {
		writeError(w, http.StatusConflict, "only a draft test case can be approved")
		return
	}
	// Approval is the gate between "someone wrote this" and "rounds execute
	// this": a draft that cannot be checked step by step is not approved.
	if problem := testCaseStepsProblem(decodeTestCaseSteps(current.Steps)); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
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
	currentRepos, err := h.Queries.ListTestCaseRepos(r.Context(), current.ID)
	if err != nil {
		slog.Warn("list test case repos failed", append(logger.RequestAttrs(r), "error", err)...)
		currentRepos = nil
	}

	params := db.UpdateTestCaseParams{
		ID:          current.ID,
		WorkspaceID: current.WorkspaceID,
		Status:      pgtype.Text{String: "active", Valid: true},
		ReviewedBy:  userUUID,
		ReviewedAt:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedBy:   userUUID,
	}
	updated, repos, ok := h.applyTestCaseUpdate(w, r, current, currentRepos, params, nil, false, "status_change", "approved", userUUID)
	if !ok {
		return
	}

	resp := testCaseToResponse(updated, repos)
	h.publish(protocol.EventTestCaseUpdated, workspaceID, "member", userID, map[string]any{"test_case": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteTestCase(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
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

	// Repo bindings, revisions and pending AI proposals have no cascade, so
	// they are swept here. Atomic with the delete: a partial sweep would orphan
	// rows that no surviving row points at.
	if err := qtx.DeleteTestCaseRepos(r.Context(), db.DeleteTestCaseReposParams{
		TestCaseID: testCase.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "delete")
		return
	}
	if err := qtx.DeleteTestCaseProposalsForCase(r.Context(), db.DeleteTestCaseProposalsForCaseParams{
		TargetCaseID: testCase.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "delete")
		return
	}
	if err := qtx.DeleteTestCaseRevisions(r.Context(), db.DeleteTestCaseRevisionsParams{
		TestCaseID: testCase.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "delete")
		return
	}
	if err := qtx.DeleteTestCaseIssueLinksForCase(r.Context(), db.DeleteTestCaseIssueLinksForCaseParams{
		TestCaseID: testCase.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "delete")
		return
	}
	if err := qtx.DeleteTestCaseTestPointLinksForCase(r.Context(), db.DeleteTestCaseTestPointLinksForCaseParams{
		TestCaseID: testCase.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "delete")
		return
	}
	if err := qtx.DeleteTestCase(r.Context(), db.DeleteTestCaseParams{
		ID: testCase.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		h.writeTestCaseWriteError(w, r, err, "delete")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit test case delete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete test case")
		return
	}

	h.publish(protocol.EventTestCaseDeleted, workspaceID, "member", userID,
		map[string]any{"test_case_id": uuidToString(testCase.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

const (
	testCaseRevisionDefaultLimit = 50
	testCaseRevisionMaxLimit     = 200
)

func (h *Handler) ListTestCaseRevisions(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	limit := int32(testCaseRevisionDefaultLimit)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > testCaseRevisionMaxLimit {
			parsed = testCaseRevisionMaxLimit
		}
		limit = int32(parsed)
	}
	revisions, err := h.Queries.ListTestCaseRevisions(r.Context(), db.ListTestCaseRevisionsParams{
		TestCaseID:  testCase.ID,
		WorkspaceID: testCase.WorkspaceID,
		Limit:       limit,
	})
	if err != nil {
		slog.Error("list test case revisions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test case revisions")
		return
	}
	resp := make([]TestCaseRevisionResponse, len(revisions))
	for i, revision := range revisions {
		resp[i] = testCaseRevisionToResponse(revision)
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": resp})
}
