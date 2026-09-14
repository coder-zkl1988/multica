package designimplementation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ReceiptSchemaV1     = "multica.design-implementation-receipt/v1"
	contextRelativePath = ".agent_context/design_implementation/context.json"
	resultRelativePath  = ".agent_context/design_implementation/result/implementation-result.json"
	maxContextBytes     = 1 << 20
	maxResultBytes      = 1 << 20
)

type FrozenIdentity struct {
	DesignRef         string   `json:"design_ref"`
	RevisionID        string   `json:"revision_id"`
	ContentDigest     string   `json:"content_digest"`
	FrameRefs         []string `json:"frame_refs"`
	ProjectID         string   `json:"project_id"`
	IssueID           string   `json:"issue_id"`
	TaskID            string   `json:"task_id"`
	ProjectResourceID string   `json:"project_resource_id"`
}

type Receipt struct {
	SchemaVersion     string         `json:"schema_version"`
	ImplementationRef string         `json:"implementation_ref"`
	CollectedAt       string         `json:"collected_at"`
	ResultDigest      string         `json:"result_digest"`
	Identity          FrozenIdentity `json:"identity"`
	Result            Result         `json:"result"`
	TargetFiles       []string       `json:"target_files"`
	PreviewPaths      []string       `json:"preview_paths"`
}

type materializedContext struct {
	SchemaVersion     string `json:"schema_version"`
	ImplementationRef string `json:"implementation_ref"`
	FrozenIdentity
}

func CollectReceipt(workDir string, now time.Time) (*Receipt, error) {
	return CollectReceiptFromRepository(workDir, workDir, now)
}

