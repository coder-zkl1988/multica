package daemon

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
)

// finalizeFailureReason enumerates the stable failure reasons a V2 finalize
// pass can stamp onto a blocked TaskResult. They are the platform contract
// for "why did this design-system task not produce a deliverable" — the UI
// and the test suite both read them, so adding a new value requires
// updating both surfaces.
const (
	finalizeFailureAuditUnavailable = "project_design_system_audit_failed"
	finalizeFailurePreviewFailed    = "project_design_system_preview_failed"
	finalizeFailurePreviewMissing   = "project_design_system_preview_unavailable"
	finalizeFailureUploadFailed     = "project_design_system_upload_failed"
)

// finalizeUploadClient is the subset of *Client that finalizeProjectDesignSystemResult
// needs to upload the collected V2 archive. Defining it as an interface lets
// tests inject a stub without spinning up an HTTP server.
type finalizeUploadClient interface {
	UploadProjectDesignSystemPackage(ctx context.Context, taskID, contentDigest string, archive []byte) (ProjectDesignSystemPackageUpload, error)
}

// finalizeDeps bundles the runtime knobs the finalize path uses so tests can
// override them without touching global state. The production daemon wires
// finalizeProjectDesignSystemResultFromDaemon which uses designpreview and the
// real client; tests inject their own browser resolver, verifier factory,
// upload client, and clock.
type finalizeDeps struct {
	BrowserPath        string
	ResolveBrowserPath func(explicitPath string) (string, error)
	NewVerifier        func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error)
	Upload             finalizeUploadClient
	Now                func() time.Time
	ServerTimeout      time.Duration
	ServerBaseAddr     string // loopback IP for the preview server; defaults to 127.0.0.1
	ServerDialTimeout  time.Duration
	// OnPreview is invoked immediately before the verifier runs. Tests use
	// it to record stage ordering; production callers can leave it nil.
	OnPreview func()
}

