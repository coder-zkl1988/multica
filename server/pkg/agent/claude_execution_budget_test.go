//go:build !windows

package agent

import (
	"encoding/json"
	"testing"
)

func TestClaudeExecutionBudgetRequiresStructuredFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, subtype, text string
		isError, wantBudget bool
	}{
		{"empty budget error", "error_max_budget_usd", "", true, true},
		{"budget error with details", "error_max_budget_usd", "Run spending cap reached", true, true},
		{"ordinary failure", "error_during_execution", "Execution failed", true, false},
		{"prose is not structured evidence", "error_during_execution", "error_max_budget_usd", true, false},
		{"successful subtype lookalike", "error_max_budget_usd", "done", false, false},
		{"successful prose lookalike", "success", "error_max_budget_usd", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			terminal, err := json.Marshal(map[string]any{
				"type": "result", "session_id": "budget-session", "subtype": tc.subtype,
				"is_error": tc.isError, "result": tc.text,
				"modelUsage": map[string]any{"fixture-model": map[string]any{
					"inputTokens": 12, "outputTokens": 3, "cacheReadInputTokens": 21, "cacheCreationInputTokens": 4,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result := runClaudeFixture(t, string(terminal)+"\n", "budget-session")
			if result.ExecutionBudgetExceeded != tc.wantBudget {
				t.Fatalf("ExecutionBudgetExceeded = %v, want %v", result.ExecutionBudgetExceeded, tc.wantBudget)
			}
			wantStatus := "completed"
			if tc.isError {
				wantStatus = "failed"
			}
			if result.Status != wantStatus || result.SessionID != "budget-session" || result.ResumeRejected {
				t.Fatalf("unexpected terminal/session state: %+v", result)
			}
			// The fixture resumes "budget-session" while the daemon-visible
			// config dir holds no transcript for it, so the baseline capture
			// fails and the result's modelUsage is an unverifiable cumulative
			// total (GH #9564): the backend must keep the per-run stream usage
			// rather than store the reported totals. This stream carries no
			// usage events, so the reported numbers must not appear at all.
			// The verifiable counterpart — a locatable transcript whose
			// baseline is subtracted before the totals are trusted — is
			// TestClaudeExecuteSubtractsResumedCostStateBaseline.
			if usage := result.Usage["fixture-model"]; usage != (TokenUsage{}) {
				t.Fatalf("unverifiable reported usage stored on resume: %+v", usage)
			}
		})
	}
}
