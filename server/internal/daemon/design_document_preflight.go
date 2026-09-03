package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
)

// DesignDocumentPreflightOptions configures PreflightDesignDocumentPackage.
type DesignDocumentPreflightOptions struct {
	// Preview renders every prototype page in a real browser after a passing
	// static audit, exactly as the finalize gate does. Off, the report stops at
	// the static audit.
	Preview bool
	// BrowserPath is an explicit Chromium executable; empty resolves the
	// installed default the daemon would use.
	BrowserPath string
	// Timeout bounds the loopback preview server and the browser run.
	Timeout time.Duration
	// Test seams; nil selects the production implementation.
	ResolveBrowserPath func(explicitPath string) (string, error)
	NewVerifier        func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error)
}

// DesignDocumentPreflightReport is the verdict PreflightDesignDocumentPackage
// returns. Stage names the gate the package reached, FailureReason uses the
// same vocabulary the finalize gate stamps onto a blocked task, and Audit /
// Preview carry the full diagnostics so the caller can fix every finding at
// once rather than the first one the task comment would have shown.
type DesignDocumentPreflightReport struct {
	Passed bool `json:"passed"`
	// Stage is "binding", "collect", "audit" or "preview" when the package
	// failed there, and "passed" otherwise.
	Stage         string `json:"stage"`
	FailureReason string `json:"failure_reason,omitempty"`
	Error         string `json:"error,omitempty"`
	// PreviewRan reports whether the browser gate was exercised; a passing
	// report without it only proves the static audit.
	PreviewRan     bool                                `json:"preview_ran"`
	Binding        *designdocument.PackageBinding      `json:"binding,omitempty"`
	ContentDigest  string                              `json:"content_digest,omitempty"`
	Files          []designdocument.ArtifactIndexEntry `json:"files,omitempty"`
	PreviewTargets []designdocument.PreviewTarget      `json:"preview_targets,omitempty"`
	Audit          *designdocument.AuditReport         `json:"audit,omitempty"`
	Preview        *designpreview.Verification         `json:"preview,omitempty"`
}

// PreflightDesignDocumentPackage runs the design document package gate the
// daemon applies after an agent exits — binding, collection, static audit and
// (optionally) the browser preview — against a package directory, without
// uploading anything or touching task state.
//
// It exists so the agent can run the gate itself before it finishes (through
// `multica design audit`). The finalize gate runs once, after the agent is
// gone, and any rule it trips ends the run with no draft; the agent has no
// other way to learn which rule a finished package breaks. The stages and
// their verdicts are the same functions finalizeDesignDocumentResult uses,
// so a package that passes here is the package the platform will accept.
func PreflightDesignDocumentPackage(
	ctx context.Context,
	task Task,
	packageDir string,
	opts DesignDocumentPreflightOptions,
) DesignDocumentPreflightReport {
	if opts.ResolveBrowserPath == nil {
		opts.ResolveBrowserPath = designpreview.ResolveBrowserPath
	}
	if opts.NewVerifier == nil {
		opts.NewVerifier = func(browserPath string, policy designpreview.Policy) (designpreview.Verifier, error) {
			return designpreview.NewChromiumVerifierWithPolicy(browserPath, policy)
		}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}

	report := DesignDocumentPreflightReport{}
	if !isDesignDocumentTask(task) {
		return failPreflight(report, "binding", finalizeFailureDesignDocumentBindingInvalid,
			"task context is not a "+designdocument.PackageSchemaV1+" design document task context")
	}
	binding, err := DecodeDesignDocumentTaskBinding(task)
	if err != nil {
		return failPreflight(report, "binding", finalizeFailureDesignDocumentBindingInvalid,
			"design document package binding invalid: "+err.Error())
	}
	report.Binding = &binding

	collected, collectErr := designdocument.CollectDirectory(packageDir, binding)
	if collectErr != nil {
		if designDocumentAuditVerdict(collected) {
			audit := collected.Audit
			report.Audit = &audit
			return failPreflight(report, "audit", finalizeFailureDesignDocumentAuditFailed,
				"design document package failed static audit: "+designDocumentAuditSummary(collected.Audit))
		}
		return failPreflight(report, "collect", finalizeFailureDesignDocumentCollectFailed,
			"design document package invalid: "+collectErr.Error())
	}
	audit := collected.Audit
	report.Audit = &audit
	report.ContentDigest = collected.Manifest.ContentDigest
	report.Files = collected.Manifest.Files
	report.PreviewTargets = collected.Manifest.PreviewTargets
	if !audit.Passed {
		return failPreflight(report, "audit", finalizeFailureDesignDocumentAuditFailed,
			"design document package failed static audit: "+designDocumentAuditSummary(collected.Audit))
	}
	if !opts.Preview {
		report.Passed = true
		report.Stage = "passed"
		return report
	}

	report.PreviewRan = true
	browserPath, resolveErr := opts.ResolveBrowserPath(opts.BrowserPath)
	if resolveErr != nil {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewMissing,
			"design document preview unavailable: "+resolveErr.Error())
	}
	prefix, prefixErr := randomLoopbackPrefix()
	if prefixErr != nil {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewMissing,
			"design document preview unavailable: "+prefixErr.Error())
	}
	server, baseURL, listenErr := startDesignDocumentPreviewServer(
		collected.Archive,
		collected.Manifest.Files,
		collected.Manifest.PreviewTargets,
		prefix,
		"127.0.0.1",
		opts.Timeout,
	)
	if listenErr != nil {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewMissing,
			"design document preview unavailable: "+listenErr.Error())
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	targets, targetErr := buildDesignDocumentTargetURLs(collected.Manifest.PreviewTargets, baseURL, prefix)
	if targetErr != nil {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewFailed,
			"design document preview failed: "+targetErr.Error())
	}
	verifier, verifierErr := opts.NewVerifier(browserPath, designpreview.DefaultPolicy())
	if verifierErr != nil {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewMissing,
			"design document preview unavailable: "+verifierErr.Error())
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, opts.Timeout)
	defer cancelVerify()
	verification, verifyErr := verifier.Verify(verifyCtx, targets)
	if verifyErr != nil {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewFailed,
			"design document preview failed: "+verifyErr.Error())
	}
	report.Preview = &verification
	if !verification.Passed {
		return failPreflight(report, "preview", finalizeFailureDesignDocumentPreviewFailed,
			"design document preview did not pass: "+previewFailureSummary(verification))
	}
	report.Passed = true
	report.Stage = "passed"
	return report
}

func failPreflight(report DesignDocumentPreflightReport, stage, reason, message string) DesignDocumentPreflightReport {
	report.Passed = false
	report.Stage = stage
	report.FailureReason = reason
	report.Error = message
	return report
}

// previewFailureSummary names the first page the browser rejected and why, so
// a failing preview is actionable without opening the whole verification.
func previewFailureSummary(verification designpreview.Verification) string {
	for _, target := range verification.Targets {
		if target.Passed {
			continue
		}
		code := target.FailureCode
		if code == "" {
			code = "preview_failed"
		}
		return code + " (" + target.Target.Path + ")"
	}
	return errors.New("no target passed").Error()
}
