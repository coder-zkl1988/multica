package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with repositories",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspace repositories",
	Long:  "Lists the repository registry for the current workspace. These are workspace-level repos, separate from project resources.",
	Args:  cobra.NoArgs,
	RunE:  runRepoList,
}

var repoAddCmd = &cobra.Command{
	Use:   "add [url]...",
	Short: "Add repositories to the workspace registry",
	Long: "Adds one or more repository URLs to the current workspace repository registry. " +
		"Existing URLs are not duplicated. Use project resources when you need project-specific context instead.",
	Args: cobra.ArbitraryArgs,
	RunE: runRepoAdd,
}

var repoRemoveCmd = &cobra.Command{
	Use:     "remove [url]...",
	Aliases: []string{"rm"},
	Short:   "Remove repositories from the workspace registry",
	Long:    "Removes one or more repository URLs from the current workspace repository registry.",
	Args:    cobra.ArbitraryArgs,
	RunE:    runRepoRemove,
}

var repoCheckoutCmd = &cobra.Command{
	Use:   "checkout [<url>]",
	Short: "Check out a repository into the working directory",
	Long: "Creates a git worktree from the daemon's bare clone cache. Used by agents to check out repos on demand.\n\n" +
		"Pass a single URL to check out one repository, or --all to check out every github_repo resource " +
		"listed in .multica/project/resources.json.\n\n" +
		"Running it again where the repository is already checked out never silently discards work: a checkout " +
		"that has uncommitted changes, untracked files, or unpushed commits, or is already on this task's branch, " +
		"is kept as it is and only its remote refs are fetched. Pass --fresh to discard its uncommitted changes and " +
		"untracked files and start over on a new branch; commits stay on the old branch, but push any you still need first.",
	Args: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		if all {
			if len(args) > 0 {
				return fmt.Errorf("--all and a positional URL argument are mutually exclusive")
			}
			return nil
		}
		return exactArgs(1)(cmd, args)
	},
	RunE: runRepoCheckout,
}

var (
	repoCheckoutRef   string
	repoCheckoutFresh bool
)

func init() {
	repoListCmd.Flags().String("output", "table", "Output format: table or json")

	repoAddCmd.Flags().StringArray("url", nil, "Repository URL to add (may be repeated)")
	repoAddCmd.Flags().String("description", "", "Optional description; only valid when adding one URL")
	repoAddCmd.Flags().String("output", "json", "Output format: table or json")

	repoRemoveCmd.Flags().StringArray("url", nil, "Repository URL to remove (may be repeated)")
	repoRemoveCmd.Flags().String("output", "json", "Output format: table or json")

	repoCheckoutCmd.Flags().StringVar(&repoCheckoutRef, "ref", "", "branch, tag, or commit to check out instead of the remote default branch")
	repoCheckoutCmd.Flags().Bool("all", false, "Check out every github_repo resource from .multica/project/resources.json")
	repoCheckoutCmd.Flags().BoolVar(&repoCheckoutFresh, "fresh", false, "discard an existing checkout's uncommitted changes and untracked files and start over on a new branch from the latest default branch (or --ref); commits stay on the old branch")

	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoCheckoutCmd)
}

type workspaceRepo struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type repoWorkspaceResponse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Slug  string          `json:"slug"`
	Repos []workspaceRepo `json:"repos"`
}

type repoMutationResult struct {
	WorkspaceID string          `json:"workspace_id"`
	Added       []workspaceRepo `json:"added,omitempty"`
	Updated     []workspaceRepo `json:"updated,omitempty"`
	Removed     []workspaceRepo `json:"removed,omitempty"`
	Repos       []workspaceRepo `json:"repos"`
}