// finalizeProjectDesignSystemResult is the V2-native finalization gate. It is
// called by handleTask only after the provider (Agent) has exited and only
// for tasks whose context carries the V2 package_schema marker (see
// isV2ProjectDesignSystemTask in execenv/context.go). Legacy non-V2 tasks
// keep flowing through attachProjectDesignSystemArtifacts; V2 tasks skip
// that path entirely so the inline 2 MiB three-file payload never reaches
// the server.
//
// Ordering — every stage in the gate runs strictly in this sequence; the
// success fake in tests must observe the same ordering:
//
//  1. Collect the agent's V2 directory (also runs the static audit).
//     A failed audit short-circuits before the browser is ever touched and
//     before any upload is attempted.
//  2. Resolve the configured browser. An unresolved browser fails the task
//     with project_design_system_preview_unavailable (no skip semantics —
//     the brief explicitly forbids them).
//  3. Serve the collected archive on 127.0.0.1:0 under an unguessable
//     per-run prefix and run the design preview verifier against every
//     discovered Preview target. The server is shut down on every return
//     path (success, audit failure, preview failure, upload failure,
//     context cancellation).
//  4. Upload the archive. A permanent upload failure fails the task with
//     project_design_system_upload_failed and leaves the package receipt
//     unset so the server never sees a "completed" status carrying an
//     unuploadable package.
//  5. Annotate the TaskResult with the receipt and let reportTaskResult
//     call CompleteTask.
//
// All five stages are required; no stage may be skipped or inferred from
// the Agent's stdout / last-response. The success fake asserts this by
// recording every callback in order.
func finalizeProjectDesignSystemResult(
	ctx context.Context,
	task Task,
	result TaskResult,
	deps finalizeDeps,
) (TaskResult, error) {
	if deps.ResolveBrowserPath == nil {
		deps.ResolveBrowserPath = designpreview.ResolveBrowserPath
	}
	if deps.NewVerifier == nil {
		deps.NewVerifier = func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(browserPath, policy)
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
	if deps.ServerDialTimeout == 0 {
		deps.ServerDialTimeout = 5 * time.Second
	}
	if deps.Upload == nil {
		return result, errors.New("finalizeProjectDesignSystemResult: upload client is required")
	}

	if strings.TrimSpace(string(task.ProjectDesignSystemContext)) == "" {
		return result, errors.New("finalizeProjectDesignSystemResult: task has no project design system context")
	}
	if result.Status != "completed" {
		// A non-completed upstream result (e.g. blocked by the agent itself)
		// is not subject to the V2 gate. Surface it unchanged so the
		// upstream failure reason keeps its semantics.
		return result, nil
	}
	if strings.TrimSpace(result.EnvRoot) == "" {
		result.Status = "blocked"
		result.Comment = "project design system package invalid: execution environment root is missing"
		result.FailureReason = finalizeFailureAuditUnavailable
		return result, nil
	}

	binding, decodeErr := decodeV2TaskBinding(task)
	if decodeErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system package invalid: " + decodeErr.Error()
		result.FailureReason = finalizeFailureAuditUnavailable
		return result, nil
	}

	collectRoot := filepath.Join(result.EnvRoot, "output", "project-design-system")
	collected, collectErr := projectdesignsystem.CollectV2Directory(collectRoot, binding)
	if collectErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system package invalid: " + collectErr.Error()
		result.FailureReason = finalizeFailureAuditUnavailable
		return result, nil
	}
	if !collected.Audit.Passed {
		result.Status = "blocked"
		result.Comment = "project design system package failed static audit"
		result.FailureReason = finalizeFailureAuditUnavailable
		return result, nil
	}

	// Resolve the browser up front so an uninstalled browser fails the task
	// without spending the cost of standing up a loopback server.
	browserPath, resolveErr := deps.ResolveBrowserPath(deps.BrowserPath)
	if resolveErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview unavailable: " + resolveErr.Error()
		result.FailureReason = finalizeFailurePreviewMissing
		return result, nil
	}

	// Stand up the loopback server that serves the collected archive. The
	// prefix is a per-run unguessable token so another process racing on
	// the same loopback port (theoretically impossible since the listener
	// is exclusive, but defence in depth) cannot poison the verifier.
	prefix, prefixErr := randomLoopbackPrefix()
	if prefixErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview unavailable: " + prefixErr.Error()
		result.FailureReason = finalizeFailurePreviewMissing
		return result, nil
	}
	server, baseURL, listenErr := startLoopbackPreviewServer(collected.Archive, manifestForServer(collected.Manifest), collected.Manifest.PreviewTargets, prefix, deps.ServerBaseAddr, deps.ServerTimeout)
	if listenErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview unavailable: " + listenErr.Error()
		result.FailureReason = finalizeFailurePreviewMissing
		return result, nil
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	targets, previewErr := buildPreviewTargetURLs(collected.Manifest.PreviewTargets, baseURL, prefix)
	if previewErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview failed: " + previewErr.Error()
		result.FailureReason = finalizeFailurePreviewFailed
		return result, nil
	}

	verifier, newVerifierErr := deps.NewVerifier(browserPath, designpreview.DefaultPolicy())
	if newVerifierErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview failed: " + newVerifierErr.Error()
		result.FailureReason = finalizeFailurePreviewMissing
		return result, nil
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, deps.ServerTimeout)
	defer cancelVerify()
	if deps.OnPreview != nil {
		deps.OnPreview()
	}
	verification, verifyErr := verifier.Verify(verifyCtx, targets)
	if verifyErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview failed: " + verifyErr.Error()
		result.FailureReason = finalizeFailurePreviewFailed
		return result, nil
	}
	if !verification.Passed {
		result.Status = "blocked"
		result.Comment = "project design system preview did not pass"
		result.FailureReason = finalizeFailurePreviewFailed
		return result, nil
	}

	receipt, receiptErr := designpreview.NewReceipt(collected.Manifest.ContentDigest, verification)
	if receiptErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system preview receipt invalid: " + receiptErr.Error()
		result.FailureReason = finalizeFailurePreviewFailed
		return result, nil
	}

	// Upload the archive AFTER the preview so a transient upload failure
	// doesn't burn the user's rendered evidence. The archive bytes never
	// change between collect and upload (deterministic build), so a retry
	// from the server side would re-hash to the same digest.
	uploadCtx, cancelUpload := context.WithTimeout(ctx, deps.ServerTimeout)
	defer cancelUpload()
	upload, uploadErr := deps.Upload.UploadProjectDesignSystemPackage(uploadCtx, task.ID, collected.Manifest.ContentDigest, collected.Archive)
	if uploadErr != nil {
		result.Status = "blocked"
		result.Comment = "project design system package upload failed: " + uploadErr.Error()
		result.FailureReason = finalizeFailureUploadFailed
		return result, nil
	}

	result.ProjectDesignSystemArtifacts = nil
	result.ProjectDesignSystemPackage = &ProjectDesignSystemPackageReceipt{
		SchemaVersion: projectdesignsystem.PackageSchemaV2,
		ObjectKey:     upload.ObjectKey,
		ContentDigest: collected.Manifest.ContentDigest,
		ArtifactIndex: collected.Manifest.Files,
		Audit:         collected.Audit,
		Preview:       receipt,
	}
	return result, nil
}

