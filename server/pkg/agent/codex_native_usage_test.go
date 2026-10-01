package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCodexExecuteReturnsNativeTokenUsageEvidence(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake app-server fixture uses a POSIX shell")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thread-native-usage"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-native-usage","turn":{"id":"turn-native-usage"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-native-usage","turnId":"turn-native-usage","tokenUsage":{"last":{"inputTokens":25,"cachedInputTokens":5,"outputTokens":4,"reasoningOutputTokens":2,"totalTokens":29},"total":{"inputTokens":225,"cachedInputTokens":105,"outputTokens":14,"reasoningOutputTokens":7,"totalTokens":239}}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thread-native-usage","turnId":"turn-native-usage","item":{"type":"agentMessage","id":"answer","text":"done","phase":"final_answer"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-native-usage","turn":{"id":"turn-native-usage","status":"completed","usage":{"input_tokens":999,"cached_input_tokens":0,"output_tokens":999}}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout: 5 * time.Second, SemanticInactivityTimeout: 5 * time.Second,
	})
	if result.Status != "completed" || result.Output != "done" {
		t.Fatalf("result = %+v", result)
	}
	want := TokenUsage{InputTokens: 20, CacheReadTokens: 5, OutputTokens: 4}
	if got := result.Usage["unknown"]; got != want {
		t.Fatalf("result usage = %+v, want native usage %+v", got, want)
	}
	assertUsageEvidence(t, result.ExecutionEvidence, 20, 5, 0, 4, true)
	if result.ExecutionEvidence.ProviderModel.Model != nil || result.ExecutionEvidence.ProviderModel.Source != EvidenceSourceMissing {
		t.Fatalf("provider model evidence = %+v", result.ExecutionEvidence.ProviderModel)
	}
}

func TestCodexNativeTokenUsageUsesCurrentTurnCumulativeDelta(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.threadID = "thread-current"
	c.notificationProtocol = "unknown"
	gate := &codexTurnNotificationGate{}
	c.acceptNotification = gate.accept

	replay := `{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-current","turnId":"turn-old","tokenUsage":{"last":{"inputTokens":50,"cachedInputTokens":10,"outputTokens":4,"reasoningOutputTokens":2,"totalTokens":54},"total":{"inputTokens":900,"cachedInputTokens":250,"outputTokens":40,"reasoningOutputTokens":10,"totalTokens":940}}}}`
	c.handleLine(replay)
	gate.arm()
	c.handleLine(replay)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-current","turn":{"id":"turn-current"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-other","turnId":"turn-current","tokenUsage":{"last":{"inputTokens":999,"cachedInputTokens":0,"outputTokens":999,"reasoningOutputTokens":0,"totalTokens":1998},"total":{"inputTokens":999,"cachedInputTokens":0,"outputTokens":999,"reasoningOutputTokens":0,"totalTokens":1998}}}}`)

	first := `{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-current","turnId":"turn-current","tokenUsage":{"last":{"inputTokens":100,"cachedInputTokens":20,"cacheWriteInputTokens":3,"outputTokens":5,"reasoningOutputTokens":2,"totalTokens":105},"total":{"inputTokens":1000,"cachedInputTokens":300,"cacheWriteInputTokens":10,"outputTokens":50,"reasoningOutputTokens":12,"totalTokens":1050}}}}`
	c.handleLine(first)
	if !c.nativeUsageSeen {
		t.Fatalf("first native notification was not accepted (protocol=%q turn=%q gate=%q)", c.notificationProtocol, c.turnID, gate.turnID)
	}
	c.handleLine(first)
	c.handleLine(`{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-current","turnId":"turn-current","tokenUsage":{"last":{"inputTokens":120,"cachedInputTokens":40,"cacheWriteInputTokens":3,"outputTokens":12,"reasoningOutputTokens":4,"totalTokens":132},"total":{"inputTokens":1120,"cachedInputTokens":340,"cacheWriteInputTokens":13,"outputTokens":62,"reasoningOutputTokens":16,"totalTokens":1182}}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-current","turn":{"id":"turn-current","status":"completed","usage":{"input_tokens":500,"cached_input_tokens":100,"cache_write_input_tokens":7,"output_tokens":30}}}}`)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	want := TokenUsage{InputTokens: 160, CacheReadTokens: 60, CacheWriteTokens: 6, OutputTokens: 17}
	if c.usage != want {
		t.Fatalf("usage = %+v, want cumulative current-turn delta %+v", c.usage, want)
	}
	if !c.nativeUsageSeen {
		t.Fatal("native usage was not marked authoritative")
	}
	assertUsageEvidence(t, c.executionEvidence, 160, 60, 6, 17, true)
	if c.executionEvidence.ProviderModel.Model != nil || c.executionEvidence.ProviderModel.Source != EvidenceSourceMissing {
		t.Fatalf("client configuration leaked into provider model evidence: %+v", c.executionEvidence.ProviderModel)
	}
}

