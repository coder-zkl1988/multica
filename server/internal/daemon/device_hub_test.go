package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The daemon does not drive phones; it reports what the device hub on this
// host can drive so a run can be bound here, and passes the hub's connector
// location along so the overlay can mount it without npm.

func fakeDeviceHub(t *testing.T, devices string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"version":"0.1.0","connector":{"command":"/usr/local/bin/node","cli":"/opt/device-mcp/dist/cli.js"}}`))
	})
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(devices))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeDeviceHubCapabilities_ReportsOnlinePhonesWithConnector(t *testing.T) {
	hub := fakeDeviceHub(t, `{"devices":[
		{"id":"android-1","serial":"SER1","model":"Pixel 9","manufacturer":"Google","os_version":"15","sdk":35,"screen":{"width":1080,"height":2400},"tracks":["adb","accessibility"],"has_app":true,"status":"available","labels":["lab-a"]},
		{"id":"android-2","model":"Old","manufacturer":"X","os_version":"9","tracks":[],"has_app":false,"status":"offline","labels":[]}
	]}`)

	caps := probeDeviceHubCapabilities(context.Background(), hub.URL)
	if len(caps) != 1 {
		t.Fatalf("got %d capabilities, want 1 (offline phone skipped): %+v", len(caps), caps)
	}
	c := caps[0]
	if c.Kind != "android_device" || c.CapabilityKey != "android:android-1" || c.Status != "available" {
		t.Errorf("unexpected capability %+v", c)
	}
	for key, want := range map[string]string{
		"model":             "Pixel 9",
		"os_version":        "15",
		"sdk":               "35",
		"screen":            "1080x2400",
		"tracks":            "adb+accessibility",
		"has_app":           "true",
		"labels":            "lab-a",
		"hub_url":           hub.URL,
		"connector_command": "/usr/local/bin/node",
		"connector_cli":     "/opt/device-mcp/dist/cli.js",
	} {
		if c.Target[key] != want {
			t.Errorf("target[%s] = %q, want %q", key, c.Target[key], want)
		}
	}
}

func TestProbeDeviceHubCapabilities_NoHubIsNotAnError(t *testing.T) {
	// A closed port: most daemons are not test hosts and must report nothing.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if caps := probeDeviceHubCapabilities(context.Background(), url); len(caps) != 0 {
		t.Errorf("unreachable hub must yield no capabilities, got %+v", caps)
	}
}

func TestDeviceHubSignature_ChangesWithTheBindableSet(t *testing.T) {
	a := fakeDeviceHub(t, `{"devices":[{"id":"x","tracks":["adb"],"status":"available"}]}`)
	b := fakeDeviceHub(t, `{"devices":[{"id":"x","tracks":["adb"],"status":"busy"}]}`)
	c := fakeDeviceHub(t, `{"devices":[{"id":"x","tracks":["adb"],"status":"available"}]}`)
	sa := deviceHubSignature(context.Background(), a.URL)
	sb := deviceHubSignature(context.Background(), b.URL)
	sc := deviceHubSignature(context.Background(), c.URL)
	if sa == "" || sa == sb || sa != sc {
		t.Errorf("signatures: a=%q b=%q c=%q", sa, sb, sc)
	}
}
