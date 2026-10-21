package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestPlanSelector is a plan's rule for picking cases, beside its hand-picked
// list. A static list rots: a case written after the plan was built never
// runs in its regression until someone remembers to add it. A rule names what
// the plan is about, and every round takes the active cases it matches at
// that moment.
//
// Dimensions combine with AND, values inside one with OR. AllActive with no
// other dimension is every active case of the plan's project. The zero value
// is no rule.
type TestPlanSelector struct {
	AllActive  bool     `json:"all_active,omitempty"`
	Modules    []string `json:"modules,omitempty"`
	Priorities []string `json:"priorities,omitempty"`
	CaseTypes  []string `json:"case_types,omitempty"`
	// IssueIDs picks cases linked to any of these issues — a plan for one
	// epic's requirements keeps up as cases are written for them.
	IssueIDs []string `json:"issue_ids,omitempty"`
}

func (s TestPlanSelector) empty() bool {
	return !s.AllActive && len(s.Modules) == 0 && len(s.Priorities) == 0 && len(s.CaseTypes) == 0 && len(s.IssueIDs) == 0
}

func decodeTestPlanSelector(raw []byte) TestPlanSelector {
	var selector TestPlanSelector
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &selector)
	}
	return selector
}

// cleanStrings trims, drops empties and de-duplicates, keeping first order.
func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func keepNonBlank(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

// normalizeTestPlanSelector validates a rule and stores issues as ids, so a
// renamed prefix or a renumbered issue cannot silently change what it picks.
func (h *Handler) normalizeTestPlanSelector(r *http.Request, wsUUID pgtype.UUID, in TestPlanSelector) (TestPlanSelector, string) {
	out := TestPlanSelector{
		AllActive: in.AllActive,
		// Module names are matched exactly as stored, so they are not
		// trimmed — only blanks and repeats are dropped.
		Modules:    keepNonBlank(in.Modules),
		Priorities: cleanStrings(in.Priorities),
		CaseTypes:  cleanStrings(in.CaseTypes),
	}
	for _, priority := range out.Priorities {
		if !slices.Contains(validTestCasePriorities, priority) {
			return TestPlanSelector{}, fmt.Sprintf("invalid selector.priorities value %q; valid values: %s", priority, strings.Join(validTestCasePriorities, ", "))
		}
	}
	for _, caseType := range out.CaseTypes {
		if !slices.Contains(validTestCaseTypes, caseType) {
			return TestPlanSelector{}, fmt.Sprintf("invalid selector.case_types value %q; valid values: %s", caseType, strings.Join(validTestCaseTypes, ", "))
		}
	}
	for _, ref := range cleanStrings(in.IssueIDs) {
		issueUUID, ok := h.resolveGeneratedIssueRef(r, wsUUID, uuidToString(wsUUID), ref)
		if !ok {
			return TestPlanSelector{}, "selector.issue_ids: issue not found: " + ref
		}
		if err := h.checkIssueWindowAuthorization(r, issueUUID, wsUUID, "test_plan_selector"); err != nil {
			return TestPlanSelector{}, "selector.issue_ids: issue not accessible: " + ref
		}
		if id := uuidToString(issueUUID); !slices.Contains(out.IssueIDs, id) {
			out.IssueIDs = append(out.IssueIDs, id)
		}
	}
	return out, ""
}

// testPlanCaseSource is why a case is in a plan's round.
type testPlanCaseSource struct {
	id     string
	source string // "manual" or "rule"
}

// planRoundCases is what a round of this plan executes: the hand-picked cases
// in their order, then every active case the rule matches that is not already
// listed, in case-number order. Deprecated hand-picked cases are dropped later
// by loadCasesForRun, as they always were.
func (h *Handler) planRoundCases(ctx context.Context, plan db.TestPlan) ([]testPlanCaseSource, error) {
	manual, err := h.Queries.ListTestPlanCases(ctx, db.ListTestPlanCasesParams{PlanID: plan.ID, WorkspaceID: plan.WorkspaceID})
	if err != nil {
		return nil, fmt.Errorf("list the plan's cases: %w", err)
	}
	// A listed case that no longer exists (deleted before plans were swept
	// on delete) is dropped, as a deprecated one is later: the round runs
	// what is left instead of failing on a dangling id.
	manualIDs := make([]pgtype.UUID, len(manual))
	for i, pc := range manual {
		manualIDs[i] = pc.TestCaseID
	}
	existing := map[string]struct{}{}
	if len(manualIDs) > 0 {
		rows, err := h.Queries.ListTestCaseSummariesByIDs(ctx, db.ListTestCaseSummariesByIDsParams{WorkspaceID: plan.WorkspaceID, CaseIds: manualIDs})
		if err != nil {
			return nil, fmt.Errorf("resolve the plan's cases: %w", err)
		}
		for _, row := range rows {
			existing[uuidToString(row.ID)] = struct{}{}
		}
	}
	out := make([]testPlanCaseSource, 0, len(manual))
	seen := make(map[string]struct{}, len(manual))
	for _, pc := range manual {
		id := uuidToString(pc.TestCaseID)
		if _, ok := existing[id]; !ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, testPlanCaseSource{id: id, source: "manual"})
	}
	selector := decodeTestPlanSelector(plan.Selector)
	if selector.empty() {
		return out, nil
	}
	issueIDs := make([]pgtype.UUID, 0, len(selector.IssueIDs))
	for _, raw := range selector.IssueIDs {
		if id, err := util.ParseUUID(raw); err == nil {
			issueIDs = append(issueIDs, id)
		}
	}
	matches, err := h.Queries.ListTestCasesMatchingPlanSelector(ctx, db.ListTestCasesMatchingPlanSelectorParams{
		WorkspaceID: plan.WorkspaceID,
		ProjectID:   plan.ProjectID,
		Modules:     nonNilStrings(selector.Modules),
		Priorities:  nonNilStrings(selector.Priorities),
		CaseTypes:   nonNilStrings(selector.CaseTypes),
		IssueIds:    issueIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("match the plan's rule: %w", err)
	}
	for _, match := range matches {
		id := uuidToString(match.ID)
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, testPlanCaseSource{id: id, source: "rule"})
	}
	return out, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func planRoundCaseIDs(sources []testPlanCaseSource) []string {
	ids := make([]string, len(sources))
	for i, source := range sources {
		ids[i] = source.id
	}
	return ids
}

// TestPlanSelectionCaseResponse is one case a round of the plan would run.
type TestPlanSelectionCaseResponse struct {
	TestCaseID string `json:"test_case_id"`
	CaseKey    string `json:"case_key"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Source     string `json:"source"`
}

// GetTestPlanSelection answers GET /api/test-plans/{id}/selection: what the
// next round of this plan will run, and which cases the rule contributed.
func (h *Handler) GetTestPlanSelection(w http.ResponseWriter, r *http.Request) {
	plan, wsUUID, ok := h.loadTestPlanForUser(w, r)
	if !ok {
		return
	}
	sources, err := h.planRoundCases(r.Context(), plan)
	if err != nil {
		slog.Error("resolve test plan selection failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve the plan's cases")
		return
	}
	ids := make([]pgtype.UUID, 0, len(sources))
	for _, source := range sources {
		if id, err := util.ParseUUID(source.id); err == nil {
			ids = append(ids, id)
		}
	}
	summaries, err := h.Queries.ListTestCaseSummariesByIDs(r.Context(), db.ListTestCaseSummariesByIDsParams{WorkspaceID: wsUUID, CaseIds: ids})
	if err != nil {
		slog.Error("load test plan selection cases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve the plan's cases")
		return
	}
	byID := make(map[string]db.ListTestCaseSummariesByIDsRow, len(summaries))
	for _, summary := range summaries {
		byID[uuidToString(summary.ID)] = summary
	}
	cases := make([]TestPlanSelectionCaseResponse, 0, len(sources))
	counts := map[string]int{"manual": 0, "rule": 0}
	for _, source := range sources {
		tc, found := byID[source.id]
		// A round leaves deprecated cases out; so does its preview.
		if !found || tc.Status == "deprecated" {
			continue
		}
		counts[source.source]++
		cases = append(cases, TestPlanSelectionCaseResponse{
			TestCaseID: source.id,
			CaseKey:    formatTestCaseKey(tc.CaseNumber),
			Title:      tc.Title,
			Status:     tc.Status,
			Source:     source.source,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cases":  cases,
		"manual": counts["manual"],
		"rule":   counts["rule"],
		"total":  len(cases),
	})
}
