package handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// testGenerationResultMarker prefixes the agent's closing summary block. Only
// the summary travels this way: the generated cases themselves are written
// through the authenticated propose endpoint, so a malformed or missing marker
// costs a status line, never data.
const testGenerationResultMarker = "TEST_GENERATION_RESULT_JSON:"

// testGenerationResultSummary is the agent's declared outcome.
type testGenerationResultSummary struct {
	Status   string         `json:"status"`
	Summary  string         `json:"summary"`
	Stats    map[string]int `json:"stats"`
	Blockers []string       `json:"blockers"`
	// Coverage is the run's own account of each test point: the cases it
	// says verify it, or why none can.
	Coverage []any `json:"coverage"`
}

// testGenerationIssueCoverage is where one scoped issue's test points stand
// when the run ends, computed by the server rather than taken from the agent.
type testGenerationIssueCoverage struct {
	IssueID   string   `json:"issue_id"`
	Active    int      `json:"active"`
	Covered   int      `json:"covered"`
	Uncovered []string `json:"uncovered"`
}

// testGenerationCoverageSnapshot reads the test points of every issue in the
// run's scope. Scope entries are UUIDs or identifiers; one that resolves to
// nothing is skipped, as everywhere else the scope is read.
func (h *Handler) testGenerationCoverageSnapshot(ctx context.Context, job db.TestGenerationJob) []testGenerationIssueCoverage {
	var refs []string
	if plan, err := h.Queries.GetTestGenerationPlanByJob(ctx, db.GetTestGenerationPlanByJobParams{JobID: job.ID, WorkspaceID: job.WorkspaceID}); err == nil {
		refs = stringsFromAny(unmarshalJSONObject(plan.Plan)["issues"])
	} else {
		refs = stringsFromAny(unmarshalJSONObject(job.Input)["issue_ids"])
	}
	out := make([]testGenerationIssueCoverage, 0, len(refs))
	seen := map[pgtype.UUID]struct{}{}
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		var issue db.Issue
		if id, err := util.ParseUUID(ref); err == nil {
			found, getErr := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: job.WorkspaceID})
			if getErr != nil {
				continue
			}
			issue = found
		} else if found, ok := h.resolveIssueByIdentifier(ctx, ref, uuidToString(job.WorkspaceID)); ok {
			issue = found
		} else {
			continue
		}
		if _, dup := seen[issue.ID]; dup {
			continue
		}
		seen[issue.ID] = struct{}{}
		points, summary, err := h.issueTestPointCoverage(ctx, issue)
		if err != nil {
			continue
		}
		entry := testGenerationIssueCoverage{IssueID: uuidToString(issue.ID), Active: summary.Active, Covered: summary.Covered, Uncovered: []string{}}
		for _, point := range points {
			if point.State == "uncovered" {
				entry.Uncovered = append(entry.Uncovered, point.Key)
			}
		}
		out = append(out, entry)
	}
	return out
}

// parseTestGenerationResultSummary scrapes the last marker block out of the
// agent's final message, tolerating a fenced code block around it.
func parseTestGenerationResultSummary(output string) (testGenerationResultSummary, bool) {
	idx := strings.LastIndex(output, testGenerationResultMarker)
	if idx < 0 {
		return testGenerationResultSummary{}, false
	}
	tail := strings.TrimSpace(output[idx+len(testGenerationResultMarker):])
	tail = strings.TrimPrefix(tail, "```json")
	tail = strings.TrimPrefix(tail, "```")
	if fence := strings.Index(tail, "```"); fence >= 0 {
		tail = tail[:fence]
	}
	tail = strings.TrimSpace(tail)

	var summary testGenerationResultSummary
	if err := json.Unmarshal([]byte(tail), &summary); err != nil {
		return testGenerationResultSummary{}, false
	}
	return summary, true
}

