package main

// multica test — command group for test run execution.
//
// The commands here are the agent-facing surface for executing a test round.
// They map 1-to-1 onto the backend endpoints documented in
// server/internal/handler/test_run.go and test_capability.go.
//
// Endpoint contract is locked by cmd_testrun_test.go (httptest-level tests).
// The skill that teaches an agent to use these commands is
// server/internal/service/builtin_skills/multica-running-tests/SKILL.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Command group: multica test
// ---------------------------------------------------------------------------

// testRunGroupCmd is the top-level "multica test" command. Named to avoid
// colliding with the testCmd() helper function declared in cmd_auth_test.go.
var testRunGroupCmd = &cobra.Command{
	Use:   "test",
	Short: "Work with test runs, results, evidence and capabilities",
}

// ---------------------------------------------------------------------------
// multica test run
// ---------------------------------------------------------------------------

var testRunSubCmd = &cobra.Command{
	Use:   "run",
	Short: "Work with test runs",
}

var testRunGetCmd = &cobra.Command{
	Use:   "get <run-id>",
	Short: "Get a test run with result counts and execution status",
	Args:  exactArgs(1),
	RunE:  runTestRunGet,
}

var testRunStartCmd = &cobra.Command{
	Use:   "start <run-id>",
	Short: "Transition a pending run to running",
	Args:  exactArgs(1),
	RunE:  runTestRunStart,
}

// ---------------------------------------------------------------------------
// multica test result
// ---------------------------------------------------------------------------

var testResultCmd = &cobra.Command{
	Use:   "result",
	Short: "Work with test case results",
}

var testResultSetCmd = &cobra.Command{
	Use:   "set <run-case-id>",
	Short: "Record the outcome of one executed test case",
	Args:  exactArgs(1),
	RunE:  runTestResultSet,
}

// ---------------------------------------------------------------------------
// multica test evidence
// ---------------------------------------------------------------------------

var testEvidenceCmd = &cobra.Command{
	Use:   "evidence",
	Short: "Work with test evidence",
}

var testEvidenceAddCmd = &cobra.Command{
	Use:   "add <run-case-id>",
	Short: "Upload a local file as evidence for a test run case",
	Args:  exactArgs(1),
	RunE:  runTestEvidenceAdd,
}

// ---------------------------------------------------------------------------
// multica test defect
// ---------------------------------------------------------------------------

var testDefectCmd = &cobra.Command{
	Use:   "defect",
	Short: "Work with test defects",
}

var testDefectOpenCmd = &cobra.Command{
	Use:   "open <run-case-id>",
	Short: "Create a defect issue for a failed or blocked test run case",
	Args:  exactArgs(1),
	RunE:  runTestDefectOpen,
}

// ---------------------------------------------------------------------------
// multica test capability
// ---------------------------------------------------------------------------

var testCapCmd = &cobra.Command{
	Use:   "capability",
	Short: "Work with test execution capabilities",
}

var testCapListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the capability binding frozen on a test run",
	RunE:  runTestCapList,
}

// ---------------------------------------------------------------------------
// multica test plan
// ---------------------------------------------------------------------------

var testPlanSubCmd = &cobra.Command{
	Use:   "plan",
	Short: "Work with test plans",
}

var testPlanListCmd = &cobra.Command{
	Use:   "list",
	Short: "List test plans in the workspace",
	RunE:  runTestPlanList,
}

var testPlanGetCmd = &cobra.Command{
	Use:   "get <plan-id>",
	Short: "Get a test plan by ID",
	Args:  exactArgs(1),
	RunE:  runTestPlanGet,
}

// ---------------------------------------------------------------------------
// Flag registration
// ---------------------------------------------------------------------------

