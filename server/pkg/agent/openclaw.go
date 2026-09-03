package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// openclawNoParseableOutput is the canonical error string surfaced when the
// adapter cannot extract any usable JSON from a run's stdout. The exact
// phrase is depended on by external log-grep / dashboard alerts; do not
// change it without also updating those consumers.
const openclawNoParseableOutput = "openclaw returned no parseable output"

// minOpenclawVersion is the lowest openclaw version that emits its
// --json result on stdout. PR #2101 swapped the adapter from reading
// stderr to stdout; older builds wrote JSON to stderr and now appear
// to silently produce no output. The check in Execute fails fast with
// a hardcoded upgrade hint so users see an actionable message instead
// of "openclaw returned no parseable output".
const minOpenclawVersion = "2026.5.5"

// openclawVersionPattern extracts a three-segment dotted version from
// arbitrary `openclaw --version` output (e.g. "openclaw 2026.5.5",
// "openclaw v2026.5.5 c37871e").
var openclawVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// openclawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var openclawBlockedArgs = map[string]blockedArgMode{
	"--local":         blockedStandalone, // local mode for daemon execution
	"--json":          blockedStandalone, // JSON output for daemon communication
	"--session-id":    blockedWithValue,  // managed by daemon for session resumption
	"--message":       blockedWithValue,  // prompt is set by daemon
	"--model":         blockedWithValue,  // openclaw agent does not accept --model; model is bound at registration via `openclaw agents add/update --model`
	"--system-prompt": blockedWithValue,  // openclaw agent does not accept --system-prompt; instructions are injected into --message
}

// openclawBackend implements Backend by spawning `openclaw agent --message <prompt>
// --output-format stream-json --yes` and reading streaming NDJSON events from
// stdout — similar to the opencode backend.
type openclawBackend struct {
	cfg Config
}

