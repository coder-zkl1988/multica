package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
)

// finalizeFailureDesignDocument* enumerate the stable failure reasons the
// page-design finalize pass can stamp onto a blocked TaskResult. They are the
// platform contract for "why did this page-design task not produce a draft
// revision" — the UI and the test suite both read them, so adding a new value
// requires updating both surfaces.
//
// The set is deliberately finer-grained than the design-system gate's: the
// design document error matrix (spec section 13.2) shows collection failure
// ("产物不完整，不进入 Audit/Preview") and audit failure ("安全或一致性失败,
// 不进入 Preview") as two different rows with two different page behaviours, so
// the daemon must not collapse them into one reason.
const (
	finalizeFailureDesignDocumentBindingInvalid = "design_document_binding_invalid"
	finalizeFailureDesignDocumentCollectFailed  = "design_document_collect_failed"
	finalizeFailureDesignDocumentAuditFailed    = "design_document_audit_failed"
	finalizeFailureDesignDocumentPreviewMissing = "design_document_preview_unavailable"
	finalizeFailureDesignDocumentPreviewFailed  = "design_document_preview_failed"
	finalizeFailureDesignDocumentUploadFailed   = "design_document_upload_failed"
)

// designDocumentUploadClient is the subset of the daemon's API client that
// finalizeDesignDocumentResult needs to upload the collected archive. Defining
// it as an interface lets tests inject a stub without spinning up an HTTP
// server, and keeps the gate independent of the object-storage endpoint, which
// is a separate slice.
type designDocumentUploadClient interface {
	UploadDesignDocumentPackage(ctx context.Context, taskID, contentDigest string, archive []byte) (DesignDocumentPackageUpload, error)
}

// DesignDocumentPackageUpload is the object-storage receipt the server returns
// for an uploaded design document archive.
type DesignDocumentPackageUpload struct {
	ObjectKey     string `json:"object_key"`
	ContentDigest string `json:"content_digest"`
}

// DesignDocumentPackageReceipt is the daemon-side carrier for a finalized
// page-design result. It carries the bare minimum the server needs to publish a
// draft revision: the uploaded archive's object key, the package's content
// digest, the artifact index for server-side cross-checks, the static audit
// report, and the designpreview.Receipt that proves a real browser rendered
// every prototype document without CSP violations or outbound requests.
type DesignDocumentPackageReceipt struct {
	SchemaVersion string                              `json:"schema_version"`
	ObjectKey     string                              `json:"object_key"`
	ContentDigest string                              `json:"content_digest"`
	ArtifactIndex []designdocument.ArtifactIndexEntry `json:"artifact_index"`
	Audit         designdocument.AuditReport          `json:"audit"`
	Preview       designpreview.Receipt               `json:"preview"`
}

// designDocumentFinalizeDeps bundles the runtime knobs the finalize path uses
// so tests can override them without touching global state.
type designDocumentFinalizeDeps struct {
	BrowserPath        string
	ResolveBrowserPath func(explicitPath string) (string, error)
	NewVerifier        func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error)
	Upload             designDocumentUploadClient
	ServerTimeout      time.Duration
	ServerBaseAddr     string // loopback IP for the preview server; defaults to 127.0.0.1
	// OnPreview is invoked immediately before the verifier runs. Tests use it to
	// record stage ordering; production callers can leave it nil.
	OnPreview func()
}