func init() {
	// Build the command tree.
	testRunSubCmd.AddCommand(testRunGetCmd, testRunStartCmd)
	testResultCmd.AddCommand(testResultSetCmd)
	testEvidenceCmd.AddCommand(testEvidenceAddCmd)
	testDefectCmd.AddCommand(testDefectOpenCmd)
	testCapCmd.AddCommand(testCapListCmd)
	testPlanSubCmd.AddCommand(testPlanListCmd, testPlanGetCmd)

	testRunGroupCmd.AddCommand(
		testRunSubCmd,
		testResultCmd,
		testEvidenceCmd,
		testDefectCmd,
		testCapCmd,
		testPlanSubCmd,
	)

	// test run get
	testRunGetCmd.Flags().String("output", "json", "Output format: json or table")

	// test run start
	testRunStartCmd.Flags().String("output", "json", "Output format: json or table")

	// test result set
	testResultSetCmd.Flags().String("result", "", "Result: passed, failed, blocked, or skipped (required)")
	testResultSetCmd.Flags().String("note", "", "Optional note to record with the result")
	testResultSetCmd.Flags().String("step-results", "", `Step results as a JSON array of {"index","result","actual","note"}`)
	testResultSetCmd.Flags().StringArray("step", nil, "One step's result as <index>=<passed|failed|blocked|skipped|not_run>[:<what you observed>]; repeat per step")

	// test evidence add
	testEvidenceAddCmd.Flags().String("file", "", "Local file path to upload (required)")
	testEvidenceAddCmd.Flags().String("kind", "", "Evidence kind, e.g. screenshot or log")

	// test defect open
	testDefectOpenCmd.Flags().String("title", "", "Override the defect issue title (optional)")
	testDefectOpenCmd.Flags().String("note", "", "Additional note appended to the defect description")

	// test capability list
	testCapListCmd.Flags().String("run", "", "Test run ID (required)")
	testCapListCmd.Flags().String("output", "json", "Output format: json or table")

	// test plan list
	testPlanListCmd.Flags().String("project", "", "Filter by project ID")
	testPlanListCmd.Flags().String("status", "", "Filter by status: draft, active, archived")
	testPlanListCmd.Flags().String("output", "table", "Output format: table or json")

	// test plan get
	testPlanGetCmd.Flags().String("output", "json", "Output format: json or table")
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// testRunAPIPath builds a workspace-scoped path for a test run resource.
func testRunAPIPath(client *cli.APIClient, runID, action string) string {
	path := "/api/test-runs/" + url.PathEscape(runID)
	if action != "" {
		path += "/" + action
	}
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

// testRunCaseAPIPath builds a workspace-scoped path for a test run case resource.
func testRunCaseAPIPath(client *cli.APIClient, runCaseID, action string) string {
	path := "/api/test-run-cases/" + url.PathEscape(runCaseID)
	if action != "" {
		path += "/" + action
	}
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

// testPlanAPIPath builds a workspace-scoped path for a test plan resource.
func testPlanAPIPath(client *cli.APIClient, planID, action string) string {
	path := "/api/test-plans/" + url.PathEscape(planID)
	if action != "" {
		path += "/" + action
	}
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

// ---------------------------------------------------------------------------
// Handler: multica test run get
// ---------------------------------------------------------------------------

func runTestRunGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, testRunAPIPath(client, args[0], ""), &result); err != nil {
		return fmt.Errorf("get test run: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"FIELD", "VALUE"}, [][]string{
			{"id", strVal(result, "id")},
			{"title", strVal(result, "title")},
			{"status", strVal(result, "status")},
			{"executor_type", strVal(result, "executor_type")},
			{"environment", strVal(result, "environment")},
			{"build_ref", strVal(result, "build_ref")},
		})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Handler: multica test run start
// ---------------------------------------------------------------------------

func runTestRunStart(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, testRunAPIPath(client, args[0], "start"), map[string]any{}, &result); err != nil {
		return fmt.Errorf("start test run: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintf(os.Stderr, "Run %s started.\n", strVal(result, "id"))
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Handler: multica test result set
// ---------------------------------------------------------------------------

func runTestResultSet(cmd *cobra.Command, args []string) error {
	result, _ := cmd.Flags().GetString("result")
	if strings.TrimSpace(result) == "" {
		return fmt.Errorf("--result is required (passed, failed, blocked, or skipped)")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"result": result,
	}
	if note, _ := cmd.Flags().GetString("note"); note != "" {
		body["notes"] = note
	}
	raw, _ := cmd.Flags().GetString("step-results")
	steps, _ := cmd.Flags().GetStringArray("step")
	if raw != "" && len(steps) > 0 {
		return fmt.Errorf("use either --step or --step-results, not both")
	}
	if raw != "" {
		var stepResults []any
		if err := json.Unmarshal([]byte(raw), &stepResults); err != nil {
			return fmt.Errorf("--step-results must be valid JSON: %w", err)
		}
		body["step_results"] = stepResults
	}
	if len(steps) > 0 {
		stepResults, err := parseStepFlags(steps)
		if err != nil {
			return err
		}
		body["step_results"] = stepResults
	}

	var out map[string]any
	if err := client.PutJSON(ctx, testRunCaseAPIPath(client, args[0], "result"), body, &out); err != nil {
		return fmt.Errorf("set test result: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

// parseStepFlags reads repeated --step values of the form
// <index>=<result>[:<actual>]. The server checks the verdicts against the
// case's frozen steps; this only gets the shape right.
func parseStepFlags(values []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		indexText, rest, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("--step %q must look like <index>=<result>[:<actual>]", value)
		}
		index, err := strconv.Atoi(strings.TrimSpace(indexText))
		if err != nil || index < 1 {
			return nil, fmt.Errorf("--step %q: the index must be the step's number, starting at 1", value)
		}
		result, actual, _ := strings.Cut(rest, ":")
		step := map[string]any{"index": index, "result": strings.TrimSpace(result)}
		if actual = strings.TrimSpace(actual); actual != "" {
			step["actual"] = actual
		}
		out = append(out, step)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Handler: multica test evidence add
// ---------------------------------------------------------------------------

// evidenceClientCapabilities is the X-Client-Capabilities value sent with
// evidence uploads. Mirrors the unexported constant in cli/client.go.
const evidenceClientCapabilities = "stable_attachment_urls"

func runTestEvidenceAdd(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("--file is required")
	}
	kind, _ := cmd.Flags().GetString("kind")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	att, err := uploadTestRunCaseEvidence(ctx, client, data, filePath, args[0], kind)
	if err != nil {
		return fmt.Errorf("upload evidence: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Uploaded:", filepath.Base(filePath))
	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":           att.ID,
		"kind":         kind,
		"markdown_url": att.MarkdownURL,
	})
}

// uploadTestRunCaseEvidence uploads a file as evidence for a test run case.
// It uses a raw multipart POST so the test_run_case_id and kind form fields
// can be set alongside the file. The auth headers are replicated manually from
// the exported APIClient fields, mirroring cli.APIClient.setHeaders.
func uploadTestRunCaseEvidence(
	ctx context.Context,
	client *cli.APIClient,
	fileData []byte,
	filename, runCaseID, kind string,
) (cli.AttachmentResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return cli.AttachmentResponse{}, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return cli.AttachmentResponse{}, fmt.Errorf("write file data: %w", err)
	}
	if err := writer.WriteField("test_run_case_id", runCaseID); err != nil {
		return cli.AttachmentResponse{}, fmt.Errorf("write test_run_case_id field: %w", err)
	}
	if kind != "" {
		if err := writer.WriteField("kind", kind); err != nil {
			return cli.AttachmentResponse{}, fmt.Errorf("write kind field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return cli.AttachmentResponse{}, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.BaseURL+"/api/upload-file", &body)
	if err != nil {
		return cli.AttachmentResponse{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Replicate cli.APIClient.setHeaders using the exported struct fields.
	req.Header.Set("X-Client-Capabilities", evidenceClientCapabilities)
	if client.Token != "" {
		req.Header.Set("Authorization", "Bearer "+client.Token)
	}
	if client.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", client.WorkspaceID)
	}
	if client.AgentID != "" {
		req.Header.Set("X-Agent-ID", client.AgentID)
	}
	if client.TaskID != "" {
		req.Header.Set("X-Task-ID", client.TaskID)
	}
	if cli.ClientPlatform != "" {
		req.Header.Set("X-Client-Platform", cli.ClientPlatform)
	}
	if cli.ClientVersion != "" {
		req.Header.Set("X-Client-Version", cli.ClientVersion)
	}
	if cli.ClientOS != "" {
		req.Header.Set("X-Client-OS", cli.ClientOS)
	}

	// Extend the HTTP client timeout when the context deadline allows it.
	httpClient := client.HTTPClient
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > httpClient.Timeout {
			clientCopy := *httpClient
			clientCopy.Timeout = remaining
			httpClient = &clientCopy
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return cli.AttachmentResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return cli.AttachmentResponse{}, fmt.Errorf("POST /api/upload-file returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result cli.AttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return cli.AttachmentResponse{}, fmt.Errorf("decode upload response: %w", err)
	}
	if result.ID == "" {
		return cli.AttachmentResponse{}, fmt.Errorf("upload response missing attachment id")
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Handler: multica test defect open
// ---------------------------------------------------------------------------

func runTestDefectOpen(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	if title, _ := cmd.Flags().GetString("title"); title != "" {
		body["title"] = title
	}
	if note, _ := cmd.Flags().GetString("note"); note != "" {
		body["note"] = note
	}

	var result map[string]any
	if err := client.PostJSON(ctx, testRunCaseAPIPath(client, args[0], "defect"), body, &result); err != nil {
		return fmt.Errorf("open defect: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Handler: multica test capability list
// ---------------------------------------------------------------------------

func runTestCapList(cmd *cobra.Command, args []string) error {
	runID, _ := cmd.Flags().GetString("run")
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("--run is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, testRunAPIPath(client, runID, "capabilities"), &result); err != nil {
		return fmt.Errorf("list capabilities: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		// The capability_binding is an opaque JSON object; print its keys.
		binding, _ := result["capability_binding"].(map[string]any)
		resolved, _ := binding["resolved"].(map[string]any)
		rows := make([][]string, 0, len(resolved))
		for kind, key := range resolved {
			rows = append(rows, []string{kind, fmt.Sprintf("%v", key)})
		}
		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, "No capabilities bound to this run.")
		} else {
			cli.PrintTable(os.Stdout, []string{"KIND", "CAPABILITY KEY"}, rows)
		}
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Handler: multica test plan list
// ---------------------------------------------------------------------------

func runTestPlanList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	for flag, query := range map[string]string{
		"project": "project_id",
		"status":  "status",
	} {
		if val, _ := cmd.Flags().GetString(flag); val != "" {
			params.Set(query, val)
		}
	}

	path := "/api/test-plans"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list test plans: %w", err)
	}

	plansRaw, _ := result["test_plans"].([]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, plansRaw)
	}

	rows := make([][]string, 0, len(plansRaw))
	for _, raw := range plansRaw {
		plan, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			strVal(plan, "id")[:min8(strVal(plan, "id"))],
			strVal(plan, "title"),
			strVal(plan, "status"),
			strVal(plan, "project_id")[:min8(strVal(plan, "project_id"))],
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "STATUS", "PROJECT"}, rows)
	return nil
}

// min8 returns the minimum of 8 and len(s), used to produce a short display ID.
func min8(s string) int {
	if len(s) < 8 {
		return len(s)
	}
	return 8
}

// ---------------------------------------------------------------------------
// Handler: multica test plan get
// ---------------------------------------------------------------------------

func runTestPlanGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, testPlanAPIPath(client, args[0], ""), &result); err != nil {
		return fmt.Errorf("get test plan: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"FIELD", "VALUE"}, [][]string{
			{"id", strVal(result, "id")},
			{"title", strVal(result, "title")},
			{"status", strVal(result, "status")},
			{"project_id", strVal(result, "project_id")},
			{"description", strVal(result, "description")},
		})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}
