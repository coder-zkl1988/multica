package agent

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeCodexBudgetBody emits N commandExecution items during a turn. With a
// budget smaller than N the backend must kill the process before
// turn/completed and report the budget error.
func fakeCodexBudgetBody(toolCalls int) string {
	var b strings.Builder
	b.WriteString(`read line` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n")
	b.WriteString(`read line` + "\n")
	b.WriteString(`read line` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-budget"}}}'` + "\n")
	b.WriteString(`read line` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","id":3,"result":{}}'` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-budget","turn":{"id":"turn-1"}}}'` + "\n")
	for i := 1; i <= toolCalls; i++ {
		b.WriteString(`echo '{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thr-budget","item":{"type":"commandExecution","id":"cmd-` +
			string(rune('0'+i)) + `","command":"echo hi"}}}'` + "\n")
		b.WriteString(`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-budget","item":{"type":"commandExecution","id":"cmd-` +
			string(rune('0'+i)) + `","command":"echo hi","aggregatedOutput":"hi\n","status":"completed"}}}'` + "\n")
	}
	b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-budget","turn":{"id":"turn-1","status":"completed"}}}'` + "\n")
	return b.String()
}

func TestCodexToolBudgetStopsRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, fakeCodexBudgetBody(5))
	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   10 * time.Second,
		HandshakeTimeout:          3 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
		MaxToolCalls:              2,
	})
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "tool-call budget exceeded") {
		t.Fatalf("error should name the budget, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "cap 2") {
		t.Fatalf("error should name the cap, got %q", result.Error)
	}
}

func TestCodexToolBudgetDisabledByZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, fakeCodexBudgetBody(5))
	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   10 * time.Second,
		HandshakeTimeout:          3 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
		MaxToolCalls:              0,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed with budget disabled, got status=%q error=%q", result.Status, result.Error)
	}
}

// fakeCodexPlanThenCommandBody emits turn/plan/updated notifications (which
// this backend normalises to todo_write MessageToolUse but must NOT charge to
// the budget) interleaved with real commandExecution items.
func fakeCodexPlanThenCommandBody(toolCalls int) string {
	var b strings.Builder
	b.WriteString(`read line` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n")
	b.WriteString(`read line` + "\n")
	b.WriteString(`read line` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-plan"}}}'` + "\n")
	b.WriteString(`read line` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","id":3,"result":{}}'` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-plan","turn":{"id":"turn-plan"}}}'` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/plan/updated","params":{"threadId":"thr-plan","plan":[{"step":"first","status":"in_progress"},{"step":"second","status":"pending"}]}}'` + "\n")
	for i := 1; i <= toolCalls; i++ {
		b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/plan/updated","params":{"threadId":"thr-plan","plan":[{"step":"first","status":"completed"},{"step":"second","status":"in_progress"}]}}'` + "\n")
		b.WriteString(`echo '{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thr-plan","item":{"type":"commandExecution","id":"cmd-` +
			string(rune('0'+i)) + `","command":"echo hi"}}}'` + "\n")
		b.WriteString(`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-plan","item":{"type":"commandExecution","id":"cmd-` +
			string(rune('0'+i)) + `","command":"echo hi","aggregatedOutput":"hi\n","status":"completed"}}}'` + "\n")
	}
	b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/plan/updated","params":{"threadId":"thr-plan","plan":[{"step":"first","status":"completed"},{"step":"second","status":"completed"}]}}'` + "\n")
	b.WriteString(`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-plan","turn":{"id":"turn-plan","status":"completed"}}}'` + "\n")
	return b.String()
}

// TestCodexToolBudgetIgnoresPlanUpdates: cap 1, one plan update before and
// after a single real command — the run must complete; plan updates are not
// tool calls and must not consume the budget.
func TestCodexToolBudgetIgnoresPlanUpdates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, fakeCodexPlanThenCommandBody(1))
	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   10 * time.Second,
		HandshakeTimeout:          3 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
		MaxToolCalls:              1,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed (plan updates are free), got status=%q error=%q", result.Status, result.Error)
	}
}

// TestCodexToolBudgetStillStopsThroughPlanUpdates: cap 1, two real commands
// interleaved with plan updates — the second command must still be denied;
// plan updates neither consume nor replenish the budget.
func TestCodexToolBudgetStillStopsThroughPlanUpdates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, fakeCodexPlanThenCommandBody(2))
	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   10 * time.Second,
		HandshakeTimeout:          3 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
		MaxToolCalls:              1,
	})
	if result.Status != "failed" {
		t.Fatalf("expected failed (second command exceeds cap 1), got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "tool-call budget exceeded") || !strings.Contains(result.Error, "cap 1") {
		t.Fatalf("error should name the budget and cap, got %q", result.Error)
	}
}