func repoURLsFromArgsAndFlags(cmd *cobra.Command, args []string) ([]string, error) {
	flagURLs, _ := cmd.Flags().GetStringArray("url")
	raw := append([]string{}, flagURLs...)
	raw = append(raw, args...)
	if len(raw) == 0 {
		return nil, fmt.Errorf("at least one repository URL is required")
	}

	urls := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, u := range raw {
		u = strings.TrimSpace(u)
		if u == "" {
			return nil, fmt.Errorf("repository URL cannot be empty")
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	return urls, nil
}

func fetchRepoWorkspace(ctx context.Context, client *cli.APIClient, workspaceID string) (repoWorkspaceResponse, error) {
	var ws repoWorkspaceResponse
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID, &ws); err != nil {
		return repoWorkspaceResponse{}, fmt.Errorf("get workspace: %w", err)
	}
	if ws.Repos == nil {
		ws.Repos = []workspaceRepo{}
	}
	return ws, nil
}

func patchWorkspaceRepos(ctx context.Context, client *cli.APIClient, workspaceID string, repos []workspaceRepo) (repoWorkspaceResponse, error) {
	var ws repoWorkspaceResponse
	if err := client.PatchJSON(ctx, "/api/workspaces/"+workspaceID, map[string]any{"repos": repos}, &ws); err != nil {
		return repoWorkspaceResponse{}, fmt.Errorf("update workspace repos: %w", err)
	}
	if ws.Repos == nil {
		ws.Repos = []workspaceRepo{}
	}
	return ws, nil
}

func repoCommandClient(cmd *cobra.Command) (*cli.APIClient, string, error) {
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return nil, "", err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, "", err
	}
	return client, workspaceID, nil
}

func runRepoList(cmd *cobra.Command, _ []string) error {
	client, workspaceID, err := repoCommandClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := fetchRepoWorkspace(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, ws.Repos)
	}
	if len(ws.Repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repositories found.")
		return nil
	}
	rows := make([][]string, 0, len(ws.Repos))
	for _, repo := range ws.Repos {
		rows = append(rows, []string{repo.URL, repo.Description})
	}
	cli.PrintTable(os.Stdout, []string{"URL", "DESCRIPTION"}, rows)
	return nil
}

