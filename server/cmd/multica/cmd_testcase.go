package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var testcaseCmd = &cobra.Command{
	Use:   "testcase",
	Short: "Work with test cases",
}

var testcaseListCmd = &cobra.Command{
	Use:   "list",
	Short: "List test cases in the workspace",
	RunE:  runTestcaseList,
}

var testcaseGetCmd = &cobra.Command{
	Use:   "get <TC-42|id>",
	Short: "Get a test case, including its related repositories",
	Args:  exactArgs(1),
	RunE:  runTestcaseGet,
}

var testcaseModulesCmd = &cobra.Command{
	Use:   "modules",
	Short: "List modules and case counts for a project",
	RunE:  runTestcaseModules,
}

var testcaseCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a test case",
	RunE:  runTestcaseCreate,
}

var testcaseUpdateCmd = &cobra.Command{
	Use:   "update <TC-42|id>",
	Short: "Update a test case",
	Args:  exactArgs(1),
	RunE:  runTestcaseUpdate,
}

var testcaseApproveCmd = &cobra.Command{
	Use:   "approve <TC-42|id>",
	Short: "Approve a draft test case",
	Args:  exactArgs(1),
	RunE:  runTestcaseApprove,
}

var testcaseDeleteCmd = &cobra.Command{
	Use:   "delete <TC-42|id>",
	Short: "Delete a test case",
	Args:  exactArgs(1),
	RunE:  runTestcaseDelete,
}

// testcaseProposeCmd submits an AI generation batch to the server's writeback
// endpoint. The caller must pipe the full {"items":[...]} JSON document on
// stdin; line-delimited formats are not supported.
var testcaseProposeCmd = &cobra.Command{
	Use:   "propose",
	Short: "Submit a generation batch from stdin to a test generation job",
	RunE:  runTestcasePropose,
}

// testcaseProposalCmd is the proposal review command group.
var testcaseProposalCmd = &cobra.Command{
	Use:   "proposal",
	Short: "Manage AI-generated test case proposals",
}

var testcaseProposalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List proposals for a test case",
	RunE:  runTestcaseProposalList,
}

var testcaseProposalAcceptCmd = &cobra.Command{
	Use:   "accept <proposal-id>",
	Short: "Accept an AI-generated proposal, applying its changes",
	Args:  exactArgs(1),
	RunE:  runTestcaseProposalAccept,
}

var testcaseProposalRejectCmd = &cobra.Command{
	Use:   "reject <proposal-id>",
	Short: "Reject an AI-generated proposal without applying it",
	Args:  exactArgs(1),
	RunE:  runTestcaseProposalReject,
}

func init() {
	testcaseProposalCmd.AddCommand(
		testcaseProposalListCmd,
		testcaseProposalAcceptCmd,
		testcaseProposalRejectCmd,
	)

	testcaseCmd.AddCommand(
		testcaseListCmd,
		testcaseGetCmd,
		testcaseModulesCmd,
		testcaseCreateCmd,
		testcaseUpdateCmd,
		testcaseApproveCmd,
		testcaseDeleteCmd,
		testcaseProposeCmd,
		testcaseProposalCmd,
	)

	testcaseListCmd.Flags().String("project", "", "Filter by project id")
	testcaseListCmd.Flags().String("status", "", "Filter by status: draft, active, deprecated")
	testcaseListCmd.Flags().String("module", "", "Filter by module")
	testcaseListCmd.Flags().String("priority", "", "Filter by priority: p0, p1, p2, p3")
	testcaseListCmd.Flags().String("case-type", "", "Filter by case type")
	testcaseListCmd.Flags().String("origin", "", "Filter by origin: ai, human")
	testcaseListCmd.Flags().Bool("digest", false, "Omit steps and test data — a compact index for agent context")
	testcaseListCmd.Flags().String("output", "table", "Output format: table or json")
	testcaseListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	testcaseGetCmd.Flags().String("output", "json", "Output format: table or json")

	testcaseModulesCmd.Flags().String("project", "", "Project id (required)")
	testcaseModulesCmd.Flags().String("output", "table", "Output format: table or json")

	for _, cmd := range []*cobra.Command{testcaseCreateCmd, testcaseUpdateCmd} {
		registerTestcaseWriteFlags(cmd)
	}
	testcaseCreateCmd.Flags().String("project", "", "Project id (required)")
	testcaseUpdateCmd.Flags().String("status", "", "Status: draft, active, deprecated")

	testcaseApproveCmd.Flags().String("output", "json", "Output format: table or json")

	// propose flags
	testcaseProposeCmd.Flags().String("job", "", "Test generation job ID (required)")
	testcaseProposeCmd.Flags().Bool("stdin", false, "Read the proposal JSON document from stdin")

	// proposal list flags
	testcaseProposalListCmd.Flags().String("case", "", "Test case ref: TC-42 or UUID (required)")
	testcaseProposalListCmd.Flags().String("status", "", "Filter by status: pending, accepted, rejected")
	testcaseProposalListCmd.Flags().String("output", "table", "Output format: table or json")
	testcaseProposalListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
}

