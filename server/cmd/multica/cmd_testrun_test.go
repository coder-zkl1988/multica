package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const (
	testRunID     = "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	testRunCaseID = "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	testPlanID    = "cccccccc-cccc-4ccc-cccc-cccccccccccc"
)

func newTestRunGetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newTestRunStartCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newTestResultSetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "set"}
	cmd.Flags().String("result", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("step-results", "", "")
	cmd.Flags().StringArray("step", nil, "")
	return cmd
}

func newTestEvidenceAddCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("kind", "", "")
	return cmd
}

func newTestDefectOpenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "open"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("note", "", "")
	return cmd
}

func newTestCapListCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("run", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newTestPlanListCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newTestPlanGetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

// ---------------------------------------------------------------------------
// multica test run get
// ---------------------------------------------------------------------------

func TestRunTestRunGetHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     testRunID,
			"title":  "Sprint 1",
			"status": "pending",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestRunGetCmd()
	out, err := captureStdout(t, func() error { return runTestRunGet(cmd, []string{testRunID}) })
	if err != nil {
		t.Fatalf("runTestRunGet: %v", err)
	}

	wantPath := "/api/test-runs/" + testRunID
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if !strings.Contains(out, testRunID) {
		t.Errorf("stdout missing run id: %q", out)
	}
}

// ---------------------------------------------------------------------------
// multica test run start
// ---------------------------------------------------------------------------

func TestRunTestRunStartHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     testRunID,
			"status": "running",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestRunStartCmd()
	_, err := captureStdout(t, func() error { return runTestRunStart(cmd, []string{testRunID}) })
	if err != nil {
		t.Fatalf("runTestRunStart: %v", err)
	}

	wantPath := "/api/test-runs/" + testRunID + "/start"
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

// ---------------------------------------------------------------------------
// multica test result set
// ---------------------------------------------------------------------------

func TestRunTestResultSetHitsCorrectPathAndBody(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     testRunCaseID,
			"result": "passed",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestResultSetCmd()
	if err := cmd.Flags().Set("result", "passed"); err != nil {
		t.Fatalf("set result flag: %v", err)
	}
	if err := cmd.Flags().Set("note", "all good"); err != nil {
		t.Fatalf("set note flag: %v", err)
	}

	_, err := captureStdout(t, func() error { return runTestResultSet(cmd, []string{testRunCaseID}) })
	if err != nil {
		t.Fatalf("runTestResultSet: %v", err)
	}

	wantPath := "/api/test-run-cases/" + testRunCaseID + "/result"
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotBody["result"] != "passed" {
		t.Errorf("body.result = %v, want passed", gotBody["result"])
	}
	if gotBody["notes"] != "all good" {
		t.Errorf("body.notes = %v, want 'all good'", gotBody["notes"])
	}
}

func TestRunTestResultSetRequiresResultFlag(t *testing.T) {
	cmd := newTestResultSetCmd()
	// No --result flag set; should fail immediately without a server.
	err := runTestResultSet(cmd, []string{testRunCaseID})
	if err == nil {
		t.Fatal("expected error when --result is missing")
	}
	if !strings.Contains(err.Error(), "--result is required") {
		t.Errorf("error should name the missing flag, got: %v", err)
	}
}

func TestRunTestResultSetRejectsMalformedStepResults(t *testing.T) {
	// No server needed — the JSON validation is client-side.
	t.Setenv("MULTICA_SERVER_URL", "http://localhost:9") // unreachable
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_tok")

	cmd := newTestResultSetCmd()
	if err := cmd.Flags().Set("result", "failed"); err != nil {
		t.Fatalf("set result: %v", err)
	}
	if err := cmd.Flags().Set("step-results", "not-json"); err != nil {
		t.Fatalf("set step-results: %v", err)
	}

	err := runTestResultSet(cmd, []string{testRunCaseID})
	if err == nil {
		t.Fatal("malformed --step-results should fail client-side")
	}
	if !strings.Contains(err.Error(), "--step-results must be valid JSON") {
		t.Errorf("error should name the flag, got: %v", err)
	}
}