func (b *openclawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "openclaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("openclaw executable not found at %q: %w", execPath, err)
	}

	if err := checkOpenclawVersion(ctx, b.cfg.commandAt(execPath)); err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}
	agentID := ResolveOpenclawAgentID(opts.Model, opts.CustomArgs)
	if agentID == "" {
		agentID = "main"
	}
	stateDir := b.openclawStateDir()
	transcriptOffset := openclawSessionSize(stateDir, agentID, sessionID)
	// Daemon-side tool-call budget (concise mode). OpenClaw 2026.5.x does
	// not stream tool_use events to stdout — tool calls land in the session
	// transcript and the final result blob — so this budget is enforced
	// post-hoc: after stdout settles the transcript's tool calls are
	// counted, and an over-budget run is failed and its process group
	// killed. The kill still matters: production has seen the process
	// linger long after emitting its result blob, holding the task's
	// execution slot. A legacy NDJSON stream with live tool_use events is
	// checked at parse time instead (see processOutputWithFinalText).
	toolBudget := newToolCallBudget(opts.MaxToolCalls)
	args := buildOpenclawArgs(prompt, sessionID, opts, b.cfg.Logger)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		signalProcessGroup(cmd, syscall.SIGKILL)
		return nil
	}
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "agent")))
	// 500ms, matching cursor-agent — the other backend whose CLI can deliver a
	// terminal result while keeping a process alive.
	//
	// Note what WaitDelay actually bounds, because it is easy to get wrong: the
	// timer starts when the context is done OR when Wait observes the child has
	// exited, whichever comes first. So a *clean* exit reaches it too, whenever
	// any descendant still holds one of the pipes os/exec manages — and this
	// backend has such a pipe, since cmd.Stderr below is a plain io.Writer. In
	// that case Wait returns exec.ErrWaitDelay even though the process exited 0,
	// which is why the status switch has to special-case it: the result is
	// already parsed and in hand, and only a tail of stderr logs is lost.
	//
	// Lowering the bound is still right. The delay is only reached when someone
	// is holding a pipe open, and on the cut-short path we deliberately kill a
	// process that is doing exactly that — a long delay there would add its full
	// length to every reply.
	cmd.WaitDelay = 500 * time.Millisecond
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	// openclaw writes its --json output to stdout. Stderr carries log
	// overflow (security warnings, tool errors, etc.) — capture it via a
	// log writer so it surfaces in daemon logs without being fed into the
	// JSON parser.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openclaw stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[openclaw:stderr] ")

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start openclaw: %w", err)
	}

	b.cfg.Logger.Info("openclaw started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so the scanner unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processOutputWithoutFinalText(stdout, msgCh, toolBudget, func() {
			signalProcessGroup(cmd, syscall.SIGKILL)
		})

		// openclaw delivered a complete result but would not exit. Cancel the
		// run context so CommandContext kills it and cmd.Wait can return —
		// otherwise this goroutine parks forever on an agent that has already
		// finished, and the reply never reaches the user. Same protocol-boundary
		// treatment cursor-agent gets on its terminal `result` event.
		if scanResult.cutShort {
			b.cfg.Logger.Warn("openclaw delivered its result but did not exit; "+
				"treating the complete result as the protocol boundary",
				"pid", cmd.Process.Pid)
			cancel()
		}

		// A denied live tool_use (NDJSON path) means the run is over budget
		// right now; kill before waiting so a lingering process cannot hold
		// the task's execution slot.
		if scanResult.budgetExhausted {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}

		// Wait for process exit.
		exitErr := cmd.Wait()
		if !waitProcessGroupGone(cmd, 0) {
			b.cfg.Logger.Warn("openclaw process group still alive after command exit; killing descendants", "pid", cmd.Process.Pid)
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		switch {
		case scanResult.cutShort:
			// A complete result is the protocol boundary. Ignore the
			// cancellation and exit error caused by stopping an openclaw that
			// lingers afterward — the run succeeded, and reporting it as
			// "aborted" would throw away a reply we already hold.
		case runCtx.Err() == context.DeadlineExceeded:
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("openclaw timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		case errors.Is(exitErr, exec.ErrWaitDelay) && scanResult.status == "completed":
			// The process itself exited successfully — that is what
			// ErrWaitDelay means by definition — and only a lingering
			// descendant kept one of os/exec's pipes open past WaitDelay.
			// stdout has already been read to EOF and the result parsed, so the
			// only thing lost is a tail of stderr log lines. Reporting this as
			// a failure would discard a deliverable reply, which is a worse
			// outcome than the hang this whole change fixes.
			//
			// Not folded into the cutShort case above: that path cancels on
			// purpose, and a Cancel call makes Wait report the kill instead of
			// ErrWaitDelay. This case is specifically the clean-exit one.
			//
			// Reword with care: this warning is the only observable proof that
			// this branch ran, so TestOpenclawExecuteToleratesLingeringStderrHolder
			// asserts on the "held a pipe past WaitDelay" fragment. That fragment
			// straddles the concatenation below, so grepping the source for it
			// finds nothing — hence this note.
			b.cfg.Logger.Warn("openclaw exited cleanly but a descendant held a "+
				"pipe past WaitDelay; delivering the parsed result and dropping "+
				"the stderr tail", "pid", cmd.Process.Pid)
		case exitErr != nil && scanResult.status == "completed":
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("openclaw exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("openclaw finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		if messages, err := readOpenclawSessionTranscript(stateDir, agentID, sessionID, transcriptOffset); err != nil {
			b.cfg.Logger.Debug("openclaw session transcript unavailable", "session_id", sessionID, "error", err)
		} else {
			for _, message := range messages {
				if message.Type == MessageToolUse && !scanResult.countedBudget.take(message.CallID) {
					// take consumed a live admission for this CallID (the
					// NDJSON path already charged it) — a miss means the
					// transcript row is the call's only charge point (the
					// 2026.5.x blob path) or a recurrence of an ID whose
					// admission was already consumed, which charges afresh.
					// Calls with no ID take from the unIDed-live pool only
					// while it lasts; after that they charge in transcript
					// order. A denied call kills any process-group
					// descendants that lingered past cmd.Wait and stops
					// forwarding.
					if !toolBudget.Allow() {
						scanResult.budgetExhausted = true
						signalProcessGroup(cmd, syscall.SIGKILL)
						break
					}
				}
				trySend(msgCh, message)
			}
		}
		if scanResult.output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: scanResult.output})
		}

		// Build usage map. Prefer the model openclaw reported in
		// `meta.agentMeta.model` (the actual LLM, e.g. `deepseek-chat`).
		// Fall back to opts.Model — which for openclaw is the agent name
		// passed via `--agent`, not a real model identifier — only when
		// the runtime didn't surface its own model. Last resort is the
		// daemon's `unknown` placeholder.
		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := scanResult.model
			if model == "" {
				model = opts.Model
			}
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		if scanResult.budgetExhausted {
			// Post-hoc enforcement: the run already happened, but budget
			// semantics still apply — the task fails with the budget named,
			// and any still-lingering process group is killed. Check before
			// the Result is assembled so every classifier above is
			// overridden.
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("openclaw: %s (cap %d)", ErrToolBudgetExceeded.Error(), opts.MaxToolCalls)
			signalProcessGroup(cmd, syscall.SIGKILL)
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildOpenclawArgs assembles the argv for a one-shot `openclaw agent` invocation.
//
// The CLI only accepts --local, --json, --session-id, --timeout, --message (and
// flags like --agent / --channel that users pass through CustomArgs). Notably
// it does NOT accept --model or --system-prompt — model is bound at agent
// registration time via `openclaw agents add/update --model`, and instructions
// must be injected inline into --message because openclaw loads AGENTS.md from
// its own workspace directory, not from cwd.
//
// Routing (issue #3260): `openclaw agent` defaults to Gateway routing; --local
// is the embedded-mode opt-in. The daemon historically forced --local so every
// run executed in-process on the daemon host. When opts.OpenclawMode ==
// "gateway" the daemon drops --local so openclaw dials its configured Gateway
// instead — useful when the daemon host is a lightweight coordinator and the
// real agent work should land on a remote machine running the Gateway.
// --local stays in openclawBlockedArgs so users cannot smuggle it back in via
// custom_args under gateway mode (mode is the single source of truth).
func buildOpenclawArgs(prompt, sessionID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"agent"}
	if opts.OpenclawMode != "gateway" {
		args = append(args, "--local")
	}
	args = append(args, "--json", "--session-id", sessionID)
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}
	// OpenClaw binds models to pre-registered agents at `openclaw agents
	// add/update --model` time; the daemon selects one at runtime by
	// passing --agent <id>. The model dropdown populates its list from
	// `openclaw agents list`, so opts.Model here is an agent id (see
	// openclawEntriesToModels — the agent's display name lives in the
	// dropdown label, not in opts.Model). Only inject when the user
	// hasn't already set --agent via custom_args — custom_args wins for
	// backward compatibility with existing configs.
	customArgs := filterCustomArgs(opts.CustomArgs, openclawBlockedArgs, logger)
	if opts.Model != "" && !customArgsContains(customArgs, "--agent") {
		args = append(args, "--agent", opts.Model)
	}
	args = append(args, customArgs...)

	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n" + prompt
	}
	args = append(args, "--message", prompt)
	return args
}

