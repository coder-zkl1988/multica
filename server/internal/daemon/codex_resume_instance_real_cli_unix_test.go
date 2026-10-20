//go:build !windows

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// This proves instance recreation, not the daemon Run/CLI restart lifecycle.
func TestCodexRealCLIInstanceRecreationResumesAfterScopedGC(t *testing.T) {
	if os.Getenv("MULTICA_TEST_REAL_CODEX_R12") != "1" {
		t.Skip("set MULTICA_TEST_REAL_CODEX_R12=1 and MULTICA_TEST_REAL_CODEX_BIN to opt in")
	}
	realCodex := os.Getenv("MULTICA_TEST_REAL_CODEX_BIN")
	if !filepath.IsAbs(realCodex) {
		t.Fatal("MULTICA_TEST_REAL_CODEX_BIN must be an explicit absolute CLI path")
	}
	if os.Getenv("MULTICA_TEST_R12_ROOT") == "" {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"home", "shared", "processes", "helpers", "xdg"} {
			if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCodexRealCLIInstanceRecreationResumesAfterScopedGC$", "-test.v", "-test.timeout=40s")
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"),
			"CODEX_HOME=" + filepath.Join(root, "shared"), "TMPDIR=" + root,
			"XDG_CONFIG_HOME=" + filepath.Join(root, "xdg"), "XDG_DATA_HOME=" + filepath.Join(root, "xdg"),
			"XDG_STATE_HOME=" + filepath.Join(root, "xdg"), "XDG_CACHE_HOME=" + filepath.Join(root, "xdg"),
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "OTEL_SDK_DISABLED=true",
			"MULTICA_TEST_REAL_CODEX_R12=1", "MULTICA_TEST_REAL_CODEX_BIN=" + realCodex,
			"MULTICA_TEST_R12_ROOT=" + root,
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = 2 * time.Second
		t.Cleanup(func() {
			cancel()
			codexR12CleanupProcesses(t, filepath.Join(root, "processes"))
		})
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated R12 test failed: %v\n%s", err, output)
		}
		t.Log(strings.TrimSpace(string(output)))
		return
	}

	root := os.Getenv("MULTICA_TEST_R12_ROOT")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(func() {
		cancel()
		codexR12CleanupProcesses(t, filepath.Join(root, "processes"))
	})
	versionCtx, versionCancel := context.WithTimeout(ctx, 2*time.Second)
	version, err := exec.CommandContext(versionCtx, realCodex, "--version").Output()
	versionCancel()
	if err != nil || strings.TrimSpace(string(version)) != "codex-cli 0.153.4" {
		t.Fatalf("expected real codex-cli 0.153.4: version=%q error=%v", version, err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cwd := filepath.Join(root, "repository")
	write(filepath.Join(cwd, "AGENTS.md"), []byte("# Fixture repository\nKeep all work local.\n"))
	git := exec.CommandContext(ctx, "git", "init", "--quiet", "--template=", cwd)
	if err := git.Run(); err != nil {
		t.Fatalf("initialize isolated Git fixture: %v", err)
	}
	const firstMarker = "r12-first-assistant-history-64f21a"
	const otherMarker = "r12-other-issue-history-8d390c"
	const finalMarker = "r12-resumed-complete-5e721b"
	resumeArrived := make(chan struct{})
	releaseResponse := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseResponse) }) }
	defer release()
	var requests, unexpected atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.URL.IsAbs() {
			unexpected.Add(1)
			http.Error(w, "unexpected fixture request", http.StatusBadRequest)
			return
		}
		var request struct {
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&request); err != nil {
			t.Error("invalid Responses fixture request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := requests.Add(1)
		firstCount, otherCount, cwdFound := 0, 0, false
		for _, item := range request.Input {
			for _, part := range item.Content {
				if item.Role == "assistant" && part.Type == "output_text" {
					if part.Text == firstMarker {
						firstCount++
					}
					if part.Text == otherMarker {
						otherCount++
					}
				}
				if item.Role == "user" && part.Type == "input_text" {
					var environment struct {
						XMLName xml.Name `xml:"environment_context"`
						Cwd     string   `xml:"cwd"`
					}
					if xml.Unmarshal([]byte(part.Text), &environment) == nil && environment.Cwd == cwd {
						cwdFound = true
					}
					if strings.Contains(part.Text, firstMarker) || strings.Contains(part.Text, otherMarker) {
						t.Error("history marker was injected into a user message instead of restored as assistant history")
					}
				}
			}
		}
		wantFirst := 0
		if n == 3 {
			wantFirst = 1
		}
		if firstCount != wantFirst || otherCount != 0 || !cwdFound {
			t.Errorf("request %d: first_history_count=%d other_history_count=%d cwd_matches=%t", n, firstCount, otherCount, cwdFound)
		}
		if n == 3 {
			close(resumeArrived)
			select {
			case <-releaseResponse:
			case <-r.Context().Done():
				return
			}
		}
		if n < 1 || n > 3 {
			t.Error("unexpected extra model turn")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		codexR12WriteResponse(t, w, n, []string{firstMarker, otherMarker, finalMarker}[n-1])
	}))
	defer provider.Close()
	// Proxy traps are loopback-only; no credentials or ambient proxy configuration survive.
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		t.Setenv(key, provider.URL)
	}
	t.Setenv("NO_PROXY", "127.0.0.1,localhost,::1")
	write(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"), []byte(fmt.Sprintf(`model = "r12-fixture-model"
model_provider = "local_fixture"
approval_policy = "never"
disable_response_storage = true
[model_providers.local_fixture]
name = "R12 loopback fixture"
base_url = %q
wire_api = "responses"
requires_openai_auth = false
[features]
responses_websockets = false
multi_agent = false
memories = false
plugins = false
`, provider.URL+"/v1")))
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	wrapper := filepath.Join(root, "codex-r12")
	write(wrapper, []byte("#!/bin/sh\numask 077\nif [ \"${1:-}\" = app-server ]; then\n"+
		"start=$(/bin/ps -p \"$$\" -o lstart=)\nprintf '%s\\n%s\\n%s\\n' \"$$\" \"$start\" \"$CODEX_HOME\" > "+quote(filepath.Join(root, "processes"))+"/$$\nfi\nexec "+quote(realCodex)+" \"$@\"\n"))
	if err := os.Chmod(wrapper, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/gc-check") {
			_ = json.NewEncoder(w).Encode(IssueGCStatus{Status: "done", UpdatedAt: old})
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/daemon/tasks/") &&
			(strings.HasSuffix(r.URL.Path, "/start") || strings.HasSuffix(r.URL.Path, "/session") ||
				strings.HasSuffix(r.URL.Path, "/progress") || strings.HasSuffix(r.URL.Path, "/messages")) {
			_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 2<<20))
			w.WriteHeader(http.StatusOK)
			return
		}
		unexpected.Add(1)
		http.Error(w, "unexpected control request", http.StatusNotFound)
	}))
	defer control.Close()
	logPath := filepath.Join(root, "lifecycle.log")
	logs, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	logger := slog.New(slog.NewTextHandler(logs, nil))
	cfg := Config{
		DaemonID: "r12-daemon", Profile: "r12-private", WorkspacesRoot: filepath.Join(root, "workspaces"),
		ServerBaseURL: control.URL, Agents: map[string]AgentEntry{"codex": {Path: wrapper}},
		AgentTimeout: 10 * time.Second, CodexHandshakeTimeout: 5 * time.Second,
		CodexThreadHandshakeTimeout: 5 * time.Second, GCTTL: time.Hour,
		GCArtifactTTL: time.Hour, GCArtifactPatterns: []string{"r12-expired-cache"},
	}
	newInstance := func() *Daemon {
		d := New(cfg, logger)
		d.client.SetToken("r12-fake-control-token")
		d.executionEnvironmentCommand = func() ([]string, error) {
			return []string{os.Args[0], "-test.run=^TestCodexR12PreparationHelper$", "--", "r12-preparation-helper"}, nil
		}
		return d
	}
	ref, err := json.Marshal(localDirectoryRef{LocalPath: cwd, DaemonID: cfg.DaemonID})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "11111111-1111-4111-8111-111111111111", WorkspaceID: "r12-workspace", IssueID: "r12-issue-a",
		AgentID: "r12-agent", RuntimeID: "r12-runtime", AuthToken: "mat_r12_fake_task_token",
		Agent:            &AgentData{ID: "r12-agent", Name: "R12 fixture", McpConfig: json.RawMessage(`{"mcpServers":{}}`)},
		ProjectResources: []ProjectResourceData{{ResourceType: "local_directory", ResourceRef: ref}},
	}
	run := func(d *Daemon, task Task) TaskResult {
		t.Helper()
		result, err := d.runTask(ctx, task, "codex", 0, logger)
		if err != nil || result.Status != "completed" || result.SessionID == "" || result.SessionRolloutMissing || result.RetiredSessionID != "" {
			t.Fatalf("runTask did not complete resumably: status=%s missing_rollout=%t error=%v comment=%s", result.Status, result.SessionRolloutMissing, err, result.Comment)
		}
		return result
	}
	readRollout := func(store, session string) (string, []byte) {
		t.Helper()
		var paths []string
		if err := filepath.WalkDir(store, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), ".jsonl") {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(paths) != 1 || !strings.Contains(filepath.Base(paths[0]), session) {
			t.Fatalf("expected exactly one native rollout for the returned session, count=%d", len(paths))
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		return paths[0], data
	}
	started := time.Now()
	firstDaemon := newInstance()
	first := run(firstDaemon, task)
	firstProcesses, err := os.ReadDir(filepath.Join(root, "processes"))
	if err != nil || len(firstProcesses) != 1 {
		t.Fatal("first task did not launch exactly one native app-server")
	}
	firstPID, err := strconv.Atoi(firstProcesses[0].Name())
	if err != nil || firstPID <= 1 || !codexR12WaitGroupGone(firstPID, time.Second) {
		t.Fatal("first native process must exit before daemon instance recreation")
	}
	storeA := execenv.CodexSessionStorePath(cfg.Profile, execenv.TaskContextForEnv{AgentID: task.AgentID, IssueID: task.IssueID})
	rolloutPath, original := readRollout(storeA, first.SessionID)
	if !bytes.Contains(original, []byte(firstMarker)) {
		t.Fatal("real rollout lacks first assistant response")
	}
	firstDaemon = nil
	secondDaemon := newInstance()
	meta, ok := gcMetaForTask(task)
	if !ok {
		t.Fatal("missing GC metadata")
	}
	meta.LocalDirectory, meta.CompletedAt = true, old
	if err := execenv.WriteGCMeta(first.EnvRoot, meta, logger); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(first.EnvRoot, "r12-expired-cache", "data")
	write(cache, []byte("regenerable"))
	action := secondDaemon.shouldCleanTaskDir(ctx, first.EnvRoot)
	if action != gcActionCleanArtifacts {
		t.Fatalf("local_directory GC action=%d, want artifact cleanup", action)
	}
	stats := &gcStats{byPattern: make(map[string]int)}
	secondDaemon.applyGCAction(context.Background(), first.EnvRoot, action, stats)
	if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) || stats.artifactDirs != 1 {
		t.Fatal("targeted artifact GC did not reclaim fixture cache")
	}
	_, retained := readRollout(storeA, first.SessionID)
	if !bytes.Equal(original, retained) {
		t.Fatal("artifact GC changed the real rollout")
	}
	otherTask := task
	otherTask.ID, otherTask.IssueID = "22222222-2222-4222-8222-222222222222", "r12-issue-b"
	other := run(secondDaemon, otherTask)
	storeB := execenv.CodexSessionStorePath(cfg.Profile, execenv.TaskContextForEnv{AgentID: task.AgentID, IssueID: otherTask.IssueID})
	_, otherData := readRollout(storeB, other.SessionID)
	if storeA == storeB || other.SessionID == first.SessionID || bytes.Contains(otherData, []byte(firstMarker)) {
		t.Fatal("other issue shared native history")
	}
	for _, store := range []string{storeA, storeB} {
		if err := filepath.WalkDir(store, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(path, old, old)
		}); err != nil {
			t.Fatal(err)
		}
	}
	followup := task
	followup.ID = "33333333-3333-4333-8333-333333333333"
	followup.PriorSessionID, followup.PriorWorkDir = first.SessionID, first.WorkDir
	type completion struct {
		result TaskResult
		err    error
	}
	done := make(chan completion, 1)
	resumeCtx, cancelResume := context.WithCancel(ctx)
	go func() {
		result, err := secondDaemon.runTask(resumeCtx, followup, "codex", 0, logger)
		done <- completion{result, err}
	}()
	joined := false
	defer func() {
		release()
		cancelResume()
		if !joined {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("resume runTask did not drain on cancellation")
			}
		}
	}()
	select {
	case <-resumeArrived:
	case early := <-done:
		joined = true
		t.Fatalf("resume ended before historical request: status=%s error=%v", early.result.Status, early.err)
	case <-ctx.Done():
		t.Fatal("resume request deadline exceeded")
	}
	removed, _ := execenv.PruneCodexSessionStores(cfg.Profile, 24*time.Hour, time.Now(), secondDaemon.reserveStoreForDeletion, logger)
	if removed != 1 {
		t.Fatalf("expired sibling stores reclaimed=%d, want 1", removed)
	}
	if _, err := os.Stat(storeB); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired sibling store survived GC")
	}
	_, beforeContinuation := readRollout(storeA, first.SessionID)
	if !bytes.HasPrefix(beforeContinuation, original) {
		t.Fatal("reopened rollout lost or rewrote original history")
	}
	release()
	var resumed completion
	select {
	case resumed = <-done:
		joined = true
	case <-ctx.Done():
		t.Fatal("resumed turn deadline exceeded")
	}
	if resumed.err != nil || resumed.result.Status != "completed" || resumed.result.SessionID != first.SessionID ||
		resumed.result.SessionRolloutMissing || resumed.result.RetiredSessionID != "" || resumed.result.Comment != finalMarker {
		t.Fatalf("native resume failed: status=%s same_session=%t error=%v", resumed.result.Status, resumed.result.SessionID == first.SessionID, resumed.err)
	}
	finalPath, finalData := readRollout(storeA, first.SessionID)
	if finalPath != rolloutPath || !bytes.HasPrefix(finalData, original) || !bytes.Contains(finalData, []byte(finalMarker)) || bytes.Contains(finalData, []byte(otherMarker)) {
		t.Fatal("native continuation did not append isolated history to the exact original rollout")
	}
	if first.EnvRoot == resumed.result.EnvRoot || first.WorkDir != resumed.result.WorkDir {
		t.Fatal("followup must create a new task home while retaining the local repository")
	}
	for _, result := range []TaskResult{first, resumed.result} {
		target, err := filepath.EvalSymlinks(filepath.Join(result.EnvRoot, "codex-home", "sessions"))
		if err != nil || target != storeA {
			t.Fatal("task-scoped sessions did not mount the expected private issue store")
		}
	}
	lifecycle, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 || unexpected.Load() != 0 || bytes.Count(lifecycle, []byte("phase=thread_resume_response")) != 1 || bytes.Contains(lifecycle, []byte("falling back to thread/start")) {
		t.Fatalf("unexpected native lifecycle: responses=%d unexpected_requests=%d", requests.Load(), unexpected.Load())
	}
	processes, err := os.ReadDir(filepath.Join(root, "processes"))
	if err != nil || len(processes) != 3 {
		t.Fatal("expected three distinct native app-server processes")
	}
	homes := map[string]bool{
		filepath.Join(first.EnvRoot, "codex-home"):          false,
		filepath.Join(other.EnvRoot, "codex-home"):          false,
		filepath.Join(resumed.result.EnvRoot, "codex-home"): false,
	}
	for _, process := range processes {
		data, err := os.ReadFile(filepath.Join(root, "processes", process.Name()))
		parts := strings.Split(strings.TrimSpace(string(data)), "\n")
		if err != nil || len(parts) != 3 {
			t.Fatal("invalid native startup receipt")
		}
		pid, err := strconv.Atoi(process.Name())
		seen, known := homes[parts[2]]
		if err != nil || pid <= 1 || !known || seen || !codexR12WaitGroupGone(pid, time.Second) {
			t.Fatal("native process was not reaped or did not use a distinct prepared task home")
		}
		homes[parts[2]] = true
	}
	helpers, err := os.ReadDir(filepath.Join(root, "helpers"))
	if err != nil || len(helpers) != 3 {
		t.Fatalf("preparation did not cross three helper processes: count=%d error=%v", len(helpers), err)
	}
	for _, helper := range helpers {
		pid, err := strconv.Atoi(helper.Name())
		if err != nil || pid == os.Getpid() || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			t.Fatal("preparation helper was not distinct and reaped")
		}
	}
	codexR12CleanupProcesses(t, filepath.Join(root, "processes"))
	if t.Failed() {
		return
	}
	t.Logf(`R12_RECEIPT {"verdict":"instance_recreation_only","codex_version":"0.153.4","daemon_instances":2,"native_processes":3,"responses":3,"preparation_helpers":3,"same_native_session":true,"original_rollout_prefix_retained":true,"other_issue_isolated":true,"expired_sibling_reclaimed":true,"reopened_store_preserved":true,"scoped_artifact_reclaimed":true,"processes_reaped":true,"global_credentials":false,"external_model":false,"elapsed_ms":%d}`, time.Since(started).Milliseconds())
}

func TestCodexR12PreparationHelper(t *testing.T) {
	if os.Getenv("MULTICA_TEST_R12_ROOT") == "" || os.Args[len(os.Args)-1] != "r12-preparation-helper" {
		t.Skip("private R12 preparation subprocess only")
	}
	marker := filepath.Join(os.Getenv("MULTICA_TEST_R12_ROOT"), "helpers", strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(marker, []byte("preparation-helper"), 0o600); err != nil {
		os.Exit(2)
	}
	if err := execenv.RunPreparationHelper(os.Stdin, os.Stdout, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func codexR12WriteResponse(t *testing.T, w io.Writer, n int32, text string) {
	t.Helper()
	item := map[string]any{"type": "message", "id": fmt.Sprintf("msg_r12_%d", n), "status": "completed", "role": "assistant",
		"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
	envelope := func(status string, output []any, usage any) map[string]any {
		return map[string]any{"id": fmt.Sprintf("resp_r12_%d", n), "object": "response", "created_at": 0, "status": status,
			"model": "r12-fixture-model", "output": output, "error": nil, "incomplete_details": nil, "instructions": nil,
			"parallel_tool_calls": false, "previous_response_id": nil, "store": false, "tools": []any{}, "usage": usage}
	}
	events := []map[string]any{
		{"type": "response.created", "sequence_number": 0, "response": envelope("in_progress", []any{}, nil)},
		{"type": "response.output_item.done", "sequence_number": 1, "output_index": 0, "item": item},
		{"type": "response.completed", "sequence_number": 2, "response": envelope("completed", []any{item}, map[string]any{
			"input_tokens": 1, "output_tokens": 1, "total_tokens": 2,
			"input_tokens_details": map[string]int{"cached_tokens": 0}, "output_tokens_details": map[string]int{"reasoning_tokens": 0}})},
	}
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Error("encode synthetic SSE")
			return
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data); err != nil {
			t.Error("write synthetic SSE")
			return
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func codexR12WaitGroupGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func codexR12CleanupProcesses(t *testing.T, directory string) {
	t.Helper()
	markers, err := os.ReadDir(directory)
	if err != nil {
		t.Error("read private native process markers")
		return
	}
	for _, marker := range markers {
		data, err := os.ReadFile(filepath.Join(directory, marker.Name()))
		parts := strings.Split(strings.TrimSpace(string(data)), "\n")
		if err != nil || len(parts) < 2 {
			t.Error("invalid native process marker")
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil || pid <= 1 || marker.Name() != parts[0] {
			t.Error("invalid native process identity")
			continue
		}
		if codexR12WaitGroupGone(pid, time.Second) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		start, startErr := exec.CommandContext(ctx, "/bin/ps", "-p", parts[0], "-o", "lstart=").Output()
		cancel()
		group, groupErr := syscall.Getpgid(pid)
		if startErr != nil || groupErr != nil || group != pid || strings.TrimSpace(string(start)) != strings.TrimSpace(parts[1]) {
			t.Error("refusing cleanup of native process whose startup identity changed")
			continue
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Error("kill owned native process group")
		}
		if !codexR12WaitGroupGone(pid, 2*time.Second) {
			t.Error("native process group remained after bounded cleanup")
		}
	}
}
