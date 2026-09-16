package testcapability

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Android phones are driven by Artemis (github.com/google/artemis) on the
// test host (TS-035). The agent does not tap and swipe itself: it hands each
// case's steps to Artemis's own agent through Artemis's MCP server, then
// judges the outcome from what Artemis reports and what the phone shows.
//
// Artemis has no leases. Its MCP tools accept any adb serial and pick an idle
// phone when none is given, so the overlay does not mount Artemis directly: it
// mounts `multica test artemis-mcp`, a stdio proxy that pins every call to the
// phone this case was assigned. That keeps the capability boundary the hub's
// lease used to enforce — a case can neither drive nor diagnose a phone that
// was not bound to it.

// ArtemisProvider is target["provider"] of an android_device capability the
// daemon found through Artemis.
const ArtemisProvider = "artemis"

// ArtemisSerial returns the adb serial an android_device capability key names
// ("android:<serial>"), or "" when the key names none.
func ArtemisSerial(key string) string {
	serial, ok := strings.CutPrefix(strings.TrimSpace(key), "android:")
	if !ok {
		return ""
	}
	return strings.TrimSpace(serial)
}

// targetString reads a string field of a capability target; anything missing
// or not a string reads as "".
func targetString(target map[string]json.RawMessage, key string) string {
	raw, ok := target[key]
	if !ok {
		return ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// artemisServer builds the `artemis` stdio entry for one phone. The paths come
// from the daemon's report (the Artemis checkout's Python and MCP entry point,
// and the multica CLI that proxies them); none of them is a secret. Without
// them there is nothing to launch, so no entry is returned and the agent finds
// no phone mounted, which it records as blocked.
func artemisServer(key string, target map[string]json.RawMessage) (capabilityMCPServer, bool) {
	serial := ArtemisSerial(key)
	python := targetString(target, "artemis_python")
	server := targetString(target, "artemis_server")
	if serial == "" || python == "" || server == "" {
		return capabilityMCPServer{}, false
	}
	cli := targetString(target, "multica_cli")
	if cli == "" {
		cli = "multica"
	}
	args := []string{"test", "artemis-mcp", "--serial", serial}
	if port := targetString(target, "artemis_daemon_port"); validPort(port) {
		args = append(args, "--daemon-port", port)
	}
	return capabilityMCPServer{
		Command: cli,
		Args:    append(args, "--", python, server),
	}, true
}

// validPort reports whether s is a TCP port number.
func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0 && n < 65536
}

// artemisDeviceTools are the Artemis MCP tools that take a device_serial.
// mobile_manage_task and mobile_inspect_trace address a trace id that only the
// task which started it has seen.
var artemisDeviceTools = map[string]bool{
	"mobile_run_task":         true,
	"mobile_get_device_state": true,
	"mobile_diagnose":         true,
}

type jsonRPCEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
}

// PinArtemisRequest rewrites one client→server JSON-RPC message so that every
// device-bearing tool call targets serial. A diagnose call additionally loses
// the host-wide remedies — restarting the adb server, clearing device locks,
// booting an emulator — because other cases may be running on the same host.
// Batches are rewritten element by element; anything else is returned as is.
func PinArtemisRequest(line []byte, serial string) []byte {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return line
	}
	switch trimmed[0] {
	case '[':
		var batch []json.RawMessage
		if json.Unmarshal(trimmed, &batch) != nil {
			return line
		}
		changed := false
		for i, msg := range batch {
			if pinned, ok := pinToolCall(msg, serial); ok {
				batch[i] = pinned
				changed = true
			}
		}
		if !changed {
			return line
		}
		return marshalLine(batch, line)
	case '{':
		if pinned, ok := pinToolCall(trimmed, serial); ok {
			return pinned
		}
	}
	return line
}

func pinToolCall(msg []byte, serial string) ([]byte, bool) {
	var env jsonRPCEnvelope
	if json.Unmarshal(msg, &env) != nil || env.Method != "tools/call" {
		return nil, false
	}
	var call map[string]any
	decoder := json.NewDecoder(bytes.NewReader(msg))
	decoder.UseNumber()
	if decoder.Decode(&call) != nil {
		return nil, false
	}
	params, _ := call["params"].(map[string]any)
	if params == nil {
		return nil, false
	}
	name, _ := params["name"].(string)
	if !artemisDeviceTools[name] {
		return nil, false
	}
	args, _ := params["arguments"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}
	args["device_serial"] = serial
	if name == "mobile_diagnose" {
		args["attempt_fix"] = false
		delete(args, "launch_avd")
	}
	params["arguments"] = args
	out, err := marshalCompact(call)
	if err != nil {
		return nil, false
	}
	return out, true
}

