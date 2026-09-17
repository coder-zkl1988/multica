package daemon

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeArtemisHost builds an Artemis checkout the way `uv sync` leaves one and
// answers adb from a table, so nothing here reaches a real adb or phone.
func fakeArtemisHost(t *testing.T, devicesOut string, props map[string]string) (home string, calls *[]string) {
	t.Helper()
	home = filepath.Join(t.TempDir(), "artemis")
	for _, p := range []string{filepath.Join(home, ".venv", "bin", "python"), filepath.Join(home, "mcp_server", "server.py")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(ArtemisHomeEnv, home)
	t.Setenv("ANDROID_HOME", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("ARTEMIS_ADB_PATH", "")

	oldLook, oldADB, oldExe, oldHome, oldDirs := capabilitiesLookPath, artemisADB, artemisExecutable, artemisUserHomeDir, artemisSystemADBDirs
	artemisSystemADBDirs = nil
	t.Cleanup(func() {
		capabilitiesLookPath, artemisADB, artemisExecutable, artemisUserHomeDir, artemisSystemADBDirs = oldLook, oldADB, oldExe, oldHome, oldDirs
		androidPropsCache.Range(func(k, _ any) bool { androidPropsCache.Delete(k); return true })
	})
	capabilitiesLookPath = func(name string) (string, error) {
		if name == "adb" {
			return "/sdk/platform-tools/adb", nil
		}
		return "", errors.New("not found")
	}
	artemisUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	artemisExecutable = func() (string, error) { return "/usr/local/bin/multica", nil }
	var mu sync.Mutex
	recorded := []string{}
	artemisADB = func(_ context.Context, adb string, args ...string) ([]byte, error) {
		mu.Lock()
		recorded = append(recorded, strings.Join(args, " "))
		mu.Unlock()
		if adb != "/sdk/platform-tools/adb" {
			t.Errorf("adb = %q, want the one found on PATH", adb)
		}
		if len(args) >= 2 && args[0] == "devices" {
			return []byte(devicesOut), nil
		}
		if len(args) >= 3 && args[0] == "-s" && args[2] == "shell" {
			if out, ok := props[args[1]]; ok {
				return []byte(out), nil
			}
			return nil, errors.New("device offline")
		}
		return nil, errors.New("unexpected adb call")
	}
	return home, &recorded
}

const adbDevicesOut = "List of devices attached\n" +
	"e8e779fa0822           device usb:538067456X product:sunstone model:22101317C device:sunstone transport_id:2\r\n" +
	"8TK7LVORDMU8PJEM       unauthorized usb:539099136X transport_id:4\n" +
	"MVXK9T59J7BY7TL7       device usb:539230208X product:beryl model:24094RAD4C device:beryl transport_id:5\n" +
	"\n"

func TestParseADBDevices(t *testing.T) {
	got := parseADBDevices([]byte("* daemon started successfully\n" + adbDevicesOut))
	if len(got) != 3 {
		t.Fatalf("devices = %+v, want three", got)
	}
	if got[0] != (adbDevice{Serial: "e8e779fa0822", State: "device", Model: "22101317C"}) || got[1].State != "unauthorized" || got[2].Model != "24094RAD4C" {
		t.Errorf("devices = %+v", got)
	}
}

// Each authorized phone becomes an android_device carrying what a case can
// match on and what the overlay launches; an unauthorized one is not bindable.
func TestProbeArtemisCapabilities_ReportsAuthorizedPhones(t *testing.T) {
	home, calls := fakeArtemisHost(t, adbDevicesOut, map[string]string{
		"e8e779fa0822":     "14\n34\nXiaomi\n22101317C\n",
		"MVXK9T59J7BY7TL7": "15\r\n35\r\nXiaomi\r\nRedmi 14C\r\n",
	})
	caps := probeArtemisCapabilities(context.Background())
	if len(caps) != 2 {
		t.Fatalf("capabilities = %+v, want the two authorized phones", caps)
	}
	if caps[0].CapabilityKey != "android:MVXK9T59J7BY7TL7" || caps[1].CapabilityKey != "android:e8e779fa0822" {
		t.Errorf("capabilities must be sorted by key: %s, %s", caps[0].CapabilityKey, caps[1].CapabilityKey)
	}
	byKey := map[string]runtimeCapabilitySummary{}
	for _, c := range caps {
		if c.Kind != "android_device" || c.Status != "available" {
			t.Errorf("unexpected capability %+v", c)
		}
		byKey[c.CapabilityKey] = c
	}
	redmi := byKey["android:MVXK9T59J7BY7TL7"].Target
	for key, want := range map[string]string{
		"platform":       "android",
		"provider":       "artemis",
		"serial":         "MVXK9T59J7BY7TL7",
		"model":          "Redmi 14C",
		"manufacturer":   "Xiaomi",
		"os_version":     "15",
		"sdk":            "35",
		"artemis_python": filepath.Join(home, ".venv", "bin", "python"),
		"artemis_server": filepath.Join(home, "mcp_server", "server.py"),
		"multica_cli":    "/usr/local/bin/multica",
		"adb_path":       "/sdk/platform-tools/adb",
	} {
		if redmi[key] != want {
			t.Errorf("target[%s] = %q, want %q", key, redmi[key], want)
		}
	}
	if _, ok := byKey["android:8TK7LVORDMU8PJEM"]; ok {
		t.Error("an unauthorized phone must not be reported")
	}
	if _, ok := redmi["artemis_daemon_port"]; ok {
		t.Error("no daemon port is reported unless the operator set one")
	}
	t.Setenv(ArtemisDaemonPortEnv, "18765")
	if got := probeArtemisCapabilities(context.Background())[0].Target["artemis_daemon_port"]; got != "18765" {
		t.Errorf("artemis_daemon_port = %q, want the operator's port", got)
	}
	t.Setenv(ArtemisDaemonPortEnv, "not-a-port")
	if _, ok := probeArtemisCapabilities(context.Background())[0].Target["artemis_daemon_port"]; ok {
		t.Error("a malformed port must not be reported")
	}

	// Build properties are read once per phone, not on every report.
	t.Setenv(ArtemisDaemonPortEnv, "")
	before := len(*calls)
	probeArtemisCapabilities(context.Background())
	for _, c := range (*calls)[before:] {
		if strings.Contains(c, "getprop") {
			t.Errorf("props re-read on the second report: %q", c)
		}
	}
}

// Without an Artemis checkout (or before `uv sync` built its venv) the host
// reports no Android phones, whatever adb can see.
func TestProbeArtemisCapabilities_NeedsAnInstalledCheckout(t *testing.T) {
	home, _ := fakeArtemisHost(t, adbDevicesOut, nil)
	if err := os.Remove(filepath.Join(home, ".venv", "bin", "python")); err != nil {
		t.Fatal(err)
	}
	if caps := probeArtemisCapabilities(context.Background()); len(caps) != 0 {
		t.Errorf("capabilities without a venv = %+v, want none", caps)
	}
	summary := probeArtemisSummary(context.Background())
	if summary.Installed || summary.Home != home || !summary.ADB || summary.Phones != 2 || summary.Unauthorized != 1 {
		t.Errorf("summary = %+v, want not installed but adb seeing 2 phones and 1 unauthorized", summary)
	}
}

// The daemon, Artemis and scrcpy must agree on one adb, so the daemon looks
// where Artemis does and in the same order — with PATH last, so a daemon the
// desktop app starts without a login shell picks the same binary as one
// started from a terminal.
func TestFindADB_FollowsArtemisOrder(t *testing.T) {
	root := t.TempDir()
	touch := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	explicit := touch("explicit", "adb")
	sdkEnv := touch("sdk-env", "platform-tools", "adb")
	homeSDK := touch("home", "Library", "Android", "sdk", "platform-tools", "adb")
	brew := touch("brew", "adb")

	oldLook, oldHome, oldDirs := capabilitiesLookPath, artemisUserHomeDir, artemisSystemADBDirs
	t.Cleanup(func() { capabilitiesLookPath, artemisUserHomeDir, artemisSystemADBDirs = oldLook, oldHome, oldDirs })
	capabilitiesLookPath = func(string) (string, error) { return "/on/path/adb", nil }
	artemisUserHomeDir = func() (string, error) { return filepath.Join(root, "home"), nil }
	artemisSystemADBDirs = []string{filepath.Join(root, "brew")}
	t.Setenv("ANDROID_SDK_ROOT", "")

	steps := []struct {
		name  string
		setup func()
		want  string
	}{
		{"ARTEMIS_ADB_PATH", func() {
			t.Setenv("ARTEMIS_ADB_PATH", explicit)
			t.Setenv("ANDROID_HOME", filepath.Join(root, "sdk-env"))
		}, explicit},
		{"ANDROID_HOME", func() { t.Setenv("ARTEMIS_ADB_PATH", "") }, sdkEnv},
		{"SDK default", func() { t.Setenv("ANDROID_HOME", "") }, homeSDK},
		{"Homebrew", func() { _ = os.Remove(homeSDK) }, brew},
		{"PATH last", func() { artemisSystemADBDirs = nil }, "/on/path/adb"},
	}
	for _, step := range steps {
		step.setup()
		if got, ok := findADB(); !ok || got != step.want {
			t.Errorf("%s: findADB() = %q, %v; want %q", step.name, got, ok, step.want)
		}
	}

	// A relative or missing ARTEMIS_ADB_PATH is not trusted.
	t.Setenv("ARTEMIS_ADB_PATH", "adb")
	if got, _ := findADB(); got != "/on/path/adb" {
		t.Errorf("relative ARTEMIS_ADB_PATH was used: %q", got)
	}
}

func TestArtemisSignature_ChangesWithTheBindableSet(t *testing.T) {
	fakeArtemisHost(t, adbDevicesOut, nil)
	a := artemisSignature(context.Background())
	fakeArtemisHost(t, strings.Replace(adbDevicesOut, "unauthorized", "device", 1), nil)
	b := artemisSignature(context.Background())
	if a == "" || a == b {
		t.Errorf("authorizing a phone must change the signature: a=%q b=%q", a, b)
	}
	t.Setenv(ArtemisHomeEnv, filepath.Join(t.TempDir(), "missing"))
	if got := artemisSignature(context.Background()); got != "" {
		t.Errorf("no Artemis must yield an empty signature, got %q", got)
	}
}

func TestArtemisCaseFromTask(t *testing.T) {
	task := Task{ID: "task-1", RuntimeID: "rt-1", TestRunContext: []byte(`{"type":"test_run","run_case_id":"case-1","assigned_capabilities":{"android_device":"android:e8e779fa0822","browser":"browser:playwright"}}`)}
	live, ok := artemisCaseFromTask(task)
	if !ok || live != (artemisLiveCase{RuntimeID: "rt-1", RunCaseID: "case-1", Serial: "e8e779fa0822"}) {
		t.Errorf("live case = %+v, %v", live, ok)
	}
	for name, ctx := range map[string]string{
		"no phone":      `{"run_case_id":"case-1","assigned_capabilities":{"browser":"browser:playwright"}}`,
		"iphone":        `{"run_case_id":"case-1","assigned_capabilities":{"ios_device":"ios:X"}}`,
		"whole round":   `{"run_id":"r","assigned_capabilities":{"android_device":"android:S"}}`,
		"not test run":  ``,
		"malformed key": `{"run_case_id":"case-1","assigned_capabilities":{"android_device":"S"}}`,
	} {
		if _, ok := artemisCaseFromTask(Task{ID: "t", RuntimeID: "rt", TestRunContext: []byte(ctx)}); ok {
			t.Errorf("%s: must not be tracked", name)
		}
	}

	d := &Daemon{}
	untrack := d.trackArtemisCase(task)
	if untrack == nil || len(d.artemisLiveCases()) != 1 {
		t.Fatalf("tracked cases = %+v", d.artemisLiveCases())
	}
	untrack()
	if len(d.artemisLiveCases()) != 0 {
		t.Errorf("cases after untrack = %+v", d.artemisLiveCases())
	}
	if d.trackArtemisCase(Task{ID: "chat"}) != nil {
		t.Error("a task without a pinned phone must not be tracked")
	}
}

func TestScaleFrameToJPEG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1080, 2400))
	for y := 0; y < 2400; y++ {
		for x := 0; x < 1080; x++ {
			c := color.NRGBA{R: 200, G: 30, B: 30, A: 255}
			if x < 540 {
				c = color.NRGBA{R: 20, G: 20, B: 220, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, src); err != nil {
		t.Fatal(err)
	}
	out, err := scaleFrameToJPEG(pngBuf.Bytes(), 728)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output is not a JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 728 || b.Dy() != 1617 {
		t.Errorf("frame = %dx%d, want 728x1617", b.Dx(), b.Dy())
	}
	if r, _, b, _ := img.At(100, 800).RGBA(); b>>8 < 150 || r>>8 > 80 {
		t.Errorf("left half lost its colour: r=%d b=%d", r>>8, b>>8)
	}

	small := image.NewRGBA(image.Rect(0, 0, 400, 800))
	pngBuf.Reset()
	_ = png.Encode(&pngBuf, small)
	out, err = scaleFrameToJPEG(pngBuf.Bytes(), 728)
	if err != nil {
		t.Fatal(err)
	}
	if img, _ := jpeg.Decode(bytes.NewReader(out)); img.Bounds().Dx() != 400 {
		t.Errorf("a narrow frame must keep its size, got %d wide", img.Bounds().Dx())
	}
	if _, err := scaleFrameToJPEG([]byte("not a png"), 728); err == nil {
		t.Error("garbage must be an error, not a frame")
	}
}
