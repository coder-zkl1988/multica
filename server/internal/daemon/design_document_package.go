package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
)

const (
	designDocumentFailureAudit          = "design_document_audit_failed"
	designDocumentFailurePreviewMissing = "design_document_preview_unavailable"
	designDocumentFailurePreview        = "design_document_preview_failed"
	designDocumentFailureUpload         = "design_document_upload_failed"
)

type DesignDocumentPackageUpload struct {
	ObjectKey     string `json:"object_key"`
	ContentDigest string `json:"content_digest"`
}

type designDocumentUploadClient interface {
	UploadDesignDocumentPackage(context.Context, string, designdocument.Binding, string, []byte) (DesignDocumentPackageUpload, error)
}

type designDocumentFinalizeDeps struct {
	BrowserPath        string
	ResolveBrowserPath func(string) (string, error)
	NewVerifier        func(string, designpreview.Policy) (designpreview.Verifier, error)
	Upload             designDocumentUploadClient
	Now                func() time.Time
	ServerTimeout      time.Duration
	ServerBaseAddr     string
}

type DesignDocumentPackageReceipt struct {
	SchemaVersion       string                             `json:"schema_version"`
	DocumentID          string                             `json:"document_id"`
	RevisionID          string                             `json:"revision_id"`
	ObjectKey           string                             `json:"object_key"`
	ContentDigest       string                             `json:"content_digest"`
	InputSnapshotSHA256 string                             `json:"input_snapshot_sha256"`
	ArtifactIndex       []designdocument.FileEntry         `json:"artifact_index"`
	Grounding           designdocument.RepositoryGrounding `json:"grounding"`
	Audit               designdocument.AuditReport         `json:"audit"`
	Preview             designpreview.Receipt              `json:"preview"`
}

func (d *Daemon) finalizeDesignDocumentResultFromDaemon(ctx context.Context, task Task, result TaskResult, grounding *designdocument.RepositoryGrounding, outputDir string) (TaskResult, error) {
	return finalizeDesignDocumentResult(ctx, task, result, grounding, outputDir, designDocumentFinalizeDeps{
		BrowserPath: d.designPreviewBrowserPath,
		NewVerifier: func(path string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(path, policy)
		},
		Upload: d.client,
	})
}

