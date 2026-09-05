package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Test execution capability surface: which phone, browser or desktop a run can
// actually drive, and how a run is bound to one.

// TestCapabilityRequirement is what a case declares it needs. It names a kind
// and optional match constraints, never a specific device: the binding to a
// concrete capability happens once, at dispatch.
type TestCapabilityRequirement struct {
	Kind     string            `json:"kind"`
	Match    map[string]string `json:"match,omitempty"`
	Optional bool              `json:"optional,omitempty"`
}

// TestRunCapabilityBinding is the frozen resolution stored on test_run so a
// retry reproduces the same environment.
type TestRunCapabilityBinding struct {
	DaemonID  string            `json:"daemon_id"`
	RuntimeID string            `json:"runtime_id,omitempty"`
	Resolved  map[string]string `json:"resolved"` // kind -> capability_key
}

// resolveRunCapabilities picks one daemon that can satisfy every required
// (non-optional) capability. Cross-machine runs are out of scope for v1, so a
// requirement set spread over two daemons is unsatisfiable by design.
//
// daemonID, when non-empty, restricts the search to that daemon. Dispatch
// passes the daemon of the agent's runtime: the MCP overlay is mounted where
// the agent runs, so a binding on any other daemon would hand the agent a
// browser or phone that is not on its machine. The browser probe hid this for
// a while (npx spawns playwright wherever the overlay lands); a device does
// not.
//
// Returns ok=false with the first missing kind when nothing can serve the run;
// the caller must then park the run as blocked rather than queue it.
// Empty requirements resolve trivially (ok=true, empty binding).
func (h *Handler) resolveRunCapabilities(
	ctx context.Context,
	wsUUID pgtype.UUID,
	requirements []TestCapabilityRequirement,
	daemonID string,
) (binding TestRunCapabilityBinding, missingKind string, ok bool) {
	// Trivially satisfied when there are no requirements.
	if len(requirements) == 0 {
		return TestRunCapabilityBinding{Resolved: map[string]string{}}, "", true
	}

	// Split into required vs optional. Optional requirements are never
	// blocking; they are satisfied on a best-effort basis if a daemon that
	// already covers all required ones also happens to have them.
	required := make([]TestCapabilityRequirement, 0, len(requirements))
	for _, r := range requirements {
		if !r.Optional {
			required = append(required, r)
		}
	}
	if len(required) == 0 {
		return TestRunCapabilityBinding{Resolved: map[string]string{}}, "", true
	}

	// Fetch all available capabilities for this workspace ordered by
	// (daemon_id, kind, capability_key) so iteration is deterministic.
	caps, err := h.Queries.ListAvailableTestCapabilities(ctx, wsUUID)
	if err != nil {
		slog.Warn("resolveRunCapabilities: failed to list available capabilities",
			"workspace_id", uuidToString(wsUUID),
			"error", err,
		)
		// Treat DB errors as blocking (fail-closed): the run is parked rather
		// than dispatched without a validated capability binding.
		if len(required) > 0 {
			return TestRunCapabilityBinding{}, required[0].Kind, false
		}
		return TestRunCapabilityBinding{}, "", false
	}

	// Group by daemon_id. Within each daemon, group by kind so match
	// evaluation is efficient (linear scan over candidates for that kind).
	type candidate struct {
		capabilityKey string
		target        map[string]string
		runtimeID     pgtype.UUID
	}
	// daemonKinds[daemonID][kind] = list of matching candidates
	daemonKinds := map[string]map[string][]candidate{}
	for _, cap := range caps {
		if daemonID != "" && cap.DaemonID != daemonID {
			continue
		}
		var tgt map[string]string
		if len(cap.Target) > 0 {
			_ = json.Unmarshal(cap.Target, &tgt)
		}
		if tgt == nil {
			tgt = map[string]string{}
		}
		if daemonKinds[cap.DaemonID] == nil {
			daemonKinds[cap.DaemonID] = map[string][]candidate{}
		}
		daemonKinds[cap.DaemonID][cap.Kind] = append(
			daemonKinds[cap.DaemonID][cap.Kind],
			candidate{cap.CapabilityKey, tgt, cap.RuntimeID},
		)
	}

	// Try each daemon in lexicographic order for determinism.
	daemonIDs := make([]string, 0, len(daemonKinds))
	for id := range daemonKinds {
		daemonIDs = append(daemonIDs, id)
	}
	sort.Strings(daemonIDs)

	for _, daemonID := range daemonIDs {
		kinds := daemonKinds[daemonID]
		resolved := make(map[string]string, len(required))
		var runtimeID pgtype.UUID
		allSatisfied := true

		for _, req := range required {
			candidates := kinds[req.Kind]
			matched := false
			for _, c := range candidates {
				if matchCapabilityTarget(req.Match, c.target) {
					resolved[req.Kind] = c.capabilityKey
					if c.runtimeID.Valid {
						runtimeID = c.runtimeID
					}
					matched = true
					break
				}
			}
			if !matched {
				allSatisfied = false
				break
			}
		}

		if allSatisfied {
			b := TestRunCapabilityBinding{
				DaemonID: daemonID,
				Resolved: resolved,
			}
			if runtimeID.Valid {
				b.RuntimeID = uuidToString(runtimeID)
			}
			return b, "", true
		}
	}

	// No single daemon can cover all requirements. Find and report the first
	// kind that is unsatisfied across ALL daemons so the caller can surface a
	// meaningful error (e.g. "no available browser capability").
	for _, req := range required {
		anyMatch := false
	outer:
		for _, kinds := range daemonKinds {
			for _, c := range kinds[req.Kind] {
				if matchCapabilityTarget(req.Match, c.target) {
					anyMatch = true
					break outer
				}
			}
		}
		if !anyMatch {
			return TestRunCapabilityBinding{}, req.Kind, false
		}
	}
	// All kinds exist globally but no single daemon covers all of them (cross-
	// machine configuration). v1 treats this as unsatisfiable.
	return TestRunCapabilityBinding{}, required[0].Kind, false
}

