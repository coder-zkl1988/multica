package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// openclawBudgetFixture writes a fake openclaw that appends nToolCalls
// toolCall records to the session transcript and prints a result blob.
func openclawBudgetFixture(t *testing.T, nToolCalls int) (fakePath, stateDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	stateDir = t.TempDir()
	sessionDir := filepath.Join(stateDir, "agents", "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session-budget.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var lines []string
	lines = append(lines, `{"type":"message","message":{"role":"user","content":"prompt"}}`)
	for i := 0; i < nToolCalls; i++ {
		lines = append(lines,
			`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"call-`+string(rune('0'+i))+`","name":"exec","input":{"command":"pwd"}}]}}`,
			`{"type":"message","message":{"role":"toolResult","toolCallId":"call-`+string(rune('0'+i))+`","toolName":"exec","content":[{"type":"text","text":"out"}]}}`,
		)
	}
	lines = append(lines, `{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`)
	transcript := strings.Join(lines, "\n") + "\n"

	fakePath = filepath.Join(t.TempDir(), "openclaw")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  echo 'openclaw 2026.7.1'\n" +
		"  exit 0\n" +
		"fi\n" +
		"cat >> \"$OPENCLAW_STATE_DIR/agents/main/sessions/session-budget.jsonl\" <<'TRANSCRIPT'\n" +
		transcript +
		"TRANSCRIPT\n" +
		"cat <<'JSON'\n" +
		`{"payloads":[{"text":"done"}],"meta":{"durationMs":1,"agentMeta":{"sessionId":"session-budget"}}}` + "\n" +
		"JSON\n"
	writeTestExecutable(t, fakePath, []byte(script))
	return fakePath, stateDir
}

// TestOpenclawToolBudgetFailsOverBudgetTranscript: five transcript tool calls
// against a budget of two — the run is failed with the budget named.
func TestOpenclawToolBudgetFailsOverBudgetTranscript(t *testing.T) {
	t.Parallel()
	fakePath, stateDir := openclawBudgetFixture(t, 5)

	backend, err := New("openclaw", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCLAW_STATE_DIR": stateDir},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		ResumeSessionID: "session-budget",
		Model:           "main",
		Timeout:         5 * time.Second,
		MaxToolCalls:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
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

// TestOpenclawToolBudgetDisabledByZero: zero cap leaves the run completed.
func TestOpenclawToolBudgetDisabledByZero(t *testing.T) {
	t.Parallel()
	fakePath, stateDir := openclawBudgetFixture(t, 5)

	backend, err := New("openclaw", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCLAW_STATE_DIR": stateDir},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		ResumeSessionID: "session-budget",
		Model:           "main",
		Timeout:         5 * time.Second,
		MaxToolCalls:    0,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed with budget disabled, got status=%q error=%q", result.Status, result.Error)
	}
}

// TestOpenclawToolBudgetLiveNDJSONDeniedNotForwarded: a live (legacy NDJSON)
// stream whose tool_use events exceed the budget fails with the budget named
// and the denied call is not forwarded to the message channel.
func TestOpenclawToolBudgetLiveNDJSONDeniedNotForwarded(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)
	budget := newToolCallBudget(2)

	events := strings.Join([]string{
		`{"type":"text","text":"starting"}`,
		`{"type":"tool_use","tool":"exec","callId":"c1","input":{"command":"pwd"}}`,
		`{"type":"tool_use","tool":"exec","callId":"c2","input":{"command":"ls"}}`,
		`{"type":"tool_use","tool":"exec","callId":"c3","input":{"command":"rm"}}`,
		`{"type":"text","text":"done"}`,
	}, "\n") + "\n"

	var killCalled atomic.Bool
	res := b.processOutputWithFinalText(strings.NewReader(events), ch, true, budget, func() {
		killCalled.Store(true)
	})

	if !res.budgetExhausted {
		t.Fatal("expected budgetExhausted=true on a live over-budget stream")
	}
	if !killCalled.Load() {
		t.Fatal("expected live over-budget stream to invoke the kill callback")
	}
	if res.status != "failed" || !strings.Contains(res.errMsg, "tool-call budget exceeded") {
		t.Fatalf("result: got status=%q errMsg=%q", res.status, res.errMsg)
	}
	// The channel is buffered and never closed by this helper; drain what
	// was sent. Exactly the two admitted calls may be forwarded — the
	// denied c3 must not appear.
	var toolUses int
	for len(ch) > 0 {
		if m := <-ch; m.Type == MessageToolUse {
			toolUses++
		}
	}
	if toolUses != 2 {
		t.Fatalf("expected exactly 2 forwarded tool uses, saw %d", toolUses)
	}
}
