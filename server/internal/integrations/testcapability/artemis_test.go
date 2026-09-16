package testcapability_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/testcapability"
)

func decodeCall(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, line)
	}
	params, _ := msg["params"].(map[string]any)
	args, _ := params["arguments"].(map[string]any)
	return args
}

func TestArtemisSerial(t *testing.T) {
	for key, want := range map[string]string{
		"android:e8e779fa0822":   "e8e779fa0822",
		" android:emulator-5554": "emulator-5554",
		"android:":               "",
		"ios:00008110":           "",
		"e8e779fa0822":           "",
	} {
		if got := testcapability.ArtemisSerial(key); got != want {
			t.Errorf("ArtemisSerial(%q) = %q, want %q", key, got, want)
		}
	}
}

// The serial an agent passes, or leaves out, never decides the phone: a call
// that would let Artemis pick any idle phone, or name another case's phone,
// reaches Artemis pinned to this case's serial.
func TestPinArtemisRequest_PinsEveryDeviceTool(t *testing.T) {
	for _, tool := range []string{"mobile_run_task", "mobile_get_device_state", "mobile_diagnose"} {
		for name, args := range map[string]string{
			"omitted":     `{"task_desc":"open settings"}`,
			"other phone": `{"task_desc":"open settings","device_serial":"uwmzqsvwxsf6fydu"}`,
			"no args":     ``,
		} {
			params := `{"name":"` + tool + `"}`
			if args != "" {
				params = `{"name":"` + tool + `","arguments":` + args + `}`
			}
			line := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":` + params + "}\n")
			got := decodeCall(t, testcapability.PinArtemisRequest(line, "e8e779fa0822"))
			if got["device_serial"] != "e8e779fa0822" {
				t.Errorf("%s / %s: device_serial = %v, want the pinned phone", tool, name, got["device_serial"])
			}
			if strings.Contains(args, "task_desc") && got["task_desc"] != "open settings" {
				t.Errorf("%s / %s: other arguments must survive, got %v", tool, name, got)
			}
		}
	}
}

// Restarting the adb server or clearing device locks would break every other
// case running on the host, and booting an emulator is not this case's call.
func TestPinArtemisRequest_DiagnoseLosesHostWideFixes(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"mobile_diagnose","arguments":{"attempt_fix":true,"launch_avd":"Pixel_9","probe_device":true}}}`)
	got := decodeCall(t, testcapability.PinArtemisRequest(line, "e8e779fa0822"))
	if got["attempt_fix"] != false {
		t.Errorf("attempt_fix = %v, want false", got["attempt_fix"])
	}
	if _, ok := got["launch_avd"]; ok {
		t.Errorf("launch_avd must be dropped, got %v", got)
	}
	if got["probe_device"] != true {
		t.Errorf("probe_device only reads the pinned phone and must survive, got %v", got)
	}
}

func TestPinArtemisRequest_LeavesEverythingElseAlone(t *testing.T) {
	for name, line := range map[string]string{
		"trace tool":    `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mobile_manage_task","arguments":{"action":"status","trace_id":"t-1"}}}` + "\n",
		"initialize":    `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n",
		"notification":  `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n",
		"not json":      "Content-Length: 12\n",
		"blank":         "\n",
		"number format": `{"jsonrpc":"2.0","id":12345678901234567890,"method":"tools/list"}` + "\n",
	} {
		got := testcapability.PinArtemisRequest([]byte(line), "e8e779fa0822")
		if string(got) != line {
			t.Errorf("%s: rewritten to %q, want it byte for byte", name, got)
		}
	}
}

func TestPinArtemisRequest_Batches(t *testing.T) {
	line := []byte(`[{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mobile_run_task","arguments":{"task_desc":"x","device_serial":"other"}}},{"jsonrpc":"2.0","id":2,"method":"ping"}]`)
	var batch []map[string]any
	if err := json.Unmarshal(testcapability.PinArtemisRequest(line, "e8e779fa0822"), &batch); err != nil {
		t.Fatal(err)
	}
	args := batch[0]["params"].(map[string]any)["arguments"].(map[string]any)
	if len(batch) != 2 || args["device_serial"] != "e8e779fa0822" || batch[1]["method"] != "ping" {
		t.Errorf("batch = %v", batch)
	}
}