func TestRunTestResultSetSendsRepeatedStepFlags(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": testRunCaseID, "result": "failed"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestResultSetCmd()
	for flag, value := range map[string]string{"result": "failed"} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []string{"1=passed", "2=failed:total stayed at 100.00: no discount", "3=not_run"} {
		if err := cmd.Flags().Set("step", step); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := captureStdout(t, func() error { return runTestResultSet(cmd, []string{testRunCaseID}) }); err != nil {
		t.Fatalf("runTestResultSet: %v", err)
	}
	steps, _ := gotBody["step_results"].([]any)
	if len(steps) != 3 {
		t.Fatalf("step_results = %v, want three steps", gotBody["step_results"])
	}
	second, _ := steps[1].(map[string]any)
	if second["index"] != float64(2) || second["result"] != "failed" || second["actual"] != "total stayed at 100.00: no discount" {
		t.Errorf("second step = %v, want index 2 failed with the observed total (colons kept)", second)
	}
	if third, _ := steps[2].(map[string]any); third["actual"] != nil {
		t.Errorf("third step = %v, want no actual", third)
	}
}

func TestRunTestResultSetRejectsMalformedStepFlags(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://localhost:9") // unreachable
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_tok")
	for _, tc := range []struct {
		steps     []string
		stepsJSON string
		want      string
	}{
		{steps: []string{"passed"}, want: "<index>=<result>"},
		{steps: []string{"0=passed"}, want: "starting at 1"},
		{steps: []string{"one=passed"}, want: "starting at 1"},
		{steps: []string{"1=passed"}, stepsJSON: `[]`, want: "not both"},
	} {
		cmd := newTestResultSetCmd()
		_ = cmd.Flags().Set("result", "passed")
		for _, step := range tc.steps {
			_ = cmd.Flags().Set("step", step)
		}
		if tc.stepsJSON != "" {
			_ = cmd.Flags().Set("step-results", tc.stepsJSON)
		}
		err := runTestResultSet(cmd, []string{testRunCaseID})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("steps %v: err = %v, want it to mention %q", tc.steps, err, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// multica test evidence add
// ---------------------------------------------------------------------------

func TestRunTestEvidenceAddHitsCorrectPathAndFields(t *testing.T) {
	var gotTestRunCaseID, gotKind, gotFilename string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/upload-file" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotTestRunCaseID = r.FormValue("test_run_case_id")
		gotKind = r.FormValue("kind")
		if f, fh, err := r.FormFile("file"); err == nil {
			gotFilename = fh.Filename
			_ = f.Close()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "att-001",
			"markdown_url": "https://example.com/att-001",
			"content_type": "image/png",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	// Evidence upload runs inside an agent task (mat_ token).
	t.Setenv("MULTICA_TOKEN", "mat_tok")

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	cmd := newTestEvidenceAddCmd()
	if err := cmd.Flags().Set("file", imgPath); err != nil {
		t.Fatalf("set file flag: %v", err)
	}
	if err := cmd.Flags().Set("kind", "screenshot"); err != nil {
		t.Fatalf("set kind flag: %v", err)
	}

	_, err := captureStdout(t, func() error { return runTestEvidenceAdd(cmd, []string{testRunCaseID}) })
	if err != nil {
		t.Fatalf("runTestEvidenceAdd: %v", err)
	}

	if gotTestRunCaseID != testRunCaseID {
		t.Errorf("test_run_case_id = %q, want %q", gotTestRunCaseID, testRunCaseID)
	}
	if gotKind != "screenshot" {
		t.Errorf("kind = %q, want screenshot", gotKind)
	}
	if gotFilename != "shot.png" {
		t.Errorf("filename = %q, want shot.png", gotFilename)
	}
}

func TestRunTestEvidenceAddRequiresFileFlag(t *testing.T) {
	cmd := newTestEvidenceAddCmd()
	// No --file flag; should fail without a server.
	err := runTestEvidenceAdd(cmd, []string{testRunCaseID})
	if err == nil {
		t.Fatal("expected error when --file is missing")
	}
	if !strings.Contains(err.Error(), "--file is required") {
		t.Errorf("error should name the missing flag, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// multica test defect open
// ---------------------------------------------------------------------------

func TestRunTestDefectOpenHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"test_run_case": map[string]any{"id": testRunCaseID},
			"issue":         map[string]any{"id": "issue-001", "key": "MUL-1"},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestDefectOpenCmd()
	if err := cmd.Flags().Set("title", "Login button broken"); err != nil {
		t.Fatalf("set title flag: %v", err)
	}

	_, err := captureStdout(t, func() error { return runTestDefectOpen(cmd, []string{testRunCaseID}) })
	if err != nil {
		t.Fatalf("runTestDefectOpen: %v", err)
	}

	wantPath := "/api/test-run-cases/" + testRunCaseID + "/defect"
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotBody["title"] != "Login button broken" {
		t.Errorf("body.title = %v, want 'Login button broken'", gotBody["title"])
	}
}

// ---------------------------------------------------------------------------
// multica test capability list
// ---------------------------------------------------------------------------

func TestRunTestCapListHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"run_id": testRunID,
			"capability_binding": map[string]any{
				"daemon_id": "daemon-1",
				"resolved":  map[string]any{"browser": "chrome-key"},
			},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestCapListCmd()
	if err := cmd.Flags().Set("run", testRunID); err != nil {
		t.Fatalf("set run flag: %v", err)
	}

	out, err := captureStdout(t, func() error { return runTestCapList(cmd, []string{}) })
	if err != nil {
		t.Fatalf("runTestCapList: %v", err)
	}

	wantPath := "/api/test-runs/" + testRunID + "/capabilities"
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if !strings.Contains(out, "chrome-key") {
		t.Errorf("stdout missing capability key: %q", out)
	}
}

func TestRunTestCapListRequiresRunFlag(t *testing.T) {
	cmd := newTestCapListCmd()
	err := runTestCapList(cmd, []string{})
	if err == nil {
		t.Fatal("expected error when --run is missing")
	}
	if !strings.Contains(err.Error(), "--run is required") {
		t.Errorf("error should name the missing flag, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// multica test plan list
// ---------------------------------------------------------------------------

func TestRunTestPlanListHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"test_plans": []any{
				map[string]any{
					"id":     testPlanID,
					"title":  "Sprint 1 Plan",
					"status": "active",
				},
			},
			"total": 1,
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestPlanListCmd()
	out, err := captureStdout(t, func() error { return runTestPlanList(cmd, []string{}) })
	if err != nil {
		t.Fatalf("runTestPlanList: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/test-plans" {
		t.Errorf("path = %q, want /api/test-plans", gotPath)
	}
	if !strings.Contains(out, testPlanID[:8]) {
		t.Errorf("stdout missing plan id prefix: %q", out)
	}
}

// ---------------------------------------------------------------------------
// multica test plan get
// ---------------------------------------------------------------------------

func TestRunTestPlanGetHitsCorrectPath(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     testPlanID,
			"title":  "Sprint 1 Plan",
			"status": "active",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newTestPlanGetCmd()
	out, err := captureStdout(t, func() error { return runTestPlanGet(cmd, []string{testPlanID}) })
	if err != nil {
		t.Fatalf("runTestPlanGet: %v", err)
	}

	wantPath := "/api/test-plans/" + testPlanID
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if !strings.Contains(out, testPlanID) {
		t.Errorf("stdout missing plan id: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Path helper unit tests
// ---------------------------------------------------------------------------

func TestTestRunAPIPathBuildsCorrectPaths(t *testing.T) {
	client := newTestAPIClient("ws-abc")

	got := testRunAPIPath(client, testRunID, "")
	want := "/api/test-runs/" + testRunID + "?workspace_id=ws-abc"
	if got != want {
		t.Errorf("testRunAPIPath() = %q, want %q", got, want)
	}

	got = testRunAPIPath(client, testRunID, "start")
	want = "/api/test-runs/" + testRunID + "/start?workspace_id=ws-abc"
	if got != want {
		t.Errorf("testRunAPIPath(start) = %q, want %q", got, want)
	}

	got = testRunAPIPath(client, testRunID, "capabilities")
	want = "/api/test-runs/" + testRunID + "/capabilities?workspace_id=ws-abc"
	if got != want {
		t.Errorf("testRunAPIPath(capabilities) = %q, want %q", got, want)
	}
}

func TestTestRunCaseAPIPathBuildsCorrectPaths(t *testing.T) {
	client := newTestAPIClient("ws-abc")

	got := testRunCaseAPIPath(client, testRunCaseID, "result")
	want := "/api/test-run-cases/" + testRunCaseID + "/result?workspace_id=ws-abc"
	if got != want {
		t.Errorf("testRunCaseAPIPath(result) = %q, want %q", got, want)
	}

	got = testRunCaseAPIPath(client, testRunCaseID, "defect")
	want = "/api/test-run-cases/" + testRunCaseID + "/defect?workspace_id=ws-abc"
	if got != want {
		t.Errorf("testRunCaseAPIPath(defect) = %q, want %q", got, want)
	}
}

func TestTestPlanAPIPathBuildsCorrectPaths(t *testing.T) {
	client := newTestAPIClient("ws-abc")

	got := testPlanAPIPath(client, testPlanID, "")
	want := "/api/test-plans/" + testPlanID + "?workspace_id=ws-abc"
	if got != want {
		t.Errorf("testPlanAPIPath() = %q, want %q", got, want)
	}
}

// newTestAPIClient creates a minimal APIClient for path-helper tests.
func newTestAPIClient(workspaceID string) *cli.APIClient {
	return &cli.APIClient{WorkspaceID: workspaceID}
}