// finalizeProjectDesignSystemResultFromDaemon wires the production finalize
// path with the daemon's design-preview browser config and the daemon's
// HTTP client. Tests should call finalizeProjectDesignSystemResult directly
// with their own finalizeDeps so they don't touch the daemon's state.
func (d *Daemon) finalizeProjectDesignSystemResultFromDaemon(ctx context.Context, task Task, result TaskResult) (TaskResult, error) {
	deps := finalizeDeps{
		BrowserPath:        d.designPreviewBrowserPath,
		ResolveBrowserPath: designpreview.ResolveBrowserPath,
		NewVerifier: func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(browserPath, policy)
		},
		Upload: d.client,
		Now:    time.Now,
	}
	return finalizeProjectDesignSystemResult(ctx, task, result, deps)
}

// isV2ProjectDesignSystemTask mirrors the execenv predicate so the daemon
// can decide whether to call the V2 finalize gate or the legacy attach
// path from a single helper. Both sides MUST agree on the marker — the
// execenv writes the marker, the daemon reads it.
func isV2ProjectDesignSystemTask(task Task) bool {
	if len(task.ProjectDesignSystemContext) == 0 {
		return false
	}
	var envelope struct {
		PackageSchema string `json:"package_schema"`
	}
	if err := jsonUnmarshal(task.ProjectDesignSystemContext, &envelope); err != nil {
		return false
	}
	return envelope.PackageSchema == projectdesignsystem.PackageSchemaV2
}

func isProjectDesignSystemRepositoryAnalysisTask(task Task) bool {
	if len(task.ProjectDesignSystemContext) == 0 {
		return false
	}
	var envelope struct {
		Type      string `json:"type"`
		Operation string `json:"operation"`
	}
	if err := jsonUnmarshal(task.ProjectDesignSystemContext, &envelope); err != nil {
		return false
	}
	return envelope.Type == "project_design_system_task" && envelope.Operation == "repository_analysis"
}

// decodeV2TaskBinding extracts the V2 PackageBinding fields the finalize
// gate needs from the task context. The binding shape is exactly what
// CollectV2Directory validates, so we reuse the same field names and let
// the package reject malformed bindings.
func decodeV2TaskBinding(task Task) (projectdesignsystem.PackageBinding, error) {
	var envelope struct {
		WorkspaceID         string `json:"workspace_id"`
		ProjectID           string `json:"project_id"`
		DesignSystemID      string `json:"project_design_system_id"`
		TaskID              string `json:"task_id"`
		AgentID             string `json:"agent_id"`
		Operation           string `json:"operation"`
		InputSnapshotSHA256 string `json:"input_snapshot_sha256"`
		BasePackageSHA256   string `json:"base_package_sha256"`
	}
	if err := jsonUnmarshal(task.ProjectDesignSystemContext, &envelope); err != nil {
		return projectdesignsystem.PackageBinding{}, fmt.Errorf("decode task context: %w", err)
	}
	if envelope.TaskID == "" {
		envelope.TaskID = task.ID
	}
	if envelope.AgentID == "" && task.Agent != nil {
		envelope.AgentID = task.Agent.ID
	}
	return projectdesignsystem.PackageBinding{
		WorkspaceID:         envelope.WorkspaceID,
		ProjectID:           envelope.ProjectID,
		DesignSystemID:      envelope.DesignSystemID,
		TaskID:              envelope.TaskID,
		AgentID:             envelope.AgentID,
		Operation:           envelope.Operation,
		InputSnapshotSHA256: envelope.InputSnapshotSHA256,
		BasePackageSHA256:   envelope.BasePackageSHA256,
	}, nil
}

// jsonUnmarshal is a thin wrapper that keeps the imports in this file
// minimal — the helpers around it only need encoding/json via a single
// chokepoint.
func jsonUnmarshal(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}

