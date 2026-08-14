package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/designdocument"
)

const designDocumentCheckoutSchema = "multica.design-document-checkout/v1"

const (
	maxDesignDocumentGroundingSourceBytes = 16 << 20
	maxDesignDocumentInputBytes           = 100 << 20
	maxDesignDocumentRepositoryFiles      = 100000
	maxDesignDocumentRepositoryBytes      = 1 << 30
)

type designDocumentGroundingState struct {
	Mode         string
	Repositories []designDocumentCheckoutRepository
	sources      []designDocumentSourceBaseline
	roots        map[string]string
	workDir      string
	pinned       *designdocument.RepositoryGrounding
}

type designDocumentSourceBaseline struct {
	root         string
	commit       string
	statusSHA256 string
	treeSHA256   string
}

type designDocumentCheckout struct {
	SchemaVersion string                             `json:"schema_version"`
	Repositories  []designDocumentCheckoutRepository `json:"repositories"`
}

type designDocumentCheckoutRepository struct {
	ID           string `json:"id"`
	CheckoutPath string `json:"checkout_path"`
	CommitSHA    string `json:"commit_sha"`
	Ref          string `json:"ref,omitempty"`
	StatusSHA256 string `json:"status_sha256"`
	TreeSHA256   string `json:"tree_sha256"`
}

type designDocumentInputEnvelope struct {
	Repository  json.RawMessage `json:"repository,omitempty"`
	Attachments []struct {
		ID        string `json:"id"`
		SizeBytes int64  `json:"size_bytes"`
		SHA256    string `json:"sha256"`
	} `json:"attachments"`
	DesignSystem *struct {
		ContentDigest string `json:"content_digest"`
	} `json:"design_system"`
}

func isDesignDocumentAdjustment(raw json.RawMessage) bool {
	var contextValue struct {
		Type           string `json:"type"`
		Operation      string `json:"operation"`
		ExecutionReady bool   `json:"execution_ready"`
	}
	return json.Unmarshal(raw, &contextValue) == nil && contextValue.Type == "design_document_task" && contextValue.Operation == "adjust" && contextValue.ExecutionReady
}

