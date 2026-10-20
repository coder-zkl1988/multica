package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Test points are the conditions a requirement is accepted on. A generation
// run splits each issue into points before writing cases, and every case says
// which points it verifies; an issue is verified only when each point is
// covered by an active case that passes.

var testcasePointCmd = &cobra.Command{
	Use:   "point",
	Short: "Work with an issue's test points (the conditions it is accepted on)",
}

var testcasePointListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an issue's test points with the cases covering each and where it stands",
	Args:  exactArgs(0),
	RunE:  runTestcasePointList,
}

var testcasePointAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a test point to an issue",
	Args:  exactArgs(0),
	RunE:  runTestcasePointAdd,
}

var testcasePointUpdateCmd = &cobra.Command{
	Use:   "update <MUL-12/TP-3|point-id>",
	Short: "Change a test point, or drop it with --status dropped",
	Args:  exactArgs(1),
	RunE:  runTestcasePointUpdate,
}

var testcaseCoverCmd = &cobra.Command{
	Use:   "cover <TC-42|id> <MUL-12/TP-3|point-id>...",
	Short: "Declare which test points a case verifies",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runTestcaseCover,
}

func init() {
	testcasePointCmd.AddCommand(testcasePointListCmd, testcasePointAddCmd, testcasePointUpdateCmd)
	testcaseCmd.AddCommand(testcasePointCmd, testcaseCoverCmd)

	testcasePointListCmd.Flags().String("issue", "", "Issue identifier or id (required)")
	testcasePointListCmd.Flags().String("output", "table", "Output format: table or json")

	testcasePointAddCmd.Flags().String("issue", "", "Issue identifier or id (required)")
	testcasePointAddCmd.Flags().String("title", "", "One condition that can be judged right or wrong on its own (required)")
	testcasePointAddCmd.Flags().String("category", "positive", "positive, negative, boundary, permission, state, data, or non_functional")
	testcasePointAddCmd.Flags().String("description", "", "Optional detail: the rule, the values, the roles involved")
	testcasePointAddCmd.Flags().String("output", "json", "Output format: table or json")

	testcasePointUpdateCmd.Flags().String("title", "", "New title")
	testcasePointUpdateCmd.Flags().String("category", "", "New category")
	testcasePointUpdateCmd.Flags().String("description", "", "New description")
	testcasePointUpdateCmd.Flags().String("status", "", "active or dropped")
	testcasePointUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	testcaseCoverCmd.Flags().String("output", "json", "Output format: table or json")
}

func issueTestPointsPath(client *cli.APIClient, issueRef string) string {
	path := "/api/issues/" + url.PathEscape(issueRef) + "/test-points"
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

func testPointPath(client *cli.APIClient, pointID string) string {
	path := "/api/test-points/" + url.PathEscape(pointID)
	if client.WorkspaceID != "" {
		path += "?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	}
	return path
}

func runTestcasePointList(cmd *cobra.Command, _ []string) error {
	issueRef, _ := cmd.Flags().GetString("issue")
	if strings.TrimSpace(issueRef) == "" {
		return fmt.Errorf("--issue is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, issueTestPointsPath(client, issueRef), &result); err != nil {
		return fmt.Errorf("list test points: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	points, _ := result["test_points"].([]any)
	rows := make([][]string, 0, len(points))
	for _, raw := range points {
		point, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cases, _ := point["cases"].([]any)
		keys := make([]string, 0, len(cases))
		for _, rawCase := range cases {
			if c, ok := rawCase.(map[string]any); ok {
				keys = append(keys, strVal(c, "case_key"))
			}
		}
		rows = append(rows, []string{
			strVal(point, "key"),
			strVal(point, "title"),
			strVal(point, "category"),
			strVal(point, "state"),
			strings.Join(keys, ", "),
			strVal(point, "origin"),
		})
	}
	cli.PrintTable(os.Stdout, []string{"KEY", "TITLE", "CATEGORY", "STATE", "CASES", "ORIGIN"}, rows)
	if summary, ok := result["summary"].(map[string]any); ok {
		fmt.Fprintf(os.Stderr, "%v of %v active points covered, %v passed\n", summary["covered"], summary["active"], summary["passed"])
	}
	return nil
}

func runTestcasePointAdd(cmd *cobra.Command, _ []string) error {
	issueRef, _ := cmd.Flags().GetString("issue")
	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(issueRef) == "" || strings.TrimSpace(title) == "" {
		return fmt.Errorf("--issue and --title are required")
	}
	category, _ := cmd.Flags().GetString("category")
	description, _ := cmd.Flags().GetString("description")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var point map[string]any
	if err := client.PostJSON(ctx, issueTestPointsPath(client, issueRef), map[string]any{
		"title": title, "category": category, "description": description,
	}, &point); err != nil {
		return fmt.Errorf("add test point: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		fmt.Fprintf(os.Stderr, "Added %s/%s\n", issueRef, strVal(point, "key"))
		return nil
	}
	return cli.PrintJSON(os.Stdout, point)
}

// resolveTestPointID turns "MUL-12/TP-3" into the point's id by reading the
// issue's points; a ref without a slash is taken to be an id already. The
// server also accepts the slash form in bodies, but not inside a URL path.
func resolveTestPointID(ctx context.Context, client *cli.APIClient, ref string) (string, error) {
	issueRef, pointRef, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok {
		return ref, nil
	}
	number, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(pointRef)), "TP-"))
	if err != nil {
		return "", fmt.Errorf("%q is not <issue>/TP-<n>", ref)
	}
	var result struct {
		TestPoints []struct {
			ID     string `json:"id"`
			Number int    `json:"number"`
		} `json:"test_points"`
	}
	if err := client.GetJSON(ctx, issueTestPointsPath(client, issueRef), &result); err != nil {
		return "", fmt.Errorf("read %s's test points: %w", issueRef, err)
	}
	for _, point := range result.TestPoints {
		if point.Number == number {
			return point.ID, nil
		}
	}
	return "", fmt.Errorf("%s has no TP-%d", issueRef, number)
}

func runTestcasePointUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	for _, flag := range []string{"title", "category", "description", "status"} {
		if cmd.Flags().Changed(flag) {
			value, _ := cmd.Flags().GetString(flag)
			body[flag] = value
		}
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to change: pass --title, --category, --description or --status")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	pointID, err := resolveTestPointID(ctx, client, args[0])
	if err != nil {
		return err
	}
	var point map[string]any
	if err := client.PatchJSON(ctx, testPointPath(client, pointID), body, &point); err != nil {
		return fmt.Errorf("update test point: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		fmt.Fprintf(os.Stderr, "Updated %s (%s)\n", args[0], strVal(point, "status"))
		return nil
	}
	return cli.PrintJSON(os.Stdout, point)
}

func runTestcaseCover(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, testcasePath(client, args[0], "test-points"), map[string]any{"test_points": args[1:]}, &result); err != nil {
		return fmt.Errorf("declare covered test points: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		fmt.Fprintf(os.Stderr, "%s now verifies %v test point(s)\n", args[0], result["total"])
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}