func finalizeDesignDocumentResult(ctx context.Context, task Task, result TaskResult, grounding *designdocument.RepositoryGrounding, outputDir string, deps designDocumentFinalizeDeps) (TaskResult, error) {
	if result.Status != "completed" {
		return result, nil
	}
	if grounding == nil || deps.Upload == nil {
		return result, errors.New("finalizeDesignDocumentResult: grounding and upload client are required")
	}
	if deps.ResolveBrowserPath == nil {
		deps.ResolveBrowserPath = designpreview.ResolveBrowserPath
	}
	if deps.NewVerifier == nil {
		deps.NewVerifier = func(path string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(path, policy)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.ServerTimeout == 0 {
		deps.ServerTimeout = 60 * time.Second
	}
	if deps.ServerBaseAddr == "" {
		deps.ServerBaseAddr = "127.0.0.1"
	}

	binding, snapshotDigest, decodeErr := decodeDesignDocumentBinding(task, grounding)
	if decodeErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailureAudit, "Design Document package invalid: "+decodeErr.Error()), nil
	}
	collected, collectErr := designdocument.CollectDirectory(outputDir, binding)
	if collectErr != nil || !collected.Audit.Passed {
		if collectErr == nil {
			collectErr = errors.New("static audit did not pass")
		}
		return blockDesignDocumentResult(result, designDocumentFailureAudit, "Design Document package invalid: "+collectErr.Error()), nil
	}
	browserPath, browserErr := deps.ResolveBrowserPath(deps.BrowserPath)
	if browserErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailurePreviewMissing, "Design Document preview unavailable: "+browserErr.Error()), nil
	}
	prefix, prefixErr := randomLoopbackPrefix()
	if prefixErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailurePreviewMissing, "Design Document preview unavailable: "+prefixErr.Error()), nil
	}
	server, baseURL, serverErr := startDesignDocumentPreviewServer(collected.Archive, prefix, deps.ServerBaseAddr, deps.ServerTimeout)
	if serverErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailurePreviewMissing, "Design Document preview unavailable: "+serverErr.Error()), nil
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	requiredInteractions := make(map[string]bool, len(collected.Coverage.Interactions))
	for _, interaction := range collected.Coverage.Interactions {
		requiredInteractions[interaction.TargetID] = true
	}
	targets, targetErr := buildDesignDocumentTargetURLs(collected.Manifest.PreviewTargets, requiredInteractions, baseURL, prefix)
	if targetErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailurePreview, "Design Document preview failed: "+targetErr.Error()), nil
	}
	verifier, verifierErr := deps.NewVerifier(browserPath, designpreview.DefaultPolicy())
	if verifierErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailurePreviewMissing, "Design Document preview unavailable: "+verifierErr.Error()), nil
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, deps.ServerTimeout)
	defer cancelVerify()
	verification, verifyErr := verifier.Verify(verifyCtx, targets)
	if verifyErr != nil || !verification.Passed {
		if verifyErr == nil {
			verifyErr = errors.New("browser verification did not pass")
		}
		return blockDesignDocumentResult(result, designDocumentFailurePreview, "Design Document preview failed: "+verifyErr.Error()), nil
	}
	receipt, receiptErr := designpreview.NewReceipt(collected.Manifest.ContentDigest, verification)
	declaredTargets := make([]designpreview.Target, len(targets))
	for i := range targets {
		declaredTargets[i] = targets[i].Target
	}
	if receiptErr == nil {
		receiptErr = designpreview.ValidateReceiptWithInteractions(receipt, collected.Manifest.ContentDigest, declaredTargets, requiredInteractions)
	}
	if receiptErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailurePreview, "Design Document preview receipt invalid: "+receiptErr.Error()), nil
	}
	uploadCtx, cancelUpload := context.WithTimeout(ctx, deps.ServerTimeout)
	defer cancelUpload()
	upload, uploadErr := deps.Upload.UploadDesignDocumentPackage(uploadCtx, task.ID, binding, collected.Manifest.ContentDigest, collected.Archive)
	if uploadErr != nil {
		return blockDesignDocumentResult(result, designDocumentFailureUpload, "Design Document package upload failed: "+uploadErr.Error()), nil
	}
	if upload.ObjectKey == "" || upload.ContentDigest != collected.Manifest.ContentDigest {
		return blockDesignDocumentResult(result, designDocumentFailureUpload, "Design Document package upload returned an invalid receipt"), nil
	}
	result.DesignDocumentGrounding = nil
	result.DesignDocumentPackage = &DesignDocumentPackageReceipt{
		SchemaVersion: designdocument.SchemaVersion, DocumentID: binding.DocumentID, RevisionID: binding.RevisionID,
		ObjectKey: upload.ObjectKey, ContentDigest: collected.Manifest.ContentDigest, InputSnapshotSHA256: snapshotDigest,
		ArtifactIndex: collected.Manifest.Files, Grounding: *grounding, Audit: collected.Audit, Preview: receipt,
	}
	return result, nil
}

