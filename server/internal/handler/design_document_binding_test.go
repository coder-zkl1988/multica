package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The daemon and the server derive the package binding independently from the
// same task context, and ValidateArchive rejects any package whose manifest
// binding differs from the server's expectation. The two sides once disagreed
// on RevisionID (the daemon binds the task id, the server left it empty), which
// rejected every real package at upload with "binding_invalid" before it could
// reach Audit. This test crosses that boundary so it cannot silently reopen
// (DC-055).
func TestDesignDocumentBindingMatchesTheDaemonBinding(t *testing.T) {
	taskID := "0f0f0f0f-0f0f-4f0f-8f0f-0f0f0f0f0f0f"
	agentID := "1a1a1a1a-1a1a-4a1a-8a1a-1a1a1a1a1a1a"
	for _, tt := range []struct {
		name    string
		context service.DesignDocumentTaskContext
	}{
		{
			name: "first generation without a repository",
			context: service.DesignDocumentTaskContext{
				Type:                service.DesignDocumentTaskContextType,
				Operation:           service.DesignDocumentGenerate,
				WorkspaceID:         "2b2b2b2b-2b2b-4b2b-8b2b-2b2b2b2b2b2b",
				ProjectID:           "3c3c3c3c-3c3c-4c3c-8c3c-3c3c3c3c3c3c",
				DesignDocumentID:    "4d4d4d4d-4d4d-4d4d-8d4d-4d4d4d4d4d4d",
				RevisionID:          "9d9d9d9d-9d9d-4d9d-8d9d-9d9d9d9d9d9d",
				AgentID:             agentID,
				Platform:            "web",
				DesignSystemDigest:  "sha256:" + strings.Repeat("e", 64),
				InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
			},
		},
		{
			name: "adjustment scoped to a repository and an issue",
			context: service.DesignDocumentTaskContext{
				Type:                service.DesignDocumentTaskContextType,
				Operation:           service.DesignDocumentAdjust,
				WorkspaceID:         "2b2b2b2b-2b2b-4b2b-8b2b-2b2b2b2b2b2b",
				ProjectID:           "3c3c3c3c-3c3c-4c3c-8c3c-3c3c3c3c3c3c",
				ProjectResourceID:   "5e5e5e5e-5e5e-4e5e-8e5e-5e5e5e5e5e5e",
				IssueID:             "6f6f6f6f-6f6f-4f6f-8f6f-6f6f6f6f6f6f",
				DesignDocumentID:    "4d4d4d4d-4d4d-4d4d-8d4d-4d4d4d4d4d4d",
				AgentID:             agentID,
				Platform:            "cross_platform",
				DesignSystemDigest:  "sha256:" + strings.Repeat("e", 64),
				InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
				BaseRevisionID:      "7a7a7a7a-7a7a-4a7a-8a7a-7a7a7a7a7a7a",
				BaseContentDigest:   "sha256:" + strings.Repeat("d", 64),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			contextJSON, err := json.Marshal(tt.context)
			if err != nil {
				t.Fatalf("marshal context: %v", err)
			}
			serverSide := designDocumentBindingFromContext(tt.context, db.AgentTaskQueue{
				ID:      parseUUID(taskID),
				AgentID: parseUUID(agentID),
				Context: contextJSON,
			})
			daemonSide, err := daemon.DecodeDesignDocumentTaskBinding(daemon.Task{
				ID:                    taskID,
				Agent:                 &daemon.AgentData{ID: agentID},
				DesignDocumentContext: contextJSON,
			})
			if err != nil {
				t.Fatalf("daemon binding: %v", err)
			}
			if serverSide != daemonSide {
				t.Fatalf("server binding\n%+v\ndoes not match the daemon binding\n%+v", serverSide, daemonSide)
			}
			// Both sides must also produce a binding the package contract
			// accepts; otherwise agreement is worthless.
			collected, err := designdocument.CollectDirectory(copyDesignDocumentFixture(t), serverSide)
			if err != nil {
				t.Fatalf("the shared binding is rejected by the package contract: %v", err)
			}
			if collected.Manifest.Binding != serverSide {
				t.Fatalf("collected binding = %+v, want %+v", collected.Manifest.Binding, serverSide)
			}
		})
	}
}

// A binding must name the revision the run produces even though the server does
// not pre-allocate revision ids: the daemon binds the task id, and so must the
// server, or the upload is rejected before Audit.
func TestDesignDocumentBindingNamesTheTaskAsTheRevision(t *testing.T) {
	taskID := parseUUID("0f0f0f0f-0f0f-4f0f-8f0f-0f0f0f0f0f0f")
	binding := designDocumentBindingFromContext(service.DesignDocumentTaskContext{}, db.AgentTaskQueue{ID: taskID})
	if binding.RevisionID != uuidToString(taskID) || binding.TaskID != uuidToString(taskID) {
		t.Fatalf("binding task/revision = %q/%q, want the task id %q", binding.TaskID, binding.RevisionID, uuidToString(taskID))
	}
	if binding.RevisionID == "" {
		t.Fatal("binding has no revision identity")
	}
	var zero pgtype.UUID
	if uuidToString(zero) != "" {
		t.Fatalf("test precondition: uuidToString(zero) = %q", uuidToString(zero))
	}
}

// The claim refuses a design document context that is not execution-ready, and
// the daemon's prepare pass reads the input envelope to decide how to ground
// the run. Both handlers must therefore stamp the context the daemon expects —
// the other half of the contract tested in TestDesignDocumentBindingMatches…
func TestDesignDocumentContextsAreExecutionReadyWithAGroundingEnvelope(t *testing.T) {
	grounded := designDocumentGenerateInput(true, nil, service.ResolvedDesignContext{})
	if grounded.SchemaVersion != service.DesignDocumentInputSchema || grounded.RepositoryGrounding != service.DesignDocumentGroundingPending {
		t.Fatalf("grounded generate input = %+v", grounded)
	}
	ungrounded := designDocumentGenerateInput(false, nil, service.ResolvedDesignContext{})
	if ungrounded.RepositoryGrounding != service.DesignDocumentGroundingUnavailable || len(ungrounded.Repository) != 0 {
		t.Fatalf("ungrounded generate input = %+v", ungrounded)
	}
	pinned, err := designDocumentPinnedInput()
	if err != nil || pinned.RepositoryGrounding != service.DesignDocumentGroundingPinned {
		t.Fatalf("pinned input = %+v (%v)", pinned, err)
	}
	// The pinned receipt must be a valid repository grounding, or the daemon
	// refuses the adjustment before the agent starts.
	if _, err := designdocument.ValidateRepositoryGrounding(pinned.Repository); err != nil {
		t.Fatalf("pinned receipt is invalid: %v", err)
	}

	// And the envelope survives the round trip through the context JSON the
	// daemon decodes, alongside execution_ready.
	raw, err := json.Marshal(service.DesignDocumentTaskContext{
		Type: service.DesignDocumentTaskContextType, Operation: service.DesignDocumentGenerate,
		ExecutionReady: true, Input: grounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ExecutionReady bool `json:"execution_ready"`
		Input          struct {
			RepositoryGrounding string `json:"repository_grounding"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || !decoded.ExecutionReady || decoded.Input.RepositoryGrounding != "pending" {
		t.Fatalf("context JSON = %s", raw)
	}
}