// customArgsContains reports whether args contains the given flag
// (either as a standalone token "--flag" or in "--flag=value" form).
func customArgsContains(args []string, flag string) bool {
	prefix := flag + "="
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// ResolveOpenclawAgentID returns the registered agent ID selected by the same
// model/custom-arg precedence used by buildOpenclawArgs.
func ResolveOpenclawAgentID(model string, customArgs []string) string {
	selected := strings.TrimSpace(model)
	for i := 0; i < len(customArgs); i++ {
		arg := unshellQuoteArg(customArgs[i])
		switch {
		case arg == "--agent":
			if i+1 >= len(customArgs) {
				return ""
			}
			selected = strings.TrimSpace(unshellQuoteArg(customArgs[i+1]))
			i++
		case strings.HasPrefix(arg, "--agent="):
			selected = strings.TrimSpace(strings.TrimPrefix(arg, "--agent="))
		}
	}
	return selected
}

// checkOpenclawVersion runs `<execPath> --version` and returns a
// user-facing error when the installed openclaw is older than
// minOpenclawVersion. The returned error becomes the task's failure
// comment, so the message intentionally names the detected version
// and the upgrade command.
func checkOpenclawVersion(ctx context.Context, runtimeCmd Command) error {
	// This runs synchronously before Execute creates the provider session, so a
	// pipe-holding descendant here would otherwise leave a task marked running
	// with no backend and no inactivity watchdog to stop it. ctx is the task's
	// and can be hours long, so the probe carries the same bound every other
	// provider's version probe gets.
	ctx, cancel := context.WithTimeout(ctx, detectVersionTimeout)
	defer cancel()

	// combinedOutputOwned, for the reason recorded on detectCLIVersion: pipe EOF
	// is the signal that no more output is coming, and the direct child's exit is
	// not. openclaw is npm-installed, so on Windows the direct child is a shim and
	// the real CLI is already a descendant.
	//
	// Both streams are parsed, and extractVersionLine is deliberately bypassed.
	// CombinedOutput rather than separate buffers because a build that prints its
	// banner on stderr must still pass the gate, and one shared writer is what
	// makes os/exec give the two streams a single pipe and therefore one
	// interleaving. parseOpenclawVersion already scans the whole text with
	// openclaw's own version pattern, whereas picking "the first line containing a
	// semver" first would let unrelated stderr noise answer for it — a node
	// deprecation warning carries one.
	cmd := runtimeCmd.exec(ctx, "--version")
	hideAgentWindow(cmd)
	raw, err := combinedOutputOwned(cmd, runtimeCmd.logger)
	out := string(raw)
	detected, parsed := parseOpenclawVersion(out)
	if err != nil {
		// The gate may proceed on a version that arrived before a lingering
		// `openclaw-config` helper held the pipes past WaitDelay — failing here
		// would fail the task over an answer we have. Anything else (non-zero
		// exit, deadline) still fails; see salvageProbeAnswer.
		if !salvageProbeAnswer(runtimeCmd, "--version", parsed, err) {
			// ExplainExecError by hand: detectCLIVersion applies it on the daemon's
			// probe path, and this is the other gate that has to name an ENOEXEC
			// shim rather than report an opaque exec failure (MUL-6164).
			return fmt.Errorf("openclaw --version failed: %w", ExplainExecError(err))
		}
	}
	if !parsed {
		return fmt.Errorf("could not parse openclaw version from output: %q", strings.TrimSpace(out))
	}
	if compareOpenclawVersion(detected, minOpenclawVersion) < 0 {
		return fmt.Errorf("openclaw %s is below the minimum supported version %s. Run `openclaw update` to upgrade and try again.", detected, minOpenclawVersion)
	}
	return nil
}

// parseOpenclawVersion extracts the first three-segment dotted version
// from arbitrary `openclaw --version` output. Returns ok=false when no
// match is found.
func parseOpenclawVersion(raw string) (string, bool) {
	m := openclawVersionPattern.FindString(raw)
	if m == "" {
		return "", false
	}
	return m, true
}

// compareOpenclawVersion compares two three-segment dotted versions
// numerically. Returns -1, 0, or +1 like bytes.Compare. Inputs must be
// well-formed (matched by openclawVersionPattern); malformed segments
// compare as zero.
func compareOpenclawVersion(a, b string) int {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		ai, _ := strconv.Atoi(aParts[i])
		bi, _ := strconv.Atoi(bParts[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

// ── Event handlers ──

// openclawEventResult holds accumulated state from processing the event stream.
type openclawEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage
	// model is the LLM identifier reported by openclaw in its result blob
	// (`meta.agentMeta.model`). Empty when the run did not emit it (older
	// openclaw versions, partial outputs). Distinct from `opts.Model`,
	// which for the openclaw backend is the openclaw *agent* name passed
	// via `--agent`, not the underlying model.
	model string
	// cutShort is true when the run ended because openclaw had delivered a
	// complete result but would not exit, rather than because stdout reached
	// EOF. The caller must cancel the run context before cmd.Wait() in that
	// case — otherwise it waits on a process that never leaves — and must not
	// report the resulting cancellation as an abort. Same treatment
	// cursor-agent gets on its terminal `result` event.
	cutShort bool
	// budgetExhausted is the sticky tool-call-budget verdict. Once a live
	// tool_use event is denied (NDJSON path) it stays true: later events
	// must not overwrite the failure, and Execute kills the process group
	// before cmd.Wait.
	budgetExhausted bool
	// countedBudget is the set of tool calls already charged to the budget
	// (live NDJSON admissions, plus any unterminated-final-line charges from
	// the fallback parse). Execute's session-transcript pass skips these
	// CallIDs so the same call is never charged twice across stream and
	// transcript. Nil when no budget is active.
	countedBudget *liveAdmittedCalls
}

func (b *openclawBackend) processOutputWithoutFinalText(r io.Reader, ch chan<- Message, toolBudget *toolCallBudget, onBudgetExceeded func()) openclawEventResult {
	return b.processOutputWithFinalText(r, ch, false, toolBudget, onBudgetExceeded)
}

// processOutput is the legacy wrapper used by unit tests: no budget.
func (b *openclawBackend) processOutput(r io.Reader, ch chan<- Message) openclawEventResult {
	return b.processOutputWithFinalText(r, ch, true, nil, nil)
}

// processOutputWithFinalText reads openclaw's stdout until the stream
// settles, then parses the buffer as a final result blob and/or NDJSON
// events. toolBudget, when non-nil, counts live tool_use events against the
// daemon-side concise-mode cap.
//
// the parsed result. OpenClaw writes its JSON output to stdout; stderr carries
// log overflow and is captured separately by the caller. The stream may
// contain:
//
//   - A final result JSON (with payloads + meta) — the format openclaw 2026.5.x
//     emits today, typically pretty-printed across many lines
//   - NDJSON streaming events (type: "text", "tool_use", "tool_result", "error",
//     "step_start", "step_finish") — supported for forward compatibility and
//     other backends sharing this code path; openclaw does not emit these today
//
// Implementation note (WOR-10 follow-up): we previously scanned line-by-line
// only, then tried a whole-buffer parse in a fallback path. Under load
// (daemon shutdown racing the scanner, partial chunked reads) the line
// scanner could see truncated input that never reassembled, surfacing the
// generic "openclaw returned no parseable output" error even though the
// agent's work succeeded. We now read the full buffer first and try a
// single whole-buffer parse against the final-result schema. Only if that
// fails do we fall through to the line-by-line NDJSON scanner. This makes
// the dominant happy path (one pretty-printed JSON blob) deterministic
// while keeping NDJSON event support intact.
func (b *openclawBackend) processOutputWithFinalText(r io.Reader, ch chan<- Message, emitFinalText bool, toolBudget *toolCallBudget, onBudgetExceeded func()) openclawEventResult {
	var (
		budgetExhausted atomic.Bool
		// live admissions: each live tool_use line consumed budget exactly
		// once at read time via Allow; counted records them so the final
		// parse and the session-transcript pass skip those calls' duplicates
		// instead of charging them a second time.
		counted liveAdmittedCalls
	)
	onLine := func(line string) {
		if toolBudget == nil {
			return
		}
		event, ok := tryParseOpenclawEvent(line)
		if !ok || event.Type != "tool_use" {
			return
		}
		if !toolBudget.Allow() {
			budgetExhausted.Store(true)
			if onBudgetExceeded != nil {
				onBudgetExceeded()
			}
			return
		}
		counted.addLive(event.CallID)
	}
	buf, cutShort, readErr := readOpenclawStdout(r, openclawResultIdleGrace, onLine)
	if readErr != nil {
		return openclawEventResult{status: "failed", errMsg: fmt.Sprintf("read stdout: %v", readErr), budgetExhausted: budgetExhausted.Load(), countedBudget: &counted}
	}

	// Whole-buffer fast path: openclaw 2026.5.x emits a single pretty-printed
	// JSON result blob. Try parsing the entire buffer (after trimming whitespace
	// and any preceding non-JSON log lines) as the final-result schema. If it
	// matches, we're done — no need to involve the line scanner at all.
	if result, ok := parseWholeBufferOpenclawResult(buf); ok {
		var output strings.Builder
		res := b.buildOpenclawEventResult(result, ch, &output, emitFinalText)
		res.cutShort = cutShort
		res.budgetExhausted = budgetExhausted.Load()
		res.countedBudget = &counted
		return res
	}

	// Fall-back path: parse the buffered NDJSON for transcript/output
	// delivery. Budget enforcement already happened in real time in onLine
	// above as each complete tool_use line arrived — Allow consumed the
	// budget there — so this pass must not charge those calls again: skip
	// duplicates via counted. Only a tool_use line the reader never saw
	// (an unterminated final line) is charged here, exactly once.
	// OpenClaw 2026.5.x emits a single final blob rather than NDJSON, but
	// legacy/future streaming formats get a true in-flight cap.
	scanner := newAgentStreamScanner(bytes.NewReader(buf))
	var output strings.Builder
	var sessionID string
	var model string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string
	gotEvents := false // true if we parsed at least one streaming event or result

	var rawLines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Try parsing as a streaming NDJSON event first.
		if event, ok := tryParseOpenclawEvent(line); ok {
			gotEvents = true
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			switch event.Type {
			case "text":
				if event.Text != "" {
					output.WriteString(event.Text)
					if emitFinalText {
						trySend(ch, Message{Type: MessageText, Content: event.Text})
					}
				}
			case "tool_use":
				var input map[string]any
				if event.Input != nil {
					_ = json.Unmarshal(event.Input, &input)
				}
				if toolBudget != nil && !counted.peek(event.CallID) {
					// A line the live reader never charged: either the run
					// used no live reader path at all, or this is the
					// unterminated final line the reader never delivered.
					// Charge it here and register the admission so the
					// session-transcript pass can take it — the transcript
					// is this call's only other sighting and must not
					// re-charge. peek (not take) for live-sighted lines:
					// their admissions belong to the transcript pass.
					if !toolBudget.Allow() {
						budgetExhausted.Store(true)
						continue
					}
					counted.addLive(event.CallID)
				}
				trySend(ch, Message{
					Type:   MessageToolUse,
					Tool:   event.Tool,
					CallID: event.CallID,
					Input:  input,
				})
			case "tool_result":
				trySend(ch, Message{
					Type:   MessageToolResult,
					Tool:   event.Tool,
					CallID: event.CallID,
					Output: event.Text,
				})
			case "error":
				errMsg := event.errorMessage()
				b.cfg.Logger.Warn("openclaw error event", "error", errMsg)
				trySend(ch, Message{Type: MessageError, Content: errMsg})
				finalStatus = "failed"
				finalError = errMsg
			case "lifecycle":
				phase := event.Phase
				if phase == "error" || phase == "failed" || phase == "cancelled" {
					errMsg := event.errorMessage()
					b.cfg.Logger.Warn("openclaw lifecycle failure", "phase", phase, "error", errMsg)
					trySend(ch, Message{Type: MessageError, Content: errMsg})
					finalStatus = "failed"
					finalError = errMsg
				}
			case "step_start":
				trySend(ch, Message{Type: MessageStatus, Status: "running"})
			case "step_finish":
				if event.Usage != nil {
					u := parseOpenclawUsage(event.Usage)
					usage.InputTokens += u.InputTokens
					usage.OutputTokens += u.OutputTokens
					usage.CacheReadTokens += u.CacheReadTokens
					usage.CacheWriteTokens += u.CacheWriteTokens
				}
			}
			continue
		}

		// Try parsing as a final result blob (legacy format).
		if result, ok := tryParseOpenclawResult(line); ok {
			gotEvents = true
			res := b.buildOpenclawEventResult(result, ch, &output, emitFinalText)
			if res.sessionID != "" {
				sessionID = res.sessionID
			}
			if res.model != "" {
				model = res.model
			}
			// Prefer usage from the final result if no streaming events reported it.
			u := res.usage
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
				usage = u
			}
			continue
		}

		// Not JSON — treat as log line.
		b.cfg.Logger.Debug("[openclaw:stdout] " + line)
		rawLines = append(rawLines, line)
	}

	if err := scanner.Err(); err != nil {
		// Carry the budget verdict and the already-charged set even on a
		// mid-parse failure: Execute's transcript pass still runs, and
		// without countedBudget it would re-charge every live-admitted
		// call.
		return openclawEventResult{
			status:          "failed",
			errMsg:          fmt.Sprintf("read stdout: %v", err),
			budgetExhausted: budgetExhausted.Load(),
			countedBudget:   &counted,
		}
	}

	// If we got no events at all, fall back to raw output. The whole-buffer
	// fast path above already tried the structured-result parse — by the time
	// we reach here the buffer truly is unstructured (just log lines, plain
	// text, or empty). Surface the trimmed text as a completed run when we
	// have any, otherwise the canonical no-parseable-output failure.
	if !gotEvents {
		trimmed := strings.TrimSpace(strings.Join(rawLines, "\n"))
		if trimmed != "" {
			return openclawEventResult{status: "completed", output: trimmed, budgetExhausted: budgetExhausted.Load(), countedBudget: &counted}
		}
		return openclawEventResult{
			status:          "failed",
			errMsg:          openclawNoParseableOutput,
			budgetExhausted: budgetExhausted.Load(),
			countedBudget:   &counted,
		}
	}

	// budgetExhausted wins over any streamed terminal status: a completed
	// blob after a denied call is still an over-budget run.
	if budgetExhausted.Load() {
		finalStatus = "failed"
		finalError = ErrToolBudgetExceeded.Error()
	}
	return openclawEventResult{
		status:          finalStatus,
		errMsg:          finalError,
		output:          output.String(),
		sessionID:       sessionID,
		usage:           usage,
		model:           model,
		budgetExhausted: budgetExhausted.Load(),
		countedBudget:   &counted,
	}
}

// parseWholeBufferOpenclawResult attempts to parse the entire stdout buffer
// as a single openclaw final-result JSON blob (the format openclaw 2026.5.x
// emits today, almost always pretty-printed across multiple lines).
//
// It first tries the buffer as-is, then strips any leading non-JSON log
// lines (lines that don't start with '{' at column 0) so a daemon log
// preamble doesn't defeat the parse. It does NOT scan into the middle of
// log lines: only line starts that begin with '{' are considered candidate
// JSON entry points, mirroring the conservative behaviour of
// tryParseOpenclawResult.
func parseWholeBufferOpenclawResult(buf []byte) (openclawResult, bool) {
	trimmed := strings.TrimSpace(string(buf))
	if trimmed == "" {
		return openclawResult{}, false
	}
	if result, ok := tryParseOpenclawResult(trimmed); ok {
		return result, true
	}
	// Strip any leading log lines that precede the JSON blob.
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		if len(line) > 0 && line[0] == '{' {
			candidate := strings.TrimSpace(strings.Join(lines[i:], "\n"))
			if result, ok := tryParseOpenclawResult(candidate); ok {
				return result, true
			}
			return openclawResult{}, false
		}
	}
	return openclawResult{}, false
}