// finalizeDesignDocumentResult is the page-design finalization gate, the
// design-document sibling of finalizeProjectDesignSystemResult. It runs after
// the provider (Agent) has exited and only for tasks whose context carries the
// multica.design-document/v1 package schema marker.
//
// Ordering — every stage runs strictly in this sequence, and the success fake
// in the tests observes the same ordering:
//
//  1. Decode and validate the task binding from the design document task
//     context. A malformed binding fails with design_document_binding_invalid
//     before the agent's output directory is even read.
//  2. Collect the agent's output directory. CollectDirectory also runs the
//     static audit, so this single stage produces two distinct outcomes: a
//     structural collection failure (design_document_collect_failed) and an
//     audit verdict on an assembled package (design_document_audit_failed).
//     Either short-circuits BEFORE the browser is touched and before any
//     upload is attempted.
//  3. Resolve the configured browser. An unresolved browser FAILS the task with
//     design_document_preview_unavailable. There is no skip and no degraded
//     path — spec section 12.3 keeps the "no downgrade" semantics for page
//     designs, so a host without Chrome produces no draft revision at all.
//  4. Serve the collected archive on 127.0.0.1:0 under a per-run unguessable
//     prefix and run the design preview verifier against every target from
//     designdocument.DiscoverPreviewTargets. The server is shut down on EVERY
//     return path (success, preview failure, upload failure, cancellation).
//  5. Upload the archive AFTER the preview so a transient upload failure does
//     not burn the user's rendered evidence. The archive bytes never change
//     between collect and upload (deterministic build), so a retry re-hashes to
//     the same digest.
//  6. Annotate the TaskResult with the receipt.
//
// No stage may be skipped or inferred from the Agent's stdout / last response.
func finalizeDesignDocumentResult(
	ctx context.Context,
	task Task,
	result TaskResult,
	deps designDocumentFinalizeDeps,
) (TaskResult, error) {
	if deps.ResolveBrowserPath == nil {
		deps.ResolveBrowserPath = designpreview.ResolveBrowserPath
	}
	if deps.NewVerifier == nil {
		deps.NewVerifier = func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(browserPath, policy)
		}
	}
	if deps.ServerTimeout == 0 {
		deps.ServerTimeout = 60 * time.Second
	}
	if deps.ServerBaseAddr == "" {
		deps.ServerBaseAddr = "127.0.0.1"
	}
	if deps.Upload == nil {
		return result, errors.New("finalizeDesignDocumentResult: upload client is required")
	}

	if strings.TrimSpace(string(task.DesignDocumentContext)) == "" {
		return result, errors.New("finalizeDesignDocumentResult: task has no design document context")
	}
	if result.Status != "completed" {
		// A non-completed upstream result (e.g. blocked by the agent itself) is
		// not subject to this gate. Surface it unchanged so the upstream failure
		// reason keeps its semantics.
		return result, nil
	}

	// Stage 1: the task binding. It is decoded before anything touches the
	// filesystem, because a package collected against the wrong binding could
	// never become a revision anyway.
	binding, decodeErr := DecodeDesignDocumentTaskBinding(task)
	if decodeErr != nil {
		return blockDesignDocumentResult(result,
			"design document package binding invalid: "+decodeErr.Error(),
			finalizeFailureDesignDocumentBindingInvalid), nil
	}

	// A missing execution environment root means the agent's output cannot be
	// located at all, which the error matrix treats as an incomplete package
	// rather than a browser or upload problem.
	if strings.TrimSpace(result.EnvRoot) == "" {
		return blockDesignDocumentResult(result,
			"design document package invalid: execution environment root is missing",
			finalizeFailureDesignDocumentCollectFailed), nil
	}

	// Stage 2: collect + static audit.
	collectRoot := filepath.Join(result.EnvRoot, "output", "design-document")
	collected, collectErr := designdocument.CollectDirectory(collectRoot, binding)
	if collectErr != nil {
		if designDocumentAuditVerdict(collected) {
			return blockDesignDocumentResultWithDiagnostics(result,
				"design document package failed static audit: "+designDocumentAuditSummary(collected.Audit),
				finalizeFailureDesignDocumentAuditFailed, collected.Audit.Diagnostics), nil
		}
		return blockDesignDocumentResult(result,
			"design document package invalid: "+collectErr.Error(),
			finalizeFailureDesignDocumentCollectFailed), nil
	}
	if !collected.Audit.Passed {
		// Defensive: CollectDirectory reports a failing audit through its error,
		// so reaching here means the contract changed underneath us. Fail closed.
		return blockDesignDocumentResultWithDiagnostics(result,
			"design document package failed static audit: "+designDocumentAuditSummary(collected.Audit),
			finalizeFailureDesignDocumentAuditFailed, collected.Audit.Diagnostics), nil
	}

	// Stage 3: resolve the browser up front so an uninstalled browser fails the
	// task without spending the cost of standing up a loopback server.
	browserPath, resolveErr := deps.ResolveBrowserPath(deps.BrowserPath)
	if resolveErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview unavailable: "+resolveErr.Error(),
			finalizeFailureDesignDocumentPreviewMissing), nil
	}

	// Stage 4: serve the archive on loopback under a per-run unguessable prefix
	// and render every prototype document.
	prefix, prefixErr := randomLoopbackPrefix()
	if prefixErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview unavailable: "+prefixErr.Error(),
			finalizeFailureDesignDocumentPreviewMissing), nil
	}
	server, baseURL, listenErr := startDesignDocumentPreviewServer(
		collected.Archive,
		collected.Manifest.Files,
		collected.Manifest.PreviewTargets,
		prefix,
		deps.ServerBaseAddr,
		deps.ServerTimeout,
	)
	if listenErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview unavailable: "+listenErr.Error(),
			finalizeFailureDesignDocumentPreviewMissing), nil
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	targets, targetErr := buildDesignDocumentTargetURLsWithDeckSlides(
		collected.Manifest.PreviewTargets,
		len(collected.Manifest.Slides),
		baseURL,
		prefix,
	)
	if targetErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview failed: "+targetErr.Error(),
			finalizeFailureDesignDocumentPreviewFailed), nil
	}

	verifier, newVerifierErr := deps.NewVerifier(browserPath, designpreview.DefaultPolicy())
	if newVerifierErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview unavailable: "+newVerifierErr.Error(),
			finalizeFailureDesignDocumentPreviewMissing), nil
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, deps.ServerTimeout)
	defer cancelVerify()
	if deps.OnPreview != nil {
		deps.OnPreview()
	}
	verification, verifyErr := verifier.Verify(verifyCtx, targets)
	if verifyErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview failed: "+verifyErr.Error(),
			finalizeFailureDesignDocumentPreviewFailed), nil
	}
	if !verification.Passed {
		return blockDesignDocumentResult(result,
			"design document preview did not pass",
			finalizeFailureDesignDocumentPreviewFailed), nil
	}
	receipt, receiptErr := designpreview.NewReceipt(collected.Manifest.ContentDigest, verification)
	if receiptErr != nil {
		return blockDesignDocumentResult(result,
			"design document preview receipt invalid: "+receiptErr.Error(),
			finalizeFailureDesignDocumentPreviewFailed), nil
	}

	// Stage 5: upload, only now that rendered evidence exists.
	uploadCtx, cancelUpload := context.WithTimeout(ctx, deps.ServerTimeout)
	defer cancelUpload()
	upload, uploadErr := deps.Upload.UploadDesignDocumentPackage(uploadCtx, task.ID, collected.Manifest.ContentDigest, collected.Archive)
	if uploadErr != nil {
		return blockDesignDocumentResult(result,
			"design document package upload failed: "+uploadErr.Error(),
			finalizeFailureDesignDocumentUploadFailed), nil
	}

	// Stage 6: attach the receipt.
	result.DesignDocumentPackage = &DesignDocumentPackageReceipt{
		SchemaVersion: designdocument.PackageSchemaV1,
		ObjectKey:     upload.ObjectKey,
		ContentDigest: collected.Manifest.ContentDigest,
		ArtifactIndex: collected.Manifest.Files,
		Audit:         collected.Audit,
		Preview:       receipt,
	}
	return result, nil
}