func materializeDesignDocumentInputs(ctx context.Context, task Task, workDir string, client *Client) error {
	if client == nil {
		return errors.New("Design Document input client is unavailable")
	}
	var envelope struct {
		Operation         string                      `json:"operation"`
		BaseContentDigest string                      `json:"base_content_digest"`
		Input             designDocumentInputEnvelope `json:"input"`
	}
	if json.Unmarshal(task.DesignDocumentContext, &envelope) != nil {
		return errors.New("invalid Design Document input context")
	}
	root := filepath.Join(workDir, ".agent_context", "design_document")
	if envelope.Operation == "adjust" {
		baseDir := filepath.Join(root, "context", "base")
		if err := os.Mkdir(baseDir, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		if !isRealGroundingDirectory(baseDir) || os.Chmod(baseDir, 0o755) != nil {
			return errors.New("Design Document base directory is unsafe")
		}
		archive, headers, err := client.DownloadDesignDocumentInput(ctx, task.ID, "base", "", 32<<20)
		if err != nil {
			return fmt.Errorf("download Design Document base: %w", err)
		}
		if headers.Get("X-Multica-Design-Package-Digest") != envelope.BaseContentDigest {
			return errors.New("Design Document base digest does not match its pinned reference")
		}
		if err := os.WriteFile(filepath.Join(baseDir, "package.zip"), archive, 0o444); err != nil {
			return err
		}
		return os.Chmod(baseDir, 0o555)
	}
	referenceDir := filepath.Join(root, "reference")
	designSystemDir := filepath.Join(root, "context", "design-system")
	for _, dir := range []string{referenceDir, designSystemDir} {
		if !isRealGroundingDirectory(dir) || os.Chmod(dir, 0o755) != nil {
			return errors.New("Design Document input directory is missing or unsafe")
		}
		defer func(path string) { _ = os.Chmod(path, 0o555) }(dir)
	}
	if len(envelope.Input.Attachments) > 0 {
		attachmentsDir := filepath.Join(referenceDir, "attachments")
		if err := os.Mkdir(attachmentsDir, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		if !isRealGroundingDirectory(attachmentsDir) || os.Chmod(attachmentsDir, 0o755) != nil {
			return errors.New("Design Document attachment directory is unsafe")
		}
		defer func() { _ = os.Chmod(attachmentsDir, 0o555) }()
		for _, attachment := range envelope.Input.Attachments {
			if !safeDesignDocumentInputID(attachment.ID) || attachment.SizeBytes < 0 || attachment.SizeBytes > maxDesignDocumentInputBytes {
				return errors.New("Design Document attachment metadata is invalid")
			}
			content, _, err := client.DownloadDesignDocumentInput(ctx, task.ID, "attachments/"+attachment.ID, attachment.SHA256, attachment.SizeBytes)
			if err != nil {
				return fmt.Errorf("download Design Document attachment: %w", err)
			}
			if err := os.WriteFile(filepath.Join(attachmentsDir, attachment.ID), content, 0o444); err != nil {
				return err
			}
		}
	}
	if envelope.Input.DesignSystem != nil {
		archive, headers, err := client.DownloadDesignDocumentInput(ctx, task.ID, "design-system", "", 64<<20)
		if err != nil {
			return fmt.Errorf("download Design Document design system: %w", err)
		}
		if headers.Get("X-Multica-Design-Package-Digest") != envelope.Input.DesignSystem.ContentDigest {
			return errors.New("Design Document design system digest does not match its pinned reference")
		}
		if err := os.WriteFile(filepath.Join(designSystemDir, "package.zip"), archive, 0o444); err != nil {
			return err
		}
	}
	return nil
}

func safeDesignDocumentInputID(value string) bool {
	return value != "" && len(value) <= 128 && value != "." && value != ".." && !strings.ContainsAny(value, "/\\:")
}

func isRealGroundingDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir()
}

func prepareDesignDocumentGrounding(ctx context.Context, task Task, workDir, daemonID string, cache repoCacheBackend, logger *slog.Logger) (*designDocumentGroundingState, error) {
	var envelope struct {
		Type           string `json:"type"`
		Operation      string `json:"operation"`
		ExecutionReady bool   `json:"execution_ready"`
		Input          struct {
			RepositoryGrounding string          `json:"repository_grounding"`
			Repository          json.RawMessage `json:"repository"`
		} `json:"input"`
	}
	if json.Unmarshal(task.DesignDocumentContext, &envelope) != nil || envelope.Type != "design_document_task" || (envelope.Operation != "first_generation" && envelope.Operation != "adjust") || !envelope.ExecutionReady {
		return nil, errors.New("invalid Design Document grounding context")
	}
	if envelope.Operation == "adjust" {
		if envelope.Input.RepositoryGrounding != "pinned" {
			return nil, errors.New("invalid pinned Design Document grounding context")
		}
		grounding, err := designdocument.ValidateRepositoryGrounding(envelope.Input.Repository)
		if err != nil {
			return nil, err
		}
		state := &designDocumentGroundingState{Mode: "pinned", pinned: &grounding, roots: map[string]string{}, workDir: workDir}
		return state, writeDesignDocumentCheckout(workDir, nil)
	}
	state := &designDocumentGroundingState{Mode: envelope.Input.RepositoryGrounding, roots: map[string]string{}, workDir: workDir}
	if state.Mode == designdocument.GroundingUnavailable {
		return state, writeDesignDocumentCheckout(workDir, state.Repositories)
	}
	if state.Mode != "pending" {
		return nil, errors.New("invalid repository grounding mode")
	}

	repositoryDir := filepath.Join(workDir, "repositories")
	if err := os.MkdirAll(repositoryDir, 0o755); err != nil {
		return nil, err
	}
	for _, repo := range task.Repos {
		if strings.TrimSpace(repo.URL) == "" || cache == nil {
			continue
		}
		agentName := "design-document-agent"
		if task.Agent != nil && strings.TrimSpace(task.Agent.Name) != "" {
			agentName = task.Agent.Name
		}
		params := repocache.WorktreeParams{
			WorkspaceID: task.WorkspaceID, RepoURL: repo.URL, WorkDir: repositoryDir, Ref: repo.Ref,
			AgentName: agentName, TaskID: task.ID, PeerURLs: repoURLs(task.Repos),
			CoAuthoredByEnabled: false, IsolatedGitMetadata: true,
		}
		var result *repocache.WorktreeResult
		var err error
		if contextCache, ok := cache.(interface {
			CreateWorktreeContext(context.Context, repocache.WorktreeParams) (*repocache.WorktreeResult, error)
		}); ok {
			result, err = contextCache.CreateWorktreeContext(ctx, params)
		} else {
			result, err = cache.CreateWorktree(params)
		}
		if err != nil {
			return nil, fmt.Errorf("repository unavailable: %w", err)
		}
		if err := state.addRepository(ctx, result.Path, repo.Ref); err != nil {
			return nil, err
		}
	}
	if assignment, err := findLocalDirectoryAssignment(task.ProjectResources, daemonID); err != nil {
		return nil, fmt.Errorf("repository unavailable: %w", err)
	} else if assignment != nil {
		commit, status, treeDigest, identityErr := gitCheckoutIdentity(ctx, assignment.RealPath)
		if identityErr != nil {
			return nil, fmt.Errorf("repository unavailable: %w", identityErr)
		}
		state.sources = append(state.sources, designDocumentSourceBaseline{
			root: assignment.RealPath, commit: commit, statusSHA256: sha256Reference(status), treeSHA256: treeDigest,
		})
		target := filepath.Join(repositoryDir, fmt.Sprintf("repository-%d", len(state.Repositories)+1))
		if out, cloneErr := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", assignment.RealPath, target).CombinedOutput(); cloneErr != nil {
			return nil, fmt.Errorf("repository unavailable: clone local repository: %w: %s", cloneErr, strings.TrimSpace(string(out)))
		}
		if err := state.addRepository(ctx, target, ""); err != nil {
			return nil, err
		}
	}
	if len(state.Repositories) == 0 {
		return nil, errors.New("repository unavailable: no repository is accessible on the selected runtime")
	}
	if logger != nil {
		logger.Info("Design Document repositories prepared", "task_id", task.ID, "repository_count", len(state.Repositories))
	}
	return state, writeDesignDocumentCheckout(workDir, state.Repositories)
}