// randomLoopbackPrefix returns 16 random hex chars for the URL prefix. The
// unguessable prefix is defence in depth — the listener is already
// exclusive on 127.0.0.1:0 — but a hostile local process reading the
// preview server's URL out of /proc would still need to guess 64 bits
// before reaching the verifier.
func randomLoopbackPrefix() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate preview prefix: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// startLoopbackPreviewServer builds an *http.Server on 127.0.0.1:0 that
// serves the collected archive. The server is restricted to the loopback
// interface and the per-run prefix — anything outside fails closed with
// 404 — so even a bug in the verifier cannot reach the wider network.
//
// routes:
//
//	GET /<prefix>/manifest.json     → manifest bytes
//	GET /<prefix>/<archive entry>   → matching file from the archive
//
// Validated HTML preview targets (ui-kit/index.html and preview/*.html per
// the package's PreviewTargets list) get the trusted CSP header and the
// tokens.css + selection/measurement bridge injection. Everything else is
// served raw — only validated HTML targets carry the bridge and CSP, so
// a malformed file slipping into assets/ or fonts/ can't trick the
// verifier into executing arbitrary script.
func startLoopbackPreviewServer(archive []byte, manifest []byte, previewTargets []projectdesignsystem.PreviewTarget, prefix, bindAddr string, timeout time.Duration) (*http.Server, string, error) {
	files, err := openArchiveFiles(archive)
	if err != nil {
		return nil, "", fmt.Errorf("open preview archive: %w", err)
	}
	listener, err := net.Listen("tcp", bindAddr+":0")
	if err != nil {
		return nil, "", fmt.Errorf("bind preview server: %w", err)
	}
	validatedHTML := make(map[string]struct{}, len(previewTargets))
	for _, target := range previewTargets {
		if target.Kind != "ui_kit" && target.Kind != "preview" {
			continue
		}
		validatedHTML[strings.TrimPrefix(target.Path, "/")] = struct{}{}
	}
	bridgeScriptHash := sha256BridgeScriptHash()
	cspHeader := buildPreviewCSP(bridgeScriptHash)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		relative := strings.TrimPrefix(r.URL.Path, "/"+prefix+"/")
		if relative == "" || strings.Contains(relative, "..") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if relative == "manifest.json" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(manifest)
			return
		}
		contents, ok := files[relative]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, isHTML := validatedHTML[relative]; isHTML {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", cspHeader)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(injectBridgeAndTokens(contents, prefix))
			return
		}
		w.Header().Set("Content-Type", contentTypeForPath(relative))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(contents)
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: timeout,
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		IdleTimeout:       timeout,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	baseURL := "http://" + listener.Addr().String()
	return server, baseURL, nil
}

// selectionBridgeScript is the trusted selection/measurement bridge the
// preview verifier injects into validated HTML targets. It forwards
// every click on a `data-design-node-id` element to the parent window via
// postMessage so the platform can pair the rendered UI with its design
// contract. The script body MUST stay byte-stable: the SHA-256 hash
// below is what the CSP whitelists, and any drift here will silently
// brick the verifier. The bridge shape mirrors the legacy
// projectdesignsystem.BuildPreviewHTML helper that served the same role
// for the pre-V2 collector — re-derived here because that helper expects
// the legacy `ValidatedPackage` schema and cannot be invoked on V2
// archives.
var selectionBridgeScript = projectdesignsystem.RuntimePreviewBridgeScript("")

// sha256BridgeScriptHash returns the SHA-256 of the trusted bridge source,
// base64-encoded so it slots directly into a CSP `script-src 'sha256-…'`
// directive. Computed at call time so the package compiles without a
// pre-baked digest and a hash mismatch is caught at startup by the
// preview self-check rather than by a silent runtime CSP mismatch.
func sha256BridgeScriptHash() string {
	digest := sha256.Sum256([]byte(selectionBridgeScript))
	return base64.StdEncoding.EncodeToString(digest[:])
}

// buildPreviewCSP assembles the trusted CSP header applied to every
// validated HTML preview target. The directives match the brief verbatim:
//
//	default-src 'self' data:
//	script-src 'sha256-<bridge-hash>'
//	connect-src 'none'
//	object-src 'none'
//	frame-src 'none'
//	form-action 'none'
//	base-uri 'none'
//
// The script hash pins the trusted bridge so the verifier cannot be
// tricked into executing an inline script the Agent may have smuggled
// into the package, even when those bytes happen to live inside a
// validated HTML target.
func buildPreviewCSP(bridgeScriptHash string) string {
	return projectdesignsystem.RuntimePreviewCSPFromHash(bridgeScriptHash)
}