// matchCapabilityTarget returns true when every constraint in match is
// satisfied by the target map. An empty match map matches any target.
// Constraint syntax: exact string equality, or one of the ordering operators
// >=, >, <=, < applied to version-like values ("1.0", "131").
func matchCapabilityTarget(match map[string]string, target map[string]string) bool {
	for k, constraint := range match {
		val, ok := target[k]
		if !ok {
			return false
		}
		if !satisfiesConstraint(val, constraint) {
			return false
		}
	}
	return true
}

func satisfiesConstraint(value, constraint string) bool {
	switch {
	case strings.HasPrefix(constraint, ">="):
		return compareVersionLike(value, strings.TrimPrefix(constraint, ">=")) >= 0
	case strings.HasPrefix(constraint, ">"):
		return compareVersionLike(value, strings.TrimPrefix(constraint, ">")) > 0
	case strings.HasPrefix(constraint, "<="):
		return compareVersionLike(value, strings.TrimPrefix(constraint, "<=")) <= 0
	case strings.HasPrefix(constraint, "<"):
		return compareVersionLike(value, strings.TrimPrefix(constraint, "<")) < 0
	default:
		return value == constraint
	}
}

// compareVersionLike compares two version strings by splitting on "." and
// comparing each numeric component in turn. A non-numeric component falls back
// to lexicographic comparison for that component only. Versions of unequal
// length compare as if the shorter one were zero-padded, so "1.0" equals "1".
func compareVersionLike(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		// A missing trailing segment is zero, not empty. Defaulting to "" sends
		// the pair into the lexicographic branch below, where "0" > "" would
		// report "1.0" as newer than "1".
		aSeg, bSeg := "0", "0"
		if i < len(aParts) {
			aSeg = aParts[i]
		}
		if i < len(bParts) {
			bSeg = bParts[i]
		}
		aNum, aErr := strconv.Atoi(aSeg)
		bNum, bErr := strconv.Atoi(bSeg)
		if aErr == nil && bErr == nil {
			if aNum != bNum {
				if aNum > bNum {
					return 1
				}
				return -1
			}
		} else {
			// Fall back to lexicographic comparison for non-numeric segments.
			if aSeg < bSeg {
				return -1
			}
			if aSeg > bSeg {
				return 1
			}
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

// ListTestCapabilities lists available test execution capabilities for the
// calling workspace member, with optional filtering by kind, status, or
// daemon_id query parameters.
func (h *Handler) ListTestCapabilities(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found"); !ok {
		return
	}

	params := db.ListTestCapabilitiesParams{
		WorkspaceID: wsUUID,
	}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		params.Kind = pgtype.Text{String: kind, Valid: true}
	}
	if status := r.URL.Query().Get("status"); status != "" {
		params.Status = pgtype.Text{String: status, Valid: true}
	}
	if daemonID := r.URL.Query().Get("daemon_id"); daemonID != "" {
		params.DaemonID = pgtype.Text{String: daemonID, Valid: true}
	}

	caps, err := h.Queries.ListTestCapabilities(r.Context(), params)
	if err != nil {
		slog.Warn("ListTestCapabilities: query failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list capabilities")
		return
	}

	resp := make([]testCapabilityResponse, 0, len(caps))
	for _, c := range caps {
		resp = append(resp, testCapabilityToResponse(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": resp})
}

// ListTestRunCapabilities returns the resolved capability binding stored on a
// test_run. The binding was frozen at dispatch and includes daemon_id,
// runtime_id, and the kind→capability_key map.
func (h *Handler) ListTestRunCapabilities(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found"); !ok {
		return
	}

	runID := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run_id")
	if !ok {
		return
	}

	run, err := h.Queries.GetTestRunInWorkspace(r.Context(), db.GetTestRunInWorkspaceParams{
		ID:          runUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "test run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load test run")
		return
	}

	// The capability_binding is stored as raw JSONB on the run row. Return it
	// directly rather than re-parsing to avoid losing any fields added in
	// future migrations.
	var binding json.RawMessage = run.CapabilityBinding
	if len(binding) == 0 || string(binding) == "null" {
		binding = json.RawMessage(`{}`)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":             runID,
		"capability_binding": binding,
	})
}

// ---------------------------------------------------------------------------
// Capability scan: pending-work store (daemon-facing)
// ---------------------------------------------------------------------------

// A scan request follows the runtime local-skill list lifecycle so the
// heartbeat can probe-then-claim it the same way: a member asks, the next
// heartbeat hands the id to the daemon, the daemon reports its inventory with
// that id and the request completes.
const (
	CapabilityScanPending   = "pending"
	CapabilityScanRunning   = "running"
	CapabilityScanCompleted = "completed"
	CapabilityScanFailed    = "failed"

	// capabilityScanRetention bounds how long a request is kept after
	// creation. A scan nobody claimed or reported inside it is forgotten;
	// the daemon reports unsolicited on registration anyway.
	capabilityScanRetention = 10 * time.Minute
)

// RuntimeCapabilityScanRequest is one "probe your capabilities" ask.
type RuntimeCapabilityScanRequest struct {
	ID        string    `json:"id"`
	RuntimeID string    `json:"runtime_id"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CapabilityScanStore keeps pending scan requests where every API node can
// see them. The in-memory implementation is for single-node and tests; the
// Redis one (test_capability_redis_store.go) is wired whenever Redis is.
type CapabilityScanStore interface {
	Create(ctx context.Context, runtimeID string) (*RuntimeCapabilityScanRequest, error)
	Get(ctx context.Context, id string) (*RuntimeCapabilityScanRequest, error)
	// HasPending is the cheap read-only probe the heartbeat runs before the
	// side-effecting PopPending, so an empty queue costs one lookup.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeCapabilityScanRequest, error)
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// InMemoryCapabilityScanStore is the process-local store.
type InMemoryCapabilityScanStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeCapabilityScanRequest
}

func NewInMemoryCapabilityScanStore() *InMemoryCapabilityScanStore {
	return &InMemoryCapabilityScanStore{requests: make(map[string]*RuntimeCapabilityScanRequest)}
}

func (s *InMemoryCapabilityScanStore) Create(_ context.Context, runtimeID string) (*RuntimeCapabilityScanRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > capabilityScanRetention {
			delete(s.requests, id)
		}
	}
	now := time.Now()
	req := &RuntimeCapabilityScanRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    CapabilityScanPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryCapabilityScanStore) Get(_ context.Context, id string) (*RuntimeCapabilityScanRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	copied := *req
	return &copied, nil
}

func (s *InMemoryCapabilityScanStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == CapabilityScanPending {
			return true, nil
		}
	}
	return false, nil
}

// PopPending claims the oldest pending request for the runtime, so a burst of
// "scan" clicks drains in the order they were made.
func (s *InMemoryCapabilityScanStore) PopPending(_ context.Context, runtimeID string) (*RuntimeCapabilityScanRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldest *RuntimeCapabilityScanRequest
	for _, req := range s.requests {
		if req.RuntimeID != runtimeID || req.Status != CapabilityScanPending {
			continue
		}
		if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
			oldest = req
		}
	}
	if oldest == nil {
		return nil, nil
	}
	oldest.Status = CapabilityScanRunning
	oldest.UpdatedAt = time.Now()
	copied := *oldest
	return &copied, nil
}

func (s *InMemoryCapabilityScanStore) Complete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[id]; ok {
		req.Status = CapabilityScanCompleted
		req.Error = ""
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryCapabilityScanStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[id]; ok {
		req.Status = CapabilityScanFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// RequestRuntimeCapabilityScan asks the daemon to probe and report its current
// test execution capabilities. The request is queued in the scan store and
// handed to the daemon on its next heartbeat (which the pending-work nudge
// makes immediate). 202 Accepted: the inventory itself arrives through
// ReportRuntimeCapabilities and the test_capability:updated event.
func (h *Handler) RequestRuntimeCapabilityScan(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeCapabilityReadAccess(w, r, obsmetrics.RuntimeLookupSourceTestCapability, runtimeID)
	if !ok {
		return
	}

	req, err := h.CapabilityScanStore.Create(r.Context(), rt.runtimeID)
	if err != nil {
		slog.Warn("RequestRuntimeCapabilityScan: create failed", "runtime_id", rt.runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to queue the capability scan")
		return
	}
	// Best-effort nudge: wake the daemon so it heartbeats immediately and
	// picks up the pending scan rather than waiting for the next tick.
	h.requestDaemonPendingWork(rt.runtimeID, "capability_scan")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"request_id": req.ID,
		"runtime_id": rt.runtimeID,
		"status":     req.Status,
	})
}

// effectiveDaemonIDForRuntime is the daemon_id a runtime's capability rows are
// filed under: the daemon identity recorded at registration, or the runtime
// UUID for runtimes that never had one. Report and dispatch must agree on it,
// or a run can be bound to capabilities the agent's daemon does not have.
func effectiveDaemonIDForRuntime(rt db.AgentRuntime) string {
	if rt.DaemonID.Valid && rt.DaemonID.String != "" {
		return rt.DaemonID.String
	}
	return uuidToString(rt.ID)
}

// ReportRuntimeCapabilities accepts a daemon's capability inventory. The report
// contains the capabilities the daemon currently has available; the handler
// upserts each and marks any previously-reported capabilities that are no longer
// present as offline.
//
// Authentication: daemon token bound to the reporting runtime's workspace.
func (h *Handler) ReportRuntimeCapabilities(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	var body struct {
		RequestID    string                   `json:"request_id,omitempty"`
		Capabilities []reportedCapabilityBody `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	workspaceID := uuidToString(rt.WorkspaceID)
	wsUUID := rt.WorkspaceID

	effectiveDaemonID := effectiveDaemonIDForRuntime(rt)

	presentKeys := make([]string, 0, len(body.Capabilities))
	upserted := make([]testCapabilityResponse, 0, len(body.Capabilities))

	for _, cap := range body.Capabilities {
		if cap.Kind == "" || cap.CapabilityKey == "" {
			continue
		}

		targetRaw, _ := json.Marshal(cap.Target)
		if targetRaw == nil {
			targetRaw = []byte("{}")
		}
		probeRaw, _ := json.Marshal(cap.Probe)
		if probeRaw == nil {
			probeRaw = []byte("{}")
		}

		result, err := h.Queries.UpsertTestCapability(r.Context(), db.UpsertTestCapabilityParams{
			WorkspaceID:   wsUUID,
			DaemonID:      effectiveDaemonID,
			RuntimeID:     rt.ID,
			Kind:          cap.Kind,
			CapabilityKey: cap.CapabilityKey,
			Target:        targetRaw,
			Status:        "available",
			Probe:         probeRaw,
		})
		if err != nil {
			slog.Warn("ReportRuntimeCapabilities: upsert failed",
				"workspace_id", workspaceID,
				"kind", cap.Kind,
				"capability_key", cap.CapabilityKey,
				"error", err,
			)
			continue
		}
		presentKeys = append(presentKeys, cap.CapabilityKey)
		upserted = append(upserted, testCapabilityToResponse(result))
	}

	// Mark any previously-reported capabilities that are no longer present
	// as offline so resolveRunCapabilities does not bind a run to a gone device.
	if err := h.Queries.MarkTestCapabilitiesOfflineForDaemon(r.Context(), db.MarkTestCapabilitiesOfflineForDaemonParams{
		WorkspaceID: wsUUID,
		DaemonID:    effectiveDaemonID,
		PresentKeys: presentKeys,
	}); err != nil {
		slog.Warn("ReportRuntimeCapabilities: mark offline failed",
			"workspace_id", workspaceID,
			"daemon_id", effectiveDaemonID,
			"error", err,
		)
	}

	// Complete the pending scan request if one was attached.
	if body.RequestID != "" {
		if err := h.CapabilityScanStore.Complete(r.Context(), body.RequestID); err != nil {
			slog.Warn("ReportRuntimeCapabilities: complete scan failed", "request_id", body.RequestID, "error", err)
		}
	}

	// The inventory is what the runtime page and the dispatch preview read;
	// pushing the event is what lets a "scan" click show its result without
	// a reload.
	h.publish(protocol.EventTestCapabilityUpdated, workspaceID, "system", "", map[string]any{
		"daemon_id":    effectiveDaemonID,
		"runtime_id":   uuidToString(rt.ID),
		"capabilities": upserted,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"daemon_id":    effectiveDaemonID,
		"capabilities": upserted,
	})
}

// ---------------------------------------------------------------------------
// Response types and helpers
// ---------------------------------------------------------------------------

type testCapabilityResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	DaemonID      string          `json:"daemon_id"`
	RuntimeID     string          `json:"runtime_id,omitempty"`
	Kind          string          `json:"kind"`
	CapabilityKey string          `json:"capability_key"`
	Target        json.RawMessage `json:"target"`
	Status        string          `json:"status"`
	LastProbeAt   *string         `json:"last_probe_at,omitempty"`
	CreatedAt     string          `json:"created_at"`
}

type reportedCapabilityBody struct {
	Kind          string            `json:"kind"`
	CapabilityKey string            `json:"capability_key"`
	Target        map[string]string `json:"target,omitempty"`
	Probe         map[string]any    `json:"probe,omitempty"`
}

func testCapabilityToResponse(c db.TestCapability) testCapabilityResponse {
	resp := testCapabilityResponse{
		ID:            uuidToString(c.ID),
		WorkspaceID:   uuidToString(c.WorkspaceID),
		DaemonID:      c.DaemonID,
		Kind:          c.Kind,
		CapabilityKey: c.CapabilityKey,
		Status:        c.Status,
		CreatedAt:     c.CreatedAt.Time.Format(time.RFC3339),
	}
	if c.RuntimeID.Valid {
		resp.RuntimeID = uuidToString(c.RuntimeID)
	}
	if len(c.Target) > 0 {
		resp.Target = c.Target
	}
	if c.LastProbeAt.Valid {
		s := c.LastProbeAt.Time.Format(time.RFC3339)
		resp.LastProbeAt = &s
	}
	return resp
}
