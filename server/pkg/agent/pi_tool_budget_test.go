package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestPiExecuteToolBudgetStopsRun asserts the daemon-side tool-call budget:
// with MaxToolCalls = 2 a stream that keeps issuing tool calls is killed and
// the result names the budget, not a crash.
func TestPiExecuteToolBudgetStopsRun(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
	}
	// Five tool calls against a budget of two: the third start event is the
	// first denied one. The fake script keeps printing so the only thing
	// that ends the run is the budget kill.
	for i := 1; i <= 5; i++ {
		events = append(events,
			`{"type":"tool_execution_start","toolCallId":"call_`+string(rune('0'+i))+`","toolName":"bash","args":{}}`,
			`{"type":"tool_execution_end","toolCallId":"call_`+string(rune('0'+i))+`","toolName":"bash","result":{},"isError":false}`,
		)
	}
	events = append(events, `{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`)

	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:      5 * time.Second,
		MaxToolCalls: 2,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "tool-call budget exceeded") {
			t.Fatalf("error should name the budget, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "cap 2") {
			t.Fatalf("error should name the cap, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestPiExecuteToolBudgetDisabledByZero asserts the escape hatch: zero cap
// leaves an unbounded tool loop running to completion exactly as before the
// budget existed.
func TestPiExecuteToolBudgetDisabledByZero(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
	}
	for i := 1; i <= 5; i++ {
		events = append(events,
			`{"type":"tool_execution_start","toolCallId":"call_`+string(rune('0'+i))+`","toolName":"bash","args":{}}`,
			`{"type":"tool_execution_end","toolCallId":"call_`+string(rune('0'+i))+`","toolName":"bash","result":{},"isError":false}`,
		)
	}
	events = append(events, `{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`)

	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:      5 * time.Second,
		MaxToolCalls: 0,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected status=completed with budget disabled, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
