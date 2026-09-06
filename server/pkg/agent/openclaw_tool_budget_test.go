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

// openclawMixedBudgetFixture: a fake openclaw whose stdout is a legacy
// NDJSON stream (tool_use events seen by the live reader) AND whose session
// transcript contains those same CallIDs plus transcriptOnly extra calls —
// mirroring a run whose stream and transcript overlap.
func openclawMixedBudgetFixture(t *testing.T, liveCallIDs, transcriptCallIDs []string) (fakePath, stateDir string) {
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

	var transcript []string
	transcript = append(transcript, `{"type":"message","message":{"role":"user","content":"prompt"}}`)
	for _, id := range transcriptCallIDs {
		transcript = append(transcript, `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"exec","id":"`+id+`","input":{"command":"pwd"}}]}}`)
	}
	transcript = append(transcript, `{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`)

	var stream []string
	stream = append(stream, `{"type":"text","text":"starting"}`)
	for _, id := range liveCallIDs {
		stream = append(stream, `{"type":"tool_use","tool":"exec","callId":"`+id+`","input":{"command":"pwd"}}`)
	}
	stream = append(stream, `{"type":"text","text":"done"}`)

	fakePath = filepath.Join(t.TempDir(), "openclaw")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  echo 'openclaw 2026.7.1'\n" +
		"  exit 0\n" +
		"fi\n" +
		"cat >> \"$OPENCLAW_STATE_DIR/agents/main/sessions/session-budget.jsonl\" <<'TRANSCRIPT'\n" +
		strings.Join(transcript, "\n") + "\n" +
		"TRANSCRIPT\n" +
		"cat <<'STREAM'\n" +
		strings.Join(stream, "\n") + "\n" +
		"STREAM\n" +
		"cat <<'JSON'\n" +
		`{"payloads":[{"text":"done"}],"meta":{"durationMs":1,"agentMeta":{"sessionId":"session-budget"}}}` + "\n" +
		"JSON\n"
	writeTestExecutable(t, fakePath, []byte(script))
	return fakePath, stateDir
}

// runOpenclawBudgetSession executes the mixed fixture and returns the result.
func runOpenclawBudgetSession(t *testing.T, fakePath, stateDir string, cap int) Result {
	t.Helper()
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
		MaxToolCalls:    cap,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	return <-session.Result
}

// TestOpenclawToolBudgetMixedStreamTranscriptNoDoubleCharge: cap 2; c1 and c2
// are live-admitted on the NDJSON stream, and the transcript repeats c1, c2
// plus a transcript-only c3. Correct accounting: c1/c2 charged once (their
// transcript rows take the admissions), c3 denied → failed "cap 2". A
// double-charge bug (transcript re-charging live-sighted IDs) would instead
// deny at the transcript's c2 row.
func TestOpenclawToolBudgetMixedStreamTranscriptNoDoubleCharge(t *testing.T) {
	t.Parallel()
	fakePath, stateDir := openclawMixedBudgetFixture(t,
		[]string{"c1", "c2"},
		[]string{"c1", "c2", "c3"})

	result := runOpenclawBudgetSession(t, fakePath, stateDir, 2)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "cap 2") {
		t.Fatalf("error should name cap 2 (c3 is the first denied call), got %q", result.Error)
	}
}

// TestOpenclawToolBudgetMixedStreamTranscriptCompletes: cap 2; live stream
// carries c1 only, transcript carries c1 (dedup) and c2 (charged post-hoc).
// Total two distinct calls → completed. A double-charge bug would fail c2.
func TestOpenclawToolBudgetMixedStreamTranscriptCompletes(t *testing.T) {
	t.Parallel()
	fakePath, stateDir := openclawMixedBudgetFixture(t,
		[]string{"c1"},
		[]string{"c1", "c2"})

	result := runOpenclawBudgetSession(t, fakePath, stateDir, 2)
	if result.Status != "completed" {
		t.Fatalf("expected completed (2 distinct calls, cap 2), got status=%q error=%q", result.Status, result.Error)
	}
}

// TestOpenclawToolBudgetRepeatedTranscriptIDChargesEachTime: cap 2; the
// transcript repeats c1 three times (no live stream). Each row must charge —
// the third is denied — rather than one admission covering every recurrence.
func TestOpenclawToolBudgetRepeatedTranscriptIDChargesEachTime(t *testing.T) {
	t.Parallel()
	fakePath, stateDir := openclawMixedBudgetFixture(t,
		nil,
		[]string{"c1", "c1", "c1"})

	result := runOpenclawBudgetSession(t, fakePath, stateDir, 2)
	if result.Status != "failed" {
		t.Fatalf("expected failed (third c1 row exceeds cap 2), got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "cap 2") {
		t.Fatalf("error should name cap 2, got %q", result.Error)
	}
}
