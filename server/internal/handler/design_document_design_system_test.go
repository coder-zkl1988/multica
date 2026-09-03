package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DC-060: design systems are workspace platform material, so the home composer
// may pin one to a page design. A bundled catalogue system is inlined into the
// frozen input rather than resolved as a saved package — it ships DESIGN.md and
// tokens.css but no validated components package, and the task context must not
// pretend otherwise.
func TestCreateDesignDocumentPinsABuiltinDesignSystem(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Design document design system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id":            projectID,
		"agent_id":              agentID,
		"platform":              "web",
		"brief":                 "客户列表页，支持筛选与批量操作。",
		"builtin_design_system": "agentic",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("CreateDesignDocument: status = %d, body = %s", response.Code, response.Body.String())
	}
	var created DesignDocumentResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document WHERE id = $1`, parseUUID(created.ID))
	})

	var inputJSON, taskContextJSON []byte
	var activeTaskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		SELECT d.input_snapshot, task.context, task.id
		FROM design_document d, agent_task_queue task
		WHERE d.id = $1 AND task.id = d.active_task_id
	`, parseUUID(created.ID)).Scan(&inputJSON, &taskContextJSON, &activeTaskID); err != nil {
		t.Fatalf("load frozen input/task context: %v", err)
	}

	// The choice is frozen with the rest of the inputs, so a regeneration
	// reruns under the same system.
	var input map[string]any
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		t.Fatalf("decode input snapshot: %v", err)
	}
	if input["builtin_design_system"] != "agentic" {
		t.Fatalf("input snapshot lost the chosen design system: %#v", input)
	}

	var taskContext struct {
		DesignContext service.ResolvedDesignContext `json:"design_context"`
	}
	if err := json.Unmarshal(taskContextJSON, &taskContext); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	design := taskContext.DesignContext
	if design.Source != service.DesignContextSourceBuiltinCatalogue {
		t.Fatalf("design context source = %q, want the builtin catalogue", design.Source)
	}
	if design.Package != nil {
		t.Fatal("a catalogue system must not be presented as a validated saved package")
	}
	if design.Builtin == nil || design.Builtin.Slug != "agentic" {
		t.Fatalf("design context builtin = %#v", design.Builtin)
	}
	if design.Builtin.DesignMarkdown == "" || design.Builtin.TokensCSS == "" {
		t.Fatal("catalogue content was not inlined into the frozen context")
	}
	// Digest pins the exact bytes, so a later bundle update cannot silently
	// change what this run designed under.
	if !strings.HasPrefix(design.Digest, "sha256:") || len(design.Digest) != len("sha256:")+64 {
		t.Fatalf("design context digest = %q, want a sha256:<hex> reference", design.Digest)
	}

	// The digest is also the binding the finished package is judged against,
	// on the daemon at collection and on the server at completion. A digest in
	// any other form than the one the package contract validates rejected the
	// agent's finished package with "invalid project design system digest" —
	// after a full run — so the binding this context yields must be accepted
	// by the real collector, not just look like a digest.
	var fullContext service.DesignDocumentTaskContext
	if err := json.Unmarshal(taskContextJSON, &fullContext); err != nil {
		t.Fatalf("decode full task context: %v", err)
	}
	if fullContext.DesignSystemDigest != design.Digest {
		t.Fatalf("design_system_digest = %q, want the design context digest %q", fullContext.DesignSystemDigest, design.Digest)
	}
	binding := designDocumentBindingFromContext(fullContext, db.AgentTaskQueue{ID: activeTaskID, Context: taskContextJSON})
	fixture := copyDesignDocumentFixture(t)
	// The agent copies the pinned digest from task.json into coverage.json,
	// where the audit compares it with the binding; the fixture carries a
	// placeholder digest, so stamp the real one the way a run would.
	setDesignDocumentFixtureDesignSystemDigest(t, fixture, design.Digest)
	if _, err := designdocument.CollectDirectory(fixture, binding); err != nil {
		t.Fatalf("the package contract rejects the binding of a run pinned to a catalogue system: %v", err)
	}
}

// setDesignDocumentFixtureDesignSystemDigest rewrites the design system digest
// the fixture's coverage.json references, so a collection can be run against a
// binding pinned to a specific system rather than the fixture's placeholder.
func setDesignDocumentFixtureDesignSystemDigest(t *testing.T, fixture, digest string) {
	t.Helper()
	coveragePath := filepath.Join(fixture, "coverage.json")
	raw, err := os.ReadFile(coveragePath)
	if err != nil {
		t.Fatalf("read fixture coverage: %v", err)
	}
	var coverage map[string]any
	if err := json.Unmarshal(raw, &coverage); err != nil {
		t.Fatalf("decode fixture coverage: %v", err)
	}
	consistency, ok := coverage["design_system_consistency"].(map[string]any)
	if !ok {
		t.Fatalf("fixture coverage has no design_system_consistency object: %#v", coverage["design_system_consistency"])
	}
	consistency["design_system_sha256"] = digest
	updated, err := json.Marshal(coverage)
	if err != nil {
		t.Fatalf("encode fixture coverage: %v", err)
	}
	if err := os.WriteFile(coveragePath, updated, 0o644); err != nil {
		t.Fatalf("write fixture coverage: %v", err)
	}
}

// One design system, or none: accepting both would leave the agent to guess
// which visual language actually governs the run.
func TestCreateDesignDocumentRejectsTwoDesignSystems(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Ambiguous design system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id":            projectID,
		"agent_id":              agentID,
		"platform":              "web",
		"brief":                 "客户列表页。",
		"design_system_id":      "8f14e45f-ceea-467a-9575-4a5b0f6d2f6f",
		"builtin_design_system": "agentic",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
	var failure map[string]any
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if failure["code"] != "design_system_ambiguous" {
		t.Fatalf("error code = %#v, want design_system_ambiguous", failure["code"])
	}
}

// An explicitly chosen system that cannot be used has to surface as an error:
// quietly designing under the project's own system instead would misrepresent
// what the run produced.
func TestCreateDesignDocumentRejectsAnUnusableChosenSystem(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Missing design system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id":       projectID,
		"agent_id":         agentID,
		"platform":         "web",
		"brief":            "客户列表页。",
		"design_system_id": "8f14e45f-ceea-467a-9575-4a5b0f6d2f6f",
	})
	if response.Code == http.StatusCreated {
		t.Fatal("a design document was created under a design system that does not exist")
	}
}