// injectBridgeAndTokens inserts the tokens.css stylesheet link and the
// trusted selection/measurement bridge into a validated HTML preview
// target. It is the runtime analogue of the legacy BuildPreviewHTML
// helper and only runs on files whose path is in the package's
// PreviewTargets list — every other archive entry is served verbatim so
// a malformed asset cannot accidentally pick up the bridge.
//
// Insertion strategy: tokens.css is injected immediately before `</head>`
// when present, otherwise prepended to the document. The bridge script
// is injected immediately before `</body>` when present, otherwise
// appended. The CSP header on the response makes the script-src hash
// the only allowed script origin, so even an inline `<script>evil()</script>`
// the agent left in the fragment will not execute — the verifier
// receives a CSP-locked page that only runs the trusted bridge.
//
// Every preview target lives exactly one directory below the package root.
// The shared runtime helper injects ../tokens.css so both this loopback route
// and the authenticated user-facing preview resolve the same Token file.
func injectBridgeAndTokens(html []byte, prefix string) []byte {
	_ = prefix // retained for compatibility with focused tests and older callers
	return projectdesignsystem.InjectRuntimePreviewHTML(html, selectionBridgeScript)
}

func contentTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	default:
		return "application/octet-stream"
	}
}

// openArchiveFiles unpacks the archive into a name → bytes map the
// preview server can serve. We re-use the same ReadCloser path that
// v2_archive.go's readAndIndexV2Archive uses; the files are tiny (each
// Preview target is at most 16 MiB and the whole archive is capped at 64
// MiB) so an in-memory map is acceptable here.
func openArchiveFiles(archive []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Name == "manifest.json" {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", file.Name, err)
		}
		contents, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.Name, err)
		}
		out[file.Name] = contents
	}
	return out, nil
}

// manifestForServer re-renders the canonical manifest the archive already
// carries so the loopback server can serve it under a stable URL. The
// archive build always writes manifest.json (see v2_archive.go's
// CollectV2Directory), so this is a thin re-decode / re-encode to keep
// the implementation honest.
func manifestForServer(manifest projectdesignsystem.ManifestV2) []byte {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil
	}
	return encoded
}

// buildPreviewTargetURLs maps the verified Preview targets to the
// loopback URLs the verifier will hit. We sort to keep the verifier's
// iteration order stable so the test fake's target URL assertions
// (which check `target.URL == v.targetURLs[i]`) are reproducible.
func buildPreviewTargetURLs(targets []projectdesignsystem.PreviewTarget, baseURL, prefix string) ([]designpreview.TargetURL, error) {
	if len(targets) == 0 {
		return nil, errors.New("V2 package has no preview targets")
	}
	// Manifest order is the signed package contract and is also what the server
	// revalidates on completion. Preserve it through browser verification; sorting
	// here made otherwise-valid multi-target receipts disagree with the manifest.
	out := make([]designpreview.TargetURL, 0, len(targets))
	for _, target := range targets {
		if target.Kind != "ui_kit" && target.Kind != "preview" {
			return nil, fmt.Errorf("V2 preview target %q has unsupported kind %q", target.ID, target.Kind)
		}
		path := strings.TrimPrefix(target.Path, "/")
		u, err := url.Parse(baseURL + "/" + prefix + "/" + path)
		if err != nil {
			return nil, fmt.Errorf("build preview URL for %q: %w", target.ID, err)
		}
		out = append(out, designpreview.TargetURL{
			Target: designpreview.Target{
				Kind: target.Kind,
				ID:   target.ID,
				Path: target.Path,
			},
			URL: u.String(),
		})
	}
	return out, nil
}

