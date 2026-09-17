package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/redis/go-redis/v9"
)

// Live test state that must not be persisted (09-02 §9.4): what a test
// host's device hub looks like right now, and the last frame an agent saw of
// a running case. Both are reported by the daemon and kept only in memory
// (or Redis with a short TTL when the API runs on several nodes).

// RuntimeDeviceHubReport is the daemon's summary of the phones its machine
// can drive, sent with every capability report: the multica-device-mcp hub,
// which serves iPhones (TS-031), and Artemis, which serves Android phones
// (TS-035).
type RuntimeDeviceHubReport struct {
	Reachable  bool                  `json:"reachable"`
	URL        string                `json:"url"`
	Version    string                `json:"version"`
	IPhones    int                   `json:"iphones"`
	Leases     int                   `json:"leases"`
	Artemis    *RuntimeArtemisReport `json:"artemis,omitempty"`
	ReportedAt time.Time             `json:"reported_at"`
}

// RuntimeArtemisReport says whether the machine can run Android cases: an
// Artemis checkout with its venv built, adb, and how many phones adb reaches
// authorized or not. Home is only shown to people who may edit the runtime.
type RuntimeArtemisReport struct {
	Installed    bool   `json:"installed"`
	Home         string `json:"home"`
	ADB          bool   `json:"adb"`
	Phones       int    `json:"phones"`
	Unauthorized int    `json:"unauthorized"`
}

// deviceHubReportRetention bounds how long a report answers for a daemon that
// stopped reporting: a stale "reachable" would send a tester to a hub that is
// gone.
const deviceHubReportRetention = 15 * time.Minute

type DeviceHubStore interface {
	Set(ctx context.Context, daemonID string, report RuntimeDeviceHubReport) error
	// Get returns nil when nothing fresh is known for the daemon.
	Get(ctx context.Context, daemonID string) (*RuntimeDeviceHubReport, error)
}

type InMemoryDeviceHubStore struct {
	mu      sync.Mutex
	reports map[string]RuntimeDeviceHubReport
}

func NewInMemoryDeviceHubStore() *InMemoryDeviceHubStore {
	return &InMemoryDeviceHubStore{reports: make(map[string]RuntimeDeviceHubReport)}
}

func (s *InMemoryDeviceHubStore) Set(_ context.Context, daemonID string, report RuntimeDeviceHubReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports[daemonID] = report
	return nil
}

func (s *InMemoryDeviceHubStore) Get(_ context.Context, daemonID string) (*RuntimeDeviceHubReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	report, ok := s.reports[daemonID]
	if !ok || time.Since(report.ReportedAt) > deviceHubReportRetention {
		delete(s.reports, daemonID)
		return nil, nil
	}
	copy := report
	return &copy, nil
}

const deviceHubRedisPrefix = "mul:" + runtimePendingRedisHashTag + ":device_hub:"

type RedisDeviceHubStore struct {
	rdb *redis.Client
}

func NewRedisDeviceHubStore(rdb *redis.Client) *RedisDeviceHubStore {
	return &RedisDeviceHubStore{rdb: rdb}
}

func (s *RedisDeviceHubStore) Set(ctx context.Context, daemonID string, report RuntimeDeviceHubReport) error {
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal device hub report: %w", err)
	}
	return s.rdb.Set(ctx, deviceHubRedisPrefix+daemonID, data, deviceHubReportRetention).Err()
}

func (s *RedisDeviceHubStore) Get(ctx context.Context, daemonID string) (*RuntimeDeviceHubReport, error) {
	data, err := s.rdb.Get(ctx, deviceHubRedisPrefix+daemonID).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var report RuntimeDeviceHubReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("unmarshal device hub report: %w", err)
	}
	return &report, nil
}

// TestRunCaseFrame is the most recent screenshot of the phone a running case
// is driving: memory only, replaced by the next one, gone two minutes after
// the last upload. Evidence the agent wants to keep goes through attachments.
type TestRunCaseFrame struct {
	JPEG       []byte    `json:"jpeg"`
	Hash       string    `json:"hash"`
	CapturedAt time.Time `json:"captured_at"`
	LeaseID    string    `json:"lease_id,omitempty"`
	Track      string    `json:"track,omitempty"`
}

const (
	liveFrameRetention = 2 * time.Minute
	liveFrameMaxBytes  = 2 << 20
	liveFrameRedisKey  = "mul:" + runtimePendingRedisHashTag + ":live_frame:"
)

type LiveFrameStore interface {
	Set(ctx context.Context, runCaseID string, frame TestRunCaseFrame) error
	Get(ctx context.Context, runCaseID string) (*TestRunCaseFrame, error)
}

type InMemoryLiveFrameStore struct {
	mu     sync.Mutex
	frames map[string]TestRunCaseFrame
}

