package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/testcapability"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
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

// deviceKindRequired names the first required phone kind, if the round has
// one. Those kinds may only be bound on a machine designated as a test host
// (agent_runtime.test_host_enabled): a shared laptop that happens to run a
// device hub must not become a lab by accident.
func deviceKindRequired(requirements []TestCapabilityRequirement) (string, bool) {
	for _, req := range requirements {
		if req.Optional {
			continue
		}
		switch req.Kind {
		case "android_device", "ios_device":
			return req.Kind, true
		}
	}
	return "", false
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

	outcome, err := h.dispatchTestRunCases(r.Context(), testRunDispatchInput{
		run:         run,
		agent:       agent,
		wsUUID:      wsUUID,
		workspaceID: workspaceID,
		originator:  userUUID,
		accountable: userUUID,
		source:      pgtype.Text{String: "direct_human", Valid: true},
		requesterID: userID,
		prompt:      strings.TrimSpace(req.Prompt),
		actorType:   "member",
		actorID:     userID,
	})
	if err != nil {
		var undispatchable *errTestRunUndispatchable
		if errors.As(err, &undispatchable) {
			writeError(w, http.StatusBadRequest, undispatchable.reason)
			return
		}
		slog.Error("dispatch test run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to dispatch the run")
		return
	}
	if outcome.blocked {
		// Explicit failure, not a silent downgrade: parking the run tells the
		// user which capability is missing instead of burning an agent run
		// that discovers it has no phone.
		writeJSON(w, http.StatusConflict, map[string]any{
			"test_run":     testRunToResponse(outcome.run),
			"missing_kind": outcome.missingKind,
			"message":      outcome.reason,
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"test_run":      testRunToResponse(outcome.run),
		"agent_task_id": uuidToString(outcome.firstTaskID),
		"case_tasks":    outcome.created,
		"cases":         outcome.cases,
	})
}

// testRunDispatchInput is everything the dispatch core needs besides an HTTP
// request: the run, the agent, how the per-case tasks are attributed and how
// the run updates are announced. A human dispatch is direct_human on the
// member; an autopilot round carries its trigger owner's attribution.
type testRunDispatchInput struct {
	run          db.TestRun
	agent        db.Agent
	wsUUID       pgtype.UUID
	workspaceID  string
	originator   pgtype.UUID
	accountable  pgtype.UUID
	source       pgtype.Text
	evidenceKind pgtype.Text
	evidenceRef  pgtype.UUID
	// requesterID travels in the task context and must be a member id string:
	// the follow-up dispatch of a capped round parses it back.
	requesterID string
	prompt      string
	actorType   string
	actorID     string
}

type testRunDispatchOutcome struct {
	run         db.TestRun
	firstTaskID pgtype.UUID
	created     int
	cases       int
	// blocked: the round was parked with missingKind / reason recorded on it.
	blocked     bool
	missingKind string
	reason      string
}

// errTestRunUndispatchable marks a round that cannot be dispatched as asked
// (no cases, the agent's runtime is gone): the caller's mistake, not a fault.
type errTestRunUndispatchable struct{ reason string }

func (e *errTestRunUndispatchable) Error() string { return e.reason }

// dispatchTestRunCases is the dispatch core shared by the run page and the
// autopilot test_run mode: resolve the run's capabilities against the
// agent's runtime, park the round when they cannot be met, otherwise queue
// one agent task per case (TS-021) — up to the parallelism cap (M4), the
// per-case completion hooks release the rest — and make the agent the run's
// executor.
func (h *Handler) dispatchTestRunCases(ctx context.Context, in testRunDispatchInput) (testRunDispatchOutcome, error) {
	run := in.run
	// The overlay is mounted on the agent's runtime, so that is the only
	// daemon whose capabilities can serve this run.
	agentRuntime, err := h.runtimeLookup(obsmetrics.RuntimeLookupSourceTestCapability).Get(ctx, in.agent.RuntimeID)
	if err != nil {
		return testRunDispatchOutcome{}, &errTestRunUndispatchable{reason: "the agent's runtime is gone; bind it to a running daemon first"}
	}
	runCases, err := h.Queries.ListTestRunCases(ctx, db.ListTestRunCasesParams{RunID: run.ID, WorkspaceID: in.wsUUID})
	if err != nil {
		return testRunDispatchOutcome{}, fmt.Errorf("load the run's cases: %w", err)
	}
	if len(runCases) == 0 {
		return testRunDispatchOutcome{}, &errTestRunUndispatchable{reason: "this run has no cases to execute"}
	}

	requirements := runRequiredCapabilities(runCases)
	if kind, needsPhone := deviceKindRequired(requirements); needsPhone && !agentRuntime.TestHostEnabled {
		return h.parkTestRun(ctx, in, kind,
			"the agent's runtime is not a test host; turn on \"Test host\" on its runtime page before dispatching device cases")
	}
	binding, missingKind, resolved := h.resolveRunCapabilities(ctx, in.wsUUID, requirements, effectiveDaemonIDForRuntime(agentRuntime))
	if !resolved {
		return h.parkTestRun(ctx, in, missingKind, "no runtime can provide the required capability: "+missingKind)
	}

	// The overlay (browser MCP, device connector) is computed per task with
	// the resolved binding on the context — the queue insert takes it as an
	// argument and nothing recomputes it at claim time.
	limit := len(runCases)
	if run.Parallelism.Valid && run.Parallelism.Int32 > 0 && int(run.Parallelism.Int32) < limit {
		limit = int(run.Parallelism.Int32)
	}
	input := testRunCaseTaskInput{
		run:          run,
		agent:        in.agent,
		originator:   in.originator,
		accountable:  in.accountable,
		source:       in.source,
		evidenceKind: in.evidenceKind,
		evidenceRef:  in.evidenceRef,
		requesterID:  in.requesterID,
		workspaceID:  in.workspaceID,
		prompt:       in.prompt,
		binding:      binding,
		requirements: requirements,
	}
	var firstTaskID pgtype.UUID
	created := 0
	for _, rc := range runCases[:limit] {
		agentTask, err := h.createTestRunCaseTask(ctx, h.Queries, input, rc)
		if err != nil {
			return testRunDispatchOutcome{}, fmt.Errorf("dispatch case %d of %d: %w", created+1, limit, err)
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
	// older readers still see the round as dispatched, and so the follow-up
	// dispatch of a capped round can read the requester and prompt back.
	updated, err := h.Queries.UpdateTestRun(ctx, db.UpdateTestRunParams{
		ID:                run.ID,
		WorkspaceID:       in.wsUUID,
		AgentTaskID:       firstTaskID,
		ExecutorType:      pgtype.Text{String: "agent", Valid: true},
		ExecutorID:        in.agent.ID,
		CapabilityBinding: marshalJSONColumn(binding, "{}"),
	})
	if err != nil {
		return testRunDispatchOutcome{}, fmt.Errorf("record the dispatch: %w", err)
	}
	h.publish(protocol.EventTestRunUpdated, in.workspaceID, in.actorType, in.actorID, map[string]any{"test_run": testRunToResponse(updated)})
	return testRunDispatchOutcome{run: updated, firstTaskID: firstTaskID, created: created, cases: len(runCases)}, nil
}

// parkTestRun records why a round could not be dispatched, so the run page
// (or the autopilot run) can say what to fix.
func (h *Handler) parkTestRun(ctx context.Context, in testRunDispatchInput, missingKind, reason string) (testRunDispatchOutcome, error) {
	blocked, err := h.Queries.UpdateTestRun(ctx, db.UpdateTestRunParams{
		ID:          in.run.ID,
		WorkspaceID: in.wsUUID,
		Status:      pgtype.Text{String: "blocked", Valid: true},
		Error:       pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		return testRunDispatchOutcome{}, fmt.Errorf("record the blocked run: %w", err)
	}
	h.publish(protocol.EventTestRunUpdated, in.workspaceID, in.actorType, in.actorID, map[string]any{"test_run": testRunToResponse(blocked)})
	return testRunDispatchOutcome{run: blocked, blocked: true, missingKind: missingKind, reason: reason}, nil
}

// testRunCaseTaskInput is everything a per-case task needs besides the case
// itself; the same values serve the initial dispatch and the follow-up
// dispatch of a capped round.
type testRunCaseTaskInput struct {
	run          db.TestRun
	agent        db.Agent
	originator   pgtype.UUID
	accountable  pgtype.UUID
	source       pgtype.Text
	evidenceKind pgtype.Text
	evidenceRef  pgtype.UUID
	requesterID  string
	workspaceID  string
	prompt       string
	binding      TestRunCapabilityBinding
	requirements []TestCapabilityRequirement
}

// createTestRunCaseTask queues the agent task for one case and binds the case
// to it. The device connector overlay carries the case's TC key as the lease
// label and the run / case / runtime ids as lease tags, which is how the
// daemon later relays the case's live frame.
func (h *Handler) createTestRunCaseTask(ctx context.Context, q *db.Queries, in testRunCaseTaskInput, rc db.TestRunCase) (db.AgentTaskQueue, error) {
	label := runCaseLabel(rc)
	assigned := caseCapabilityKeys(in.binding, uuidToString(in.run.ID), rc.Position)
	contextPayload := service.TestRunContext{
		Type:                 service.TestRunContextType,
		Prompt:               in.prompt,
		RequesterID:          in.requesterID,
		WorkspaceID:          in.workspaceID,
		ProjectID:            uuidToString(in.run.ProjectID),
		AgentID:              uuidToString(in.agent.ID),
		RunID:                uuidToString(in.run.ID),
		CapabilityBinding:    json.RawMessage(marshalJSONColumn(in.binding, "{}")),
		AssignedCapabilities: assigned,
		RunCaseID:            uuidToString(rc.ID),
		CaseKey:              label,
		CaseSnapshot:         json.RawMessage(rc.CaseSnapshot),
	}
	contextJSON, err := json.Marshal(contextPayload)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("build the agent task context: %w", err)
	}
	tags := map[string]string{
		"run_case_id":  uuidToString(rc.ID),
		"run_id":       uuidToString(in.run.ID),
		"runtime_id":   uuidToString(in.agent.RuntimeID),
		"workspace_id": in.workspaceID,
	}
	octx := testcapability.WithResolvedCapabilities(ctx, capabilityEntriesForOverlay(in.binding, assigned, in.requirements, label, tags))
	overlay, connectedApps := h.TaskService.BuildRuntimeMCPOverlayForMerge(octx, in.originator, in.agent)
	agentTask, err := q.CreateQuickCreateTask(octx, db.CreateQuickCreateTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              in.agent.ID,
		RuntimeID:            in.agent.RuntimeID,
		Priority:             0,
		Context:              contextJSON,
		OriginatorUserID:     in.originator,
		AccountableUserID:    in.accountable,
		OriginatorSource:     in.source,
		TriggerEvidenceKind:  in.evidenceKind,
		TriggerEvidenceRefID: in.evidenceRef,
		RuntimeMcpOverlay:    overlay,
		RuntimeConnectedApps: connectedApps,
	})
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if _, err := q.UpdateTestRunCaseAgentTask(ctx, db.UpdateTestRunCaseAgentTaskParams{
		ID:          rc.ID,
		WorkspaceID: in.run.WorkspaceID,
		AgentTaskID: agentTask.ID,
	}); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("record the case's agent task: %w", err)
	}
	return agentTask, nil
}

// dispatchNextTestRunCases releases the next cases of a capped round once a
// case settles ("finish one, release one"). Uncapped rounds queued everything
// at dispatch and return at once. The requester and prompt come back from the
// first case task's context, its attribution from the task row itself; the
// binding is the frozen one on the run.
func (h *Handler) dispatchNextTestRunCases(ctx context.Context, q *db.Queries, run db.TestRun) error {
	if !run.Parallelism.Valid || run.Parallelism.Int32 <= 0 {
		return nil
	}
	if run.Status == "completed" || run.Status == "aborted" || run.Status == "blocked" {
		return nil
	}
	if run.ExecutorType != "agent" || !run.AgentTaskID.Valid {
		return nil
	}
	active, err := q.CountDispatchedActiveTestRunCases(ctx, db.CountDispatchedActiveTestRunCasesParams{RunID: run.ID, WorkspaceID: run.WorkspaceID})
	if err != nil {
		return err
	}
	free := int(run.Parallelism.Int32) - int(active)
	if free <= 0 {
		return nil
	}
	waiting, err := q.ListUndispatchedTestRunCases(ctx, db.ListUndispatchedTestRunCasesParams{
		RunID:       run.ID,
		WorkspaceID: run.WorkspaceID,
		Limit:       int32(free),
	})
	if err != nil {
		return err
	}
	if len(waiting) == 0 {
		return nil
	}
	firstTask, err := q.GetAgentTask(ctx, run.AgentTaskID)
	if err != nil {
		return fmt.Errorf("load the round's first case task: %w", err)
	}
	runCtx, ok := testRunContextForTask(firstTask)
	if !ok {
		return fmt.Errorf("the round's first task carries no test run context")
	}
	originator := firstTask.OriginatorUserID
	if !originator.Valid {
		parsed, err := util.ParseUUID(runCtx.RequesterID)
		if err != nil {
			return fmt.Errorf("requester id on the first case task: %w", err)
		}
		originator = parsed
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: run.ExecutorID, WorkspaceID: run.WorkspaceID})
	if err != nil {
		return fmt.Errorf("load the executing agent: %w", err)
	}
	var binding TestRunCapabilityBinding
	if len(run.CapabilityBinding) > 0 {
		if err := json.Unmarshal(run.CapabilityBinding, &binding); err != nil {
			return fmt.Errorf("decode the frozen capability binding: %w", err)
		}
	}
	if binding.Resolved == nil {
		binding.Resolved = map[string]string{}
	}
	allCases, err := q.ListTestRunCases(ctx, db.ListTestRunCasesParams{RunID: run.ID, WorkspaceID: run.WorkspaceID})
	if err != nil {
		return err
	}
	input := testRunCaseTaskInput{
		run:          run,
		agent:        agent,
		originator:   originator,
		accountable:  firstTask.AccountableUserID,
		source:       firstTask.OriginatorSource,
		evidenceKind: firstTask.TriggerEvidenceKind,
		evidenceRef:  firstTask.TriggerEvidenceRefID,
		requesterID:  runCtx.RequesterID,
		workspaceID:  runCtx.WorkspaceID,
		prompt:       runCtx.Prompt,
		binding:      binding,
		requirements: runRequiredCapabilities(allCases),
	}
	for _, rc := range waiting {
		if _, err := h.createTestRunCaseTask(ctx, q, input, rc); err != nil {
			return err
		}
	}
	return nil
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

// caseCapabilityKeys is the capability each kind of the binding means for one
// case. A pooled kind (Android through Artemis, which has no leases) pins the
// case to one phone of the pool: rotating by position spreads a round's cases
// over the phones, and offsetting by the run keeps two rounds dispatched
// together from starting on the same phone. A collision is still correct —
// Artemis queues a phone's tasks — only slower.
func caseCapabilityKeys(binding TestRunCapabilityBinding, runID string, position int32) map[string]string {
	keys := make(map[string]string, len(binding.Resolved))
	for kind, key := range binding.Resolved {
		keys[kind] = key
	}
	for kind, pool := range binding.Pools {
		if len(pool) == 0 {
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(runID))
		pos := int(position)
		if pos < 0 {
			pos = -pos
		}
		keys[kind] = pool[(int(h.Sum32()%uint32(len(pool)))+pos)%len(pool)]
	}
	return keys
}

// capabilityEntriesForOverlay turns the frozen binding into the shape the MCP
// overlay provider consumes, with each kind's key as assigned to the case.
func capabilityEntriesForOverlay(
	binding TestRunCapabilityBinding,
	assigned map[string]string,
	requirements []TestCapabilityRequirement,
	label string,
	tags map[string]string,
) []testcapability.TestRunCapabilityEntry {
	entries := make([]testcapability.TestRunCapabilityEntry, 0, len(binding.Resolved))
	for _, req := range requirements {
		key, bound := binding.Resolved[req.Kind]
		if !bound {
			continue
		}
		if pinned := assigned[req.Kind]; pinned != "" {
			key = pinned
		}
		entries = append(entries, testcapability.TestRunCapabilityEntry{
			Kind:   req.Kind,
			Key:    key,
			Target: binding.Targets[req.Kind],
			Match:  req.Match,
			Label:  label,
			Tags:   tags,
		})
	}
	return entries
}