// DescribePinnedArtemisTools rewrites a tools/list result so each
// device_serial parameter says it is fixed. Artemis's own descriptions tell the
// caller to ask the user which phone to use when several are attached; in a
// test run there is no one to ask and the answer is already decided.
func DescribePinnedArtemisTools(line []byte, serial string) []byte {
	var resp map[string]any
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(line)))
	decoder.UseNumber()
	if decoder.Decode(&resp) != nil {
		return line
	}
	result, _ := resp["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	changed := false
	for _, entry := range tools {
		tool, _ := entry.(map[string]any)
		schema, _ := tool["inputSchema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		param, _ := props["device_serial"].(map[string]any)
		if param == nil {
			continue
		}
		param["description"] = fmt.Sprintf("Fixed to %s, the phone Multica assigned to this test case. Any other value is replaced; there is no other phone to choose.", serial)
		changed = true
	}
	if !changed {
		return line
	}
	return marshalLine(resp, line)
}

func marshalCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// marshalLine re-encodes v, falling back to the original line if that fails.
func marshalLine(v any, original []byte) []byte {
	out, err := marshalCompact(v)
	if err != nil {
		return original
	}
	return out
}

// ArtemisProxyEnv is appended to the environment of Artemis's MCP server:
// unbuffered output so stdio frames are not held back, and no desktop toast on
// the test host every time a case's Artemis task finishes.
var ArtemisProxyEnv = []string{"PYTHONUNBUFFERED=1", "ARTEMIS_DESKTOP_NOTIFY=false"}

// ArtemisProxyOptions configures one proxied Artemis MCP server.
type ArtemisProxyOptions struct {
	// Serial is the adb serial every device-bearing call is pinned to.
	Serial string
	// DaemonPort, when set, is the port of the Artemis daemon every case on
	// the host shares (ARTEMIS_DAEMON_PORT). Artemis defaults to 8000, which
	// a dev server on the test host often holds; the daemon's operator picks
	// another once, and every case's proxy passes the same one.
	DaemonPort string
}

// RunArtemisProxy runs argv (Artemis's MCP server) as a child process and
// relays newline-delimited JSON-RPC between it and the MCP client on
// stdin/stdout, pinning device-bearing calls to the serial on the way in and
// marking the device_serial parameter as fixed on the way out.
func RunArtemisProxy(ctx context.Context, opts ArtemisProxyOptions, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	serial := strings.TrimSpace(opts.Serial)
	if serial == "" {
		return errors.New("artemis-mcp: --serial is required")
	}
	if len(argv) == 0 {
		return errors.New("artemis-mcp: the Artemis MCP server command is required after --")
	}
	env := append(os.Environ(), ArtemisProxyEnv...)
	if port := strings.TrimSpace(opts.DaemonPort); port != "" {
		if !validPort(port) {
			return fmt.Errorf("artemis-mcp: --daemon-port %q is not a port", port)
		}
		env = append(env, "ARTEMIS_DAEMON_PORT="+port)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stderr = stderr
	childIn, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("artemis-mcp: %w", err)
	}
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("artemis-mcp: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("artemis-mcp: start %s: %w", argv[0], err)
	}

	var (
		mu        sync.Mutex
		listCalls = map[string]struct{}{}
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(childOut)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				var env jsonRPCEnvelope
				if json.Unmarshal(bytes.TrimSpace(line), &env) == nil && len(env.ID) > 0 {
					mu.Lock()
					_, isList := listCalls[string(env.ID)]
					delete(listCalls, string(env.ID))
					mu.Unlock()
					if isList {
						line = DescribePinnedArtemisTools(line, serial)
						if !bytes.HasSuffix(line, []byte("\n")) {
							line = append(line, '\n')
						}
					}
				}
				if _, err := stdout.Write(line); err != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	reader := bufio.NewReader(stdin)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var env jsonRPCEnvelope
			if json.Unmarshal(bytes.TrimSpace(line), &env) == nil && env.Method == "tools/list" && len(env.ID) > 0 {
				mu.Lock()
				listCalls[string(env.ID)] = struct{}{}
				mu.Unlock()
			}
			hadNewline := bytes.HasSuffix(line, []byte("\n"))
			out := PinArtemisRequest(line, serial)
			if hadNewline && !bytes.HasSuffix(out, []byte("\n")) {
				out = append(out, '\n')
			}
			if _, err := childIn.Write(out); err != nil {
				break
			}
		}
		if readErr != nil {
			break
		}
	}
	_ = childIn.Close()
	<-done
	return cmd.Wait()
}
