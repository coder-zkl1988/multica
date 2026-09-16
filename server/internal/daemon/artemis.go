package daemon

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Android phones on a test host are driven by Artemis
// (github.com/google/artemis, TS-035): its own agent reads the screen and acts
// on a natural-language task. The daemon neither runs Artemis nor touches a
// phone's UI. It finds the Artemis checkout the operator installed, lists the
// phones adb reaches as android_device capabilities carrying the paths the
// overlay needs to launch Artemis's MCP server behind `multica test
// artemis-mcp`, and relays a live frame of every phone a running case was
// pinned to.

// ArtemisHomeEnv names the Artemis checkout (the directory holding
// mcp_server/ and the `uv sync`-built .venv). Unset, ~/artemis is tried.
const ArtemisHomeEnv = "MULTICA_ARTEMIS_HOME"

// ArtemisDaemonPortEnv moves the Artemis daemon every case on this host shares
// off Artemis's default port 8000, which a dev server on a test host often
// holds. It travels to each case in the capability target, not through the
// agent's environment.
const ArtemisDaemonPortEnv = "MULTICA_ARTEMIS_DAEMON_PORT"

// artemisProvider is target["provider"] of the capabilities reported here; the
// server pools phones carrying it (testcapability.ArtemisProvider).
const artemisProvider = "artemis"

const (
	// artemisFrameInterval matches the hub relay: close enough to what the
	// agent's Artemis task is doing without turning screencap into load.
	artemisFrameInterval = 2 * time.Second
	// artemisFrameWidth is the width a relayed frame is scaled down to.
	artemisFrameWidth = 728
	artemisADBTimeout = 8 * time.Second
)

// Replaced in tests: nothing here may resolve a real Artemis, adb or phone.
var (
	artemisUserHomeDir = os.UserHomeDir
	artemisExecutable  = os.Executable
	artemisStat        = os.Stat
	artemisADB         = func(ctx context.Context, adb string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, artemisADBTimeout)
		defer cancel()
		return exec.CommandContext(ctx, adb, args...).Output()
	}
)

type artemisInstall struct {
	Home   string
	Python string
	Server string
}

// findArtemis resolves the Artemis checkout. It counts only once `uv sync` has
// built the venv the MCP server runs in.
func findArtemis() (artemisInstall, bool) {
	home := strings.TrimSpace(os.Getenv(ArtemisHomeEnv))
	if home == "" {
		if dir, err := artemisUserHomeDir(); err == nil && dir != "" {
			home = filepath.Join(dir, "artemis")
		}
	}
	if home == "" {
		return artemisInstall{}, false
	}
	python := filepath.Join(home, ".venv", "bin", "python")
	if runtime.GOOS == "windows" {
		python = filepath.Join(home, ".venv", "Scripts", "python.exe")
	}
	server := filepath.Join(home, "mcp_server", "server.py")
	for _, p := range []string{python, server} {
		if _, err := artemisStat(p); err != nil {
			return artemisInstall{Home: home}, false
		}
	}
	return artemisInstall{Home: home, Python: python, Server: server}, true
}

// findADB looks where Artemis itself would: the SDK named by ANDROID_HOME /
// ANDROID_SDK_ROOT, PATH, then Android Studio's default SDK location.
func findADB() (string, bool) {
	name := "adb"
	if runtime.GOOS == "windows" {
		name = "adb.exe"
	}
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := strings.TrimSpace(os.Getenv(env)); root != "" {
			p := filepath.Join(root, "platform-tools", name)
			if _, err := artemisStat(p); err == nil {
				return p, true
			}
		}
	}
	if p, err := capabilitiesLookPath("adb"); err == nil {
		return p, true
	}
	if dir, err := artemisUserHomeDir(); err == nil && dir != "" {
		for _, p := range []string{
			filepath.Join(dir, "Library", "Android", "sdk", "platform-tools", name),
			filepath.Join(dir, "Android", "Sdk", "platform-tools", name),
		} {
			if _, err := artemisStat(p); err == nil {
				return p, true
			}
		}
	}
	return "", false
}

type adbDevice struct {
	Serial string
	State  string
	Model  string
}

// parseADBDevices reads `adb devices -l`.
func parseADBDevices(out []byte) []adbDevice {
	var devices []adbDevice
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		d := adbDevice{Serial: fields[0], State: fields[1]}
		for _, f := range fields[2:] {
			if model, ok := strings.CutPrefix(f, "model:"); ok {
				d.Model = strings.ReplaceAll(model, "_", " ")
			}
		}
		devices = append(devices, d)
	}
	return devices
}

type androidProps struct {
	Release      string
	SDK          string
	Manufacturer string
	Model        string
}

// androidPropsCache keeps a phone's build properties: they do not change while
// it stays attached, and the watch loop re-reports every few seconds.
var androidPropsCache sync.Map // serial -> androidProps