func TestCodexNativeTokenUsageKeepsMalformedPartialEvidenceIncomplete(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.threadID = "thread-current"
	c.notificationProtocol = "raw"
	c.setActiveTurnID("turn-current")
	c.handleLine(`{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-current","turnId":"turn-current","tokenUsage":{"last":{"inputTokens":10,"outputTokens":2,"reasoningOutputTokens":1,"totalTokens":12},"total":{"inputTokens":110,"outputTokens":12,"reasoningOutputTokens":3,"totalTokens":122}}}}`)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.executionEvidence == nil {
		t.Fatal("partial native usage evidence was dropped")
	}
	if c.executionEvidence.Usage.InputUncachedTokens != nil || c.executionEvidence.Usage.InputCacheReadTokens != nil {
		t.Fatalf("missing cache-read bucket was fabricated: %+v", c.executionEvidence.Usage)
	}
	if c.executionEvidence.Usage.InputCacheWriteTokens == nil || *c.executionEvidence.Usage.InputCacheWriteTokens != 0 {
		t.Fatalf("native schema default cache-write zero was not preserved: %+v", c.executionEvidence.Usage)
	}
	if c.executionEvidence.Usage.OutputTokens == nil || *c.executionEvidence.Usage.OutputTokens != 2 || c.executionEvidence.Usage.Complete {
		t.Fatalf("partial native usage was not preserved honestly: %+v", c.executionEvidence.Usage)
	}
}

func TestCodexNativeTokenUsageRetainsBaselineAfterCompletion(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.threadID = "thread-current"
	c.setActiveTurnID("turn-current")
	c.handleRawNotification("thread/tokenUsage/updated", codexThreadTokenUsageParams(
		"thread-current", "turn-current",
		map[string]any{"inputTokens": float64(1000), "cachedInputTokens": float64(300), "outputTokens": float64(50)},
		map[string]any{"inputTokens": float64(100), "cachedInputTokens": float64(20), "outputTokens": float64(5)},
	))
	c.handleRawNotification("turn/completed", map[string]any{
		"threadId": "thread-current", "turn": map[string]any{"id": "turn-current", "status": "completed"},
	})
	c.handleRawNotification("thread/tokenUsage/updated", codexThreadTokenUsageParams(
		"thread-current", "turn-current",
		map[string]any{"inputTokens": float64(1100), "cachedInputTokens": float64(320), "outputTokens": float64(60)},
		map[string]any{"inputTokens": float64(100), "cachedInputTokens": float64(20), "outputTokens": float64(10)},
	))

	if want := (TokenUsage{InputTokens: 160, CacheReadTokens: 40, OutputTokens: 15}); c.usage != want {
		t.Fatalf("late cumulative usage = %+v, want %+v", c.usage, want)
	}
	assertUsageEvidence(t, c.executionEvidence, 160, 40, 0, 15, true)
}

func TestCodexSessionFallbackProducesPartialEvidenceWithoutModelInference(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"timestamp":"2026-09-06T01:00:00Z","type":"turn_context","payload":{"model":"client-configured-model"}}` + "\n" +
		`{"timestamp":"2026-09-06T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":7,"reasoning_output_tokens":3,"total_tokens":107},"model":"client-configured-model"}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	got := parseCodexSessionFile(path)
	if got == nil {
		t.Fatal("expected rollout usage")
	}
	assertUsageEvidence(t, got.executionEvidence, 80, 20, -1, 7, false)
	if got.executionEvidence.Usage.InputCacheWriteTokens != nil {
		t.Fatalf("legacy rollout fabricated cache-write zero: %+v", got.executionEvidence.Usage)
	}
	if got.executionEvidence.ProviderModel.Model != nil || got.executionEvidence.ProviderModel.Source != EvidenceSourceMissing {
		t.Fatalf("turn_context model was treated as provider evidence: %+v", got.executionEvidence.ProviderModel)
	}
}