// Artemis tells callers to ask the user which phone to use; in a test run the
// description has to say the choice is already made.
func TestDescribePinnedArtemisTools(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"mobile_run_task","description":"When several devices are attached, confirm the target with the user first.","inputSchema":{"type":"object","properties":{"task_desc":{"type":"string"},"device_serial":{"type":"string","description":"confirm with the user"}}}},{"name":"mobile_manage_task","inputSchema":{"type":"object","properties":{"trace_id":{"type":"string"}}}}]}}`)
	out := testcapability.DescribePinnedArtemisTools(line, "e8e779fa0822")
	if !strings.Contains(string(out), "Fixed to e8e779fa0822") {
		t.Errorf("device_serial description not rewritten: %s", out)
	}
	if !strings.Contains(string(out), `"name":"mobile_manage_task"`) || !strings.Contains(string(out), `"id":2`) {
		t.Errorf("the rest of the listing must survive: %s", out)
	}
	unrelated := []byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"ok"}]}}`)
	if got := testcapability.DescribePinnedArtemisTools(unrelated, "x"); !bytes.Equal(got, unrelated) {
		t.Errorf("a result without tools was rewritten: %s", got)
	}
}

// TestHelperArtemisServer is not a test: RunArtemisProxy launches the test
// binary with GO_WANT_HELPER_ARTEMIS=1 as a stand-in for Artemis's MCP
// server. It answers tools/list with one device tool and echoes every
// tools/call's arguments and the environment it was started with.
func TestHelperArtemisServer(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_ARTEMIS") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue
		}
		var result any
		switch req.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name":        "mobile_run_task",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"device_serial": map[string]any{"type": "string"}}},
			}}}
		default:
			result = map[string]any{"arguments": req.Params.Arguments, "notify": os.Getenv("ARTEMIS_DESKTOP_NOTIFY"), "port": os.Getenv("ARTEMIS_DAEMON_PORT")}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		fmt.Println(string(out))
	}
	os.Exit(0)
}

func TestRunArtemisProxy_RelaysAndPins(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_ARTEMIS", "1")
	stdinReader, stdinWriter := io.Pipe()
	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- testcapability.RunArtemisProxy(ctx, testcapability.ArtemisProxyOptions{Serial: "e8e779fa0822", DaemonPort: "18765"}, []string{os.Args[0], "-test.run=^TestHelperArtemisServer$"}, stdinReader, &stdout, io.Discard)
	}()
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mobile_run_task","arguments":{"task_desc":"open settings","device_serial":"uwmzqsvwxsf6fydu"}}}`,
	}
	for _, r := range requests {
		if _, err := io.WriteString(stdinWriter, r+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	_ = stdinWriter.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("proxy returned %v; stdout %s", err, stdout.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one response per request, got %d: %q", len(lines), stdout.String())
	}
	if !strings.Contains(lines[0], "Fixed to e8e779fa0822") {
		t.Errorf("tools/list response not annotated: %s", lines[0])
	}
	var call struct {
		Result struct {
			Arguments map[string]any `json:"arguments"`
			Notify    string         `json:"notify"`
			Port      string         `json:"port"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &call); err != nil {
		t.Fatal(err)
	}
	if call.Result.Arguments["device_serial"] != "e8e779fa0822" {
		t.Errorf("Artemis received device_serial %v, want the pinned phone", call.Result.Arguments["device_serial"])
	}
	if call.Result.Notify != "false" {
		t.Errorf("Artemis must start with desktop notifications off, got %q", call.Result.Notify)
	}
	if call.Result.Port != "18765" {
		t.Errorf("Artemis must share the configured daemon port, got %q", call.Result.Port)
	}
}

func TestRunArtemisProxy_RequiresSerialAndCommand(t *testing.T) {
	if err := testcapability.RunArtemisProxy(context.Background(), testcapability.ArtemisProxyOptions{Serial: " "}, []string{"python"}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Error("an empty serial must be refused: without it Artemis picks any idle phone")
	}
	if err := testcapability.RunArtemisProxy(context.Background(), testcapability.ArtemisProxyOptions{Serial: "e8e779fa0822"}, nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Error("a missing server command must be refused")
	}
	if err := testcapability.RunArtemisProxy(context.Background(), testcapability.ArtemisProxyOptions{Serial: "e8e779fa0822", DaemonPort: "80a"}, []string{"python"}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Error("a malformed daemon port must be refused rather than handed to Artemis")
	}
}
