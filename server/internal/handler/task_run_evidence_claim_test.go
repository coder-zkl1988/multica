package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskRunEvidenceModelUsageClaimFlagWireCompatibility(t *testing.T) {
	request := func(capability string) *http.Request {
		req, err := http.NewRequest(http.MethodPost, "/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if capability != "" {
			req.Header.Set("X-Client-Capabilities", capability)
		}
		return req
	}

	for _, tc := range []struct {
		name       string
		capability string
		present    bool
	}{
		{name: "old daemon omitted"},
		{name: "unrelated capability omitted", capability: protocol.DaemonCapabilitySkillBundlesV1},
		{name: "advertised capability enabled", capability: protocol.DaemonCapabilityTaskRunEvidenceModelUsageV1, present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := AgentTaskResponse{ID: "task-1"}
			resp.TaskRunEvidenceModelUsageV1 = requestHasClientCapability(request(tc.capability), protocol.DaemonCapabilityTaskRunEvidenceModelUsageV1)
			raw, err := json.Marshal(resp)
			if err != nil {
				t.Fatal(err)
			}
			present := strings.Contains(string(raw), `"task_run_evidence_model_usage_v1":true`)
			if present != tc.present {
				t.Fatalf("claim flag present=%v, want %v: %s", present, tc.present, raw)
			}
		})
	}
}

func TestClaimTaskRunEvidenceModelUsageCapabilityGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	for _, tc := range []struct {
		name       string
		capability string
		present    bool
	}{
		{name: "old daemon omitted"},
		{name: "advertised capability enabled", capability: protocol.DaemonCapabilityTaskRunEvidenceModelUsageV1, present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Model usage claim "+tc.name)
			// Since MUL-8154 a daemon token reaches only runtimes carrying
			// its own daemon_id, so bind the claim runtime to the token.
			dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = 'model-usage-claim' WHERE id = $1`, runtimeID)
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Model usage claim "+tc.name)
			dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})

			req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "model-usage-claim")
			if tc.capability != "" {
				req.Header.Set("X-Client-Capabilities", tc.capability)
			}
			req = withURLParam(req, "runtimeId", runtimeID)
			response := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
			var body struct {
				Task map[string]json.RawMessage `json:"task"`
			}
			response.JSON(&body)
			flag, present := body.Task["task_run_evidence_model_usage_v1"]
			if present != tc.present {
				t.Fatalf("claim flag present=%v, want %v: %s", present, tc.present, response.Body.String())
			}
			if present && string(flag) != "true" {
				t.Fatalf("claim flag = %s, want true", flag)
			}
		})
	}
}