// tryParseOpenclawEvent attempts to parse a line as a streaming NDJSON event.
// Returns the event and true if the line is a valid event with a known type.
func tryParseOpenclawEvent(line string) (openclawEvent, bool) {
	if len(line) == 0 || line[0] != '{' {
		return openclawEvent{}, false
	}
	var event openclawEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return openclawEvent{}, false
	}
	if event.Type == "" {
		return openclawEvent{}, false
	}
	return event, true
}

// tryParseOpenclawResult attempts to parse a line as a final result blob
// (the legacy format with payloads + meta). Lines must start with '{' to be
// considered — we no longer scan for braces at arbitrary positions, which
// avoids false matches on log lines containing JSON fragments.
func tryParseOpenclawResult(raw string) (openclawResult, bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return openclawResult{}, false
	}
	var result openclawResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return openclawResult{}, false
	}
	if result.Payloads == nil && result.Meta.DurationMs == 0 {
		return openclawResult{}, false
	}
	return result, true
}

// buildOpenclawEventResult extracts text and metadata from a final result blob.
// Text payloads are appended to the shared output builder and optionally emitted to ch.
func (b *openclawBackend) buildOpenclawEventResult(result openclawResult, ch chan<- Message, output *strings.Builder, emitText bool) openclawEventResult {
	for _, p := range result.Payloads {
		if p.Text != "" {
			output.WriteString(p.Text)
			if emitText {
				trySend(ch, Message{Type: MessageText, Content: p.Text})
			}
		}
	}

	var sessionID string
	var model string
	var usage TokenUsage
	if result.Meta.AgentMeta != nil {
		if sid, ok := result.Meta.AgentMeta["sessionId"].(string); ok {
			sessionID = sid
		}
		// `meta.agentMeta.model` is openclaw's true LLM identifier
		// (e.g. "deepseek-chat", "claude-sonnet-4"). Take it as-is — the
		// dashboard expects whatever string the runtime reports, mirroring
		// claude/pi/codex which read model directly off their stream.
		if m, ok := result.Meta.AgentMeta["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
		if u, ok := result.Meta.AgentMeta["usage"].(map[string]any); ok {
			usage = parseOpenclawUsage(u)
		}
	}

	return openclawEventResult{
		status:    "completed",
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
		model:     model,
	}
}

// parseOpenclawUsage extracts token usage from a map, supporting multiple
// field name conventions used by different OpenClaw versions and PaperClip:
//
//	input / inputTokens / input_tokens
//	output / outputTokens / output_tokens
//	cacheRead / cachedInputTokens / cached_input_tokens / cache_read
//	cacheWrite / cacheCreationInputTokens / cache_creation_input_tokens / cache_write
func parseOpenclawUsage(data map[string]any) TokenUsage {
	return TokenUsage{
		InputTokens:      openclawInt64FirstOf(data, "input", "inputTokens", "input_tokens"),
		OutputTokens:     openclawInt64FirstOf(data, "output", "outputTokens", "output_tokens"),
		CacheReadTokens:  openclawInt64FirstOf(data, "cacheRead", "cachedInputTokens", "cached_input_tokens", "cache_read", "cache_read_input_tokens"),
		CacheWriteTokens: openclawInt64FirstOf(data, "cacheWrite", "cacheCreationInputTokens", "cache_creation_input_tokens", "cache_write"),
	}
}

// openclawInt64FirstOf returns the first non-zero int64 value found under any
// of the given keys. This supports field name variants across protocol versions.
func openclawInt64FirstOf(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v := openclawInt64(data, key); v != 0 {
			return v
		}
	}
	return 0
}