func runRepoAdd(cmd *cobra.Command, args []string) error {
	urls, err := repoURLsFromArgsAndFlags(cmd, args)
	if err != nil {
		return err
	}
	description, _ := cmd.Flags().GetString("description")
	descriptionChanged := cmd.Flags().Changed("description")
	if descriptionChanged && len(urls) > 1 {
		return fmt.Errorf("--description can only be used when adding one repository URL")
	}

	client, workspaceID, err := repoCommandClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := fetchRepoWorkspace(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	indexByURL := make(map[string]int, len(ws.Repos))
	for i, repo := range ws.Repos {
		indexByURL[repo.URL] = i
	}

	added := []workspaceRepo{}
	updated := []workspaceRepo{}
	repos := append([]workspaceRepo{}, ws.Repos...)
	for _, u := range urls {
		if idx, ok := indexByURL[u]; ok {
			if descriptionChanged && repos[idx].Description != description {
				repos[idx].Description = description
				updated = append(updated, repos[idx])
			}
			continue
		}
		repo := workspaceRepo{URL: u}
		if descriptionChanged {
			repo.Description = description
		}
		indexByURL[u] = len(repos)
		repos = append(repos, repo)
		added = append(added, repo)
	}

	if len(added) > 0 || len(updated) > 0 {
		ws, err = patchWorkspaceRepos(ctx, client, workspaceID, repos)
		if err != nil {
			return err
		}
	} else {
		ws.Repos = repos
	}

	result := repoMutationResult{
		WorkspaceID: ws.ID,
		Added:       added,
		Updated:     updated,
		Repos:       ws.Repos,
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if len(added) == 0 && len(updated) == 0 {
		fmt.Fprintln(os.Stdout, "No repository changes.")
		return nil
	}
	rows := make([][]string, 0, len(added)+len(updated))
	for _, repo := range added {
		rows = append(rows, []string{"added", repo.URL, repo.Description})
	}
	for _, repo := range updated {
		rows = append(rows, []string{"updated", repo.URL, repo.Description})
	}
	cli.PrintTable(os.Stdout, []string{"ACTION", "URL", "DESCRIPTION"}, rows)
	return nil
}

func runRepoRemove(cmd *cobra.Command, args []string) error {
	urls, err := repoURLsFromArgsAndFlags(cmd, args)
	if err != nil {
		return err
	}

	client, workspaceID, err := repoCommandClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := fetchRepoWorkspace(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	removeSet := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		removeSet[u] = struct{}{}
	}
	removedSet := make(map[string]struct{}, len(urls))
	removed := []workspaceRepo{}
	repos := make([]workspaceRepo, 0, len(ws.Repos))
	for _, repo := range ws.Repos {
		if _, ok := removeSet[repo.URL]; ok {
			removed = append(removed, repo)
			removedSet[repo.URL] = struct{}{}
			continue
		}
		repos = append(repos, repo)
	}
	missing := []string{}
	for _, u := range urls {
		if _, ok := removedSet[u]; !ok {
			missing = append(missing, u)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("repository not found in workspace registry: %s", strings.Join(missing, ", "))
	}

	ws, err = patchWorkspaceRepos(ctx, client, workspaceID, repos)
	if err != nil {
		return err
	}

	result := repoMutationResult{
		WorkspaceID: ws.ID,
		Removed:     removed,
		Repos:       ws.Repos,
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	rows := make([][]string, 0, len(removed))
	for _, repo := range removed {
		rows = append(rows, []string{repo.URL, repo.Description})
	}
	cli.PrintTable(os.Stdout, []string{"REMOVED URL", "DESCRIPTION"}, rows)
	return nil
}

// doCheckoutRequest sends one checkout request to the daemon and returns the
// path and branch name on success.
// doCheckoutRequest posts one checkout to the daemon and decodes the result.
// A 503 tagged X-Multica-Retryable: repo-busy means another task holds the
// same repository's lock; the daemon names the wait via Retry-After and this
// loop honors it (bounded by the 5-minute context) instead of failing the
// checkout — mirrors upstream's retry-aware single-checkout flow so the
// fork's --all path gets the same behavior per repo.
func doCheckoutRequest(parentCtx context.Context, daemonPort, taskToken string, reqBody map[string]any) (repoCheckoutResult, error) {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return repoCheckoutResult{}, fmt.Errorf("encode request: %w", err)
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
	defer cancel()
	client := &http.Client{}
	checkoutURL := fmt.Sprintf("http://127.0.0.1:%s/repo/checkout", daemonPort)
	var body []byte
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, checkoutURL, bytes.NewReader(data))
		if err != nil {
			return repoCheckoutResult{}, fmt.Errorf("create daemon checkout request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+taskToken)
		resp, err := client.Do(req)
		if err != nil {
			return repoCheckoutResult{}, fmt.Errorf("connect to daemon: %w", err)
		}
		body, err = io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if err != nil {
			return repoCheckoutResult{}, fmt.Errorf("read daemon checkout response: %w", err)
		}
		if closeErr != nil {
			return repoCheckoutResult{}, fmt.Errorf("close daemon checkout response: %w", closeErr)
		}
		if resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("X-Multica-Retryable") == "repo-busy" {
			delay := repoCheckoutRetryDelay(resp.Header.Get("Retry-After"), time.Now())
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return repoCheckoutResult{}, fmt.Errorf("connect to daemon: %w", context.Cause(ctx))
			case <-timer.C:
				continue
			}
		}
		if resp.StatusCode != http.StatusOK {
			return repoCheckoutResult{}, fmt.Errorf("checkout failed: %s", string(body))
		}
		break
	}

	var result repoCheckoutResult
	if err := json.Unmarshal(body, &result); err != nil {
		return repoCheckoutResult{}, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func runRepoCheckout(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	if all {
		workDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		return runRepoCheckoutAll(workDir)
	}

	repoURL := args[0]

	daemonPort := os.Getenv("MULTICA_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("MULTICA_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}

	workspaceID := os.Getenv("MULTICA_WORKSPACE_ID")
	agentName := os.Getenv("MULTICA_AGENT_NAME")
	taskToken := os.Getenv("MULTICA_TOKEN")
	if taskToken == "" {
		return fmt.Errorf("MULTICA_TOKEN not set (repo checkout requires the active task credential)")
	}
	taskID := os.Getenv("MULTICA_TASK_ID")

	// Use current working directory as the checkout target.
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	reqBody := map[string]any{
		"url":           repoURL,
		"workspace_id":  workspaceID,
		"workdir":       workDir,
		"ref":           repoCheckoutRef,
		"agent_name":    agentName,
		"task_id":       taskID,
		"checkout_mode": strings.TrimSpace(os.Getenv("MULTICA_REPO_CHECKOUT_MODE")),
		"retry_busy":    true,
		"fresh":         repoCheckoutFresh,
	}

	result, err := doCheckoutRequest(cmd.Context(), daemonPort, taskToken, reqBody)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "%s\n", result.Path)
	fmt.Fprintln(os.Stderr, repoCheckoutSummary(repoURL, result))
	return nil
}

// repoCheckoutResult is the daemon's /repo/checkout response. Daemons older
// than MUL-7284 never keep an existing checkout and omit Kept and the counts.
type repoCheckoutResult struct {
	Path             string `json:"path"`
	BranchName       string `json:"branch_name"`
	Kept             string `json:"kept"`
	UncommittedFiles int    `json:"uncommitted_files"`
	UnpushedCommits  int    `json:"unpushed_commits"`
}

// repoCheckoutSummary says what the checkout did. A kept checkout has to read
// differently from a new branch off the default branch, or the agent works on
// as if the checkout were fresh and loses track of what it holds.
func repoCheckoutSummary(repoURL string, result repoCheckoutResult) string {
	if result.Kept == "" {
		return fmt.Sprintf("Checked out %s → %s (branch: %s)", repoURL, result.Path, result.BranchName)
	}
	branch := result.BranchName
	if branch == "" {
		branch = "detached HEAD"
	}
	if result.Kept == "task_branch" {
		branch += ", this task's branch"
	}
	return fmt.Sprintf("Kept the existing checkout of %s at %s (branch: %s; %d uncommitted file%s, %d unpushed commit%s): "+
		"nothing was reset, cleaned, or switched; only remote refs were fetched.\n"+
		"To discard its uncommitted changes and untracked files and start over on a new branch from the latest default branch (or --ref), "+
		"re-run with --fresh; commits stay on the old branch, but push any you still need first.",
		repoURL, result.Path, branch,
		result.UncommittedFiles, pluralS(result.UncommittedFiles),
		result.UnpushedCommits, pluralS(result.UnpushedCommits))
}

func repoCheckoutRetryDelay(value string, now time.Time) time.Duration {
	const (
		defaultDelay = time.Second
		maxDelay     = 30 * time.Second
	)
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, maxDelay)
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return min(max(retryAt.Sub(now), time.Duration(0)), maxDelay)
	}
	return defaultDelay
}

// projectResourcesFileForCheckout is the minimal shape of
// .multica/project/resources.json that --all needs to read.
type projectResourcesFileForCheckout struct {
	Resources []projectResourceEntryForCheckout `json:"resources"`
}

type projectResourceEntryForCheckout struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
}

// githubRepoRefForCheckout is the JSONB shape stored in resource_ref for
// resource_type=github_repo entries, as written by the server's
// execenv.writeProjectResources.
type githubRepoRefForCheckout struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

// runRepoCheckoutAll reads .multica/project/resources.json from workDir,
// iterates every github_repo resource, and checks each one out via the daemon.
// It reports per-repo success or failure to stderr and exits non-zero if any
// checkout fails. It continues past individual failures so the caller sees the
// full picture in one run.
func runRepoCheckoutAll(workDir string) error {
	daemonPort := os.Getenv("MULTICA_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("MULTICA_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}

	workspaceID := os.Getenv("MULTICA_WORKSPACE_ID")
	agentName := os.Getenv("MULTICA_AGENT_NAME")
	taskToken := os.Getenv("MULTICA_TOKEN")
	taskID := os.Getenv("MULTICA_TASK_ID")
	checkoutMode := strings.TrimSpace(os.Getenv("MULTICA_REPO_CHECKOUT_MODE"))

	resourcesPath := filepath.Join(workDir, ".multica", "project", "resources.json")
	raw, err := os.ReadFile(resourcesPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", resourcesPath, err)
	}

	var file projectResourcesFileForCheckout
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("parse %s: %w", resourcesPath, err)
	}

	// Collect only github_repo resources.
	type repoEntry struct {
		url string
		ref string
	}
	var repos []repoEntry
	for _, res := range file.Resources {
		if res.ResourceType != "github_repo" {
			continue
		}
		var ref githubRepoRefForCheckout
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			return fmt.Errorf("parse github_repo resource_ref: %w", err)
		}
		if ref.URL == "" {
			continue
		}
		repos = append(repos, repoEntry{url: ref.URL, ref: ref.Ref})
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No github_repo resources found in resources.json.")
		return nil
	}

	var errs []error
	for _, repo := range repos {
		reqBody := map[string]any{
			"url":           repo.url,
			"workspace_id":  workspaceID,
			"workdir":       workDir,
			"ref":           repo.ref,
			"agent_name":    agentName,
			"task_id":       taskID,
			"checkout_mode": checkoutMode,
			"retry_busy":    true,
		}
		result, checkErr := doCheckoutRequest(context.Background(), daemonPort, taskToken, reqBody)
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "FAIL  %s: %v\n", repo.url, checkErr)
			errs = append(errs, fmt.Errorf("%s: %w", repo.url, checkErr))
			continue
		}
		fmt.Fprintf(os.Stdout, "%s\n", result.Path)
		fmt.Fprintf(os.Stderr, "OK    %s → %s (branch: %s)\n", repo.url, result.Path, result.BranchName)
	}

	return errors.Join(errs...)
}
