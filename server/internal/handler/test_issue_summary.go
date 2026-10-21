package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"encoding/json"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// M5 requirement loop: what a task card can say about its tests beyond the
// list of covering cases — the latest round, whether every covering case
// passes ("verified"), the defects those cases opened, and, for a defect
// issue, the round and case that found it. Verification is a badge, never a
// status change: 09-02 §8.3 keeps that decision with a person.

type IssueTestRunSummaryResponse struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Status      string           `json:"status"`
	CreatedAt   string           `json:"created_at"`
	CompletedAt *string          `json:"completed_at"`
	Results     map[string]int64 `json:"results"`
}

type IssueTestDefectResponse struct {
	IssueID     string  `json:"issue_id"`
	IssueNumber int32   `json:"issue_number"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	RunID       string  `json:"run_id"`
	RunTitle    string  `json:"run_title"`
	RunCaseID   string  `json:"run_case_id"`
	CaseKey     string  `json:"case_key"`
	Result      string  `json:"result"`
	OpenedAt    *string `json:"opened_at"`
}

type IssueFoundByResponse struct {
	RunID       string  `json:"run_id"`
	RunTitle    string  `json:"run_title"`
	RunStatus   string  `json:"run_status"`
	RunCaseID   string  `json:"run_case_id"`
	TestCaseID  string  `json:"test_case_id"`
	CaseKey     string  `json:"case_key"`
	CaseTitle   string  `json:"case_title"`
	Result      string  `json:"result"`
	Environment string  `json:"environment"`
	BuildRef    string  `json:"build_ref"`
	ExecutedAt  *string `json:"executed_at"`
}

type IssueTestSummaryResponse struct {
	// Cases is the number of covering cases. Verified is decided on Basis:
	//
	//	test_points  the issue has active test points: every one is covered by
	//	             an active case that passed, and no active linked case's
	//	             latest result is failed
	//	cases_only   no test points yet: at least one active linked case, and
	//	             every one's latest result is passed — a weaker claim the UI
	//	             labels as such, since nobody listed what must be checked
	//	none         nothing active covers the issue
	Cases      int                          `json:"cases"`
	Verified   bool                         `json:"verified"`
	Basis      string                       `json:"basis"`
	TestPoints TestPointSummaryResponse     `json:"test_points"`
	LatestRun  *IssueTestRunSummaryResponse `json:"latest_run"`
	Defects    []IssueTestDefectResponse    `json:"defects"`
	FoundBy    []IssueFoundByResponse       `json:"found_by"`
}

// GetIssueTestSummary answers GET /api/issues/{id}/test-summary.
func (h *Handler) GetIssueTestSummary(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ctx := r.Context()
	resp := IssueTestSummaryResponse{Defects: []IssueTestDefectResponse{}, FoundBy: []IssueFoundByResponse{}}

	links, err := h.Queries.ListTestCasesForIssue(ctx, db.ListTestCasesForIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Error("issue test summary: list cases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load test coverage")
		return
	}
	resp.Cases = len(links)
	activeCases, activePassed, activeFailing := 0, 0, 0
	for _, link := range links {
		// Only an active case is a claim about the product: a draft has not
		// been reviewed, and a deprecated case describes behaviour that is
		// gone. Neither may make an issue look verified.
		if link.CaseStatus != "active" {
			continue
		}
		activeCases++
		// The query reports "" for a case that never ran; that is not a pass.
		switch link.LatestResult {
		case "passed":
			activePassed++
		case "failed":
			activeFailing++
		}
	}
	_, pointSummary, err := h.issueTestPointCoverage(ctx, issue)
	if err != nil {
		slog.Error("issue test summary: test point coverage failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load test coverage")
		return
	}
	resp.TestPoints = pointSummary
	switch {
	case pointSummary.Active > 0:
		resp.Basis = "test_points"
		resp.Verified = pointSummary.Passed == pointSummary.Active && activeFailing == 0
	case activeCases > 0:
		resp.Basis = "cases_only"
		resp.Verified = activePassed == activeCases
	default:
		resp.Basis = "none"
	}

	if len(links) > 0 {
		run, err := h.Queries.GetLatestTestRunForIssue(ctx, db.GetLatestTestRunForIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
		switch {
		case err == nil:
			counts, err := h.Queries.CountIssueRunCaseResults(ctx, db.CountIssueRunCaseResultsParams{RunID: run.ID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
			if err != nil {
				slog.Warn("issue test summary: count results failed", append(logger.RequestAttrs(r), "error", err)...)
			}
			results := make(map[string]int64, len(counts))
			for _, c := range counts {
				results[c.Result] = c.ResultCount
			}
			resp.LatestRun = &IssueTestRunSummaryResponse{
				ID:          uuidToString(run.ID),
				Title:       run.Title,
				Status:      run.Status,
				CreatedAt:   timestampToString(run.CreatedAt),
				CompletedAt: timestampToPtr(run.CompletedAt),
				Results:     results,
			}
		case errors.Is(err, pgx.ErrNoRows):
			// Linked but never executed: no round to show.
		default:
			slog.Warn("issue test summary: latest run failed", append(logger.RequestAttrs(r), "error", err)...)
		}

		defects, err := h.Queries.ListDefectsForIssue(ctx, db.ListDefectsForIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			slog.Warn("issue test summary: list defects failed", append(logger.RequestAttrs(r), "error", err)...)
		}
		seen := make(map[string]struct{}, len(defects))
		for _, d := range defects {
			id := uuidToString(d.DefectID)
			// One defect per row even when several rounds re-opened it: the
			// newest finding wins, which is the order the query returns.
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			resp.Defects = append(resp.Defects, IssueTestDefectResponse{
				IssueID:     id,
				IssueNumber: d.DefectNumber,
				Title:       d.DefectTitle,
				Status:      d.DefectStatus,
				RunID:       uuidToString(d.RunID),
				RunTitle:    d.RunTitle,
				RunCaseID:   uuidToString(d.RunCaseID),
				CaseKey:     d.CaseKey,
				Result:      d.Result,
				OpenedAt:    timestampToPtr(d.OpenedAt),
			})
		}
	}

	found, err := h.Queries.ListRunCasesThatOpenedIssue(ctx, db.ListRunCasesThatOpenedIssueParams{DefectIssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Warn("issue test summary: found-by failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	for _, f := range found {
		resp.FoundBy = append(resp.FoundBy, IssueFoundByResponse{
			RunID:       uuidToString(f.RunID),
			RunTitle:    f.RunTitle,
			RunStatus:   f.RunStatus,
			RunCaseID:   uuidToString(f.RunCaseID),
			TestCaseID:  uuidToString(f.TestCaseID),
			CaseKey:     f.CaseKey,
			CaseTitle:   f.CaseTitle,
			Result:      f.Result,
			Environment: f.Environment,
			BuildRef:    f.BuildRef,
			ExecutedAt:  timestampToPtr(f.ExecutedAt),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── plan statistics ──────────────────────────────────────────────────

type TestPlanRunStatResponse struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Status      string           `json:"status"`
	CreatedAt   string           `json:"created_at"`
	CompletedAt *string          `json:"completed_at"`
	Results     map[string]int64 `json:"results"`
	Total       int64            `json:"total"`
	// PassRate is passed / (cases that reached a terminal result), 0..1;
	// null while nothing has finished.
	PassRate *float64 `json:"pass_rate"`
}

type TestPlanModuleStatResponse struct {
	Module  string           `json:"module"`
	Results map[string]int64 `json:"results"`
	Total   int64            `json:"total"`
}

type TestPlanStatsResponse struct {
	// Runs are the plan's newest rounds, newest first.
	Runs []TestPlanRunStatResponse `json:"runs"`
	// Matrix is module × result for the newest round (Runs[0]); nil without one.
	MatrixRunID string                       `json:"matrix_run_id"`
	Matrix      []TestPlanModuleStatResponse `json:"matrix"`
}

const (
	defaultTestPlanStatRuns = 10
	maxTestPlanStatRuns     = 50
)

func testRunPassRate(results map[string]int64) *float64 {
	terminal := results["passed"] + results["failed"] + results["blocked"] + results["skipped"]
	if terminal == 0 {
		return nil
	}
	rate := float64(results["passed"]) / float64(terminal)
	return &rate
}

// GetTestPlanStats answers GET /api/test-plans/{id}/stats?runs=N: pass rate
// per recent round and the module × result matrix of the newest one — the
// board a plan owner reads instead of opening every round.
func (h *Handler) GetTestPlanStats(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	limit := defaultTestPlanStatRuns
	if raw := strings.TrimSpace(r.URL.Query().Get("runs")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxTestPlanStatRuns {
		limit = maxTestPlanStatRuns
	}
	ctx := r.Context()
	runs, err := h.Queries.ListTestRuns(ctx, db.ListTestRunsParams{
		WorkspaceID: wsUUID,
		PlanID:      plan.ID,
		Limit:       int32(limit),
	})
	if err != nil {
		slog.Error("plan stats: list runs failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the plan's runs")
		return
	}
	resp := TestPlanStatsResponse{Runs: []TestPlanRunStatResponse{}, Matrix: []TestPlanModuleStatResponse{}}
	for _, run := range runs {
		counts, err := h.Queries.CountTestRunResults(ctx, db.CountTestRunResultsParams{RunID: run.ID, WorkspaceID: wsUUID})
		if err != nil {
			slog.Warn("plan stats: count results failed", append(logger.RequestAttrs(r), "run_id", uuidToString(run.ID), "error", err)...)
			continue
		}
		results := make(map[string]int64, len(counts))
		var total int64
		for _, c := range counts {
			results[c.Result] = c.ResultCount
			total += c.ResultCount
		}
		resp.Runs = append(resp.Runs, TestPlanRunStatResponse{
			ID:          uuidToString(run.ID),
			Title:       run.Title,
			Status:      run.Status,
			CreatedAt:   timestampToString(run.CreatedAt),
			CompletedAt: timestampToPtr(run.CompletedAt),
			Results:     results,
			Total:       total,
			PassRate:    testRunPassRate(results),
		})
	}
	if len(runs) > 0 {
		newest := runs[0]
		cases, err := h.Queries.ListTestRunCases(ctx, db.ListTestRunCasesParams{RunID: newest.ID, WorkspaceID: wsUUID})
		if err != nil {
			slog.Warn("plan stats: list newest run cases failed", append(logger.RequestAttrs(r), "error", err)...)
		} else {
			resp.MatrixRunID = uuidToString(newest.ID)
			resp.Matrix = moduleResultMatrix(cases)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// moduleResultMatrix groups a round's cases by the module frozen in their
// snapshot. Cases without a module share one unnamed row; rows are sorted by
// module name so the board reads the same on every load.
func moduleResultMatrix(cases []db.TestRunCase) []TestPlanModuleStatResponse {
	byModule := make(map[string]*TestPlanModuleStatResponse)
	order := make([]string, 0)
	for _, rc := range cases {
		module := runCaseModule(rc)
		row, ok := byModule[module]
		if !ok {
			row = &TestPlanModuleStatResponse{Module: module, Results: map[string]int64{}}
			byModule[module] = row
			order = append(order, module)
		}
		row.Results[rc.Result]++
		row.Total++
	}
	sort.Strings(order)
	out := make([]TestPlanModuleStatResponse, 0, len(order))
	for _, module := range order {
		out = append(out, *byModule[module])
	}
	return out
}

func runCaseModule(rc db.TestRunCase) string {
	var snapshot struct {
		Module string `json:"module"`
	}
	if len(rc.CaseSnapshot) == 0 {
		return ""
	}
	if err := json.Unmarshal(rc.CaseSnapshot, &snapshot); err != nil {
		return ""
	}
	return strings.TrimSpace(snapshot.Module)
}