// CollectReceiptFromRepository reads task-owned context and preview evidence
// from workDir while binding source changes to the selected checkout.
func CollectReceiptFromRepository(workDir, repositoryDir string, now time.Time) (*Receipt, error) {
	contextBytes, err := readRegularFileWithin(workDir, contextRelativePath, maxContextBytes)
	if err != nil {
		return nil, fmt.Errorf("implementation context unavailable: %w", err)
	}
	var contextValue materializedContext
	decoder := json.NewDecoder(bytes.NewReader(contextBytes))
	if err := decoder.Decode(&contextValue); err != nil {
		return nil, fmt.Errorf("implementation context invalid: %w", err)
	}
	if contextValue.SchemaVersion != "multica.design-implementation-context/v1" || contextValue.ImplementationRef == "" ||
		!validFrozenIdentity(contextValue.FrozenIdentity) {
		return nil, errors.New("implementation context identity is incomplete")
	}
	resultBytes, err := readRegularFileWithin(workDir, resultRelativePath, maxResultBytes)
	if err != nil {
		return nil, fmt.Errorf("implementation result unavailable: %w", err)
	}
	result, err := ValidateJSONForContext(resultBytes, ExpectedIdentity{
		DesignRef: contextValue.DesignRef, RevisionID: contextValue.RevisionID, FrameRefs: contextValue.FrameRefs,
	})
	if err != nil {
		return nil, err
	}
	canonicalResult, err := canonicalResultJSON(result)
	if err != nil {
		return nil, err
	}
	if err := validateRepositoryCommit(repositoryDir, result.RepositoryCommitBefore); err != nil {
		return nil, err
	}
	targetFiles := resultTargetFiles(result)
	previewPaths := resultPreviewPaths(result)
	if result.Status == "completed" && len(previewPaths) == 0 {
		return nil, errors.New("completed implementation result requires preview artifact paths")
	}
	for _, relative := range targetFiles {
		if _, err := readRegularFileWithin(repositoryDir, relative, 0); err != nil {
			return nil, fmt.Errorf("implementation evidence %q unavailable: %w", relative, err)
		}
	}
	for _, relative := range previewPaths {
		if _, err := readRegularFileWithin(workDir, relative, 0); err != nil {
			return nil, fmt.Errorf("implementation evidence %q unavailable: %w", relative, err)
		}
	}
	digest := sha256.Sum256(canonicalResult)
	receipt := &Receipt{
		SchemaVersion: ReceiptSchemaV1, ImplementationRef: contextValue.ImplementationRef,
		CollectedAt: now.UTC().Format(time.RFC3339Nano), ResultDigest: "sha256:" + hex.EncodeToString(digest[:]),
		Identity: contextValue.FrozenIdentity, Result: result, TargetFiles: targetFiles, PreviewPaths: previewPaths,
	}
	if err := validateReceiptEvidence(receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func ValidateReceipt(receipt *Receipt, now time.Time, taskID string) (ReferenceClaim, error) {
	if err := validateReceiptEvidence(receipt); err != nil {
		return ReferenceClaim{}, err
	}
	claim, err := OpenReference(receipt.ImplementationRef, now)
	if err != nil {
		return ReferenceClaim{}, err
	}
	if taskID == "" || claim.TaskID != taskID || !claimMatchesFrozenIdentity(claim, receipt.Identity) {
		return ReferenceClaim{}, errors.New("design implementation receipt identity does not match its signed task reference")
	}
	return claim, nil
}

func validateReceiptEvidence(receipt *Receipt) error {
	if receipt == nil || receipt.SchemaVersion != ReceiptSchemaV1 || receipt.ImplementationRef == "" || !validFrozenIdentity(receipt.Identity) {
		return errors.New("design implementation receipt is missing or unsupported")
	}
	if _, err := ValidateForContext(receipt.Result, ExpectedIdentity{
		DesignRef: receipt.Identity.DesignRef, RevisionID: receipt.Identity.RevisionID, FrameRefs: receipt.Identity.FrameRefs,
	}); err != nil {
		return err
	}
	canonicalResult, err := canonicalResultJSON(receipt.Result)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonicalResult)
	if receipt.ResultDigest != "sha256:"+hex.EncodeToString(digest[:]) ||
		!sameStrings(receipt.TargetFiles, resultTargetFiles(receipt.Result)) ||
		!sameStrings(receipt.PreviewPaths, resultPreviewPaths(receipt.Result)) {
		return errors.New("design implementation receipt evidence does not match its result")
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.CollectedAt); err != nil {
		return errors.New("design implementation receipt collection time is invalid")
	}
	return nil
}

func canonicalResultJSON(result Result) ([]byte, error) {
	canonical, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(canonical, []byte(`\u0000`)) {
		return nil, errors.New("implementation result contains unsupported NUL characters")
	}
	return canonical, nil
}

func validFrozenIdentity(identity FrozenIdentity) bool {
	return identity.DesignRef != "" && identity.RevisionID != "" && identity.ContentDigest != "" && identity.TaskID != "" &&
		identity.ProjectID != "" && identity.IssueID != "" && identity.ProjectResourceID != "" && uniqueNonEmpty(identity.FrameRefs)
}

func claimMatchesFrozenIdentity(claim ReferenceClaim, identity FrozenIdentity) bool {
	return claim.DesignRef == identity.DesignRef && claim.RevisionID == identity.RevisionID &&
		claim.ContentDigest == identity.ContentDigest && claim.ProjectID == identity.ProjectID &&
		claim.IssueID == identity.IssueID && claim.ProjectResourceID == identity.ProjectResourceID &&
		claim.TaskID == identity.TaskID && sameStrings(claim.FrameRefs, identity.FrameRefs)
}

func resultTargetFiles(result Result) []string {
	seen := map[string]struct{}{}
	for _, mapping := range result.Mappings {
		for _, path := range mapping.TargetFiles {
			seen[path] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func resultPreviewPaths(result Result) []string {
	seen := map[string]struct{}{}
	for _, preview := range result.PreviewEvidence {
		if preview.Path != "" {
			seen[preview.Path] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateRepositoryCommit(workDir, commit string) error {
	if len(commit) != 40 && len(commit) != 64 {
		return errors.New("repository_commit_before must be a full commit hash")
	}
	for _, char := range commit {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return errors.New("repository_commit_before must be a full commit hash")
		}
	}
	command := exec.Command("git", "-C", workDir, "cat-file", "-e", commit+"^{commit}")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("repository_commit_before is not present in the target repository: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func readRegularFileWithin(root, relative string, maxBytes int64) ([]byte, error) {
	if !boundedRelativePath(relative) {
		return nil, errors.New("path must be bounded and relative")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(rootAbs, filepath.FromSlash(relative))
	current := rootAbs
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symbolic links are not accepted as implementation evidence")
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("implementation evidence must be a regular file")
	}
	if maxBytes == 0 {
		return nil, nil
	}
	if info.Size() > maxBytes {
		return nil, errors.New("implementation evidence exceeds size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, errors.New("implementation evidence exceeds size limit")
	}
	return content, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
