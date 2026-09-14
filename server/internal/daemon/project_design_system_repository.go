package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
)

type projectDesignSystemCheckout struct {
	Root         string
	URL          string
	Name         string
	RequestedRef string
	ResolvedRef  string
	CommitSHA    string
}

type projectDesignSystemRepositoryState struct {
	root         string
	commitSHA    string
	statusSHA256 string
	treeSHA256   string
	evidence     projectdesignsystem.RepositoryEvidenceIndex
}

func isRepositoryScopedProjectDesignSystemTask(task Task) bool {
	if len(task.ProjectDesignSystemContext) == 0 {
		return false
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if json.Unmarshal(task.ProjectDesignSystemContext, &taskContext) != nil ||
		taskContext.Type != service.ProjectDesignSystemTaskContextType ||
		taskContext.PackageSchema != projectdesignsystem.PackageSchemaV2 {
		return false
	}
	if taskContext.Operation != service.ProjectDesignSystemGenerate &&
		taskContext.Operation != service.ProjectDesignSystemAdjust &&
		taskContext.Operation != service.ProjectDesignSystemRegenerate {
		return false
	}
	return strings.TrimSpace(taskContext.ProjectResourceID) != "" || strings.TrimSpace(taskContext.WorkspaceRepositoryID) != ""
}

// prepareProjectDesignSystemRepositoryEvidence ports Open Design v0.19.2's
// complete-tree inventory plus bounded high-signal snapshots into Multica's
// existing Agent workspace. It produces evidence only; the selected Agent is
// the sole design-system author.
func (d *Daemon) prepareProjectDesignSystemRepositoryEvidence(
	ctx context.Context,
	task Task,
	envRoot string,
	workDir string,
	taskLog *slog.Logger,
) (*projectDesignSystemRepositoryState, error) {
	if !isRepositoryScopedProjectDesignSystemTask(task) {
		return nil, nil
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if err := json.Unmarshal(task.ProjectDesignSystemContext, &taskContext); err != nil {
		return nil, fmt.Errorf("decode project design system repository context: %w", err)
	}
	checkout, err := d.prepareProjectDesignSystemRepository(ctx, task, taskContext, workDir)
	if err != nil {
		return nil, err
	}
	commit, status, treeDigest, err := gitCheckoutIdentity(ctx, checkout.Root)
	if err != nil {
		return nil, err
	}
	if len(status) != 0 {
		return nil, errors.New("project design system repository checkout is not clean")
	}
	checkoutRelative, err := filepath.Rel(workDir, checkout.Root)
	if err != nil || !pathWithinDirectory(workDir, checkout.Root) {
		return nil, errors.New("project design system repository checkout leaves the task workspace")
	}
	bundle, err := projectdesignsystem.CollectRepositoryEvidence(ctx, checkout.Root, projectdesignsystem.RepositoryEvidenceInput{
		RepositoryName:      checkout.Name,
		RepositoryURL:       checkout.URL,
		RequestedRef:        checkout.RequestedRef,
		ResolvedRef:         checkout.ResolvedRef,
		CommitSHA:           commit,
		CheckoutPath:        filepath.ToSlash(checkoutRelative),
		InputSnapshotSHA256: taskContext.InputSnapshotSHA256,
	})
	if err != nil {
		return nil, fmt.Errorf("collect project design system repository evidence: %w", err)
	}
	if err := execenv.ExtractProjectDesignSystemRepositoryEvidence(envRoot, workDir, bundle.Files); err != nil {
		return nil, err
	}
	if taskLog != nil {
		taskLog.Info("project design system repository evidence prepared",
			"task_id", task.ID,
			"repository", checkout.Name,
			"ref", checkout.ResolvedRef,
			"commit", commit,
			"tree_files", bundle.Index.TreeFileCount,
			"evidence_files", bundle.Index.SelectedFileCount,
		)
	}
	return &projectDesignSystemRepositoryState{
		root: checkout.Root, commitSHA: commit, statusSHA256: sha256Reference(status), treeSHA256: treeDigest, evidence: bundle.Index,
	}, nil
}

func (state *projectDesignSystemRepositoryState) verify(ctx context.Context) error {
	if state == nil {
		return nil
	}
	commit, status, treeDigest, err := gitCheckoutIdentity(ctx, state.root)
	if err != nil || commit != state.commitSHA || sha256Reference(status) != state.statusSHA256 || treeDigest != state.treeSHA256 {
		return errors.New("source checkout changed during project design system generation")
	}
	return nil
}

func (d *Daemon) prepareProjectDesignSystemRepository(
	ctx context.Context,
	task Task,
	taskContext service.ProjectDesignSystemTaskContext,
	workDir string,
) (projectDesignSystemCheckout, error) {
	repositoriesDir := filepath.Join(workDir, "repositories")
	if err := os.MkdirAll(repositoriesDir, 0o755); err != nil {
		return projectDesignSystemCheckout{}, err
	}

	if strings.TrimSpace(taskContext.WorkspaceRepositoryID) != "" {
		repositoryURL := strings.TrimSpace(taskContext.WorkspaceRepositoryURL)
		repositoryName := strings.TrimSpace(taskContext.WorkspaceRepositoryLabel)
		// Repository design systems always pin the remote default branch. The
		// stored hint remains UI metadata and cannot override origin/HEAD.
		requestedRef := ""
		if repositoryURL == "" {
			return projectDesignSystemCheckout{}, errors.New("settings repository URL is missing from the task claim")
		}
		root, err := d.createProjectDesignSystemRemoteCheckout(ctx, task, repositoriesDir, repositoryURL, requestedRef)
		if err != nil {
			return projectDesignSystemCheckout{}, err
		}
		return projectDesignSystemCheckout{
			Root: root, URL: repositoryURL, Name: firstProjectDesignSystemValue(repositoryName, repositoryNameFromURL(repositoryURL)),
			RequestedRef: requestedRef, ResolvedRef: resolvedProjectDesignSystemRef(ctx, root, requestedRef), CommitSHA: mustProjectDesignSystemCommit(ctx, root),
		}, nil
	}

	selectedResources := make([]ProjectResourceData, 0, 1)
	for _, resource := range task.ProjectResources {
		if resource.ID == strings.TrimSpace(taskContext.ProjectResourceID) {
			selectedResources = append(selectedResources, resource)
			break
		}
	}
	if len(selectedResources) == 0 {
		return projectDesignSystemCheckout{}, errors.New("selected repository resource is missing from the task claim")
	}
	resourceName, repositoryURL, _ := selectedProjectDesignSystemResource(selectedResources)
	if repositoryURL != "" {
		requestedRef := ""
		root, err := d.createProjectDesignSystemRemoteCheckout(ctx, task, repositoriesDir, repositoryURL, requestedRef)
		if err != nil {
			return projectDesignSystemCheckout{}, err
		}
		return projectDesignSystemCheckout{
			Root: root, URL: repositoryURL, Name: firstProjectDesignSystemValue(resourceName, repositoryNameFromURL(repositoryURL)),
			RequestedRef: requestedRef, ResolvedRef: resolvedProjectDesignSystemRef(ctx, root, requestedRef), CommitSHA: mustProjectDesignSystemCommit(ctx, root),
		}, nil
	}

	assignment, err := findLocalDirectoryAssignment(selectedResources, d.cfg.DaemonID)
	if err != nil {
		return projectDesignSystemCheckout{}, err
	}
	if assignment == nil {
		return projectDesignSystemCheckout{}, errors.New("selected repository resource is unavailable on this runtime")
	}
	target := filepath.Join(repositoriesDir, "repository")
	command := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", assignment.RealPath, target)
	if output, cloneErr := command.CombinedOutput(); cloneErr != nil {
		return projectDesignSystemCheckout{}, fmt.Errorf("clone local repository: %w: %s", cloneErr, strings.TrimSpace(string(output)))
	}
	return projectDesignSystemCheckout{
		Root: target, Name: firstProjectDesignSystemValue(resourceName, assignment.DisplayName()),
		ResolvedRef: resolvedProjectDesignSystemRef(ctx, target, ""), CommitSHA: mustProjectDesignSystemCommit(ctx, target),
	}, nil
}

func (d *Daemon) createProjectDesignSystemRemoteCheckout(ctx context.Context, task Task, repositoriesDir, repositoryURL, requestedRef string) (string, error) {
	if d.repoCache == nil {
		return "", errors.New("repository cache is unavailable")
	}
	if err := d.ensureRepoReady(ctx, task.WorkspaceID, repositoryURL); err != nil {
		return "", err
	}
	agentName := "project-design-system-agent"
	if task.Agent != nil && strings.TrimSpace(task.Agent.Name) != "" {
		agentName = task.Agent.Name
	}
	params := repocache.WorktreeParams{
		WorkspaceID: task.WorkspaceID, RepoURL: repositoryURL, WorkDir: repositoriesDir, Ref: requestedRef,
		AgentName: agentName, TaskID: task.ID, PeerURLs: []string{repositoryURL}, CoAuthoredByEnabled: false, IsolatedGitMetadata: true,
	}
	var checkout *repocache.WorktreeResult
	var err error
	if contextCache, ok := d.repoCache.(interface {
		CreateWorktreeContext(context.Context, repocache.WorktreeParams) (*repocache.WorktreeResult, error)
	}); ok {
		checkout, err = contextCache.CreateWorktreeContext(ctx, params)
	} else {
		checkout, err = d.repoCache.CreateWorktree(params)
	}
	if err != nil {
		return "", err
	}
	return checkout.Path, nil
}

func selectedProjectDesignSystemResource(resources []ProjectResourceData) (name, repositoryURL, ref string) {
	if len(resources) == 0 {
		return "", "", ""
	}
	resource := resources[0]
	name = strings.TrimSpace(resource.Label)
	if resource.ResourceType != "github_repo" {
		return name, "", ""
	}
	var payload struct {
		URL               string `json:"url"`
		Ref               string `json:"ref,omitempty"`
		DefaultBranchHint string `json:"default_branch_hint,omitempty"`
	}
	if json.Unmarshal(resource.ResourceRef, &payload) == nil {
		return name, strings.TrimSpace(payload.URL), firstProjectDesignSystemValue(payload.Ref, payload.DefaultBranchHint)
	}
	return name, "", ""
}

func resolvedProjectDesignSystemRef(ctx context.Context, root, requested string) string {
	if value := strings.TrimSpace(requested); value != "" {
		return value
	}
	if output, err := exec.CommandContext(ctx, "git", "-C", root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if value := projectDesignSystemRemoteBranchName(string(output)); value != "" {
			return value
		}
	}

	// Isolated checkouts copy the cache's refs/remotes/origin/HEAD as a direct
	// ref, which loses the symref target even though the checked-out commit is
	// correct. Recover the human branch name from the remote branch refs that
	// point at that exact HEAD. Never fall back to the task's agent/* branch.
	originHead, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "refs/remotes/origin/HEAD^{commit}").Output()
	if err == nil {
		commit := strings.TrimSpace(string(originHead))
		refs, refsErr := exec.CommandContext(ctx, "git", "-C", root, "for-each-ref", "--format=%(refname)", "--points-at", commit, "refs/remotes/origin/").Output()
		if refsErr == nil {
			var candidates []string
			for _, ref := range strings.Split(string(refs), "\n") {
				ref = strings.TrimSpace(ref)
				if ref == "" || ref == "refs/remotes/origin/HEAD" {
					continue
				}
				if branch := projectDesignSystemRemoteBranchName(ref); branch != "" {
					candidates = append(candidates, branch)
				}
			}
			if len(candidates) == 1 {
				return candidates[0]
			}
		}
		// Multiple remote branches can temporarily share the same tip. Ask the
		// remote only in this ambiguous case; failure stays fail-closed rather
		// than recording the generated agent branch as provenance.
		if branch := projectDesignSystemRemoteHeadFromOrigin(ctx, root); branch != "" {
			return branch
		}
		return "remote-default"
	}

	// A cloned local-directory resource may not have remote-tracking refs. Its
	// current branch is the source branch rather than a generated agent branch.
	if output, err := exec.CommandContext(ctx, "git", "-C", root, "branch", "--show-current").Output(); err == nil {
		if value := strings.TrimSpace(string(output)); value != "" && !strings.HasPrefix(value, "agent/") {
			return value
		}
	}
	return "remote-default"
}

func projectDesignSystemRemoteBranchName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "refs/remotes/origin/")
	value = strings.TrimPrefix(value, "origin/")
	if value == "" || value == "HEAD" {
		return ""
	}
	return value
}

func projectDesignSystemRemoteHeadFromOrigin(ctx context.Context, root string) string {
	output, err := exec.CommandContext(ctx, "git", "-C", root, "ls-remote", "--symref", "origin", "HEAD").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			return strings.TrimPrefix(fields[1], "refs/heads/")
		}
	}
	return ""
}

func mustProjectDesignSystemCommit(ctx context.Context, root string) string {
	output, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func repositoryNameFromURL(raw string) string {
	value := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(raw), "/"), ".git")
	if index := strings.LastIndexAny(value, "/:"); index >= 0 && index+1 < len(value) {
		return value[index+1:]
	}
	return value
}

func firstProjectDesignSystemValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