// openclawInt64 safely extracts an int64 from a JSON-decoded map value (which
// may be float64 due to Go's JSON number handling).
func openclawInt64(data map[string]any, key string) int64 {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// ── JSON types for `openclaw agent --json` output ──

// openclawEvent represents a single streaming NDJSON event from openclaw --json.
//
// Event types:
//   - "text"        — text output (text field)
//   - "tool_use"    — tool invocation (tool, callId, input)
//   - "tool_result" — tool output (tool, callId, text)
//   - "error"       — error (text, or structured error object)
//   - "lifecycle"   — phase changes (phase: "error"/"failed"/"cancelled")
//   - "step_start"  — agent step begins
//   - "step_finish" — agent step ends (usage)
type openclawEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callId,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Usage     map[string]any  `json:"usage,omitempty"`
	Phase     string          `json:"phase,omitempty"`   // lifecycle event phase
	Error     *openclawError  `json:"error,omitempty"`   // structured error object
	Message   string          `json:"message,omitempty"` // alternative error message field
}

// errorMessage extracts a human-readable error message from the event,
// checking multiple fields: structured error object, text, message, or fallback.
func (e openclawEvent) errorMessage() string {
	if e.Error != nil {
		if msg := e.Error.message(); msg != "" {
			return msg
		}
	}
	if e.Text != "" {
		return e.Text
	}
	if e.Message != "" {
		return e.Message
	}
	return "unknown openclaw error"
}