// attachProjectDesignSystemArtifacts keeps the legacy inline three-file
// path working for package-producing non-V2 tasks. It is a no-op for V2
// tasks (the new finalize gate handles those) and repository analysis
// (which returns structured context rather than package files).
//
// The legacy collector remains live for package-producing non-V2
// project-design-system tasks that still flow through the daemon.
// Tests for this function live in
// project_design_system_artifacts_test.go.
func attachProjectDesignSystemArtifacts(task Task, result TaskResult) TaskResult {
	if len(task.ProjectDesignSystemContext) == 0 || result.Status != "completed" {
		return result
	}
	if isProjectDesignSystemRepositoryAnalysisTask(task) || isV2ProjectDesignSystemTask(task) {
		// V2 tasks produce the package via finalizeProjectDesignSystemResult;
		// repository analysis produces no package. Neither path may attach
		// the legacy inline payload.
		return result
	}
	if strings.TrimSpace(result.EnvRoot) == "" {
		result.Status = "blocked"
		result.Comment = "project design system artifacts invalid: execution environment root is missing"
		result.FailureReason = "project_design_system_artifacts_invalid"
		return result
	}

	outputDir := filepath.Join(result.EnvRoot, "output", "project-design-system")
	artifacts, err := readProjectDesignSystemArtifacts(outputDir)
	if err != nil {
		result.Status = "blocked"
		result.Comment = "project design system artifacts invalid: " + err.Error()
		result.FailureReason = "project_design_system_artifacts_invalid"
		result.ProjectDesignSystemArtifacts = nil
		return result
	}
	result.ProjectDesignSystemArtifacts = &artifacts
	return result
}

// readProjectDesignSystemArtifacts is the legacy three-file collector. It
// keeps the Open Design supervisor's contract alive for any non-V2 task
// that still flows through the daemon; the V2 finalize path bypasses this
// function entirely. Tests live in
// project_design_system_artifacts_test.go.
func readProjectDesignSystemArtifacts(outputDir string) (ProjectDesignSystemArtifacts, error) {
	root, err := filepath.Abs(outputDir)
	if err != nil {
		return ProjectDesignSystemArtifacts{}, fmt.Errorf("resolve output directory: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return ProjectDesignSystemArtifacts{}, fmt.Errorf("inspect output directory: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return ProjectDesignSystemArtifacts{}, errors.New("output directory must be a real directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ProjectDesignSystemArtifacts{}, fmt.Errorf("resolve output directory links: %w", err)
	}

	artifacts := ProjectDesignSystemArtifacts{}
	total := 0
	files := []struct {
		name  string
		limit int
		set   func(string)
	}{
		{name: "DESIGN.md", limit: projectdesignsystem.MaxDesignMDBytes, set: func(value string) { artifacts.DesignMD = value }},
		{name: "tokens.css", limit: projectdesignsystem.MaxTokensCSSBytes, set: func(value string) { artifacts.TokensCSS = value }},
		{name: "components.html", limit: projectdesignsystem.MaxComponentsHTMLBytes, set: func(value string) { artifacts.ComponentsHTML = value }},
	}
	for _, artifact := range files {
		path := filepath.Join(root, artifact.name)
		info, err := os.Lstat(path)
		if err != nil {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("inspect %s: %w", artifact.name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("%s must be a regular file", artifact.name)
		}
		if info.Size() > int64(artifact.limit) {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("%s exceeds its size limit", artifact.name)
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("resolve %s: %w", artifact.name, err)
		}
		if !pathWithinDirectory(resolvedRoot, resolvedPath) {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("%s resolves outside the output directory", artifact.name)
		}

		file, err := os.Open(path)
		if err != nil {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("open %s: %w", artifact.name, err)
		}
		openedInfo, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("inspect opened %s: %w", artifact.name, statErr)
		}
		if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("%s changed during collection", artifact.name)
		}
		contents, readErr := io.ReadAll(io.LimitReader(file, int64(artifact.limit)+1))
		closeErr := file.Close()
		if readErr != nil {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("read %s: %w", artifact.name, readErr)
		}
		if closeErr != nil {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("close %s: %w", artifact.name, closeErr)
		}
		if len(contents) > artifact.limit {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("%s exceeds its size limit", artifact.name)
		}
		total += len(contents)
		if total > projectdesignsystem.MaxAggregateBytes {
			return ProjectDesignSystemArtifacts{}, fmt.Errorf("artifact package exceeds its aggregate size limit")
		}
		artifact.set(string(contents))
	}
	return artifacts, nil
}

// pathWithinDirectory is a small helper kept around for the legacy
// three-file collector. Returns true when path resolves inside root
// (i.e. is not an absolute or upward escape from root).
func pathWithinDirectory(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// progressLogger is a tiny slog adapter used by the finalize path so the
// tests can swap in a noop logger without importing slog directly.
// Kept as a typed alias so future observers can be added in one place.
type progressLogger = *slog.Logger

var _ progressLogger = slog.Default()