// testcaseDigestFields are the keys kept by --digest. A generation task needs to
// know which cases already exist without paying for every step body, so the
// digest is title-level metadata only.
var testcaseDigestFields = []string{
	"id", "key", "case_number", "title", "module", "case_type",
	"priority", "status", "origin", "scope", "updated_at",
}

func digestTestCase(testCase map[string]any) map[string]any {
	digest := make(map[string]any, len(testcaseDigestFields))
	for _, field := range testcaseDigestFields {
		if value, ok := testCase[field]; ok {
			digest[field] = value
		}
	}
	return digest
}

// formatTestcaseRepos renders repo bindings as "alias(role)" so the table shows
// at a glance which systems a case spans.
func formatTestcaseRepos(testCase map[string]any) string {
	raw, _ := testCase["repos"].([]any)
	parts := make([]string, 0, len(raw))
	for _, entry := range raw {
		repo, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", strVal(repo, "alias"), strVal(repo, "role")))
	}
	return strings.Join(parts, ", ")
}

func runTestcaseList(cmd *cobra.Command, _ []string) error {
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
		"project":   "project_id",
		"status":    "status",
		"module":    "module",
		"priority":  "priority",
		"case-type": "case_type",
		"origin":    "origin",
	} {
		if value, _ := cmd.Flags().GetString(flag); value != "" {
			params.Set(query, value)
		}
	}

	path := "/api/test-cases"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list test cases: %w", err)
	}
	casesRaw, _ := result["test_cases"].([]any)

	digest, _ := cmd.Flags().GetBool("digest")
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		if !digest {
			return cli.PrintJSON(os.Stdout, casesRaw)
		}
		digested := make([]map[string]any, 0, len(casesRaw))
		for _, raw := range casesRaw {
			testCase, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			digested = append(digested, digestTestCase(testCase))
		}
		return cli.PrintJSON(os.Stdout, digested)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"KEY", "TITLE", "MODULE", "TYPE", "PRIO", "STATUS", "ORIGIN", "REPOS"}
	if fullID {
		headers = append([]string{"ID"}, headers...)
	}
	rows := make([][]string, 0, len(casesRaw))
	for _, raw := range casesRaw {
		testCase, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		row := []string{
			strVal(testCase, "key"),
			strVal(testCase, "title"),
			strVal(testCase, "module"),
			strVal(testCase, "case_type"),
			strVal(testCase, "priority"),
			strVal(testCase, "status"),
			strVal(testCase, "origin"),
			formatTestcaseRepos(testCase),
		}
		if fullID {
			row = append([]string{strVal(testCase, "id")}, row...)
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTestcaseGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	// The ref goes to the server verbatim: TC-<n> keys are resolved there, so
	// the CLI never needs a local prefix index the way short UUIDs would.
	var testCase map[string]any
	if err := client.GetJSON(ctx, testcasePath(client, args[0], ""), &testCase); err != nil {
		return fmt.Errorf("get test case: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, testCase)
	}
	cli.PrintTable(os.Stdout,
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"key", strVal(testCase, "key")},
			{"title", strVal(testCase, "title")},
			{"module", strVal(testCase, "module")},
			{"type", strVal(testCase, "case_type")},
			{"priority", strVal(testCase, "priority")},
			{"status", strVal(testCase, "status")},
			{"origin", strVal(testCase, "origin")},
			{"scope", strVal(testCase, "scope")},
			{"execution_mode", strVal(testCase, "execution_mode")},
			{"repos", formatTestcaseRepos(testCase)},
		})
	return nil
}

