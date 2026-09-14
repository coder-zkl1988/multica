package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestProjectDesignSystemAdjustmentRestoresCompleteReadOnlyBaseArchive(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)
	collected, err := collectV2ForTest(t, envRoot)
	if err != nil {
		t.Fatalf("collect V2 base: %v", err)
	}
	reference := projectdesignsystem.BasePackageReference{
		Schema: projectdesignsystem.BasePackageReferenceSchema, Slot: "saved",
		ContentDigest: collected.Manifest.ContentDigest, SourceTaskID: collected.Manifest.Binding.TaskID,
		Binding: collected.Manifest.Binding,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/adjust-task/project-design-system/base-package" {
			t.Fatalf("base request path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", projectdesignsystem.BasePackageArchiveContentType)
		w.Header().Set(projectdesignsystem.BasePackageDigestHeader, reference.ContentDigest)
		w.Header().Set(projectdesignsystem.BasePackageSlotHeader, reference.Slot)
		w.Header().Set(projectdesignsystem.BasePackageSourceTaskHeader, reference.SourceTaskID)
		_, _ = w.Write(collected.Archive)
	}))
	t.Cleanup(server.Close)

	baseJSON, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	contextJSON, err := json.Marshal(service.ProjectDesignSystemTaskContext{
		Type: service.ProjectDesignSystemTaskContextType, Operation: service.ProjectDesignSystemAdjust,
		WorkspaceID: collected.Manifest.Binding.WorkspaceID, ProjectID: collected.Manifest.Binding.ProjectID,
		ProjectDesignSystemID: collected.Manifest.Binding.DesignSystemID, AgentID: collected.Manifest.Binding.AgentID,
		PackageSchema: projectdesignsystem.PackageSchemaV2, BasePackage: baseJSON,
		InputSnapshotSHA256: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		BasePackageSHA256:   reference.ContentDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "adjust-task", ProjectDesignSystemContext: contextJSON}
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir()}, client: NewClient(server.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx := execenv.TaskContextForEnv{ProjectDesignSystemContext: string(contextJSON)}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: d.cfg.WorkspacesRoot, WorkspaceID: "workspace", TaskID: task.ID,
		Provider: "opencode", Task: ctx,
	}, d.logger)
	if err != nil {
		t.Fatalf("prepare environment: %v", err)
	}
	t.Cleanup(func() { _ = execenv.RestoreV2SidecarWritability(env.WorkDir) })

	if err := d.restoreProjectDesignSystemBaseArchive(context.Background(), task, env.RootDir, env.WorkDir); err != nil {
		t.Fatalf("restore complete base: %v", err)
	}
	baseDir := filepath.Join(env.WorkDir, ".agent_context", "project_design_system", "base")
	for _, entry := range collected.Manifest.Files {
		info, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Fatalf("base entry %s missing: %v", entry.Path, err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("base entry %s is writable", entry.Path)
		}
	}
	if _, err := os.Stat(filepath.Join(baseDir, "manifest.json")); err != nil {
		t.Fatalf("complete base did not include manifest.json: %v", err)
	}
}
