package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newRepoRegistryTestCmd(serverURL string) *cobra.Command {
	cmd := &cobra.Command{Use: "repo-test"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().StringArray("url", nil, "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("output", "json", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	return cmd
}

func TestRunRepoAddAppendsAndDedupes(t *testing.T) {
	initialRepos := []workspaceRepo{{URL: "https://git.example.com/web.git"}}
	var patched []workspaceRepo
	patchCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initialRepos})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			patchCount++
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("url", "https://git.example.com/web.git"); err != nil {
		t.Fatal(err)
	}
	err := runRepoAdd(cmd, []string{
		"https://git.example.com/api.git",
		"https://git.example.com/api.git",
	})
	if err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	if patchCount != 1 {
		t.Fatalf("patchCount = %d, want 1", patchCount)
	}
	if len(patched) != 2 {
		t.Fatalf("patched repos = %+v, want 2 entries", patched)
	}
	if patched[0].URL != "https://git.example.com/web.git" || patched[1].URL != "https://git.example.com/api.git" {
		t.Fatalf("unexpected patched repos: %+v", patched)
	}
}

func TestRunRepoAddUpdatesDescriptionForExistingRepo(t *testing.T) {
	initialRepos := []workspaceRepo{{URL: "https://git.example.com/web.git", Description: "old"}}
	var patched []workspaceRepo

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initialRepos})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("description", "new"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoAdd(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	if len(patched) != 1 || patched[0].Description != "new" {
		t.Fatalf("patched repos = %+v, want updated description", patched)
	}
}

func TestRunRepoAddRejectsDescriptionForMultipleRepos(t *testing.T) {
	cmd := newRepoRegistryTestCmd("http://127.0.0.1:0")
	if err := cmd.Flags().Set("description", "shared"); err != nil {
		t.Fatal(err)
	}
	err := runRepoAdd(cmd, []string{"https://git.example.com/a.git", "https://git.example.com/b.git"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--description") {
		t.Fatalf("error = %q, want description guidance", err)
	}
}

func TestRunRepoRemoveDeletesExistingRepos(t *testing.T) {
	initialRepos := []workspaceRepo{
		{URL: "https://git.example.com/web.git"},
		{URL: "https://git.example.com/api.git"},
		{URL: "https://git.example.com/mobile.git"},
	}
	var patched []workspaceRepo

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initialRepos})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("url", "https://git.example.com/mobile.git"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoRemove(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoRemove: %v", err)
	}
	if len(patched) != 1 || patched[0].URL != "https://git.example.com/api.git" {
		t.Fatalf("patched repos = %+v, want only api repo", patched)
	}
}

func TestRunRepoRemoveRejectsMissingRepoWithoutPatch(t *testing.T) {
	patchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{
				ID:    "ws-1",
				Repos: []workspaceRepo{{URL: "https://git.example.com/web.git"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			patchCount++
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	err := runRepoRemove(cmd, []string{"https://git.example.com/missing.git"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want not found", err)
	}
	if patchCount != 0 {
		t.Fatalf("patchCount = %d, want 0", patchCount)
	}
}

func TestRunRepoCheckoutForwardsManagedCheckoutMode(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repo/checkout" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode checkout body: %v", err)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mat_repo_checkout_test" {
			t.Fatalf("Authorization = %q, want task-scoped bearer", got)
		}
		json.NewEncoder(w).Encode(map[string]string{
			"path":        "/work/repo",
			"branch_name": "agent/test/task",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_DAEMON_PORT", strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_AGENT_NAME", "Test Agent")
	t.Setenv("MULTICA_TASK_ID", "task-1")
	t.Setenv("MULTICA_TOKEN", "mat_repo_checkout_test")
	t.Setenv("MULTICA_REPO_CHECKOUT_MODE", "isolated")

	previousRef, previousFresh := repoCheckoutRef, repoCheckoutFresh
	repoCheckoutRef, repoCheckoutFresh = "release/v2", true
	defer func() { repoCheckoutRef, repoCheckoutFresh = previousRef, previousFresh }()

	if err := runRepoCheckout(&cobra.Command{}, []string{"https://github.com/org/repo.git"}); err != nil {
		t.Fatalf("runRepoCheckout: %v", err)
	}
	if got := body["checkout_mode"]; got != "isolated" {
		t.Fatalf("checkout_mode = %q, want isolated", got)
	}
	if got := body["ref"]; got != "release/v2" {
		t.Fatalf("ref = %q, want release/v2", got)
	}
	if got := body["retry_busy"]; got != true {
		t.Fatalf("retry_busy = %v, want true", got)
	}
	if got := body["fresh"]; got != true {
		t.Fatalf("fresh = %v, want true", got)
	}
}

func TestRepoCheckoutSummary(t *testing.T) {
	t.Parallel()
	const repoURL = "https://github.com/org/repo.git"
	for _, tc := range []struct {
		name   string
		result repoCheckoutResult
		want   []string
	}{
		{
			name:   "new branch",
			result: repoCheckoutResult{Path: "/work/repo", BranchName: "agent/test/task"},
			want:   []string{"Checked out " + repoURL + " → /work/repo (branch: agent/test/task)"},
		},
		{
			name:   "kept for local work",
			result: repoCheckoutResult{Path: "/work/repo", BranchName: "agent/test/old", Kept: "local_work", UncommittedFiles: 2, UnpushedCommits: 1},
			want: []string{
				"Kept the existing checkout of " + repoURL + " at /work/repo (branch: agent/test/old; 2 uncommitted files, 1 unpushed commit)",
				"nothing was reset, cleaned, or switched",
				"re-run with --fresh",
			},
		},
		{
			name:   "kept on the task branch",
			result: repoCheckoutResult{Path: "/work/repo", BranchName: "agent/test/task", Kept: "task_branch"},
			want:   []string{"(branch: agent/test/task, this task's branch; 0 uncommitted files, 0 unpushed commits)"},
		},
		{
			name:   "kept on a detached HEAD",
			result: repoCheckoutResult{Path: "/work/repo", Kept: "local_work", UnpushedCommits: 3},
			want:   []string{"(branch: detached HEAD; 0 uncommitted files, 3 unpushed commits)"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := repoCheckoutSummary(repoURL, tc.result)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("summary missing %q:\n%s", want, got)
				}
			}
			if kept := strings.HasPrefix(got, "Kept "); kept != (tc.result.Kept != "") {
				t.Fatalf("summary reads kept=%v for Kept=%q:\n%s", kept, tc.result.Kept, got)
			}
		})
	}
}

func TestRunRepoCheckoutRequiresTaskCredential(t *testing.T) {
	t.Setenv("MULTICA_DAEMON_PORT", "12345")
	t.Setenv("MULTICA_TOKEN", "")

	err := runRepoCheckout(&cobra.Command{}, []string{"https://github.com/org/repo.git"})
	if err == nil || !strings.Contains(err.Error(), "MULTICA_TOKEN not set") {
		t.Fatalf("runRepoCheckout error = %v, want missing task credential", err)
	}
}

func TestRunRepoCheckoutRetriesServiceUnavailable(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("X-Multica-Retryable", "repo-busy")
			w.Header().Set("Retry-After", "0")
			http.Error(w, "repository busy", http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"path":        "/work/repo",
			"branch_name": "agent/test/task",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_DAEMON_PORT", strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_AGENT_NAME", "Test Agent")
	t.Setenv("MULTICA_TASK_ID", "task-1")
	t.Setenv("MULTICA_TOKEN", "mat_repo_checkout_test")

	if err := runRepoCheckout(&cobra.Command{}, []string{"https://github.com/org/repo.git"}); err != nil {
		t.Fatalf("runRepoCheckout: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("checkout attempts = %d, want 2", attempts)
	}
}

func TestRunRepoCheckoutDoesNotRetryUnmarkedServiceUnavailable(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "daemon unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_DAEMON_PORT", strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	t.Setenv("MULTICA_TOKEN", "mat_repo_checkout_test")
	if err := runRepoCheckout(&cobra.Command{}, []string{"https://github.com/org/repo.git"}); err == nil {
		t.Fatal("runRepoCheckout unexpectedly succeeded")
	}
	if attempts != 1 {
		t.Fatalf("checkout attempts = %d, want 1", attempts)
	}
}

func TestRepoCheckoutRetryDelay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	if got := repoCheckoutRetryDelay("7", now); got != 7*time.Second {
		t.Fatalf("seconds delay = %s, want 7s", got)
	}
	if got := repoCheckoutRetryDelay(now.Add(time.Minute).Format(http.TimeFormat), now); got != 30*time.Second {
		t.Fatalf("capped date delay = %s, want 30s", got)
	}
	if got := repoCheckoutRetryDelay("invalid", now); got != time.Second {
		t.Fatalf("default delay = %s, want 1s", got)
	}
}

// TestRunRepoCheckoutAllChecksOutEachGithubRepo verifies that --all reads
// resources.json and issues one checkout request per github_repo resource,
// honouring each resource's own ref when present.

// bodyStr reads a string field out of a decoded JSON body that may also
// carry non-string fields (retry_busy is a bool).
func bodyStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func TestRunRepoCheckoutAllChecksOutEachGithubRepo(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repo/checkout" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode checkout body: %v", err)
		}
		requests = append(requests, body)
		// Return a fake result whose path is based on the requested URL so we
		// can verify ordering independently.
		json.NewEncoder(w).Encode(map[string]string{
			"path":        "/work/" + bodyStr(body, "url"),
			"branch_name": "agent/test/task",
		})
	}))
	defer srv.Close()

	// Write a resources.json that contains two github_repo resources plus one
	// local_directory resource (which must be ignored by --all).
	workDir := t.TempDir()
	resourcesDir := workDir + "/.multica/project"
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resourcesJSON := `{
		"project_id": "proj-1",
		"resources": [
			{
				"id": "r-1",
				"resource_type": "github_repo",
				"resource_ref": {"url": "https://github.com/org-a/app.git"}
			},
			{
				"id": "r-2",
				"resource_type": "github_repo",
				"resource_ref": {"url": "https://github.com/org-b/app.git", "ref": "feature/x"}
			},
			{
				"id": "r-3",
				"resource_type": "local_directory",
				"resource_ref": {"local_path": "/tmp/local", "daemon_id": "d-1"}
			}
		]
	}`
	if err := os.WriteFile(resourcesDir+"/resources.json", []byte(resourcesJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MULTICA_DAEMON_PORT", strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_AGENT_NAME", "Test Agent")
	t.Setenv("MULTICA_TASK_ID", "task-1")
	t.Setenv("MULTICA_REPO_CHECKOUT_MODE", "")

	if err := runRepoCheckoutAll(workDir); err != nil {
		t.Fatalf("runRepoCheckoutAll: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected 2 checkout requests, got %d", len(requests))
	}

	// Build a url→ref map from the actual requests.
	byURL := make(map[string]string, len(requests))
	for _, req := range requests {
		byURL[bodyStr(req, "url")] = bodyStr(req, "ref")
	}

	if _, ok := byURL["https://github.com/org-a/app.git"]; !ok {
		t.Error("expected checkout request for org-a/app.git")
	}
	if byURL["https://github.com/org-a/app.git"] != "" {
		t.Errorf("org-a/app.git ref = %q, want empty (use default)", byURL["https://github.com/org-a/app.git"])
	}
	if _, ok := byURL["https://github.com/org-b/app.git"]; !ok {
		t.Error("expected checkout request for org-b/app.git")
	}
	if byURL["https://github.com/org-b/app.git"] != "feature/x" {
		t.Errorf("org-b/app.git ref = %q, want feature/x", byURL["https://github.com/org-b/app.git"])
	}
}