func NewInMemoryLiveFrameStore() *InMemoryLiveFrameStore {
	return &InMemoryLiveFrameStore{frames: make(map[string]TestRunCaseFrame)}
}

func (s *InMemoryLiveFrameStore) Set(_ context.Context, runCaseID string, frame TestRunCaseFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic sweep so a long-lived API node does not hoard frames of
	// rounds that ended hours ago.
	for id, f := range s.frames {
		if time.Since(f.CapturedAt) > liveFrameRetention {
			delete(s.frames, id)
		}
	}
	s.frames[runCaseID] = frame
	return nil
}

func (s *InMemoryLiveFrameStore) Get(_ context.Context, runCaseID string) (*TestRunCaseFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	frame, ok := s.frames[runCaseID]
	if !ok || time.Since(frame.CapturedAt) > liveFrameRetention {
		delete(s.frames, runCaseID)
		return nil, nil
	}
	copy := frame
	return &copy, nil
}

type RedisLiveFrameStore struct {
	rdb *redis.Client
}

func NewRedisLiveFrameStore(rdb *redis.Client) *RedisLiveFrameStore {
	return &RedisLiveFrameStore{rdb: rdb}
}

func (s *RedisLiveFrameStore) Set(ctx context.Context, runCaseID string, frame TestRunCaseFrame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal live frame: %w", err)
	}
	return s.rdb.Set(ctx, liveFrameRedisKey+runCaseID, data, liveFrameRetention).Err()
}

func (s *RedisLiveFrameStore) Get(ctx context.Context, runCaseID string) (*TestRunCaseFrame, error) {
	data, err := s.rdb.Get(ctx, liveFrameRedisKey+runCaseID).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var frame TestRunCaseFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return nil, fmt.Errorf("unmarshal live frame: %w", err)
	}
	if time.Since(frame.CapturedAt) > liveFrameRetention {
		return nil, nil
	}
	return &frame, nil
}

// ReportTestRunCaseFrame accepts the daemon's relay of the hub's last frame
// for a case (POST /api/daemon/runtimes/{runtimeId}/test-run-cases/{runCaseId}/frame).
// The daemon token must belong to the case's workspace; the runtime in the
// path is the one whose overlay tagged the lease.
func (h *Handler) ReportTestRunCaseFrame(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	runCaseUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "runCaseId"), "run case id")
	if !ok {
		return
	}
	rc, err := h.Queries.GetTestRunCaseInWorkspace(r.Context(), db.GetTestRunCaseInWorkspaceParams{ID: runCaseUUID, WorkspaceID: rt.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "test run case not found")
		return
	}
	var body struct {
		JPEGBase64 string `json:"jpeg_base64"`
		Hash       string `json:"hash"`
		CapturedAt int64  `json:"captured_at"`
		LeaseID    string `json:"lease_id"`
		Track      string `json:"track"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, liveFrameMaxBytes*2)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	jpeg, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.JPEGBase64))
	if err != nil || len(jpeg) == 0 {
		writeError(w, http.StatusBadRequest, "jpeg_base64 is required")
		return
	}
	if len(jpeg) > liveFrameMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "frame is too large")
		return
	}
	capturedAt := time.Now()
	if body.CapturedAt > 0 {
		capturedAt = time.UnixMilli(body.CapturedAt)
		// A frame stamped in the future or long ago is still "the latest
		// the hub has": keep it fresh for the retention window from now.
		if capturedAt.After(time.Now()) || time.Since(capturedAt) > liveFrameRetention/2 {
			capturedAt = time.Now()
		}
	}
	if err := h.LiveFrameStore.Set(r.Context(), uuidToString(rc.ID), TestRunCaseFrame{
		JPEG:       jpeg,
		Hash:       body.Hash,
		CapturedAt: capturedAt,
		LeaseID:    body.LeaseID,
		Track:      body.Track,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store the frame")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetTestRunCaseFrame serves the last relayed frame of a case as image/jpeg
// (GET /api/test-run-cases/{id}/frame). ETag is the hub's frame hash, so a
// poller that sends If-None-Match pays nothing while the screen is still.
func (h *Handler) GetTestRunCaseFrame(w http.ResponseWriter, r *http.Request) {
	rc, _, ok := h.loadTestRunCaseForUser(w, r)
	if !ok {
		return
	}
	frame, err := h.LiveFrameStore.Get(r.Context(), uuidToString(rc.ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the frame")
		return
	}
	if frame == nil {
		writeError(w, http.StatusNotFound, "no live frame for this case")
		return
	}
	etag := `"` + frame.Hash + `"`
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Captured-At", frame.CapturedAt.UTC().Format(time.RFC3339))
	if frame.Track != "" {
		w.Header().Set("X-Track", frame.Track)
	}
	if frame.Hash != "" && r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(frame.JPEG)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(frame.JPEG)
}
