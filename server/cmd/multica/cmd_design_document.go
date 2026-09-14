package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/spf13/cobra"
)

var designDocumentCmd = newDesignDocumentCommand()

func newDesignDocumentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "design-document",
		Short:  "Design Document task helpers",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate the current task's staged Design Document package",
		Args:  cobra.NoArgs,
		RunE:  runDesignDocumentValidate,
	})
	return cmd
}

func runDesignDocumentValidate(cmd *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve task work directory: %w", err)
	}
	report, err := validateDesignDocumentPackage(
		workDir,
		os.Getenv("MULTICA_OUTPUT_DIR"),
		os.Getenv("MULTICA_TASK_ID"),
	)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
		return fmt.Errorf("encode Design Document validation report: %w", err)
	}
	if !report.Passed {
		return errSilent
	}
	return nil
}

func validateDesignDocumentPackage(workDir, outputDir, taskID string) (designdocument.AuditReport, error) {
	if strings.TrimSpace(taskID) == "" {
		return designdocument.AuditReport{}, fmt.Errorf("Design Document validation requires MULTICA_TASK_ID")
	}
	if strings.TrimSpace(outputDir) == "" {
		return designdocument.AuditReport{}, fmt.Errorf("Design Document validation requires MULTICA_OUTPUT_DIR")
	}
	contextPath := filepath.Join(workDir, ".agent_context", "design_document", "context", "task.json")
	contextJSON, err := os.ReadFile(contextPath)
	if err != nil {
		return designdocument.AuditReport{}, fmt.Errorf("read Design Document task context: %w", err)
	}
	binding, err := daemon.DecodeDesignDocumentTaskBinding(daemon.Task{
		ID:                    taskID,
		DesignDocumentContext: contextJSON,
	})
	if err != nil {
		return designdocument.AuditReport{}, fmt.Errorf("decode Design Document task binding: %w", err)
	}
	collected, collectErr := designdocument.CollectDirectory(outputDir, binding)
	if collectErr != nil && collected.Audit.SchemaVersion == "" {
		return designdocument.AuditReport{}, fmt.Errorf("collect Design Document package: %w", collectErr)
	}
	return collected.Audit, nil
}