// TestRunRepoCheckoutAllReportsPartialFailure verifies that --all exits non-zero
// when at least one repo checkout fails, and continues to attempt the remaining repos.
func TestRunRepoCheckoutAllReportsPartialFailure(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repo/checkout" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode checkout body: %v", err)
		}
		requested = append(requested, bodyStr(body, "url"))
		if strings.Contains(bodyStr(body, "url"), "bad") {
			http.Error(w, "checkout failed: repo not found", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"path":        "/work/good",
			"branch_name": "agent/test/task",
		})
	}))
	defer srv.Close()

	workDir := t.TempDir()
	resourcesDir := workDir + "/.multica/project"
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resourcesJSON := `{
		"project_id": "proj-1",
		"resources": [
			{"id": "r-1", "resource_type": "github_repo", "resource_ref": {"url": "https://github.com/org/good.git"}},
			{"id": "r-2", "resource_type": "github_repo", "resource_ref": {"url": "https://github.com/org/bad.git"}}
		]
	}`
	if err := os.WriteFile(resourcesDir+"/resources.json", []byte(resourcesJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MULTICA_DAEMON_PORT", strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_AGENT_NAME", "Test Agent")
	t.Setenv("MULTICA_TASK_ID", "task-1")
	t.Setenv("MULTICA_REPO_CHECKOUT_MODE", "")

	err := runRepoCheckoutAll(workDir)
	if err == nil {
		t.Fatal("expected non-nil error when at least one checkout fails")
	}

	// Both repos must have been attempted despite the failure.
	if len(requested) != 2 {
		t.Fatalf("expected 2 checkout attempts, got %d: %v", len(requested), requested)
	}
}

// TestRunRepoCheckoutAllNoResources verifies that --all succeeds (exit 0) when
// resources.json has no github_repo entries.
func TestRunRepoCheckoutAllNoResources(t *testing.T) {
	workDir := t.TempDir()
	resourcesDir := workDir + "/.multica/project"
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resourcesJSON := `{"project_id": "proj-1", "resources": []}`
	if err := os.WriteFile(resourcesDir+"/resources.json", []byte(resourcesJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MULTICA_DAEMON_PORT", "9999")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_AGENT_NAME", "Test Agent")
	t.Setenv("MULTICA_TASK_ID", "task-1")
	t.Setenv("MULTICA_REPO_CHECKOUT_MODE", "")

	if err := runRepoCheckoutAll(workDir); err != nil {
		t.Fatalf("runRepoCheckoutAll with no github_repos: %v", err)
	}
}