// blockDesignDocumentResult stamps one blocked outcome onto the result. Every
// blocked return path goes through here so a blocked result can never keep a
// half-built receipt from an earlier attempt.
func blockDesignDocumentResult(result TaskResult, comment, failureReason string) TaskResult {
	return blockDesignDocumentResultWithDiagnostics(result, comment, failureReason, nil)
}

// blockDesignDocumentResultWithDiagnostics is the audit-failure form: it also
// carries every error-severity finding so the server can persist the full rule
// list in the document's last_error rather than only the first line of the
// comment. Warnings are filtered out — they never rejected the package.
func blockDesignDocumentResultWithDiagnostics(result TaskResult, comment, failureReason string, diagnostics []designdocument.Diagnostic) TaskResult {
	result.Status = "blocked"
	result.Comment = comment
	result.FailureReason = failureReason
	result.FailureDiagnostics = designdocument.ErrorDiagnostics(diagnostics)
	result.DesignDocumentPackage = nil
	return result
}

// designDocumentAuditVerdict reports whether a CollectDirectory error is the
// audit's verdict on an assembled package rather than a structural collection
// failure. CollectDirectory returns the populated audit report only on an audit
// failure; every structural failure (undeclared path, size limit, link, invalid
// archive) returns the zero CollectedPackage.
func designDocumentAuditVerdict(collected designdocument.CollectedPackage) bool {
	return collected.Audit.SchemaVersion == designdocument.AuditSchemaV1 && !collected.Audit.Passed
}

