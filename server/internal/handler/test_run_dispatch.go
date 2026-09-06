package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/testcapability"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type DispatchTestRunRequest struct {
	AgentID string `json:"agent_id"`
	Prompt  string `json:"prompt"`
}

// runRequiredCapabilities collects what the round needs from the frozen case
// snapshots rather than from the live cases: the run executes what it froze, so
// it must be bound to the devices that snapshot asked for.
func runRequiredCapabilities(runCases []db.TestRunCase) []TestCapabilityRequirement {
	seen := make(map[string]struct{})
	out := make([]TestCapabilityRequirement, 0)
	for _, rc := range runCases {
		var snapshot struct {
			RequiredCapabilities []TestCapabilityRequirement `json:"required_capabilities"`
		}
		if err := json.Unmarshal(rc.CaseSnapshot, &snapshot); err != nil {
			continue
		}
		for _, req := range snapshot.RequiredCapabilities {
			if strings.TrimSpace(req.Kind) == "" {
				continue
			}
			// Dedupe on kind plus its constraints: two cases needing the same
			// kind of device share one binding.
			key := req.Kind + "\x00" + fmt.Sprintf("%v", req.Match)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, req)
		}
	}
	return out
}

// DispatchTestRun hands a round to an agent. Capability resolution happens
// here, before anything is queued: a run that has no device to drive is parked
// as blocked with the missing kind named, because a dispatched run with no
// device only reveals itself as broken minutes later, inside the agent.
func (h *Handler) DispatchTestRun(w http.ResponseWriter, r *http.Request) {
	run, wsUUID, ok := h.loadTestRunForUser(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var req DispatchTestRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if run.Status == "running" {
		writeError(w, http.StatusConflict, "this run is already running")
		return
	}
	if run.AgentTaskID.Valid {
		writeError(w, http.StatusConflict, "this run has already been dispatched")
		return
	}

	agentUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.AgentID), "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "this agent is archived")
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "this agent has no runtime bound; start a daemon for it first")
		return
	}

	runCases, err := h.Queries.ListTestRunCases(r.Context(), db.ListTestRunCasesParams{
		RunID:       run.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the run's cases")
		return
	}
	if len(runCases) == 0 {
		writeError(w, http.StatusBadRequest, "this run has no cases to execute")
		return
	}

	// The overlay is mounted on the agent's runtime, so that is the only
	// daemon whose capabilities can serve this run.
	agentRuntime, err := h.runtimeLookup(obsmetrics.RuntimeLookupSourceTestCapability).Get(r.Context(), agent.RuntimeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the agent's runtime is gone; bind it to a running daemon first")
		return
	}

	requirements := runRequiredCapabilities(runCases)
	binding, missingKind, resolved := h.resolveRunCapabilities(r.Context(), wsUUID, requirements, effectiveDaemonIDForRuntime(agentRuntime))
	if !resolved {
		// Explicit failure, not a silent downgrade: parking the run tells the
		// user which capability is missing instead of burning an agent run that
		// discovers it has no phone.
		reason := "no runtime can provide the required capability: " + missingKind
		blocked, updateErr := h.Queries.UpdateTestRun(r.Context(), db.UpdateTestRunParams{
			ID:          run.ID,
			WorkspaceID: wsUUID,
			Status:      pgtype.Text{String: "blocked", Valid: true},
			Error:       pgtype.Text{String: reason, Valid: true},
		})
		if updateErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to record the blocked run")
			return
		}
		resp := testRunToResponse(blocked)
		h.publish(protocol.EventTestRunUpdated, workspaceID, "member", userID, map[string]any{"test_run": resp})
		writeJSON(w, http.StatusConflict, map[string]any{
			"test_run":     resp,
			"missing_kind": missingKind,
			"message":      reason,
		})
		return
	}

	bindingJSON := marshalJSONColumn(binding, "{}")

	// One agent task per case (TS-021): cases run independently, in parallel
	// across phones, and each records its own result. The overlay (browser
	// MCP, device connector) is computed here, with the resolved binding on
	// the context, and stamped on every task — the queue insert takes it as
	// an argument and nothing recomputes it at claim time.
	var firstTaskID pgtype.UUID
	created := 0
	for _, rc := range runCases {
		label := runCaseLabel(rc)
		contextPayload := service.TestRunContext{
			Type:              service.TestRunContextType,
			Prompt:            strings.TrimSpace(req.Prompt),
			RequesterID:       userID,
			WorkspaceID:       workspaceID,
			ProjectID:         uuidToString(run.ProjectID),
			AgentID:           uuidToString(agent.ID),
			RunID:             uuidToString(run.ID),
			CapabilityBinding: json.RawMessage(bindingJSON),
			RunCaseID:         uuidToString(rc.ID),
			CaseKey:           label,
			CaseSnapshot:      json.RawMessage(rc.CaseSnapshot),
		}
		contextJSON, err := json.Marshal(contextPayload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build the agent task context")
			return
		}
		ctx := testcapability.WithResolvedCapabilities(r.Context(), capabilityEntriesForOverlay(binding, requirements, label))
		overlay, connectedApps := h.TaskService.BuildRuntimeMCPOverlayForMerge(ctx, userUUID, agent)
		agentTask, err := h.Queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
			ID:                   dbid.NewV7(),
			AgentID:              agent.ID,
			RuntimeID:            agent.RuntimeID,
			Priority:             0,
			Context:              contextJSON,
			OriginatorUserID:     userUUID,
			AccountableUserID:    userUUID,
			OriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
			RuntimeMcpOverlay:    overlay,
			RuntimeConnectedApps: connectedApps,
		})
		if err != nil {
			slog.Error("dispatch test run failed", append(logger.RequestAttrs(r), "error", err, "created", created, "of", len(runCases))...)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to dispatch case %d of %d", created+1, len(runCases)))
			return
		}
		if _, err := h.Queries.UpdateTestRunCaseAgentTask(r.Context(), db.UpdateTestRunCaseAgentTaskParams{
			ID:          rc.ID,
			WorkspaceID: wsUUID,
			AgentTaskID: agentTask.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record the case's agent task")
			return
		}
		if !firstTaskID.Valid {
			firstTaskID = agentTask.ID
		}
		created++
	}

	// The agent becomes the run's executor here. UpdateTestRunCaseResult reads
	// run.ExecutorID to attribute an agent-written result, so leaving the
	// creating member on those columns would file the agent's results under a
	// human who never ran them. agent_task_id keeps the first case task so
	// older readers still see the round as dispatched.
	updated, err := h.Queries.UpdateTestRun(r.Context(), db.UpdateTestRunParams{
		ID:                run.ID,
		WorkspaceID:       wsUUID,
		AgentTaskID:       firstTaskID,
		ExecutorType:      pgtype.Text{String: "agent", Valid: true},
		ExecutorID:        agent.ID,
		CapabilityBinding: bindingJSON,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the dispatch")
		return
	}
	resp := testRunToResponse(updated)
	h.publish(protocol.EventTestRunUpdated, workspaceID, "member", userID, map[string]any{"test_run": resp})
	writeJSON(w, http.StatusCreated, map[string]any{
		"test_run":      resp,
		"agent_task_id": uuidToString(firstTaskID),
		"case_tasks":    created,
	})
}

// runCaseLabel is what a case is called on the phone owner's approval prompt
// and in the hub's audit log: its TC key from the frozen snapshot, else the
// run case id.
func runCaseLabel(rc db.TestRunCase) string {
	var snapshot struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rc.CaseSnapshot, &snapshot); err == nil && strings.TrimSpace(snapshot.Key) != "" {
		return strings.TrimSpace(snapshot.Key)
	}
	return uuidToString(rc.ID)
}

// capabilityEntriesForOverlay turns the frozen binding into the shape the MCP
// overlay provider consumes.
func capabilityEntriesForOverlay(
	binding TestRunCapabilityBinding,
	requirements []TestCapabilityRequirement,
	label string,
) []testcapability.TestRunCapabilityEntry {
	entries := make([]testcapability.TestRunCapabilityEntry, 0, len(binding.Resolved))
	for _, req := range requirements {
		key, bound := binding.Resolved[req.Kind]
		if !bound {
			continue
		}
		entries = append(entries, testcapability.TestRunCapabilityEntry{
			Kind:   req.Kind,
			Key:    key,
			Target: binding.Targets[req.Kind],
			Match:  req.Match,
			Label:  label,
		})
	}
	return entries
}
