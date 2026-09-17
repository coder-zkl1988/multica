package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// The device hub (multica-device-mcp) is how a test host drives iPhones,
// through PulsePhone (TS-031). Android phones are Artemis's (artemis.go,
// TS-035): the hub may still list them when it runs with adb, but they are
// never reported from here, so no case can reach a phone through both. The
// daemon does not drive phones itself; it reports what the hub has so runs can
// be bound to this machine, and the per-task overlay mounts the hub's
// connector.

// DeviceHubURLEnv overrides where the daemon looks for the hub.
const DeviceHubURLEnv = "MULTICA_DEVICE_HUB_URL"

const defaultDeviceHubURL = "http://127.0.0.1:18801"

// deviceHubWatchInterval bounds how quickly a phone plugged in or paired
// after startup becomes bindable without a manual scan.
const deviceHubWatchInterval = 10 * time.Second

func deviceHubURL() string {
	if v := strings.TrimSpace(os.Getenv(DeviceHubURLEnv)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultDeviceHubURL
}

// deviceHubClient is the HTTP client used for hub probes. Replaced in tests.
var deviceHubClient = &http.Client{Timeout: 3 * time.Second}

type deviceHubHealth struct {
	OK        bool   `json:"ok"`
	Version   string `json:"version"`
	Leases    int    `json:"leases"`
	Connector *struct {
		Command string `json:"command"`
		CLI     string `json:"cli"`
	} `json:"connector,omitempty"`
}

// deviceHubSummary is the `device_hub` block of a capability report: enough
// for the runtime page to say whether this machine can run iPhone cases.
type deviceHubSummary struct {
	Reachable bool   `json:"reachable"`
	URL       string `json:"url"`
	Version   string `json:"version,omitempty"`
	IPhones   int    `json:"iphones"`
	Leases    int    `json:"leases"`
}

// probeDeviceHubSummary describes the hub for the runtime page. An
// unreachable hub is still a summary (reachable=false): "this machine has no
// hub running" is what the page must say then.
func probeDeviceHubSummary(ctx context.Context, hubURL string) deviceHubSummary {
	summary := deviceHubSummary{URL: hubURL}
	var health deviceHubHealth
	if err := deviceHubGet(ctx, hubURL+"/health", &health); err != nil || !health.OK {
		return summary
	}
	summary.Reachable = true
	summary.Version = health.Version
	summary.Leases = health.Leases
	var listing struct {
		Devices []deviceHubDevice `json:"devices"`
	}
	if err := deviceHubGet(ctx, hubURL+"/api/devices", &listing); err == nil {
		for _, d := range listing.Devices {
			if d.Platform == "ios" && d.Status != "offline" {
				summary.IPhones++
			}
		}
	}
	return summary
}

// deviceHubLease is one row of the hub's GET /api/leases.
type deviceHubLease struct {
	ID        string            `json:"id"`
	DeviceID  string            `json:"device_id"`
	Label     string            `json:"label"`
	Tags      map[string]string `json:"tags"`
	FrameHash string            `json:"frame_hash"`
	FrameAt   int64             `json:"frame_at"`
}

// deviceHubFrameInterval is how often the daemon looks for a new frame on a
// leased phone. Two seconds keeps the run page's live view close to what the
// agent sees without turning a loopback poll into load.
const deviceHubFrameInterval = 2 * time.Second

func deviceHubGetBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := deviceHubClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device hub %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// deviceHubFrameLoop relays the last frame of every lease a Multica case task
// holds (tagged run_case_id / runtime_id by the overlay) to the server, which
// keeps it in memory for the run page. Only frames whose hash changed are
// fetched and sent, so a still screen costs one small listing per tick.
func (d *Daemon) deviceHubFrameLoop(ctx context.Context) {
	hub := deviceHubURL()
	ticker := time.NewTicker(deviceHubFrameInterval)
	defer ticker.Stop()
	sent := make(map[string]string) // run case id -> last relayed frame hash
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var listing struct {
			Leases []deviceHubLease `json:"leases"`
		}
		if err := deviceHubGet(ctx, hub+"/api/leases", &listing); err != nil {
			continue
		}
		live := make(map[string]struct{}, len(listing.Leases))
		for _, lease := range listing.Leases {
			runCaseID := lease.Tags["run_case_id"]
			runtimeID := lease.Tags["runtime_id"]
			if runCaseID == "" || runtimeID == "" || lease.FrameHash == "" {
				continue
			}
			live[runCaseID] = struct{}{}
			if sent[runCaseID] == lease.FrameHash {
				continue
			}
			jpeg, err := deviceHubGetBytes(ctx, hub+"/api/leases/"+lease.ID+"/frame")
			if err != nil || len(jpeg) == 0 {
				continue
			}
			payload := map[string]any{
				"jpeg_base64": base64.StdEncoding.EncodeToString(jpeg),
				"hash":        lease.FrameHash,
				"captured_at": lease.FrameAt,
				"lease_id":    lease.ID,
			}
			if err := d.client.ReportTestRunCaseFrame(ctx, runtimeID, runCaseID, payload); err != nil {
				d.logger.Debug("live frame relay failed", "run_case_id", runCaseID, "error", err)
				continue
			}
			sent[runCaseID] = lease.FrameHash
		}
		for id := range sent {
			if _, still := live[id]; !still {
				delete(sent, id)
			}
		}
	}
}