func readAndroidProps(ctx context.Context, adb, serial string) androidProps {
	if v, ok := androidPropsCache.Load(serial); ok {
		return v.(androidProps)
	}
	out, err := artemisADB(ctx, adb, "-s", serial, "shell",
		"getprop ro.build.version.release; getprop ro.build.version.sdk; getprop ro.product.manufacturer; getprop ro.product.model")
	if err != nil {
		return androidProps{}
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n")
	line := func(i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	props := androidProps{Release: line(0), SDK: line(1), Manufacturer: line(2), Model: line(3)}
	if props.Release != "" {
		androidPropsCache.Store(serial, props)
	}
	return props
}

// artemisPhones lists what adb reaches, or ok=false when this host has no
// Artemis or no adb and so contributes no Android phones at all.
func artemisPhones(ctx context.Context) (install artemisInstall, adb string, devices []adbDevice, ok bool) {
	install, ok = findArtemis()
	if !ok {
		return install, "", nil, false
	}
	adb, ok = findADB()
	if !ok {
		return install, "", nil, false
	}
	out, err := artemisADB(ctx, adb, "devices", "-l")
	if err != nil {
		return install, adb, nil, false
	}
	return install, adb, parseADBDevices(out), true
}

// probeArtemisCapabilities reports each authorized phone as an android_device.
// Target fields are what a case constraint matches on (`os_version`, `model`,
// …) plus the paths the overlay launches (none of them secret).
func probeArtemisCapabilities(ctx context.Context) []runtimeCapabilitySummary {
	install, adb, devices, ok := artemisPhones(ctx)
	if !ok {
		return nil
	}
	cli, err := artemisExecutable()
	if err != nil {
		cli = ""
	}
	var out []runtimeCapabilitySummary
	for _, dev := range devices {
		if dev.State != "device" {
			continue
		}
		props := readAndroidProps(ctx, adb, dev.Serial)
		model := props.Model
		if model == "" {
			model = dev.Model
		}
		target := map[string]string{
			"platform":       "android",
			"provider":       artemisProvider,
			"serial":         dev.Serial,
			"model":          model,
			"manufacturer":   props.Manufacturer,
			"os_version":     props.Release,
			"sdk":            props.SDK,
			"artemis_python": install.Python,
			"artemis_server": install.Server,
			"multica_cli":    cli,
		}
		if port := strings.TrimSpace(os.Getenv(ArtemisDaemonPortEnv)); port != "" {
			if n, err := strconv.Atoi(port); err == nil && n > 0 && n < 65536 {
				target["artemis_daemon_port"] = port
			}
		}
		for k, v := range target {
			if v == "" {
				delete(target, k)
			}
		}
		out = append(out, runtimeCapabilitySummary{
			Kind:          "android_device",
			CapabilityKey: "android:" + dev.Serial,
			Target:        target,
			Status:        "available",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityKey < out[j].CapabilityKey })
	return out
}

// artemisSummary is the `artemis` block of a capability report: what the
// runtime page needs to say whether this machine can run Android cases, and
// why not.
type artemisSummary struct {
	Installed    bool   `json:"installed"`
	Home         string `json:"home"`
	ADB          bool   `json:"adb"`
	Phones       int    `json:"phones"`
	Unauthorized int    `json:"unauthorized"`
}

func probeArtemisSummary(ctx context.Context) artemisSummary {
	install, installed := findArtemis()
	summary := artemisSummary{Installed: installed, Home: install.Home}
	adb, ok := findADB()
	if !ok {
		return summary
	}
	summary.ADB = true
	out, err := artemisADB(ctx, adb, "devices", "-l")
	if err != nil {
		return summary
	}
	for _, dev := range parseADBDevices(out) {
		if dev.State == "device" {
			summary.Phones++
		} else {
			summary.Unauthorized++
		}
	}
	return summary
}

// artemisSignature changes exactly when the set of bindable Android phones
// would: serials and their adb state, on a host with Artemis.
func artemisSignature(ctx context.Context) string {
	_, _, devices, ok := artemisPhones(ctx)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(devices))
	for _, d := range devices {
		parts = append(parts, d.Serial+":"+d.State)
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// artemisLiveCase is a running case pinned to an Android phone.
type artemisLiveCase struct {
	RuntimeID string
	RunCaseID string
	Serial    string
}

// artemisCaseFromTask reads the phone a test-run case task was pinned to.
func artemisCaseFromTask(task Task) (artemisLiveCase, bool) {
	if len(task.TestRunContext) == 0 {
		return artemisLiveCase{}, false
	}
	var ctx struct {
		RunCaseID string            `json:"run_case_id"`
		Assigned  map[string]string `json:"assigned_capabilities"`
	}
	if json.Unmarshal(task.TestRunContext, &ctx) != nil || ctx.RunCaseID == "" {
		return artemisLiveCase{}, false
	}
	serial, ok := strings.CutPrefix(ctx.Assigned["android_device"], "android:")
	if !ok || strings.TrimSpace(serial) == "" || task.RuntimeID == "" {
		return artemisLiveCase{}, false
	}
	return artemisLiveCase{RuntimeID: task.RuntimeID, RunCaseID: ctx.RunCaseID, Serial: strings.TrimSpace(serial)}, true
}

// trackArtemisCase registers a running case for the frame relay; the returned
// func unregisters it. Tasks that drive no Android phone return nil.
func (d *Daemon) trackArtemisCase(task Task) func() {
	live, ok := artemisCaseFromTask(task)
	if !ok {
		return nil
	}
	d.artemisCasesMu.Lock()
	if d.artemisCases == nil {
		d.artemisCases = make(map[string]artemisLiveCase)
	}
	d.artemisCases[task.ID] = live
	d.artemisCasesMu.Unlock()
	return func() {
		d.artemisCasesMu.Lock()
		delete(d.artemisCases, task.ID)
		d.artemisCasesMu.Unlock()
	}
}

func (d *Daemon) artemisLiveCases() []artemisLiveCase {
	d.artemisCasesMu.Lock()
	defer d.artemisCasesMu.Unlock()
	out := make([]artemisLiveCase, 0, len(d.artemisCases))
	for _, c := range d.artemisCases {
		out = append(out, c)
	}
	return out
}

// artemisFrameLoop relays the screen of every phone a running case was pinned
// to, as the server's live view of the case. Frames come from adb screencap —
// a read of the display that neither Artemis nor the app under test notices —
// and are sent only when the screen changed.
func (d *Daemon) artemisFrameLoop(ctx context.Context) {
	ticker := time.NewTicker(artemisFrameInterval)
	defer ticker.Stop()
	var (
		mu   sync.Mutex
		sent = map[string]string{} // run case id -> last relayed frame hash
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		cases := d.artemisLiveCases()
		if len(cases) == 0 {
			mu.Lock()
			clear(sent)
			mu.Unlock()
			continue
		}
		adb, ok := findADB()
		if !ok {
			continue
		}
		var wg sync.WaitGroup
		for _, c := range cases {
			wg.Add(1)
			go func(c artemisLiveCase) {
				defer wg.Done()
				pngBytes, err := artemisADB(ctx, adb, "-s", c.Serial, "exec-out", "screencap", "-p")
				if err != nil || len(pngBytes) == 0 {
					return
				}
				sum := sha1.Sum(pngBytes)
				hash := hex.EncodeToString(sum[:8])
				mu.Lock()
				unchanged := sent[c.RunCaseID] == hash
				mu.Unlock()
				if unchanged {
					return
				}
				jpegBytes, err := scaleFrameToJPEG(pngBytes, artemisFrameWidth)
				if err != nil {
					return
				}
				payload := map[string]any{
					"jpeg_base64": base64.StdEncoding.EncodeToString(jpegBytes),
					"hash":        hash,
					"captured_at": time.Now().UnixMilli(),
					"track":       artemisProvider,
				}
				if err := d.client.ReportTestRunCaseFrame(ctx, c.RuntimeID, c.RunCaseID, payload); err != nil {
					d.logger.Debug("artemis live frame relay failed", "run_case_id", c.RunCaseID, "error", err)
					return
				}
				mu.Lock()
				sent[c.RunCaseID] = hash
				mu.Unlock()
			}(c)
		}
		wg.Wait()
		live := make(map[string]struct{}, len(cases))
		for _, c := range cases {
			live[c.RunCaseID] = struct{}{}
		}
		mu.Lock()
		for id := range sent {
			if _, ok := live[id]; !ok {
				delete(sent, id)
			}
		}
		mu.Unlock()
	}
}

// scaleFrameToJPEG turns a screencap PNG into a JPEG at most maxWidth wide,
// averaging each block of source pixels so text stays legible.
func scaleFrameToJPEG(pngBytes []byte, maxWidth int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	rgba, ok := src.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	}
	sw, sh := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	out := image.Image(rgba)
	if sw > maxWidth && maxWidth > 0 {
		dw := maxWidth
		dh := sh * dw / sw
		if dh < 1 {
			dh = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
		for y := 0; y < dh; y++ {
			y0, y1 := y*sh/dh, (y+1)*sh/dh
			if y1 <= y0 {
				y1 = y0 + 1
			}
			for x := 0; x < dw; x++ {
				x0, x1 := x*sw/dw, (x+1)*sw/dw
				if x1 <= x0 {
					x1 = x0 + 1
				}
				var r, g, bl, a, n uint32
				for sy := y0; sy < y1; sy++ {
					row := rgba.Pix[(sy-rgba.Rect.Min.Y)*rgba.Stride:]
					for sx := x0; sx < x1; sx++ {
						i := (sx - rgba.Rect.Min.X) * 4
						r += uint32(row[i])
						g += uint32(row[i+1])
						bl += uint32(row[i+2])
						a += uint32(row[i+3])
						n++
					}
				}
				j := dst.PixOffset(x, y)
				dst.Pix[j], dst.Pix[j+1], dst.Pix[j+2], dst.Pix[j+3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
			}
		}
		out = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