func runTestcaseModules(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	projectID, _ := cmd.Flags().GetString("project")
	if projectID == "" {
		return fmt.Errorf("--project is required")
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	params.Set("project_id", projectID)

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/test-cases/modules?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("list test case modules: %w", err)
	}
	modulesRaw, _ := result["modules"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, modulesRaw)
	}
	rows := make([][]string, 0, len(modulesRaw))
	for _, raw := range modulesRaw {
		module, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		count := ""
		if value, ok := module["case_count"].(float64); ok {
			count = fmt.Sprintf("%d", int64(value))
		}
		rows = append(rows, []string{strVal(module, "module"), count})
	}
	cli.PrintTable(os.Stdout, []string{"MODULE", "CASES"}, rows)
	return nil
}

// testcasePath builds a workspace-scoped path for one case ref, with an
// optional trailing action segment.
func testcasePath(client *cli.APIClient, ref, action string) string {
	path := "/api/test-cases/" + url.PathEscape(ref)
	if action != "" {
		path += "/" + action
	}
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

// registerTestcaseWriteFlags is the flag surface create and update share. The
// -stdin / -file variants are the ones resolveTextFlag reads; a long procedure
// or a backslash-heavy precondition goes through them rather than through
// shell quoting.
func registerTestcaseWriteFlags(cmd *cobra.Command) {
	cmd.Flags().String("title", "", "Test case title")
	cmd.Flags().String("module", "", "Module used for grouping")
	cmd.Flags().String("priority", "", "Priority: p0, p1, p2, p3")
	cmd.Flags().String("case-type", "", "Case type, e.g. functional, business_flow, boundary")
	cmd.Flags().String("scope", "", "Scope: single_repo, cross_repo, no_repo")
	cmd.Flags().String("execution-mode", "", "Execution mode: manual, agent, both")
	cmd.Flags().String("preconditions", "", "Preconditions (decodes \\n, \\r, \\t, \\\\; pipe via --preconditions-stdin to preserve literal backslashes)")
	cmd.Flags().Bool("preconditions-stdin", false, "Read the preconditions from stdin")
	cmd.Flags().String("preconditions-file", "", "Read the preconditions from a file in the working directory")
	cmd.Flags().String("expected", "", "Overall expected result (decodes \\n, \\r, \\t, \\\\; pipe via --expected-stdin to preserve literal backslashes)")
	cmd.Flags().Bool("expected-stdin", false, "Read the overall expected result from stdin")
	cmd.Flags().String("expected-file", "", "Read the overall expected result from a file in the working directory")
	cmd.Flags().String("steps", "", `Steps as a JSON array: [{"index":1,"action":"…","expected":"…"}]`)
	cmd.Flags().Bool("steps-stdin", false, "Read the steps JSON array from stdin")
	cmd.Flags().String("steps-file", "", "Read the steps JSON array from a file in the working directory")
	cmd.Flags().Bool("allow-external-file", false, "Allow a --*-file path outside the current working directory")
	cmd.Flags().String("repos", "", `Repository bindings as a JSON array, replacing the case's: [{"project_resource_id":"…","alias":"web","role":"under_test","path_globs":["src/**"]}]`)
	cmd.Flags().String("output", "json", "Output format: table or json")
}

// testcaseJSONFlag reads a JSON-valued flag from its inline, -stdin or -file
// form. Inline JSON is taken verbatim: the backslash decoding text flags get
// would turn a JSON "\n" escape into a raw newline and break the document.
func testcaseJSONFlag(cmd *cobra.Command, name string) (string, bool, error) {
	if cmd.Flags().Changed(name) {
		useStdin, _ := cmd.Flags().GetBool(name + "-stdin")
		filePath, _ := cmd.Flags().GetString(name + "-file")
		if useStdin || filePath != "" {
			return "", false, fmt.Errorf("--%s, --%s-stdin, and --%s-file are mutually exclusive", name, name, name)
		}
		raw, _ := cmd.Flags().GetString(name)
		return raw, true, nil
	}
	return resolveTextFlag(cmd, name)
}

// testcaseBodyFromFlags collects the flags the caller actually set. Every field
// is gated on Changed() so `--module ""` reaches the server as a clear rather
// than being indistinguishable from an omitted flag.
func testcaseBodyFromFlags(cmd *cobra.Command) (map[string]any, error) {
	body := map[string]any{}
	// stdin can be read once.
	stdinFlags := 0
	for _, flag := range []string{"preconditions-stdin", "expected-stdin", "steps-stdin"} {
		if on, _ := cmd.Flags().GetBool(flag); on {
			stdinFlags++
		}
	}
	if stdinFlags > 1 {
		return nil, fmt.Errorf("only one of --preconditions-stdin, --expected-stdin and --steps-stdin can read stdin")
	}
	for flag, field := range map[string]string{
		"title":          "title",
		"module":         "module",
		"priority":       "priority",
		"case-type":      "case_type",
		"scope":          "scope",
		"execution-mode": "execution_mode",
		"status":         "status",
	} {
		if cmd.Flags().Lookup(flag) == nil || !cmd.Flags().Changed(flag) {
			continue
		}
		value, _ := cmd.Flags().GetString(flag)
		body[field] = value
	}
	for flag, field := range map[string]string{
		"preconditions": "preconditions",
		"expected":      "expected_result",
	} {
		value, provided, err := resolveTextFlag(cmd, flag)
		if err != nil {
			return nil, err
		}
		switch {
		case provided:
			body[field] = value
		case cmd.Flags().Changed(flag):
			// An explicitly empty value is a clear, like `--module ""`.
			body[field] = ""
		}
	}
	if raw, provided, err := testcaseJSONFlag(cmd, "steps"); err != nil {
		return nil, err
	} else if provided {
		var steps []map[string]any
		if err := json.Unmarshal([]byte(raw), &steps); err != nil {
			return nil, fmt.Errorf("--steps must be valid JSON: %w", err)
		}
		body["steps"] = steps
	}
	if cmd.Flags().Changed("repos") {
		raw, _ := cmd.Flags().GetString("repos")
		var repos []map[string]any
		if err := json.Unmarshal([]byte(raw), &repos); err != nil {
			return nil, fmt.Errorf("--repos must be a JSON array: %w", err)
		}
		body["repos"] = repos
	}
	return body, nil
}

func runTestcaseCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}
	projectID, _ := cmd.Flags().GetString("project")
	if projectID == "" {
		return fmt.Errorf("--project is required")
	}
	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		return err
	}
	body["project_id"] = projectID
	body["title"] = title

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/test-cases"
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	var created map[string]any
	if err := client.PostJSON(ctx, path, body, &created); err != nil {
		return fmt.Errorf("create test case: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintf(os.Stderr, "Created %s\n", strVal(created, "key"))
		return nil
	}
	return cli.PrintJSON(os.Stdout, created)
}

func runTestcaseUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	body, err := testcaseBodyFromFlags(cmd)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --steps")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var updated map[string]any
	if err := client.PutJSON(ctx, testcasePath(client, args[0], ""), body, &updated); err != nil {
		return fmt.Errorf("update test case: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintf(os.Stderr, "Updated %s\n", strVal(updated, "key"))
		return nil
	}
	return cli.PrintJSON(os.Stdout, updated)
}

func runTestcaseApprove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var approved map[string]any
	if err := client.PostJSON(ctx, testcasePath(client, args[0], "approve"), map[string]any{}, &approved); err != nil {
		return fmt.Errorf("approve test case: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintf(os.Stderr, "Approved %s\n", strVal(approved, "key"))
		return nil
	}
	return cli.PrintJSON(os.Stdout, approved)
}

func runTestcaseDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, testcasePath(client, args[0], "")); err != nil {
		return fmt.Errorf("delete test case: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Test case %s deleted.\n", args[0])
	return nil
}

// buildProposeBody reads one complete JSON document from r, validates it is
// non-empty and well-formed JSON, and returns it as a generic map. The server
// performs full schema validation; this layer only guards against the easy
// mistakes (empty pipe, malformed JSON) before opening a network connection.
func buildProposeBody(r io.Reader) (map[string]any, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read proposal document: %w", err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, fmt.Errorf("empty input: pipe the proposal JSON document to this command via --stdin")
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("proposal document is not valid JSON: %w", err)
	}
	return body, nil
}

