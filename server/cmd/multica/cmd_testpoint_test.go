package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newTestPointUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	for _, flag := range []string{"title", "category", "description", "status"} {
		cmd.Flags().String(flag, "", "")
	}
	cmd.Flags().String("output", "json", "")
	return cmd
}

// A "MUL-12/TP-3" reference cannot travel in a URL path, so the CLI reads the
// issue's points to find the id before patching it.
func TestTestcasePointUpdateResolvesAnIssueScopedReference(t *testing.T) {
	var patchedPath string
	var patchedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-12/test-points":
			_ = json.NewEncoder(w).Encode(map[string]any{"test_points": []map[string]any{
				{"id": "p-1", "number": 1}, {"id": "p-3", "number": 3},
			}})
		case r.Method == http.MethodPatch:
			patchedPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&patchedBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "p-3", "status": "dropped"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestPointUpdateCmd()
	_ = cmd.Flags().Set("status", "dropped")
	if _, err := captureStdout(t, func() error { return runTestcasePointUpdate(cmd, []string{"MUL-12/tp-3"}) }); err != nil {
		t.Fatalf("update: %v", err)
	}
	if patchedPath != "/api/test-points/p-3" || patchedBody["status"] != "dropped" || len(patchedBody) != 1 {
		t.Fatalf("patched %s with %v, want only the status of p-3", patchedPath, patchedBody)
	}

	missing := newTestPointUpdateCmd()
	_ = missing.Flags().Set("title", "x")
	if err := runTestcasePointUpdate(missing, []string{"MUL-12/TP-9"}); err == nil || !strings.Contains(err.Error(), "has no TP-9") {
		t.Fatalf("missing point: err = %v, want it named", err)
	}
	if err := runTestcasePointUpdate(newTestPointUpdateCmd(), []string{"p-1"}); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("empty update: err = %v", err)
	}
}

func TestTestcaseCoverSendsEveryPointReference(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"test_points": []any{}, "total": 2})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := &cobra.Command{Use: "cover"}
	cmd.Flags().String("output", "json", "")
	if _, err := captureStdout(t, func() error {
		return runTestcaseCover(cmd, []string{"TC-42", "MUL-12/TP-1", "MUL-12/TP-3"})
	}); err != nil {
		t.Fatalf("cover: %v", err)
	}
	refs, _ := gotBody["test_points"].([]any)
	if gotPath != "/api/test-cases/TC-42/test-points" || len(refs) != 2 || refs[1] != "MUL-12/TP-3" {
		t.Fatalf("posted %s %v, want both refs to TC-42", gotPath, gotBody)
	}
}