// designDocumentAuditSummary renders every error diagnostic, one per line, so
// the blocked comment names every failing rule and file instead of only the
// first. A package can break several independent rules at once; naming one
// produces a rerun that fixes it and trips on the next, which is the failure
// loop this summary exists to end. Warnings are excluded — they never gate.
func designDocumentAuditSummary(report designdocument.AuditReport) string {
	lines := make([]string, 0, len(report.Diagnostics))
	for _, diagnostic := range designdocument.ErrorDiagnostics(report.Diagnostics) {
		lines = append(lines, designDocumentDiagnosticLine(diagnostic))
	}
	if len(lines) == 0 {
		return "audit reported no error diagnostic"
	}
	return strings.Join(lines, "\n")
}

func designDocumentDiagnosticLine(diagnostic designdocument.Diagnostic) string {
	if diagnostic.Path == "" {
		return diagnostic.Code + ": " + diagnostic.Message
	}
	return diagnostic.Code + " (" + diagnostic.Path + "): " + diagnostic.Message
}

// isDesignDocumentTask mirrors the enqueue-time marker so the daemon can decide
// whether a task belongs to the page-design gate from a single helper. Both the
// type and the package schema must agree — a design system task carries its own
// schema marker and must never reach this gate.
func isDesignDocumentTask(task Task) bool {
	if len(task.DesignDocumentContext) == 0 {
		return false
	}
	var envelope struct {
		Type          string `json:"type"`
		PackageSchema string `json:"package_schema"`
	}
	if err := jsonUnmarshal(task.DesignDocumentContext, &envelope); err != nil {
		return false
	}
	return envelope.Type == "design_document_task" && envelope.PackageSchema == designdocument.PackageSchemaV1
}