func TestCodexSessionFallbackPreservesExplicitAllZeroBuckets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"timestamp":"2026-09-06T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"total_tokens":0}}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	got := parseCodexSessionFile(path)
	if got == nil {
		t.Fatal("explicit all-zero rollout usage was dropped")
	}
	assertUsageEvidence(t, got.executionEvidence, 0, 0, 0, 0, true)
}

func TestObservedExecutionEvidenceSeedsCompleteFirstEvent(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	observed := &ExecutionEvidence{
		Usage: UsageEvidence{
			InputUncachedTokens: &zero, InputCacheReadTokens: &zero,
			InputCacheWriteTokens: &zero, OutputTokens: &zero,
			Complete: true, Source: EvidenceSourceProviderEvent,
		},
		ProviderModel: ProviderModelEvidence{Source: EvidenceSourceMissing},
		ProviderCost:  ProviderCostEvidence{Authority: CostAuthorityMissing, Basis: CostBasisMissing, Source: EvidenceSourceMissing},
	}
	got := appendObservedExecutionEvidence(nil, observed)
	if got == nil || !got.Usage.Complete {
		t.Fatalf("first observed event was treated as a missing attempt: %#v", got)
	}
	if merged := MergeExecutionEvidence(nil, observed); merged == nil || merged.Usage.Complete {
		t.Fatalf("missing-attempt merge semantics changed: %#v", merged)
	} else if merged.ProviderModel.Model != nil || merged.ProviderModel.Source != EvidenceSourceMissing {
		t.Fatalf("missing attempt retained provider model identity: %+v", merged.ProviderModel)
	}
}

func TestExecutionEvidenceAttemptAfterMissingFirstEventStaysIncomplete(t *testing.T) {
	t.Parallel()

	model := "provider-model"
	zero := int64(0)
	complete := &ExecutionEvidence{
		Usage: UsageEvidence{
			InputUncachedTokens: &zero, InputCacheReadTokens: &zero,
			InputCacheWriteTokens: &zero, OutputTokens: &zero,
			Complete: true, Source: EvidenceSourceProviderEvent,
		},
		ProviderModel: ProviderModelEvidence{Model: &model, Source: EvidenceSourceProviderEvent},
		ProviderCost:  ProviderCostEvidence{Authority: CostAuthorityMissing, Basis: CostBasisMissing, Source: EvidenceSourceMissing},
	}
	got := appendExecutionEvidenceAttempt(nil, nil, false)
	got = appendExecutionEvidenceAttempt(got, complete, true)
	if got == nil || got.Usage.Complete || got.ProviderModel.Model != nil || got.ProviderModel.Source != EvidenceSourceMissing {
		t.Fatalf("missing first attempt was hidden: %#v", got)
	}
}

func TestClaudeAssistantEvidenceSeedsCompleteFirstEvent(t *testing.T) {
	t.Parallel()

	var msg claudeSDKMessage
	if err := json.Unmarshal([]byte(`{"type":"assistant","message":{"model":"provider-model","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}}`), &msg); err != nil {
		t.Fatalf("decode assistant event: %v", err)
	}
	got := appendObservedExecutionEvidence(nil, claudeAssistantExecutionEvidence(msg))
	if got == nil || !got.Usage.Complete {
		t.Fatalf("first Claude assistant event was marked incomplete: %#v", got)
	}
}

func assertUsageEvidence(t *testing.T, evidence *ExecutionEvidence, input, cacheRead, cacheWrite, output int64, complete bool) {
	t.Helper()
	if evidence == nil {
		t.Fatal("missing execution evidence")
	}
	if evidence.Usage.InputUncachedTokens == nil || *evidence.Usage.InputUncachedTokens != input ||
		evidence.Usage.InputCacheReadTokens == nil || *evidence.Usage.InputCacheReadTokens != cacheRead ||
		evidence.Usage.OutputTokens == nil || *evidence.Usage.OutputTokens != output {
		t.Fatalf("usage evidence = %+v", evidence.Usage)
	}
	if cacheWrite >= 0 && (evidence.Usage.InputCacheWriteTokens == nil || *evidence.Usage.InputCacheWriteTokens != cacheWrite) {
		t.Fatalf("cache-write evidence = %+v, want %d", evidence.Usage.InputCacheWriteTokens, cacheWrite)
	}
	if evidence.Usage.Complete != complete {
		t.Fatalf("usage complete = %v, want %v (%+v)", evidence.Usage.Complete, complete, evidence.Usage)
	}
}
