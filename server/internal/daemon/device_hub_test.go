package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The daemon does not drive phones; it reports the iPhones the device hub on
// this host can drive so a run can be bound here, and passes the hub's
// connector location along so the overlay can mount it without npm.

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

// Android phones on the hub belong to Artemis on this host (TS-035): only the
// iPhones are reported from here, so no case can reach a phone both ways.
func TestProbeDeviceHubCapabilities_ReportsOnlyIPhones(t *testing.T) {
	hub := fakeDeviceHub(t, `{"devices":[
		{"id":"android-1","serial":"SER1","model":"Pixel 9","manufacturer":"Google","os_version":"15","sdk":35,"tracks":["adb","accessibility"],"has_app":true,"status":"available","labels":["lab-a"]},
		{"id":"android-2","platform":"android","serial":"SER2","model":"Pixel 8","os_version":"14","tracks":["adb"],"status":"available"},
		{"id":"ios:00008120-000B","platform":"ios","model":"iPhone 12","os_version":"16.7","tracks":[],"status":"offline"},
		{"id":"ios:00008120-000A","platform":"ios","serial":"00008120-000A","model":"iPhone 15","manufacturer":"Apple","os_version":"17.5","screen":{"width":1179,"height":2556},"tracks":["pulsephone"],"has_app":false,"status":"available","labels":["lab-b"]}
	]}`)

	caps := probeDeviceHubCapabilities(context.Background(), hub.URL)
	if len(caps) != 1 {
		t.Fatalf("got %d capabilities, want only the online iPhone: %+v", len(caps), caps)
	}
	iphone := caps[0]
	if iphone.Kind != "ios_device" || iphone.CapabilityKey != "ios:00008120-000A" || iphone.Status != "available" {
		t.Errorf("unexpected iOS capability %+v", iphone)
	}
	for key, want := range map[string]string{
		"platform":          "ios",
		"model":             "iPhone 15",
		"os_version":        "17.5",
		"tracks":            "pulsephone",
		"serial":            "00008120-000A",
		"screen":            "1179x2556",
		"labels":            "lab-b",
		"hub_url":           hub.URL,
		"connector_command": "/usr/local/bin/node",
		"connector_cli":     "/opt/device-mcp/dist/cli.js",
	} {
		if iphone.Target[key] != want {
			t.Errorf("ios target[%s] = %q, want %q", key, iphone.Target[key], want)
		}
	}
}

func TestProbeDeviceHubSummary_CountsIPhones(t *testing.T) {
	hub := fakeDeviceHub(t, `{"devices":[
		{"id":"android-1","tracks":["adb"],"status":"available"},
		{"id":"ios:A","platform":"ios","tracks":["pulsephone"],"status":"available"},
		{"id":"ios:B","platform":"ios","tracks":["pulsephone"],"status":"busy"},
		{"id":"ios:C","platform":"ios","tracks":[],"status":"offline"}
	]}`)
	summary := probeDeviceHubSummary(context.Background(), hub.URL)
	if !summary.Reachable || summary.Version != "0.1.0" || summary.IPhones != 2 {
		t.Errorf("summary = %+v, want a reachable hub with two usable iPhones", summary)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if gone := probeDeviceHubSummary(context.Background(), url); gone.Reachable || gone.URL != url {
		t.Errorf("an unreachable hub must still be described, got %+v", gone)
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
	a := fakeDeviceHub(t, `{"devices":[{"id":"ios:x","platform":"ios","tracks":["pulsephone"],"status":"available"}]}`)
	b := fakeDeviceHub(t, `{"devices":[{"id":"ios:x","platform":"ios","tracks":["pulsephone"],"status":"busy"}]}`)
	c := fakeDeviceHub(t, `{"devices":[{"id":"ios:x","platform":"ios","tracks":["pulsephone"],"status":"available"},{"id":"android-1","tracks":["adb"],"status":"available"}]}`)
	sa := deviceHubSignature(context.Background(), a.URL)
	sb := deviceHubSignature(context.Background(), b.URL)
	sc := deviceHubSignature(context.Background(), c.URL)
	if sa == "" || sa == sb || sa != sc {
		t.Errorf("signatures: a=%q b=%q c=%q (an Android phone on the hub must not change it)", sa, sb, sc)
	}
}