func decodeDesignDocumentBinding(task Task, grounding *designdocument.RepositoryGrounding) (designdocument.Binding, string, error) {
	var envelope struct {
		Type              string          `json:"type"`
		TaskProtocol      string          `json:"task_protocol"`
		Operation         string          `json:"operation"`
		ExecutionReady    bool            `json:"execution_ready"`
		Input             json.RawMessage `json:"input"`
		WorkspaceID       string          `json:"workspace_id"`
		ProjectID         string          `json:"project_id"`
		IssueID           string          `json:"issue_id"`
		AgentID           string          `json:"agent_id"`
		TargetPlatform    string          `json:"target_platform"`
		DocumentID        string          `json:"document_id"`
		BaseRevisionID    string          `json:"base_revision_id"`
		BaseContentDigest string          `json:"base_content_digest"`
	}
	if err := json.Unmarshal(task.DesignDocumentContext, &envelope); err != nil || envelope.Type != "design_document_task" || envelope.TaskProtocol != "multica.design-document-task/v1" || (envelope.Operation != "first_generation" && envelope.Operation != "adjust") || !envelope.ExecutionReady {
		return designdocument.Binding{}, "", errors.New("task context is not an execution-ready Design Document task")
	}
	if envelope.WorkspaceID != task.WorkspaceID || envelope.ProjectID != task.ProjectID || envelope.IssueID != task.IssueID || envelope.AgentID != task.AgentID {
		return designdocument.Binding{}, "", errors.New("task context identity does not match the claimed task")
	}
	groundingRaw, err := json.Marshal(grounding)
	if err != nil {
		return designdocument.Binding{}, "", err
	}
	_, digest, err := designdocument.SnapshotWithRepositoryGrounding(envelope.Input, groundingRaw)
	if err != nil {
		return designdocument.Binding{}, "", err
	}
	var input struct {
		TargetPlatform string `json:"target_platform"`
		DesignSystem   *struct {
			ID            string `json:"id"`
			SourceTaskID  string `json:"source_task_id"`
			ContentDigest string `json:"content_digest"`
		} `json:"design_system"`
	}
	if err := json.Unmarshal(envelope.Input, &input); err != nil || input.TargetPlatform != envelope.TargetPlatform {
		return designdocument.Binding{}, "", errors.New("task input platform is invalid")
	}
	binding := designdocument.Binding{
		DocumentID: uuid.NewString(), RevisionID: uuid.NewString(), WorkspaceID: task.WorkspaceID, ProjectID: task.ProjectID,
		IssueID: task.IssueID, TaskID: task.ID, AgentID: task.AgentID, TargetPlatform: input.TargetPlatform, InputSnapshotSHA256: digest,
	}
	if envelope.Operation == "adjust" {
		if _, err := uuid.Parse(envelope.DocumentID); err != nil {
			return designdocument.Binding{}, "", errors.New("adjustment document identity is invalid")
		}
		if _, err := uuid.Parse(envelope.BaseRevisionID); err != nil || !validProjectDesignSystemPackageDigest(envelope.BaseContentDigest) {
			return designdocument.Binding{}, "", errors.New("adjustment base identity is invalid")
		}
		binding.DocumentID = envelope.DocumentID
		binding.BaseRevisionID = envelope.BaseRevisionID
		binding.BaseContentDigest = envelope.BaseContentDigest
	}
	if input.DesignSystem != nil {
		binding.DesignSystemID = input.DesignSystem.ID
		binding.DesignSystemSourceTaskID = input.DesignSystem.SourceTaskID
		binding.DesignSystemContentDigest = input.DesignSystem.ContentDigest
	}
	return binding, digest, nil
}

func buildDesignDocumentTargetURLs(targets []designdocument.PreviewTarget, required map[string]bool, baseURL, prefix string) ([]designpreview.TargetURL, error) {
	if len(targets) == 0 {
		return nil, errors.New("package has no preview targets")
	}
	out := make([]designpreview.TargetURL, 0, len(targets))
	for _, target := range targets {
		if target.Kind != "page" {
			return nil, fmt.Errorf("preview target %q has unsupported kind %q", target.ID, target.Kind)
		}
		parsed, err := url.Parse(baseURL + "/" + prefix + "/" + strings.TrimPrefix(target.Path, "/"))
		if err != nil {
			return nil, err
		}
		out = append(out, designpreview.TargetURL{
			Target: designpreview.Target{Kind: "preview", ID: target.ID, Path: target.Path}, URL: parsed.String(), RequireInteraction: required[target.ID],
		})
	}
	return out, nil
}

func startDesignDocumentPreviewServer(archive []byte, prefix, bindAddr string, timeout time.Duration) (*http.Server, string, error) {
	files, err := openArchiveFiles(archive)
	if err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp", bindAddr+":0")
	if err != nil {
		return nil, "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		relative := strings.TrimPrefix(r.URL.Path, "/"+prefix+"/")
		contents, ok := files[relative]
		if !ok || relative == "" || strings.Contains(relative, "..") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentTypeForPath(relative))
		w.Header().Set("Content-Security-Policy", "default-src 'self' data:; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(contents)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: timeout, ReadTimeout: timeout, WriteTimeout: timeout, IdleTimeout: timeout}
	go func() { _ = server.Serve(listener) }()
	return server, "http://" + listener.Addr().String(), nil
}

func blockDesignDocumentResult(result TaskResult, reason, comment string) TaskResult {
	result.Status = "blocked"
	result.Comment = comment
	result.FailureReason = reason
	result.DesignDocumentGrounding = nil
	result.DesignDocumentPackage = nil
	return result
}
