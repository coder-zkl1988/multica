package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/designdocument"
)

const (
	designAuditDefaultContext = ".agent_context/design_document/context/task.json"
	designAuditOutputDirEnv   = "MULTICA_OUTPUT_DIR"
	designAuditTaskIDEnv      = "MULTICA_TASK_ID"
)

var designCmd = &cobra.Command{
	Use:   "design",
	Short: "Work with design documents",
}

// designAuditCmd lets the agent run the design document package gate itself,
// inside the task, before it exits. The daemon runs the same gate once after
// the agent is gone, and any rule it trips ends the run without a draft — so
// this is the only place the agent can see every failing rule and fix it.
var designAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check a design document package against the platform gate before finishing the task",
	Long: `Run the platform's design document package gate — binding, collection,
static audit and (by default) the headless Chromium preview — against the
package you wrote, without uploading anything.

Inside a design document task every default comes from the environment:
the package directory is $MULTICA_OUTPUT_DIR, the task context is
.agent_context/design_document/context/task.json under the working directory,
and the task id is $MULTICA_TASK_ID. Exit status 1 means the platform would
reject the package as it is; the output names every failing rule and file.`,
	RunE: runDesignAudit,
}

func init() {
	registerDesignAuditFlags(designAuditCmd.Flags())
	designCmd.AddCommand(designAuditCmd)
}

// registerDesignAuditFlags declares the audit flags on a flag set. Tests build
// a fresh command per case from it so flag values cannot leak between cases.
func registerDesignAuditFlags(flags *pflag.FlagSet) {
	flags.String("dir", "", "Package directory (default: $"+designAuditOutputDirEnv+")")
	flags.String("context", designAuditDefaultContext, "Design document task context JSON")
	flags.String("task-id", "", "Task the package is bound to (default: $"+designAuditTaskIDEnv+")")
	flags.Bool("preview", true, "Render every prototype page in a headless Chromium, as the platform does")
	flags.Duration("timeout", 60*time.Second, "Time budget for the preview")
	flags.String("output", "text", "Output format: text or json")
}

var errDesignAuditFailed = errors.New("design audit failed: the platform would reject this package")

func runDesignAudit(cmd *cobra.Command, _ []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	contextPath, _ := cmd.Flags().GetString("context")
	taskID, _ := cmd.Flags().GetString("task-id")
	preview, _ := cmd.Flags().GetBool("preview")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	output, _ := cmd.Flags().GetString("output")

	if strings.TrimSpace(dir) == "" {
		dir = os.Getenv(designAuditOutputDirEnv)
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("--dir is required outside a design document task ($%s is not set)", designAuditOutputDirEnv)
	}
	if strings.TrimSpace(taskID) == "" {
		taskID = os.Getenv(designAuditTaskIDEnv)
	}
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("--task-id is required outside a design document task ($%s is not set)", designAuditTaskIDEnv)
	}
	rawContext, err := os.ReadFile(contextPath)
	if err != nil {
		return fmt.Errorf("read design document task context %s: %w", contextPath, err)
	}
	if !json.Valid(rawContext) {
		return fmt.Errorf("design document task context %s is not valid JSON", contextPath)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve package directory: %w", err)
	}

	report := daemon.PreflightDesignDocumentPackage(context.Background(), daemon.Task{
		ID:                    strings.TrimSpace(taskID),
		DesignDocumentContext: rawContext,
	}, absDir, daemon.DesignDocumentPreflightOptions{
		Preview: preview,
		Timeout: timeout,
	})

	if output == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("encode report: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	} else {
		fmt.Fprint(cmd.OutOrStdout(), formatDesignAuditReport(report, absDir))
	}
	if !report.Passed {
		return errDesignAuditFailed
	}
	return nil
}

// formatDesignAuditReport renders the report for an agent reading a terminal:
// the verdict first, then every error with its rule and file, then what to do.
func formatDesignAuditReport(report daemon.DesignDocumentPreflightReport, dir string) string {
	var b strings.Builder
	if report.Passed {
		fmt.Fprintf(&b, "design audit: PASS — %s\n", dir)
		fmt.Fprintf(&b, "  package %s, %d file(s), %d preview page(s)\n", report.ContentDigest, len(report.Files), len(report.PreviewTargets))
		if report.PreviewRan {
			b.WriteString("  static audit and Chromium preview both passed; the platform gate accepts this package as written.\n")
		} else {
			b.WriteString("  static audit passed; the preview was skipped (--preview=false), so the browser gate is still unproven.\n")
		}
		writeDesignAuditWarnings(&b, report.Audit)
		return b.String()
	}

	fmt.Fprintf(&b, "design audit: FAIL at %s (%s) — %s\n", report.Stage, report.FailureReason, dir)
	if report.Audit != nil && len(report.Audit.Diagnostics) > 0 {
		for _, diagnostic := range report.Audit.Diagnostics {
			if diagnostic.Severity != designdocument.DiagnosticError {
				continue
			}
			b.WriteString("  error " + formatDesignAuditDiagnostic(diagnostic) + "\n")
		}
		writeDesignAuditWarnings(&b, report.Audit)
	} else if report.Error != "" {
		b.WriteString("  " + report.Error + "\n")
	}
	if report.Preview != nil {
		for _, target := range report.Preview.Targets {
			if target.Passed {
				continue
			}
			code := target.FailureCode
			if code == "" {
				code = "preview_failed"
			}
			fmt.Fprintf(&b, "  preview %s (%s): console errors %d, failed resources %d, outbound requests %d, visible text %d\n",
				code, target.Target.Path, target.ConsoleErrorCount, target.FailedResourceCount, target.OutboundRequestCount, target.VisibleTextLength)
		}
	}
	b.WriteString("Fix every error above and run `multica design audit` again. The platform runs this same gate once after you exit; a package that fails it ends the run with no draft.\n")
	return b.String()
}

func writeDesignAuditWarnings(b *strings.Builder, audit *designdocument.AuditReport) {
	if audit == nil {
		return
	}
	for _, diagnostic := range audit.Diagnostics {
		if diagnostic.Severity == designdocument.DiagnosticError {
			continue
		}
		b.WriteString("  " + string(diagnostic.Severity) + " " + formatDesignAuditDiagnostic(diagnostic) + "\n")
	}
}

func formatDesignAuditDiagnostic(diagnostic designdocument.Diagnostic) string {
	if diagnostic.Path == "" {
		return diagnostic.Code + ": " + diagnostic.Message
	}
	return diagnostic.Code + " (" + diagnostic.Path + "): " + diagnostic.Message
}
