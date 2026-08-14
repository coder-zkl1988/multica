//go:build !windows

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOpenclawExecuteReapsDetachedDescendant(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	pidFile := filepath.Join(dir, "child.pid")
	script := `#!/bin/sh
case "$1" in
  --version) echo "openclaw 2026.5.27"; exit 0 ;;
esac
sleep 300 >/dev/null 2>&1 &
echo $! > "$OPENCLAW_CHILD_PID_FILE"
cat <<'JSON'
` + completeOpenclawResult + `
JSON
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write openclaw stub: %v", err)
	}

	b := newOpenclawTestBackend(bin)
	b.cfg.Env = map[string]string{"OPENCLAW_CHILD_PID_FILE": pidFile}
	session, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || !strings.Contains(result.Output, "the agent reply text") {
		t.Fatalf("result = %#v, want completed reply", result)
	}

	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatalf("parse descendant pid: %v", err)
	}
	proc, _ := os.FindProcess(pid)
	defer proc.Kill()
	waitProcessGone(t, pid)
}