// DecodeDesignDocumentTaskBinding extracts the PackageBinding fields the gate
// needs from service.DesignDocumentTaskContext. The binding shape is exactly
// what CollectDirectory validates, so the field names are reused and the
// package is left to reject malformed bindings.
//
// Two identities are taken from the task rather than the envelope:
//
//   - TaskID is always the task the daemon is finalizing. The context does not
//     carry one, and taking it from the run means a context can never bind a
//     package to a different task.
//   - RevisionID identifies the revision this run produces. The server does not
//     pin one at enqueue time yet, and a task produces exactly one revision, so
//     the task identity is the deterministic revision identity. An explicit
//     revision_id in the context wins as soon as the server starts sending one.
//
// Exported because it is one side of a two-sided contract: the server handler
// package derives the same binding from the task context it wrote, and the two
// once disagreed on RevisionID, which rejected every package at upload. The
// handler's cross-boundary test asserts both sides stay identical (DC-055).
func DecodeDesignDocumentTaskBinding(task Task) (designdocument.PackageBinding, error) {
	var envelope struct {
		WorkspaceID         string `json:"workspace_id"`
		ProjectID           string `json:"project_id"`
		ProjectResourceID   string `json:"project_resource_id"`
		IssueID             string `json:"issue_id"`
		DesignDocumentID    string `json:"design_document_id"`
		RevisionID          string `json:"revision_id"`
		AgentID             string `json:"agent_id"`
		Platform            string `json:"platform"`
		InputSnapshotSHA256 string `json:"input_snapshot_sha256"`
		BaseContentDigest   string `json:"base_content_digest"`
		DesignSystemDigest  string `json:"design_system_digest"`
	}
	if err := jsonUnmarshal(task.DesignDocumentContext, &envelope); err != nil {
		return designdocument.PackageBinding{}, fmt.Errorf("decode task context: %w", err)
	}
	if strings.TrimSpace(task.ID) == "" {
		return designdocument.PackageBinding{}, errors.New("task has no ID")
	}
	if envelope.RevisionID == "" {
		envelope.RevisionID = task.ID
	}
	if envelope.AgentID == "" && task.Agent != nil {
		envelope.AgentID = task.Agent.ID
	}
	binding := designdocument.PackageBinding{
		WorkspaceID:         envelope.WorkspaceID,
		ProjectID:           envelope.ProjectID,
		ProjectResourceID:   envelope.ProjectResourceID,
		IssueID:             envelope.IssueID,
		DesignDocumentID:    envelope.DesignDocumentID,
		RevisionID:          envelope.RevisionID,
		TaskID:              task.ID,
		AgentID:             envelope.AgentID,
		Platform:            envelope.Platform,
		InputSnapshotSHA256: envelope.InputSnapshotSHA256,
		BaseRevisionSHA256:  envelope.BaseContentDigest,
		DesignSystemSHA256:  envelope.DesignSystemDigest,
	}
	return binding, nil
}

// startDesignDocumentPreviewServer builds an *http.Server on 127.0.0.1:0 that
// serves the collected archive. The server is restricted to the loopback
// interface and the per-run prefix — anything outside fails closed with 404 —
// so even a bug in the verifier cannot reach the wider network.
//
// route:
//
//	GET /<prefix>/<archive entry> → matching file from the archive
//
// Every archive entry is served at its own package path, which is the whole
// resolution contract: a prototype references its siblings and assets with
// package-relative paths (`../assets/mark.svg` from `prototype/index.html`,
// `../../assets/mark.svg` from `prototype/orders/list.html`), and the audit
// resolved those references against the same package paths. Serving the archive
// under one flat prefix therefore makes every audited reference resolve to the
// file the audit checked. Nothing is rewritten and nothing is injected, so the
// bytes the browser renders are byte-identical to the bytes the audit read and
// the bytes that get uploaded.
//
// This is deliberately unlike the design-system preview server, which injects a
// tokens.css link and a trusted selection bridge into its validated HTML. A
// design document prototype carries its own CSS and its own behaviour; an
// injected bridge would also have to be pinned by a CSP script hash, and a
// hash-pinned script-src silently disables 'unsafe-inline', which would block
// every inline <script> the prototype contract explicitly allows. The
// design-system bridge could not run here anyway: it calls parent.postMessage,
// which the design document script audit rejects outright.
//
// manifest.json is NOT served. It is not an accepted agent package path, no
// prototype may reference it, and connect-src 'none' means no prototype could
// fetch it — so exposing it would only widen the surface.
func startDesignDocumentPreviewServer(
	archive []byte,
	index []designdocument.ArtifactIndexEntry,
	previewTargets []designdocument.PreviewTarget,
	prefix, bindAddr string,
	timeout time.Duration,
) (*http.Server, string, error) {
	files, err := openArchiveFiles(archive)
	if err != nil {
		return nil, "", fmt.Errorf("open design document preview archive: %w", err)
	}
	// The audited media type is authoritative: it is the type the collector
	// classified the entry as, so serving anything else would let the browser
	// interpret a file differently from the way the audit parsed it.
	mediaTypes := make(map[string]string, len(index))
	for _, entry := range index {
		mediaTypes[entry.Path] = entry.MediaType
	}
	documents := make(map[string]struct{}, len(previewTargets))
	for _, target := range previewTargets {
		if target.Kind != "prototype_entry" && target.Kind != "prototype_page" {
			continue
		}
		documents[strings.TrimPrefix(target.Path, "/")] = struct{}{}
	}
	listener, err := net.Listen("tcp", bindAddr+":0")
	if err != nil {
		return nil, "", fmt.Errorf("bind design document preview server: %w", err)
	}
	cspHeader := buildDesignDocumentPreviewCSP()
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
		contents, ok := files[relative]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		contentType, known := mediaTypes[relative]
		if !known || contentType == "" {
			contentType = contentTypeForPath(relative)
		}
		w.Header().Set("Content-Type", contentType)
		// nosniff keeps the browser from re-interpreting an asset as a document:
		// the audit parsed each file as the type its extension declares, and the
		// renderer must agree.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, isDocument := documents[relative]; isDocument {
			w.Header().Set("Content-Security-Policy", cspHeader)
		}
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
	return server, "http://" + listener.Addr().String(), nil
}

