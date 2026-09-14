package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
)

// restoreProjectDesignSystemBaseArchive directly migrates the proven Open
// Design / Design Document base flow: download the pinned complete archive,
// revalidate its binding, digest and artifact index, then expose every file to
// the Agent as a read-only base tree.
func (d *Daemon) restoreProjectDesignSystemBaseArchive(ctx context.Context, task Task, envRoot, workDir string) error {
	if len(task.ProjectDesignSystemContext) == 0 {
		return nil
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if err := json.Unmarshal(task.ProjectDesignSystemContext, &taskContext); err != nil {
		return fmt.Errorf("decode project design system task context: %w", err)
	}
	if taskContext.Type != service.ProjectDesignSystemTaskContextType ||
		(taskContext.Operation != service.ProjectDesignSystemAdjust && taskContext.Operation != service.ProjectDesignSystemRegenerate) ||
		len(taskContext.BasePackage) == 0 {
		return nil
	}
	var reference projectdesignsystem.BasePackageReference
	if err := json.Unmarshal(taskContext.BasePackage, &reference); err != nil {
		return fmt.Errorf("decode project design system base package reference: %w", err)
	}
	if reference.Schema == "" {
		// Backward compatibility for already-queued inline-base tasks. execenv
		// materialized their three files during Prepare.
		return nil
	}
	if err := projectdesignsystem.ValidateBasePackageReference(reference); err != nil {
		return fmt.Errorf("validate project design system base package reference: %w", err)
	}
	if d.client == nil {
		return errors.New("project design system base archive client is unavailable")
	}
	archive, err := d.client.DownloadProjectDesignSystemBaseArchive(ctx, task.ID, reference)
	if err != nil {
		return fmt.Errorf("download project design system base archive: %w", err)
	}
	files, err := projectdesignsystem.ReadV2BaseArchive(archive, reference)
	if err != nil {
		return fmt.Errorf("validate project design system base archive: %w", err)
	}
	if err := execenv.ExtractProjectDesignSystemBase(envRoot, workDir, files); err != nil {
		return fmt.Errorf("extract project design system base archive: %w", err)
	}
	return nil
}
