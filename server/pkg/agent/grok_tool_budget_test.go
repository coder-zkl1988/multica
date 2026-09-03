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

// fakeGrokBudgetScript emits N tool_call updates after a configurable number
// of in-flight calls, then a final end_turn result. With a budget smaller
// than N the backend must kill the process before end_turn and report the
// budget error.
func fakeGrokBudgetScript(toolCalls int) string {
	var b strings.Builder
	b.WriteString(`#!/bin/sh
authenticated=
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token","name":"Cached login"},{"id":"xai.api_key","name":"API key"}],"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      authenticated=1
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
`)
	for i := 1; i <= toolCalls; i++ {
		b.WriteString(`      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call","toolCallId":"tc-`)
		b.WriteString(itoaGrok(i))
		b.WriteString(`","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}\n'` + "\n")
		b.WriteString(`      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-`)
		b.WriteString(itoaGrok(i))
		b.WriteString(`","status":"completed","name":"Shell","output":"hi\\n"}}}\n'` + "\n")
	}
	b.WriteString(`      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`)
	return b.String()
}

func itoaGrok(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestGrokToolBudgetStopsRun(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokBudgetScript(5)))

	backend, err := New("grok", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second, MaxToolCalls: 2})
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
			t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
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

func TestGrokToolBudgetDisabledByZero(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokBudgetScript(5)))

	backend, err := New("grok", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second, MaxToolCalls: 0})
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
			t.Fatalf("expected completed with budget disabled, got status=%q error=%q", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