// buildDesignDocumentPreviewCSP assembles the CSP applied to every prototype
// document served by the preview server:
//
//	default-src 'self'
//	script-src 'self' 'unsafe-inline'
//	style-src 'self' 'unsafe-inline'
//	img-src 'self'
//	font-src 'self'
//	connect-src 'none'
//	worker-src 'none'
//	object-src 'none'
//	frame-src 'none'
//	form-action 'none'
//	base-uri 'none'
//
// Why it differs from the design-system CSP, which pins one script hash:
//
//   - A design system UI Kit is a static visual contract whose package forbids
//     all script, so exactly one trusted script runs and a hash pins it. A
//     design document prototype has to prove a flow works, so its own scripts
//     must run. 'self' covers `<script src>` (the loopback origin serves only
//     this package's audited files), and 'unsafe-inline' covers the `<script>`
//     blocks the package contract explicitly allows. Pinning a hash or a nonce
//     instead would be worse than useless: CSP ignores 'unsafe-inline' as soon
//     as a hash or nonce is present, so every inline prototype script would be
//     blocked, each block would raise a console error, and RequireConsoleClean
//     would fail every prototype that puts behaviour in a `<script>` block.
//   - 'unsafe-inline' is not the layer that keeps the prototype safe. The static
//     audit is: it rejects fetch / XMLHttpRequest / WebSocket / EventSource /
//     sendBeacon / Service Worker / Worker, eval and the Function constructor,
//     dynamic import, computed global lookups, inline on* handlers, and any
//     absolute remote URL anywhere in prototype source. This header is the
//     runtime backstop for whatever static analysis cannot decide.
//   - connect-src 'none' is the directive that actually enforces "the prototype
//     runs with the network switched off": every fetch, XHR, WebSocket,
//     EventSource and sendBeacon is refused at the browser, and the verifier
//     records the attempt as an outbound request and a console error, so a
//     package that tries to reach the network fails the gate rather than
//     silently degrading.
//   - worker-src / object-src / frame-src 'none' remove the remaining ways to
//     run code or embed a document outside the audited source set; form-action
//     'none' stops a form from posting anywhere; base-uri 'none' stops a <base>
//     element from re-pointing every relative reference in the document.
//   - img-src / font-src stay at 'self' rather than allowing data:, because the
//     audit only accepts package-relative asset references — a data: URL never
//     passes collection, so the header has no reason to permit one.
func buildDesignDocumentPreviewCSP() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self'",
		"font-src 'self'",
		"connect-src 'none'",
		"worker-src 'none'",
		"object-src 'none'",
		"frame-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
	}, "; ")
}