func (state *designDocumentGroundingState) addRepository(ctx context.Context, root, ref string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if !pathWithin(state.workDir, root) {
		return errors.New("repository checkout leaves the task workspace")
	}
	commit, status, treeDigest, err := gitCheckoutIdentity(ctx, root)
	if err != nil {
		return fmt.Errorf("repository unavailable: %w", err)
	}
	id := fmt.Sprintf("repository-%d", len(state.Repositories)+1)
	checkoutRelative, err := filepath.Rel(state.workDir, root)
	if err != nil {
		return err
	}
	checkoutPath := filepath.ToSlash(checkoutRelative)
	repository := designDocumentCheckoutRepository{ID: id, CheckoutPath: checkoutPath, CommitSHA: commit, Ref: strings.TrimSpace(ref), StatusSHA256: sha256Reference(status), TreeSHA256: treeDigest}
	state.Repositories = append(state.Repositories, repository)
	state.roots[id] = root
	return nil
}

func finalizeDesignDocumentGrounding(state *designDocumentGroundingState, workDir, outputDir string) (*designdocument.RepositoryGrounding, error) {
	if state == nil {
		return nil, errors.New("Design Document grounding state is missing")
	}
	if _, err := designdocument.ValidateStagingDirectory(outputDir); err != nil {
		return nil, err
	}
	if state.pinned != nil {
		value := *state.pinned
		return &value, nil
	}
	if state.Mode == designdocument.GroundingUnavailable {
		value := designdocument.RepositoryGrounding{
			SchemaVersion: designdocument.GroundingSchemaVersion, Status: designdocument.GroundingUnavailable,
			Repositories: []designdocument.GroundedRepository{}, Facts: []designdocument.GroundingFact{},
			Conflicts: []designdocument.GroundingObservation{}, Missing: []designdocument.GroundingObservation{},
			Warnings: []string{"Repository access was explicitly unavailable; this result is not source-grounded."},
		}
		raw, _ := json.Marshal(value)
		validated, err := designdocument.ValidateRepositoryGrounding(raw)
		return &validated, err
	}
	groundingPath := filepath.Join(workDir, ".agent_context", "design_document", "work", "repository-grounding.json")
	raw, err := os.ReadFile(groundingPath)
	if err != nil {
		return nil, fmt.Errorf("read repository grounding: %w", err)
	}
	grounding, err := designdocument.ValidateRepositoryGrounding(raw)
	if err != nil {
		return nil, err
	}
	if len(grounding.Repositories) != len(state.Repositories) {
		return nil, errors.New("repository grounding does not match prepared checkouts")
	}
	baseline := make(map[string]designDocumentCheckoutRepository, len(state.Repositories))
	for _, repository := range state.Repositories {
		baseline[repository.ID] = repository
	}
	verifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, source := range state.sources {
		commit, status, treeDigest, identityErr := gitCheckoutIdentity(verifyCtx, source.root)
		if identityErr != nil || commit != source.commit || sha256Reference(status) != source.statusSHA256 || treeDigest != source.treeSHA256 {
			return nil, errors.New("source repository changed during Design Document generation")
		}
	}
	for _, repository := range grounding.Repositories {
		expected, ok := baseline[repository.ID]
		root := state.roots[repository.ID]
		if !ok || repository.CheckoutPath != expected.CheckoutPath || repository.CommitSHA != expected.CommitSHA || repository.StatusSHA256 != expected.StatusSHA256 || repository.TreeSHA256 != expected.TreeSHA256 || root == "" {
			return nil, errors.New("repository grounding does not match prepared checkout identity")
		}
		commit, status, treeDigest, identityErr := gitCheckoutIdentity(verifyCtx, root)
		if identityErr != nil || commit != expected.CommitSHA || sha256Reference(status) != expected.StatusSHA256 || treeDigest != expected.TreeSHA256 {
			return nil, errors.New("source checkout changed during Design Document generation")
		}
		for _, file := range repository.Files {
			path := filepath.Join(root, filepath.FromSlash(file.Path))
			if !pathWithin(root, path) {
				return nil, errors.New("repository grounding source path leaves the checkout")
			}
			info, statErr := os.Lstat(path)
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, errors.New("repository grounding source file is missing or unsafe")
			}
			content, readErr := readBoundedGroundingSource(path, info.Size())
			if readErr != nil || sha256Reference(content) != file.SHA256 {
				return nil, errors.New("repository grounding source digest does not match checkout")
			}
		}
	}
	return &grounding, nil
}

