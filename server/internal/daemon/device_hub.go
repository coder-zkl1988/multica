package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// The device hub (multica-device-mcp) is the machine-level phone pool of a
// test host: adb devices plus apps paired over the LAN. The daemon does not
// drive phones itself; it reports what the hub has so runs can be bound to
// this machine, and the per-task overlay mounts the hub's connector.

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
	OK        bool `json:"ok"`
	Connector *struct {
		Command string `json:"command"`
		CLI     string `json:"cli"`
	} `json:"connector,omitempty"`
}

type deviceHubDevice struct {
	ID           string                       `json:"id"`
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

// probeDeviceHubCapabilities lists the hub's online phones as android_device
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
		if d.Status == "offline" || len(d.Tracks) == 0 {
			continue
		}
		target := map[string]string{
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
			Kind:          "android_device",
			CapabilityKey: "android:" + d.ID,
			Target:        target,
			Status:        "available",
		})
	}
	return out
}

// deviceHubSignature reduces a listing to a string that changes exactly when
// the bindable set would: ids, tracks and busy/available.
func deviceHubSignature(ctx context.Context, hubURL string) string {
	var listing struct {
		Devices []deviceHubDevice `json:"devices"`
	}
	if err := deviceHubGet(ctx, hubURL+"/api/devices", &listing); err != nil {
		return ""
	}
	parts := make([]string, 0, len(listing.Devices))
	for _, d := range listing.Devices {
		parts = append(parts, d.ID+":"+strings.Join(d.Tracks, "+")+":"+d.Status)
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// deviceHubWatchLoop re-reports capabilities whenever the hub's device set
// changes, so a phone that appears after registration becomes bindable in
// seconds rather than at the next manual scan.
func (d *Daemon) deviceHubWatchLoop(ctx context.Context) {
	hub := deviceHubURL()
	ticker := time.NewTicker(deviceHubWatchInterval)
	defer ticker.Stop()
	last := deviceHubSignature(ctx, hub)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sig := deviceHubSignature(ctx, hub)
		if sig == last {
			continue
		}
		last = sig
		d.logger.Info("device hub changed; reporting capabilities", "hub", hub)
		for _, rid := range d.allRuntimeIDs() {
			if rt := d.findRuntime(rid); rt != nil {
				d.reportRuntimeCapabilities(ctx, *rt, "")
			}
		}
	}
}
