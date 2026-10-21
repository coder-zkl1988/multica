package handler

import (
	"encoding/json"
	"fmt"
	"slices"
)

// What makes a recorded result trustworthy.
//
// A run case's result used to be whatever the writer said, with step results
// an unchecked JSON blob. An agent could record `passed` without a word about
// any step and without a screenshot, which is precisely the claim the testing
// center exists to verify. The rules below give step results a shape, tie them
// to the frozen snapshot, and hold an agent's verdict to what it recorded.

// TestRunStepResult is one step's verdict inside a run case. Index is the
// step's index in the frozen case snapshot. Actual is what the executor
// observed — required in spirit whenever it differs from the step's expected
// result, which is what makes a failure triageable without re-running it.
type TestRunStepResult struct {
	Index  int32  `json:"index"`
	Result string `json:"result"`
	Actual string `json:"actual,omitempty"`
	Note   string `json:"note,omitempty"`
}

// validTestRunStepResults: `not_run` is a step the executor never reached
// (an earlier step failed or blocked); `skipped` is a step that did not apply.
var validTestRunStepResults = []string{"passed", "failed", "blocked", "skipped", "not_run"}

// snapshotStepIndexes reads the step indexes of a frozen case snapshot, in
// order. A snapshot without steps — a legacy case written before steps were
// required — yields none, and step coverage is then vacuous.
func snapshotStepIndexes(snapshot []byte) []int32 {
	var frozen struct {
		Steps []TestCaseStep `json:"steps"`
	}
	if len(snapshot) == 0 || json.Unmarshal(snapshot, &frozen) != nil {
		return nil
	}
	indexes := make([]int32, 0, len(frozen.Steps))
	for i, step := range frozen.Steps {
		index := step.Index
		if index == 0 {
			// Snapshots taken before steps were renumbered on write.
			index = int32(i + 1)
		}
		indexes = append(indexes, index)
	}
	return indexes
}

// stepResultsProblem checks step results against the run case's snapshot and
// the case-level result they back. It returns "" when they are consistent.
//
// requireComplete is the agent contract for a verdict: a `passed` or `failed`
// recorded by an agent must say what happened at every step of the snapshot.
// A person executing by hand may leave step results out — forcing a checkbox
// per step pushes people to tick everything, which yields worse data than none
// — but whatever is given is held to the same shape.
func stepResultsProblem(result string, steps []TestRunStepResult, snapshotIndexes []int32, requireComplete bool) string {
	seen := make(map[int32]string, len(steps))
	for i, step := range steps {
		if !slices.Contains(validTestRunStepResults, step.Result) {
			return fmt.Sprintf("step_results[%d].result must be one of passed, failed, blocked, skipped, not_run", i)
		}
		if len(snapshotIndexes) > 0 && !slices.Contains(snapshotIndexes, step.Index) {
			return fmt.Sprintf("step_results[%d].index %d is not a step of this case", i, step.Index)
		}
		if _, dup := seen[step.Index]; dup {
			return fmt.Sprintf("step_results[%d] repeats step %d", i, step.Index)
		}
		seen[step.Index] = step.Result
	}

	if requireComplete {
		for _, index := range snapshotIndexes {
			if _, ok := seen[index]; !ok {
				return fmt.Sprintf("a %s verdict needs a result for every step; step %d has none", result, index)
			}
		}
	}

	switch result {
	case "passed":
		for _, step := range steps {
			switch step.Result {
			case "failed", "blocked", "not_run":
				return fmt.Sprintf("the case cannot pass while step %d is %s", step.Index, step.Result)
			}
		}
	case "failed":
		// Only checked when step results were given at all: a person may
		// record a bare `failed`; an agent cannot (requireComplete above).
		if len(steps) > 0 && !slices.ContainsFunc(steps, func(step TestRunStepResult) bool { return step.Result == "failed" }) {
			return "a failed case needs at least one failed step: say which step diverged from its expected result"
		}
	}
	return ""
}

// isTestRunVerdict is whether a result says how the product behaved. blocked
// and skipped say the case was not executed, so they never stand as a case's
// current verdict.
func isTestRunVerdict(result string) bool {
	return result == "passed" || result == "failed"
}

// storedStepResults reads the step results already on a run case, keeping only
// entries this build understands — rows written before step results had a
// shape may hold anything, and an unreadable entry asserts nothing.
func storedStepResults(raw []byte, snapshotIndexes []int32) []TestRunStepResult {
	var entries []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	out := make([]TestRunStepResult, 0, len(entries))
	seen := map[int32]bool{}
	for _, entry := range entries {
		var step TestRunStepResult
		if json.Unmarshal(entry, &step) != nil || !slices.Contains(validTestRunStepResults, step.Result) || seen[step.Index] {
			continue
		}
		if len(snapshotIndexes) > 0 && !slices.Contains(snapshotIndexes, step.Index) {
			continue
		}
		seen[step.Index] = true
		out = append(out, step)
	}
	return out
}

// agentVerdictNeedsEvidence: the verdicts whose truth rests on what the agent
// saw. `blocked` and `skipped` describe the environment or applicability, and a
// device that is gone may leave nothing to capture.
func agentVerdictNeedsEvidence(result string) bool {
	return result == "passed" || result == "failed"
}

// runAcceptsResultWrite says whether a run in this status may still have a
// case result written. An aborted round is final for everyone. A completed
// round may be corrected by a person (a mis-clicked result), never by an agent
// task — an agent writing after its round settled is a stale task.
func runAcceptsResultWrite(runStatus string, byAgent bool) (bool, string) {
	switch runStatus {
	case "aborted":
		return false, "the round was aborted; its results can no longer change"
	case "completed", "blocked":
		if byAgent {
			return false, "the round is " + runStatus + "; an agent can no longer record results in it"
		}
	}
	return true, ""
}