// testGenerationJobForTask resolves the domain row behind an agent task, or
// reports that this task is not a generation run.
func (h *Handler) testGenerationJobForTask(ctx context.Context, task db.AgentTaskQueue) (db.TestGenerationJob, bool, error) {
	var genCtx service.TestGenerationContext
	if err := json.Unmarshal(task.Context, &genCtx); err != nil || genCtx.Type != service.TestGenerationContextType {
		return db.TestGenerationJob{}, false, nil
	}
	wsUUID, err := util.ParseUUID(genCtx.WorkspaceID)
	if err != nil {
		return db.TestGenerationJob{}, false, err
	}
	job, err := h.Queries.GetTestGenerationJobByAgentTask(ctx, db.GetTestGenerationJobByAgentTaskParams{
		AgentTaskID: task.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return db.TestGenerationJob{}, false, err
	}
	return job, true, nil
}

func (h *Handler) markTestGenerationJobRunning(ctx context.Context, task db.AgentTaskQueue) error {
	job, isGeneration, err := h.testGenerationJobForTask(ctx, task)
	if !isGeneration {
		return err
	}
	_, err = h.Queries.UpdateTestGenerationJob(ctx, db.UpdateTestGenerationJobParams{
		ID:          job.ID,
		WorkspaceID: job.WorkspaceID,
		Status:      pgtype.Text{String: "running", Valid: true},
	})
	return err
}

func (h *Handler) updateTestGenerationJobFromAgentCompletion(ctx context.Context, task db.AgentTaskQueue, req TaskCompleteRequest) error {
	job, isGeneration, err := h.testGenerationJobForTask(ctx, task)
	if !isGeneration {
		return err
	}

	summary, hasSummary := parseTestGenerationResultSummary(req.Output)
	// Only the agent's declared status decides the outcome. design_restore also
	// scans the raw output for the substring "blocked", which fails a run whose
	// narration merely says "nothing was blocked".
	status := "completed"
	switch summary.Status {
	case "blocked", "failed":
		status = "failed"
	}

	result := unmarshalJSONObject(job.Result)
	result["output"] = req.Output
	result["session_id"] = req.SessionID
	result["work_dir"] = req.WorkDir
	if hasSummary {
		result["summary"] = summary.Summary
		result["blockers"] = summary.Blockers
		if len(summary.Stats) > 0 {
			result["declared_stats"] = summary.Stats
		}
		if len(summary.Coverage) > 0 {
			result["declared_coverage"] = summary.Coverage
		}
	} else {
		// Cases may still have landed through propose; say so rather than
		// implying the run produced nothing.
		result["warning"] = "missing_test_generation_result_json"
	}
	// What the run left uncovered, from the points themselves: the agent's
	// report says what it meant to cover, this says what is actually covered.
	result["coverage"] = h.testGenerationCoverageSnapshot(ctx, job)

	_, err = h.Queries.UpdateTestGenerationJob(ctx, db.UpdateTestGenerationJobParams{
		ID:          job.ID,
		WorkspaceID: job.WorkspaceID,
		Status:      pgtype.Text{String: status, Valid: true},
		Result:      marshalJSONColumn(result, "{}"),
	})
	return err
}

func (h *Handler) updateTestGenerationJobFromAgentFailure(ctx context.Context, task db.AgentTaskQueue, req TaskFailRequest) error {
	job, isGeneration, err := h.testGenerationJobForTask(ctx, task)
	if !isGeneration {
		return err
	}
	result := unmarshalJSONObject(job.Result)
	result["session_id"] = req.SessionID
	result["work_dir"] = req.WorkDir

	_, err = h.Queries.UpdateTestGenerationJob(ctx, db.UpdateTestGenerationJobParams{
		ID:          job.ID,
		WorkspaceID: job.WorkspaceID,
		Status:      pgtype.Text{String: "failed", Valid: true},
		Result:      marshalJSONColumn(result, "{}"),
		Error:       pgtype.Text{String: req.Error, Valid: req.Error != ""},
	})
	return err
}
