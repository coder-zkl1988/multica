package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// newTestcaseWriteTestCmd mirrors the flag surface shared by
// testcaseCreateCmd / testcaseUpdateCmd so the body builder can be exercised
// without a server.
func newTestcaseWriteTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "write"}
	registerTestcaseWriteFlags(c)
	c.Flags().String("status", "", "")
	return c
}

// The skill pipes long procedures and rebinds repositories through these.
func TestTestcaseBodyReadsStepsFromAFileAndRepos(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "steps.json"), []byte(`[{"action":"Pay","expected":"Line one\nline two"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newTestcaseWriteTestCmd()
	cmd.Flags().Set("steps-file", "steps.json")
	cmd.Flags().Set("repos", `[{"project_resource_id":"r1","alias":"web","role":"under_test"}]`)
	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}
	steps, _ := body["steps"].([]map[string]any)
	if len(steps) != 1 || steps[0]["expected"] != "Line one\nline two" {
		t.Fatalf("steps = %#v, want the file's JSON with its escape decoded once", body["steps"])
	}
	repos, _ := body["repos"].([]map[string]any)
	if len(repos) != 1 || repos[0]["alias"] != "web" {
		t.Fatalf("repos = %#v", body["repos"])
	}

	both := newTestcaseWriteTestCmd()
	both.Flags().Set("steps", "[]")
	both.Flags().Set("steps-file", "steps.json")
	if _, err := testcaseBodyFromFlags(both); err == nil {
		t.Fatal("inline and file steps together were accepted")
	}
	twoStdin := newTestcaseWriteTestCmd()
	twoStdin.Flags().Set("steps-stdin", "true")
	twoStdin.Flags().Set("expected-stdin", "true")
	if _, err := testcaseBodyFromFlags(twoStdin); err == nil {
		t.Fatal("two flags reading stdin were accepted")
	}
}

func TestTestcaseBodyOnlyIncludesChangedFlags(t *testing.T) {
	cmd := newTestcaseWriteTestCmd()
	if err := cmd.Flags().Set("module", "订单"); err != nil {
		t.Fatalf("set module: %v", err)
	}

	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("body = %v, want only the module field", body)
	}
	if body["module"] != "订单" {
		t.Fatalf("module = %v, want 订单", body["module"])
	}
}

func TestTestcaseBodyTreatsEmptyStringAsAClear(t *testing.T) {
	cmd := newTestcaseWriteTestCmd()
	if err := cmd.Flags().Set("module", ""); err != nil {
		t.Fatalf("set module: %v", err)
	}

	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}
	value, present := body["module"]
	if !present {
		t.Fatal("an explicitly empty --module must reach the server as a clear")
	}
	if value != "" {
		t.Fatalf("module = %v, want the empty string", value)
	}
}

func TestTestcaseBodyRejectsMalformedSteps(t *testing.T) {
	cmd := newTestcaseWriteTestCmd()
	if err := cmd.Flags().Set("steps", "not-json"); err != nil {
		t.Fatalf("set steps: %v", err)
	}

	_, err := testcaseBodyFromFlags(cmd)
	if err == nil {
		t.Fatal("malformed --steps should fail client-side")
	}
	if !strings.Contains(err.Error(), "--steps must be valid JSON") {
		t.Fatalf("error should name the flag, got: %v", err)
	}
}

func TestTestcaseBodyParsesSteps(t *testing.T) {
	cmd := newTestcaseWriteTestCmd()
	if err := cmd.Flags().Set("steps", `[{"index":1,"action":"点击下单","expected":"跳转支付页"}]`); err != nil {
		t.Fatalf("set steps: %v", err)
	}

	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}
	steps, ok := body["steps"].([]map[string]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("steps = %#v, want one parsed step", body["steps"])
	}
	if steps[0]["action"] != "点击下单" {
		t.Fatalf("action = %v", steps[0]["action"])
	}
}

func TestTestcasePathSendsTheRefVerbatim(t *testing.T) {
	client := &cli.APIClient{WorkspaceID: "ws-1"}

	got := testcasePath(client, "TC-42", "")
	if got != "/api/test-cases/TC-42?workspace_id=ws-1" {
		t.Fatalf("path = %q", got)
	}

	got = testcasePath(client, "TC-42", "approve")
	if got != "/api/test-cases/TC-42/approve?workspace_id=ws-1" {
		t.Fatalf("approve path = %q", got)
	}
}

func TestTestcasePathEscapesTheRef(t *testing.T) {
	client := &cli.APIClient{WorkspaceID: "ws-1"}
	got := testcasePath(client, "TC 42/../secrets", "")
	if strings.Contains(got, "..") && !strings.Contains(got, "%2F") {
		t.Fatalf("ref must be escaped, got %q", got)
	}
}

func TestDigestTestCaseOmitsStepBodies(t *testing.T) {
	full := map[string]any{
		"id":          "case-1",
		"key":         "TC-1",
		"title":       "下单成功",
		"module":      "订单",
		"status":      "active",
		"steps":       []any{map[string]any{"action": "点击下单"}},
		"test_data":   map[string]any{"amount": 129},
		"source_refs": map[string]any{"issue_ids": []any{"MUL-1"}},
	}

	digest := digestTestCase(full)

	if _, present := digest["steps"]; present {
		t.Fatal("digest must not carry step bodies")
	}
	if _, present := digest["test_data"]; present {
		t.Fatal("digest must not carry test data")
	}
	if digest["key"] != "TC-1" || digest["title"] != "下单成功" || digest["module"] != "订单" {
		t.Fatalf("digest lost identifying fields: %v", digest)
	}
}

func TestFormatTestcaseRepos(t *testing.T) {
	summary := formatTestcaseRepos(map[string]any{
		"repos": []any{
			map[string]any{"alias": "admin-web", "role": "driver"},
			map[string]any{"alias": "mobile-app", "role": "verifier"},
		},
	})
	if summary != "admin-web(driver), mobile-app(verifier)" {
		t.Fatalf("summary = %q", summary)
	}

	if got := formatTestcaseRepos(map[string]any{}); got != "" {
		t.Fatalf("missing repos should render empty, got %q", got)
	}
}

// TestProposeParsesValidJSON tests that buildProposeBody correctly unmarshals
// a well-formed {"items":[...]} document from a reader.
func TestProposeParsesValidJSON(t *testing.T) {
	input := `{"items":[{"kind":"new","case":{"title":"下单成功","steps":[]}},{"kind":"obsolete","target":"TC-7","rationale":"入口已下线"}]}`
	body, err := buildProposeBody(strings.NewReader(input))
	if err != nil {
		t.Fatalf("valid JSON should parse without error: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 items, got %#v", body["items"])
	}
}

// TestProposeRejectsEmptyInput tests that an empty reader produces an error,
// not a silent empty POST. The server also rejects empty items, but catching it
// client-side avoids a round-trip.
func TestProposeRejectsEmptyInput(t *testing.T) {
	_, err := buildProposeBody(strings.NewReader(""))
	if err == nil {
		t.Fatal("empty stdin should be rejected before POSTing")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error should mention empty input, got: %v", err)
	}
}

// TestProposeRejectsWhitespaceOnlyInput ensures whitespace-only content is
// treated the same as empty: do not POST a blank body to the server.
func TestProposeRejectsWhitespaceOnlyInput(t *testing.T) {
	_, err := buildProposeBody(strings.NewReader("   \n\t  "))
	if err == nil {
		t.Fatal("whitespace-only stdin should be rejected before POSTing")
	}
}

// TestProposeRejectsMalformedJSON tests that malformed JSON is caught
// client-side with a clear error rather than let the server parse it.
func TestProposeRejectsMalformedJSON(t *testing.T) {
	_, err := buildProposeBody(strings.NewReader("{not valid json"))
	if err == nil {
		t.Fatal("malformed JSON should fail")
	}
}

// TestProposeCmdRegistered verifies the propose sub-command is wired into the
// testcase command group with the expected flags.
func TestProposeCmdRegistered(t *testing.T) {
	var found *cobra.Command
	for _, sub := range testcaseCmd.Commands() {
		if sub.Name() == "propose" {
			found = sub
			break
		}
	}
	if found == nil {
		t.Fatal("testcase propose command is not registered")
	}
	if found.Flags().Lookup("job") == nil {
		t.Error("propose command must have --job flag")
	}
	if found.Flags().Lookup("stdin") == nil {
		t.Error("propose command must have --stdin flag")
	}
}

// TestProposalCmdGroupRegistered verifies the proposal sub-command group and
// its list/accept/reject subcommands exist.
func TestProposalCmdGroupRegistered(t *testing.T) {
	var proposalCmd *cobra.Command
	for _, sub := range testcaseCmd.Commands() {
		if sub.Name() == "proposal" {
			proposalCmd = sub
			break
		}
	}
	if proposalCmd == nil {
		t.Fatal("testcase proposal command group is not registered")
	}

	names := map[string]bool{}
	for _, sub := range proposalCmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"list", "accept", "reject"} {
		if !names[want] {
			t.Errorf("proposal sub-command %q is not registered", want)
		}
	}
}

// TestProposalListCmdFlags verifies proposal list carries the expected flags.
func TestProposalListCmdFlags(t *testing.T) {
	var proposalCmd *cobra.Command
	for _, sub := range testcaseCmd.Commands() {
		if sub.Name() == "proposal" {
			proposalCmd = sub
			break
		}
	}
	if proposalCmd == nil {
		t.Skip("proposal command not registered")
	}
	var listCmd *cobra.Command
	for _, sub := range proposalCmd.Commands() {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	if listCmd == nil {
		t.Fatal("proposal list command not found")
	}
	for _, flag := range []string{"case", "status", "output"} {
		if listCmd.Flags().Lookup(flag) == nil {
			t.Errorf("proposal list must have --%s flag", flag)
		}
	}
}

// TestProposeBuildBodyReadsFromReader tests that buildProposeBody uses the
// supplied reader, not os.Stdin — critical for testability.
func TestProposeBuildBodyReadsFromReader(t *testing.T) {
	payload := `{"items":[{"kind":"new","case":{"title":"Test","steps":[]}}]}`
	body, err := buildProposeBody(bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

// TestProposalListPathBuilding checks proposalListPath generates a correctly
// scoped URL for the list endpoint.
func TestProposalListPathBuilding(t *testing.T) {
	client := &cli.APIClient{WorkspaceID: "ws-abc"}
	path := proposalListPath(client, "TC-42", "pending")
	if !strings.Contains(path, "/api/test-cases/TC-42/proposals") {
		t.Errorf("path %q missing proposals segment", path)
	}
	if !strings.Contains(path, "workspace_id=ws-abc") {
		t.Errorf("path %q missing workspace_id param", path)
	}
	if !strings.Contains(path, "status=pending") {
		t.Errorf("path %q missing status filter", path)
	}
}

// TestProposalListPathNoStatusFilter checks that when status is empty the
// query parameter is omitted entirely, not sent as status=.
func TestProposalListPathNoStatusFilter(t *testing.T) {
	client := &cli.APIClient{WorkspaceID: "ws-abc"}
	path := proposalListPath(client, "TC-42", "")
	if strings.Contains(path, "status=") {
		t.Errorf("path %q should not carry an empty status param", path)
	}
}

// newProposeTestCmd builds the minimal flag surface runTestcasePropose reads,
// pointed at a stub server.
func newProposeTestCmd(serverURL, jobID string, stdin io.Reader) *cobra.Command {
	c := &cobra.Command{Use: "propose"}
	c.Flags().String("job", "", "")
	c.Flags().String("server-url", "", "")
	c.Flags().String("workspace-id", "", "")
	c.Flags().String("output", "json", "")
	_ = c.Flags().Set("job", jobID)
	_ = c.Flags().Set("server-url", serverURL)
	_ = c.Flags().Set("workspace-id", "ws-1")
	c.SetIn(stdin)
	return c
}

// The helper-level tests cover JSON parsing; this one covers the wiring, which
// is what actually breaks: the body has to reach the job's propose endpoint.
func TestRunTestcaseProposePostsBodyToJobEndpoint(t *testing.T) {
	const jobID = "job-42"
	var gotPath, gotMethod string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode propose body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"stats": map[string]int{"new": 1}})
	}))
	defer srv.Close()

	payload := `{"items":[{"kind":"new","case":{"title":"下单成功"}}]}`
	cmd := newProposeTestCmd(srv.URL, jobID, bytes.NewBufferString(payload))
	if err := runTestcasePropose(cmd, nil); err != nil {
		t.Fatalf("runTestcasePropose: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/api/test-generation-jobs/" + jobID + "/propose"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	items, _ := gotBody["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want the stdin payload forwarded verbatim", gotBody["items"])
	}
}

func TestRunTestcaseProposeRequiresJob(t *testing.T) {
	cmd := newProposeTestCmd("http://127.0.0.1:1", "", bytes.NewBufferString(`{"items":[]}`))
	if err := runTestcasePropose(cmd, nil); err == nil {
		t.Fatal("expected an error when --job is missing")
	}
}

func TestRunTestcaseProposalAcceptHitsAcceptEndpoint(t *testing.T) {
	const proposalID = "prop-7"
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewEncoder(w).Encode(map[string]any{"proposal": map[string]any{"id": proposalID}})
	}))
	defer srv.Close()

	c := &cobra.Command{Use: "accept"}
	c.Flags().String("server-url", "", "")
	c.Flags().String("workspace-id", "", "")
	c.Flags().String("output", "json", "")
	_ = c.Flags().Set("server-url", srv.URL)
	_ = c.Flags().Set("workspace-id", "ws-1")

	if err := runTestcaseProposalAccept(c, []string{proposalID}); err != nil {
		t.Fatalf("runTestcaseProposalAccept: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/api/test-case-proposals/" + proposalID + "/accept"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
}

// `--preconditions ""` clears the field, as `--module ""` does.
func TestTestcaseBodyClearsAnExplicitlyEmptyTextFlag(t *testing.T) {
	cmd := newTestcaseWriteTestCmd()
	cmd.Flags().Set("preconditions", "")
	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}
	if value, ok := body["preconditions"]; !ok || value != "" {
		t.Fatalf("body = %#v, want preconditions cleared", body)
	}
}
