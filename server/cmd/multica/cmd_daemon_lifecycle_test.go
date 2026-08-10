package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	lifecycleHelperEnv        = "MULTICA_DAEMON_LIFECYCLE_HELPER"
	lifecycleHelperDaemonID   = "MULTICA_DAEMON_LIFECYCLE_ID"
	lifecycleHelperCLIVersion = "MULTICA_DAEMON_LIFECYCLE_VERSION"
)

func init() {
	if os.Getenv(lifecycleHelperEnv) != "1" {
		return
	}
	if err := runDaemonLifecycleHelper(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func runDaemonLifecycleHelper() error {
	profile := ""
	for i, arg := range os.Args {
		if arg == "--profile" && i+1 < len(os.Args) {
			profile = os.Args[i+1]
			break
		}
	}
	record := daemonPIDRecord{PID: os.Getpid(), DaemonID: os.Getenv(lifecycleHelperDaemonID)}
	if err := writeDaemonPIDFile(profile, record); err != nil {
		return err
	}
	defer removeDaemonPIDFile(profile, record)

	shutdown := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":      "running",
				"pid":         record.PID,
				"daemon_id":   record.DaemonID,
				"cli_version": os.Getenv(lifecycleHelperCLIVersion),
				"uptime":      "1s",
				"agents":      []string{},
				"workspaces":  []string{},
			})
		case "/shutdown":
			w.WriteHeader(http.StatusOK)
			select {
			case shutdown <- struct{}{}:
			default:
			}
		default:
			http.NotFound(w, r)
		}
	})
	server := &http.Server{Handler: handler}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", healthPortForProfile(profile)))
	if err != nil {
		return err
	}
	go func() { _ = server.Serve(ln) }()
	<-shutdown
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

type lifecycleHealth struct {
	mu       sync.RWMutex
	status   string
	pid      int
	daemonID string
	version  string
}

func (h *lifecycleHealth) set(status string, pid int, daemonID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
	h.pid = pid
	h.daemonID = daemonID
}

func (h *lifecycleHealth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/shutdown" && r.Method == http.MethodPost {
		h.mu.Lock()
		h.status = "stopped"
		h.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path != "/health" {
		http.NotFound(w, r)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      h.status,
		"pid":         h.pid,
		"daemon_id":   h.daemonID,
		"cli_version": h.version,
		"uptime":      "3s",
		"agents":      []string{"codex"},
		"workspaces":  []string{},
	})
}

func startLifecycleHealthServer(t *testing.T, profile string, health *lifecycleHealth) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", healthPortForProfile(profile)))
	if err != nil {
		t.Skipf("health port for profile %s unavailable: %v", profile, err)
	}
	server := &http.Server{Handler: health}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
}

func daemonLifecycleCommand(t *testing.T, profile string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("foreground", false, "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "table", "")
	if err := cmd.Flags().Set("profile", profile); err != nil {
		t.Fatalf("set profile: %v", err)
	}
	return cmd
}

func writeCanonicalDaemonID(t *testing.T, daemonID string) {
	t.Helper()
	dir := daemonDirForProfile("")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create daemon dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.id"), []byte(daemonID+"\n"), 0o600); err != nil {
		t.Fatalf("write daemon.id: %v", err)
	}
}

func TestDaemonPIDFileRoundTripIsAtomic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "pid-round-trip"
	want := daemonPIDRecord{PID: 12345, DaemonID: "daemon-round-trip"}

	if err := writeDaemonPIDFile(profile, want); err != nil {
		t.Fatalf("writeDaemonPIDFile: %v", err)
	}
	got, err := readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("readDaemonPIDFile: %v", err)
	}
	if got != want {
		t.Fatalf("PID record = %+v, want %+v", got, want)
	}
	replacement := daemonPIDRecord{PID: 12346, DaemonID: "daemon-replacement"}
	if err := writeDaemonPIDFile(profile, replacement); err != nil {
		t.Fatalf("replaceDaemonPIDFile: %v", err)
	}
	got, err = readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("read replaced PID file: %v", err)
	}
	if got != replacement {
		t.Fatalf("replaced PID record = %+v, want %+v", got, replacement)
	}
	temps, err := filepath.Glob(filepath.Join(daemonDirForProfile(profile), ".daemon-*.pid.tmp"))
	if err != nil {
		t.Fatalf("glob PID temp files: %v", err)
	}
	if len(temps) != 0 {
		t.Fatalf("atomic PID write left temp files: %v", temps)
	}
}

func TestDaemonLifecycleCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(lifecycleHelperEnv, "1")
	t.Setenv(lifecycleHelperDaemonID, "daemon-lifecycle")
	t.Setenv(lifecycleHelperCLIVersion, "lifecycle-test-version")
	profile := fmt.Sprintf("lifecycle-%d", os.Getpid())

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(api.Close)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "mul_lifecycle", ServerURL: api.URL}, profile); err != nil {
		t.Fatalf("save profile config: %v", err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	originalExecutable := daemonExecutable
	daemonExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { daemonExecutable = originalExecutable })
	cmd := daemonLifecycleCommand(t, profile)
	t.Cleanup(func() { _ = runDaemonStop(cmd, nil) })

	if err := runDaemonStart(cmd, nil); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	first, err := readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("read started PID: %v", err)
	}
	assertLifecycleStatus(t, cmd, first.PID, "lifecycle-test-version")

	if err := runDaemonRestart(cmd, nil); err != nil {
		t.Fatalf("restart daemon: %v", err)
	}
	second, err := readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("read restarted PID: %v", err)
	}
	if second.PID == first.PID {
		t.Fatalf("restart PID = %d, want a new PID", second.PID)
	}
	assertLifecycleStatus(t, cmd, second.PID, "lifecycle-test-version")

	if err := runDaemonStop(cmd, nil); err != nil {
		t.Fatalf("stop daemon: %v", err)
	}
	out, err := captureStdout(t, func() error { return runDaemonStatus(cmd, nil) })
	if err != nil {
		t.Fatalf("status after stop: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("status after stop = %q, want stopped", out)
	}
	if _, err := os.Stat(daemonPIDPathForProfile(profile)); !os.IsNotExist(err) {
		t.Fatalf("PID file after stop: %v", err)
	}
}

func assertLifecycleStatus(t *testing.T, cmd *cobra.Command, pid int, cliVersion string) {
	t.Helper()
	out, err := captureStdout(t, func() error { return runDaemonStatus(cmd, nil) })
	if err != nil {
		t.Fatalf("daemon status: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("running (pid %d", pid)) || !strings.Contains(out, cliVersion) {
		t.Fatalf("status = %q, want running PID %d and version %q", out, pid, cliVersion)
	}
}

func TestDaemonStatusReportsRunningWithMatchingPIDAndIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "status-running"
	record := daemonPIDRecord{PID: os.Getpid(), DaemonID: "daemon-running"}
	health := &lifecycleHealth{status: "running", pid: record.PID, daemonID: record.DaemonID, version: "0.4.18-sso.1"}
	startLifecycleHealthServer(t, profile, health)
	if err := writeDaemonPIDFile(profile, record); err != nil {
		t.Fatalf("write PID file: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runDaemonStatus(daemonLifecycleCommand(t, profile), nil)
	})
	if err != nil {
		t.Fatalf("runDaemonStatus: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("running (pid %d", record.PID)) {
		t.Fatalf("status output = %q, want running PID %d", out, record.PID)
	}
	if !strings.Contains(out, "0.4.18-sso.1") {
		t.Fatalf("status output = %q, want CLI version", out)
	}
}

func TestDaemonStateRepairsMissingPIDFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "state-missing-pid"
	record := daemonPIDRecord{PID: os.Getpid(), DaemonID: "daemon-persisted"}
	writeCanonicalDaemonID(t, record.DaemonID)
	startLifecycleHealthServer(t, profile, &lifecycleHealth{
		status: "running", pid: record.PID, daemonID: record.DaemonID, version: "test",
	})

	state := daemonStateForProfile(context.Background(), profile)
	if !daemonAlive(state) {
		t.Fatalf("state = %v, want running", state)
	}
	got, err := readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("repaired PID file: %v", err)
	}
	if got != record {
		t.Fatalf("repaired PID record = %+v, want %+v", got, record)
	}
}

func TestDaemonStateRejectsHealthWithDeadPID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "state-dead-health-pid"
	const daemonID = "daemon-dead-health-pid"
	writeCanonicalDaemonID(t, daemonID)
	startLifecycleHealthServer(t, profile, &lifecycleHealth{
		status: "running", pid: 999999, daemonID: daemonID, version: "test",
	})

	state := daemonStateForProfile(context.Background(), profile)
	if daemonAlive(state) {
		t.Fatalf("state = %v, want stopped for dead health PID", state)
	}
	if _, err := os.Stat(daemonPIDPathForProfile(profile)); !os.IsNotExist(err) {
		t.Fatalf("dead health PID was persisted: %v", err)
	}
}

func TestDaemonStateRejectsStalePIDFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "state-stale-pid"
	record := daemonPIDRecord{PID: 999999, DaemonID: "daemon-stale"}
	if err := writeDaemonPIDFile(profile, record); err != nil {
		t.Fatalf("write stale PID: %v", err)
	}

	state := daemonStateForProfile(context.Background(), profile)
	if daemonAlive(state) {
		t.Fatalf("state = %v, want stopped", state)
	}
	if _, err := os.Stat(daemonPIDPathForProfile(profile)); !os.IsNotExist(err) {
		t.Fatalf("stale PID file was not removed: %v", err)
	}
}

func TestDaemonStatePreservesLivePIDWhenHealthIsUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "state-health-unavailable"
	record := daemonPIDRecord{PID: os.Getpid(), DaemonID: "daemon-starting"}
	if err := writeDaemonPIDFile(profile, record); err != nil {
		t.Fatalf("write PID file: %v", err)
	}

	state := daemonStateForProfile(context.Background(), profile)
	if daemonAlive(state) {
		t.Fatalf("state = %v, want stopped without health response", state)
	}
	got, err := readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("live PID file was removed: %v", err)
	}
	if got != record {
		t.Fatalf("PID record = %+v, want %+v", got, record)
	}
}

func TestDaemonStateRejectsReusedPID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "state-reused-pid"
	record := daemonPIDRecord{PID: os.Getpid(), DaemonID: "daemon-old"}
	if err := writeDaemonPIDFile(profile, record); err != nil {
		t.Fatalf("write reused PID: %v", err)
	}
	startLifecycleHealthServer(t, profile, &lifecycleHealth{
		status: "running", pid: record.PID, daemonID: "daemon-other", version: "test",
	})

	state := daemonStateForProfile(context.Background(), profile)
	if daemonAlive(state) {
		t.Fatalf("state = %v, want stopped for reused PID", state)
	}
}

func TestDaemonRestartCleanupPreservesSuccessorPID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "restart-successor"
	oldRecord := daemonPIDRecord{PID: 12001, DaemonID: "daemon-restart"}
	newRecord := daemonPIDRecord{PID: 12002, DaemonID: "daemon-restart"}
	health := &lifecycleHealth{status: "running", pid: newRecord.PID, daemonID: newRecord.DaemonID, version: "next"}
	startLifecycleHealthServer(t, profile, health)
	if err := writeDaemonPIDFile(profile, newRecord); err != nil {
		t.Fatalf("write successor PID: %v", err)
	}

	if err := removeDaemonPIDFile(profile, oldRecord); err != nil {
		t.Fatalf("old daemon cleanup: %v", err)
	}
	got, err := readDaemonPIDFile(profile)
	if err != nil {
		t.Fatalf("successor PID file missing: %v", err)
	}
	if got != newRecord {
		t.Fatalf("PID record = %+v, want successor %+v", got, newRecord)
	}
	state := daemonStateForProfile(context.Background(), profile)
	if !daemonAlive(state) || state["pid"] != float64(newRecord.PID) {
		t.Fatalf("restart state = %v, want running successor", state)
	}
}

func TestDaemonStopTransitionsStateToStopped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "stop-lifecycle"
	record := daemonPIDRecord{PID: os.Getpid(), DaemonID: "daemon-stop"}
	health := &lifecycleHealth{status: "running", pid: record.PID, daemonID: record.DaemonID, version: "test"}
	startLifecycleHealthServer(t, profile, health)
	if err := writeDaemonPIDFile(profile, record); err != nil {
		t.Fatalf("write PID file: %v", err)
	}

	if err := runDaemonStop(daemonLifecycleCommand(t, profile), nil); err != nil {
		t.Fatalf("runDaemonStop: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state := daemonStateForProfile(ctx, profile)
	if daemonAlive(state) {
		t.Fatalf("state after stop = %v, want stopped", state)
	}
	if _, err := os.Stat(daemonPIDPathForProfile(profile)); !os.IsNotExist(err) {
		t.Fatalf("PID file after stop: %v", err)
	}
}