func (b *openclawBackend) openclawStateDir() string {
	if stateDir := strings.TrimSpace(b.cfg.Env["OPENCLAW_STATE_DIR"]); stateDir != "" {
		return stateDir
	}
	if stateDir := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR")); stateDir != "" {
		return stateDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw")
}

type openclawSessionRecord struct {
	Type    string                 `json:"type"`
	Message openclawSessionMessage `json:"message"`
}

type openclawSessionMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
}

type openclawSessionContent struct {
	Type     string          `json:"type"`
	Thinking string          `json:"thinking"`
	Text     string          `json:"text"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

func openclawSessionPath(stateDir, agentID, sessionID string) (string, error) {
	if stateDir == "" || filepath.Base(agentID) != agentID || filepath.Base(sessionID) != sessionID {
		return "", fmt.Errorf("invalid openclaw session path")
	}
	return filepath.Join(stateDir, "agents", agentID, "sessions", sessionID+".jsonl"), nil
}

func openclawSessionSize(stateDir, agentID, sessionID string) int64 {
	path, err := openclawSessionPath(stateDir, agentID, sessionID)
	if err != nil {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func readOpenclawSessionTranscript(stateDir, agentID, sessionID string, offset int64) ([]Message, error) {
	path, err := openclawSessionPath(stateDir, agentID, sessionID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}

	scanner := newAgentStreamScanner(file)
	var messages []Message
	for scanner.Scan() {
		var record openclawSessionRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.Type != "message" {
			continue
		}
		switch record.Message.Role {
		case "user":
			messages = nil
		case "assistant":
			var content []openclawSessionContent
			if json.Unmarshal(record.Message.Content, &content) != nil {
				continue
			}
			for _, item := range content {
				switch item.Type {
				case "thinking":
					if item.Thinking != "" {
						messages = append(messages, Message{Type: MessageThinking, Content: item.Thinking})
					}
				case "toolCall":
					var input map[string]any
					if len(item.Input) > 0 {
						_ = json.Unmarshal(item.Input, &input)
					}
					messages = append(messages, Message{Type: MessageToolUse, Tool: item.Name, CallID: item.ID, Input: input})
				}
			}
		case "toolResult":
			output := openclawSessionText(record.Message.Content)
			messages = append(messages, Message{
				Type:   MessageToolResult,
				Tool:   record.Message.ToolName,
				CallID: record.Message.ToolCallID,
				Output: output,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func openclawSessionText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var content []openclawSessionContent
	if json.Unmarshal(raw, &content) != nil {
		return ""
	}
	var output strings.Builder
	for _, item := range content {
		if item.Type == "text" {
			output.WriteString(item.Text)
		}
	}
	return output.String()
}

// openclawError represents a structured error in an openclaw event,
// compatible with PaperClip's error format (name + data.message).
type openclawError struct {
	Name    string             `json:"name,omitempty"`
	Data    *openclawErrorData `json:"data,omitempty"`
	Message string             `json:"message,omitempty"`
}

func (e *openclawError) message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type openclawErrorData struct {
	Message string `json:"message,omitempty"`
}

// openclawResult represents the final JSON output from `openclaw agent --json`
// (the legacy single-blob format with payloads + meta).
type openclawResult struct {
	Payloads []openclawPayload `json:"payloads"`
	Meta     openclawMeta      `json:"meta"`
}

type openclawPayload struct {
	Text string `json:"text"`
}

type openclawMeta struct {
	DurationMs int64          `json:"durationMs"`
	AgentMeta  map[string]any `json:"agentMeta"`
}