type deviceHubDevice struct {
	ID           string                       `json:"id"`
	Platform     string                       `json:"platform,omitempty"`
	Serial       string                       `json:"serial,omitempty"`
	Model        string                       `json:"model"`
	Manufacturer string                       `json:"manufacturer"`
	OSVersion    string                       `json:"os_version"`
	SDK          int                          `json:"sdk,omitempty"`
	Screen       *struct{ Width, Height int } `json:"screen,omitempty"`
	Tracks       []string                     `json:"tracks"`
	HasApp       bool                         `json:"has_app"`
	Status       string                       `json:"status"`
	Labels       []string                     `json:"labels"`
}

func deviceHubGet(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := deviceHubClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device hub %s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// probeDeviceHubCapabilities lists the hub's online iPhones as ios_device
// capabilities. An unreachable hub is not an error: most daemons are not test
// hosts, and they simply report no devices.
//
// Target fields are what the resolver matches on (`os_version`, `model`, …)
// plus what the overlay needs to mount the connector (`hub_url`,
// `connector_command`, `connector_cli`). None of them are secrets.
func probeDeviceHubCapabilities(ctx context.Context, hubURL string) []runtimeCapabilitySummary {
	var health deviceHubHealth
	if err := deviceHubGet(ctx, hubURL+"/health", &health); err != nil || !health.OK {
		return nil
	}
	var listing struct {
		Devices []deviceHubDevice `json:"devices"`
	}
	if err := deviceHubGet(ctx, hubURL+"/api/devices", &listing); err != nil {
		return nil
	}
	out := make([]runtimeCapabilitySummary, 0, len(listing.Devices))
	for _, d := range listing.Devices {
		// Android phones the hub lists belong to Artemis on this host.
		if d.Platform != "ios" || d.Status == "offline" || len(d.Tracks) == 0 {
			continue
		}
		const kind, keyPrefix = "ios_device", "ios:"
		target := map[string]string{
			"platform":     "ios",
			"model":        d.Model,
			"manufacturer": d.Manufacturer,
			"os_version":   d.OSVersion,
			"tracks":       strings.Join(d.Tracks, "+"),
			"has_app":      fmt.Sprintf("%t", d.HasApp),
			"hub_url":      hubURL,
		}
		if d.SDK > 0 {
			target["sdk"] = fmt.Sprintf("%d", d.SDK)
		}
		if d.Serial != "" {
			target["serial"] = d.Serial
		}
		if d.Screen != nil && d.Screen.Width > 0 {
			target["screen"] = fmt.Sprintf("%dx%d", d.Screen.Width, d.Screen.Height)
		}
		if len(d.Labels) > 0 {
			target["labels"] = strings.Join(d.Labels, ",")
		}
		if health.Connector != nil && health.Connector.Command != "" && health.Connector.CLI != "" {
			target["connector_command"] = health.Connector.Command
			target["connector_cli"] = health.Connector.CLI
		}
		out = append(out, runtimeCapabilitySummary{
			Kind:          kind,
			CapabilityKey: keyPrefix + strings.TrimPrefix(d.ID, keyPrefix),
			Target:        target,
			Status:        "available",
		})
	}
	return out
}

// deviceHubSignature reduces a listing to a string that changes exactly when
// the bindable set would: iPhone ids, tracks and busy/available.
func deviceHubSignature(ctx context.Context, hubURL string) string {
	var listing struct {
		Devices []deviceHubDevice `json:"devices"`
	}
	if err := deviceHubGet(ctx, hubURL+"/api/devices", &listing); err != nil {
		return ""
	}
	parts := make([]string, 0, len(listing.Devices))
	for _, d := range listing.Devices {
		if d.Platform != "ios" {
			continue
		}
		parts = append(parts, d.ID+":"+strings.Join(d.Tracks, "+")+":"+d.Status)
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// testHostDeviceWatchLoop re-reports capabilities whenever the phones this
// host can drive change — an Android phone plugged in or authorized for
// Artemis, an iPhone appearing on the hub — so it becomes bindable in seconds
// rather than at the next manual scan.
func (d *Daemon) testHostDeviceWatchLoop(ctx context.Context) {
	hub := deviceHubURL()
	signature := func() string {
		return "hub=" + deviceHubSignature(ctx, hub) + "#artemis=" + artemisSignature(ctx)
	}
	ticker := time.NewTicker(deviceHubWatchInterval)
	defer ticker.Stop()
	last := signature()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sig := signature()
		if sig == last {
			continue
		}
		last = sig
		d.logger.Info("test host phones changed; reporting capabilities", "hub", hub)
		for _, rid := range d.allRuntimeIDs() {
			if rt := d.findRuntime(rid); rt != nil {
				d.reportRuntimeCapabilities(ctx, *rt, "")
			}
		}
	}
}
