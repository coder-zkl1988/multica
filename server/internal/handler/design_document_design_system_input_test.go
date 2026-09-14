package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type designDocumentDesignSystemDownloadFixture struct {
	TaskID string
	Agent  string
	Key    string
	Store  *mockStorage
}

func createDesignDocumentDesignSystemDownloadTask(t *testing.T, agentID, runtimeID, projectID, resourceID string, system db.ProjectDesignSystem, saved db.ProjectDesignSystemPackage, digest string, mutate func(*service.DesignDocumentTaskContext)) string {
	t.Helper()
	contextValue := service.DesignDocumentTaskContext{
		Type: service.DesignDocumentTaskContextType, Operation: service.DesignDocumentGenerate,
		WorkspaceID: testWorkspaceID, ProjectID: projectID, ProjectResourceID: resourceID, AgentID: agentID,
		DesignSystemDigest: digest,
		Input: service.DesignDocumentTaskInput{
			SchemaVersion: service.DesignDocumentInputSchema, RepositoryGrounding: service.DesignDocumentGroundingPending,
			DesignSystem: &service.DesignDocumentDesignSystemReference{ContentDigest: digest},
		},
		ExecutionReady: true,
		DesignContext: json.RawMessage(mustMarshalDesignSystemContext(t, service.ResolvedDesignContext{
			Version: service.DesignContextVersion, ProjectID: projectID,
			Source: service.DesignContextSourceCloudSavedRepository,
			Digest: digest,
			Package: &service.SavedProjectDesignContext{
				Scope: service.DesignContextScopeRepository, ProjectID: projectID, ProjectResourceID: resourceID,
				DesignSystemID: uuidToString(system.ID), SavedPackageID: uuidToString(saved.ID),
				ArchiveObjectKey: saved.ArchiveObjectKey.String,
				PackageSchema:    saved.PackageSchema,
				V2Manifest:       append(json.RawMessage(nil), saved.Manifest...),
				V2ArtifactIndex:  artifactIndexFromRaw(saved.ArtifactIndex),
			},
		})),
	}
	if mutate != nil {
		mutate(&contextValue)
	}
	raw, err := json.Marshal(contextValue)
	if err != nil {
		t.Fatal(err)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context, started_at)
		VALUES ($1,$2,'running',0,$3,now()) RETURNING id
	`, agentID, runtimeID, raw).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=$1`, taskID) })
	return taskID
}

func mustMarshalDesignSystemContext(t *testing.T, value service.ResolvedDesignContext) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func performDesignDocumentDesignSystemDownload(t *testing.T, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	request := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/design-document/design-system", nil, testWorkspaceID, "design-system-download")
	request = withURLParam(request, "taskId", taskID)
	recorder := httptest.NewRecorder()
	testHandler.DownloadDesignDocumentDesignSystem(recorder, request)
	return recorder
}

func TestDownloadDesignDocumentDesignSystemServesOnlyPinnedArchive(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Design system download")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	system, saved, archive, digest := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")
	taskID := createDesignDocumentDesignSystemDownloadTask(t, agentID, runtimeID, uuidToString(projectID), resourceID, system, saved, digest, nil)

	download := performDesignDocumentDesignSystemDownload(t, taskID)
	if download.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", download.Code, download.Body.String())
	}
	if download.Header().Get(nativePackageDigestHeader) != digest || download.Header().Get("Content-Type") != "application/zip" ||
		download.Header().Get("Cache-Control") != "no-store" || download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers=%v", download.Header())
	}
	if string(download.Body.Bytes()) != string(archive) || strings.Contains(download.Body.String(), saved.ArchiveObjectKey.String) {
		t.Fatal("download did not return the exact archive without leaking its object key")
	}
}

func TestDownloadDesignDocumentDesignSystemRejectsMissingProvenanceAndChangedArchive(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Design system download failures")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	system, saved, archive, digest := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")

	missingTask := createDesignDocumentDesignSystemDownloadTask(t, agentID, runtimeID, uuidToString(projectID), resourceID, system, saved, digest, func(contextValue *service.DesignDocumentTaskContext) {
		contextValue.DesignContext = nil
		contextValue.Input.DesignSystem = nil
	})
	if download := performDesignDocumentDesignSystemDownload(t, missingTask); download.Code != http.StatusNotFound || strings.Contains(download.Body.String(), saved.ArchiveObjectKey.String) {
		t.Fatalf("missing provenance download=%d body=%s", download.Code, download.Body.String())
	}

	taskID := createDesignDocumentDesignSystemDownloadTask(t, agentID, runtimeID, uuidToString(projectID), resourceID, system, saved, digest, nil)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system_package
		SET archive_object_key='later/unrelated.zip', integrity_sha256=repeat('9', 64), package_schema='legacy'
		WHERE id=$1
	`, saved.ID); err != nil {
		t.Fatal(err)
	}
	afterOverwrite := performDesignDocumentDesignSystemDownload(t, taskID)
	if afterOverwrite.Code != http.StatusOK || afterOverwrite.Header().Get(nativePackageDigestHeader) != digest ||
		string(afterOverwrite.Body.Bytes()) != string(archive) || strings.Contains(afterOverwrite.Body.String(), saved.ArchiveObjectKey.String) {
		t.Fatalf("after saved-slot overwrite download=%d body-bytes=%d headers=%v", afterOverwrite.Code, afterOverwrite.Body.Len(), afterOverwrite.Header())
	}
	storage, ok := testHandler.Storage.(*mockStorage)
	if !ok {
		t.Fatalf("test storage type %T does not support archive mutation", testHandler.Storage)
	}
	storage.mu.Lock()
	storage.files[saved.ArchiveObjectKey.String] = []byte("changed")
	storage.mu.Unlock()
	if download := performDesignDocumentDesignSystemDownload(t, taskID); download.Code != http.StatusConflict || strings.Contains(download.Body.String(), saved.ArchiveObjectKey.String) {
		t.Fatalf("changed archive download=%d body=%s", download.Code, download.Body.String())
	}
}