// proposalListPath builds a workspace-scoped URL for the proposal list
// endpoint. If status is non-empty it is added as a query parameter.
func proposalListPath(client *cli.APIClient, caseRef, status string) string {
	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if status != "" {
		params.Set("status", status)
	}
	path := "/api/test-cases/" + url.PathEscape(caseRef) + "/proposals"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return path
}

// testGenerationJobPath builds the URL for a test generation job action.
func testGenerationJobPath(client *cli.APIClient, jobID, action string) string {
	path := "/api/test-generation-jobs/" + url.PathEscape(jobID)
	if action != "" {
		path += "/" + action
	}
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

func runTestcasePropose(cmd *cobra.Command, _ []string) error {
	jobID, _ := cmd.Flags().GetString("job")
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("--job is required")
	}

	body, err := buildProposeBody(cmd.InOrStdin())
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, testGenerationJobPath(client, jobID, "propose"), body, &result); err != nil {
		return fmt.Errorf("propose test cases: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runTestcaseProposalList(cmd *cobra.Command, _ []string) error {
	caseRef, _ := cmd.Flags().GetString("case")
	if strings.TrimSpace(caseRef) == "" {
		return fmt.Errorf("--case is required")
	}
	status, _ := cmd.Flags().GetString("status")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, proposalListPath(client, caseRef, status), &result); err != nil {
		return fmt.Errorf("list proposals: %w", err)
	}

	proposalsRaw, _ := result["proposals"].([]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, proposalsRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"PROPOSAL ID", "KIND", "TARGET CASE", "STATUS", "RATIONALE"}
	if fullID {
		headers = append([]string{"FULL ID"}, headers[1:]...)
	}
	rows := make([][]string, 0, len(proposalsRaw))
	for _, raw := range proposalsRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strVal(p, "id")
		if !fullID {
			if len(id) > 8 {
				id = id[:8]
			}
		}
		rows = append(rows, []string{
			id,
			strVal(p, "kind"),
			strVal(p, "target_case_id"),
			strVal(p, "status"),
			strVal(p, "rationale"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// proposalActionPath returns the URL for accept or reject endpoints. These
// endpoints do not carry workspace_id as a query param — the token carries the
// workspace context.
func proposalActionPath(client *cli.APIClient, proposalID, action string) string {
	path := "/api/test-case-proposals/" + url.PathEscape(proposalID) + "/" + action
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

func runTestcaseProposalAccept(cmd *cobra.Command, args []string) error {
	return runProposalDecision(cmd, args[0], "accept")
}

func runTestcaseProposalReject(cmd *cobra.Command, args []string) error {
	return runProposalDecision(cmd, args[0], "reject")
}

func runProposalDecision(cmd *cobra.Command, proposalID, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, proposalActionPath(client, proposalID, action), map[string]any{}, &result); err != nil {
		return fmt.Errorf("%s proposal: %w", action, err)
	}
	return cli.PrintJSON(os.Stdout, result)
}