func writeDesignDocumentCheckout(workDir string, repositories []designDocumentCheckoutRepository) error {
	if repositories == nil {
		repositories = []designDocumentCheckoutRepository{}
	}
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].ID < repositories[j].ID })
	raw, err := json.Marshal(designDocumentCheckout{SchemaVersion: designDocumentCheckoutSchema, Repositories: repositories})
	if err != nil {
		return err
	}
	return execenv.WriteDesignDocumentRepositoryFacts(workDir, raw)
}

func gitCheckoutIdentity(ctx context.Context, root string) (string, []byte, string, error) {
	head, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", nil, "", fmt.Errorf("read checkout commit: %w", err)
	}
	status, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return "", nil, "", fmt.Errorf("read checkout status: %w", err)
	}
	treeDigest, err := repositoryTreeDigest(ctx, root)
	if err != nil {
		return "", nil, "", err
	}
	return strings.TrimSpace(string(head)), status, treeDigest, nil
}

func repositoryTreeDigest(ctx context.Context, root string) (string, error) {
	hash := sha256.New()
	files, total := 0, int64(0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(os.PathSeparator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "." || entry.IsDir() {
			return nil
		}
		files++
		if files > maxDesignDocumentRepositoryFiles {
			return errors.New("repository has too many files")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative)+"\x00"+info.Mode().String()+"\x00")
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			total += int64(len(target))
			if total > maxDesignDocumentRepositoryBytes {
				return errors.New("repository exceeds grounding size limit")
			}
			_, _ = fmt.Fprintf(hash, "%d\x00%s", len(target), target)
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("repository contains an unsupported file type")
		}
		if info.Size() > maxDesignDocumentRepositoryBytes-total {
			return errors.New("repository exceeds grounding size limit")
		}
		total += info.Size()
		_, _ = fmt.Fprintf(hash, "%d\x00", info.Size())
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, &contextReader{ctx: ctx, reader: file})
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", fmt.Errorf("digest repository tree: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func readBoundedGroundingSource(path string, size int64) ([]byte, error) {
	if size < 0 || size > maxDesignDocumentGroundingSourceBytes {
		return nil, errors.New("repository grounding source file exceeds size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxDesignDocumentGroundingSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxDesignDocumentGroundingSourceBytes {
		return nil, errors.New("repository grounding source file exceeds size limit")
	}
	return content, nil
}

func sha256Reference(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func repoURLs(repos []RepoData) []string {
	values := make([]string, 0, len(repos))
	for _, repo := range repos {
		if value := strings.TrimSpace(repo.URL); value != "" {
			values = append(values, value)
		}
	}
	return values
}
