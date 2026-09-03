package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/designpreview"
)

// The preflight is the gate the agent runs on itself before it exits, so it
// must reach the same verdict the finalize gate reaches on the same package:
// a package that passes here and then fails after the agent is gone would be
// worse than no preflight at all.
func TestPreflightDesignDocumentPackagePassesTheFinalizeFixture(t *testing.T) {
	envRoot := t.TempDir()
	packageDir := stageDesignDocumentPackage(t, envRoot)
	task := designDocumentTask("task-1", stageDesignDocumentTaskContext(t))
	verifier := &designDocumentVerifier{}

	report := PreflightDesignDocumentPackage(context.Background(), task, packageDir, DesignDocumentPreflightOptions{
		Preview:            true,
		Timeout:            10 * time.Second,
		ResolveBrowserPath: func(string) (string, error) { return "/dev/null/chromium", nil },
		NewVerifier: func(string, designpreview.Policy) (designpreview.Verifier, error) {
			return verifier, nil
		},
	})
	if !report.Passed || report.Stage != "passed" || !report.PreviewRan {
		t.Fatalf("report = %+v, want a passing report with the preview run", report)
	}
	if report.Audit == nil || !report.Audit.Passed {
		t.Fatalf("audit = %+v, want a passing audit report", report.Audit)
	}
	if report.Preview == nil || !report.Preview.Passed || len(report.Preview.Targets) != len(report.PreviewTargets) {
		t.Fatalf("preview = %+v, want every preview target verified", report.Preview)
	}
	if !strings.HasPrefix(report.ContentDigest, "sha256:") || len(report.Files) == 0 {
		t.Fatalf("report digest/files = %q/%d, want the collected package identity", report.ContentDigest, len(report.Files))
	}

	// Same package, same binding: the finalize gate must agree.
	uploader := newDesignDocumentUploader()
	finalized, err := finalizeDesignDocumentResult(context.Background(), task,
		TaskResult{Status: "completed", EnvRoot: envRoot}, designDocumentDeps(uploader, &designDocumentVerifier{}))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "completed" || finalized.DesignDocumentPackage == nil {
		t.Fatalf("finalize result = %+v, want completed with a receipt", finalized)
	}
	if finalized.DesignDocumentPackage.ContentDigest != report.ContentDigest {
		t.Fatalf("finalize digest %s != preflight digest %s", finalized.DesignDocumentPackage.ContentDigest, report.ContentDigest)
	}
}

// Without the preview the report can only vouch for the static audit, and it
// must say so rather than read as a full pass.
func TestPreflightDesignDocumentPackageWithoutPreviewStopsAtTheAudit(t *testing.T) {
	envRoot := t.TempDir()
	packageDir := stageDesignDocumentPackage(t, envRoot)
	task := designDocumentTask("task-1", stageDesignDocumentTaskContext(t))

	report := PreflightDesignDocumentPackage(context.Background(), task, packageDir, DesignDocumentPreflightOptions{
		Preview: false,
		ResolveBrowserPath: func(string) (string, error) {
			t.Fatal("the browser must not be resolved when the preview is off")
			return "", nil
		},
	})
	if !report.Passed || report.PreviewRan || report.Preview != nil {
		t.Fatalf("report = %+v, want a static pass with no preview", report)
	}
}

// The report carries every audit error, not just the first one the task
// comment would show, and uses the finalize gate's failure vocabulary.
func TestPreflightDesignDocumentPackageReportsEveryAuditError(t *testing.T) {
	envRoot := t.TempDir()
	packageDir := stageDesignDocumentPackage(t, envRoot)
	writeDesignDocumentFile(t, packageDir, "prototype/app.js",
		"const params = new URLSearchParams(window.location.search);\nfetch('/api/orders');\n")
	task := designDocumentTask("task-1", stageDesignDocumentTaskContext(t))

	report := PreflightDesignDocumentPackage(context.Background(), task, packageDir, DesignDocumentPreflightOptions{
		Preview: true,
		ResolveBrowserPath: func(string) (string, error) {
			t.Fatal("a failing audit must short-circuit before the browser")
			return "", nil
		},
	})
	if report.Passed || report.Stage != "audit" || report.FailureReason != finalizeFailureDesignDocumentAuditFailed {
		t.Fatalf("report = %+v, want an audit-stage failure", report)
	}
	if report.Audit == nil {
		t.Fatal("report has no audit diagnostics")
	}
	codes := make(map[string]bool)
	for _, diagnostic := range report.Audit.Diagnostics {
		codes[diagnostic.Code] = true
	}
	for _, want := range []string{"prototype_script_navigation_forbidden", "prototype_script_forbidden_api"} {
		if !codes[want] {
			t.Fatalf("diagnostics %v do not include %s", codes, want)
		}
	}
	if !strings.Contains(report.Error, "static audit") {
		t.Fatalf("error = %q, want the finalize gate's audit wording", report.Error)
	}
}

// A malformed binding fails before the package directory is read, as in the
// finalize gate, and a context that is not a design document context is
// refused rather than audited against an empty binding.
func TestPreflightDesignDocumentPackageRejectsBadContexts(t *testing.T) {
	envRoot := t.TempDir()
	packageDir := stageDesignDocumentPackage(t, envRoot)

	broken := designDocumentTaskContextWith(t, func(context map[string]any) {
		context["design_system_digest"] = strings.Repeat("e", 64) // bare hex, not a sha256: reference
	})
	report := PreflightDesignDocumentPackage(context.Background(), designDocumentTask("task-1", broken), packageDir,
		DesignDocumentPreflightOptions{})
	// The binding decodes; the collector is what rejects the digest form, so
	// the failure surfaces at collection with the binding message.
	if report.Passed || report.Stage != "collect" || !strings.Contains(report.Error, "design system digest") {
		t.Fatalf("report = %+v, want the collector's binding rejection", report)
	}

	notDesign, err := json.Marshal(map[string]any{"type": "quick_create"})
	if err != nil {
		t.Fatal(err)
	}
	report = PreflightDesignDocumentPackage(context.Background(), designDocumentTask("task-1", notDesign), packageDir,
		DesignDocumentPreflightOptions{})
	if report.Passed || report.Stage != "binding" || report.FailureReason != finalizeFailureDesignDocumentBindingInvalid {
		t.Fatalf("report = %+v, want a binding-stage refusal", report)
	}

	report = PreflightDesignDocumentPackage(context.Background(), designDocumentTask("task-1", stageDesignDocumentTaskContext(t)),
		filepath.Join(envRoot, "does-not-exist"), DesignDocumentPreflightOptions{})
	if report.Passed || report.Stage != "collect" || report.FailureReason != finalizeFailureDesignDocumentCollectFailed {
		t.Fatalf("report = %+v, want a collect-stage failure for a missing package", report)
	}
}