// buildDesignDocumentTargetURLs maps the package's Preview targets onto the
// loopback URLs the verifier will open. The manifest order is preserved rather
// than re-sorted: DiscoverPreviewTargets puts the prototype entry first on
// purpose so the gate opens the document root before its sub pages.
//
// This form keeps the pre-deck call sites working: no target declares a deck
// slide count, so the verifier performs no deck protocol sweep.
func buildDesignDocumentTargetURLs(targets []designdocument.PreviewTarget, baseURL, prefix string) ([]designpreview.TargetURL, error) {
	return buildDesignDocumentTargetURLsWithDeckSlides(targets, 0, baseURL, prefix)
}

// designDocumentPreviewTargetEntryKind mirrors the kind
// designdocument.DiscoverPreviewTargets assigns to prototype/index.html. A deck
// is a single-file prototype, so only that target carries the deck sweep.
const designDocumentPreviewTargetEntryKind = "prototype_entry"

// buildDesignDocumentTargetURLsWithDeckSlides is the deck-aware form of
// buildDesignDocumentTargetURLs. deckSlides is the manifest slide projection;
// it is attached to the prototype entry target alone and stays zero for a
// non-deck package or a manifest written before deck support (nil Slides).
func buildDesignDocumentTargetURLsWithDeckSlides(targets []designdocument.PreviewTarget, deckSlides int, baseURL, prefix string) ([]designpreview.TargetURL, error) {
	if len(targets) == 0 {
		return nil, errors.New("design document package has no preview targets")
	}
	if deckSlides < 0 {
		return nil, errors.New("design document package has an invalid deck slide count")
	}
	out := make([]designpreview.TargetURL, 0, len(targets))
	for _, target := range targets {
		kind, ok := designDocumentPreviewTargetKind(target.Kind)
		if !ok {
			return nil, fmt.Errorf("design document preview target %q has unsupported kind %q", target.ID, target.Kind)
		}
		path := strings.TrimPrefix(target.Path, "/")
		parsed, err := url.Parse(baseURL + "/" + prefix + "/" + path)
		if err != nil {
			return nil, fmt.Errorf("build preview URL for %q: %w", target.ID, err)
		}
		targetDeckSlides := 0
		if deckSlides > 0 && target.Kind == designDocumentPreviewTargetEntryKind {
			targetDeckSlides = deckSlides
		}
		out = append(out, designpreview.TargetURL{
			Target: designpreview.Target{
				Kind: kind,
				ID:   target.ID,
				Path: target.Path,
			},
			URL:        parsed.String(),
			DeckSlides: targetDeckSlides,
		})
	}
	return out, nil
}

// designDocumentPreviewTargetKind delegates to the shared mapping in
// designdocument, which both this gate and the server-side receipt validation
// must apply. Keeping a second copy here is how the two would drift.
func designDocumentPreviewTargetKind(kind string) (string, bool) {
	return designdocument.PreviewTargetKind(kind)
}

// finalizeDesignDocumentResultFromDaemon wires the gate with the daemon's real
// browser config, verifier and API client. Tests call
// finalizeDesignDocumentResult directly with their own deps so they never
// touch daemon state.
func (d *Daemon) finalizeDesignDocumentResultFromDaemon(ctx context.Context, task Task, result TaskResult) (TaskResult, error) {
	deps := designDocumentFinalizeDeps{
		BrowserPath:        d.designPreviewBrowserPath,
		ResolveBrowserPath: designpreview.ResolveBrowserPath,
		NewVerifier: func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(browserPath, policy)
		},
		Upload: d.client,
	}
	return finalizeDesignDocumentResult(ctx, task, result, deps)
}
