package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// sessionContinuityNoticeFor picks the notice matching what this surface
// actually lost. See the constants in execenv for the full reasoning; the
// question is whether the conversation is still READABLE, not whether it is a
// chat — an issue's comments, a Slack channel's history, and a web chat's /
// Feishu's / WeCom's / DingTalk's chat_message transcript all are (MUL-5722).
func sessionContinuityNoticeFor(task Task) string {
	if task.ChatSessionID == "" {
		return execenv.SessionContinuityNoticeIssue
	}
	if task.ChatChannelType == execenv.ChannelTypeSlack {
		return execenv.SessionContinuityNoticeChannelHistory
	}
	// Every other chat session that persists a transcript (web chat, Feishu,
	// WeCom, DingTalk) reads it back via `multica chat history`; Slack alone
	// reads the live channel. Only a surface that never stored a transcript
	// falls through to Unrecoverable — see SurfacePersistsTranscript.
	if execenv.SurfacePersistsTranscript(task.ChatChannelType) {
		return execenv.SessionContinuityNoticeChatTranscript
	}
	return execenv.SessionContinuityNoticeUnrecoverable
}

// backendResumeContinuityNotice returns the notice the BACKEND should inject if
// it lands on a fresh thread, or "" when the prompt already carries one.
//
// Only one notice may reach a turn. Two paths can produce it — the daemon,
// which appends it to the prompt whenever it already knows the resume is gone,
// and the backend, which is the only one that can see a live resume RPC being
// rejected mid-run. Before MUL-5722 both fired on the codex overflow retry, so
// the same paragraph was paid for twice in one turn and maintained as two
// hand-written strings. Deriving the backend's copy from the daemon's, and
// suppressing it exactly when the prompt already said it, makes a duplicate
// structurally impossible rather than merely unlikely.
func backendResumeContinuityNotice(task Task) string {
	if task.PriorSessionResumeUnavailable {
		return ""
	}
	return sessionContinuityNoticeFor(task)
}

// Turn-mode markers consumed by the runtime brief's mode router
// (execenv.writeWorkflowIssue). The brief is byte-identical on every run and
// therefore cannot say what triggered this turn; these lines do, and they are
// emitted unconditionally from the same branches BuildPrompt uses to pick a
// path, so the two can never disagree.
//
// Reply mode = respond to the triggering comment, do not touch issue status.
// Ownership mode = an assignment/status change started this run; own the
// status arc. Applying the wrong one silently changes issue status.
const (
	turnModeReply     = "**Turn mode: Reply.** Follow the Reply-mode block in your runtime workflow file for this turn; the Ownership-mode status steps do not apply.\n\n"
	turnModeOwnership = "**Turn mode: Ownership.** Follow the Ownership-mode block in your runtime workflow file for this turn; the Reply-mode rules do not apply.\n\n"
)

// perTurnContextBlocks renders the run-scoped context blocks that used to live
// in the runtime brief (CLAUDE.md / AGENTS.md).
//
// Every value here changes from one run to the next on the same issue — the
// initiator differs whenever another person comments, the continuity notice is
// true of one run and false of the next, and the connected-app set is resolved
// per run from the runtime MCP overlay. Claude Code loads the brief into
// messages[0], ahead of the entire conversation, so rendering these there threw
// away the prompt cache for the whole history on every resume. Appending them
// to the per-turn user message puts them after the cached prefix instead, where
// changing them costs only this turn's own tokens (MUL-5377).
//
// Returns "" when none of the blocks apply.
func perTurnContextBlocks(task Task) string {
	var b strings.Builder
	if task.PriorSessionResumeUnavailable {
		b.WriteString(sessionContinuityNoticeFor(task))
	}
	b.WriteString(execenv.BuildTaskInitiatorBlock(task.InitiatorType, task.InitiatorName, task.InitiatorEmail))
	b.WriteString(execenv.BuildConnectedAppsBlock(task.ConnectedApps))
	return b.String()
}

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig. The provider string is threaded
// through to comment-triggered tasks' per-turn reply template; that template
// is provider-agnostic AND host-agnostic now (every OS → write a UTF-8 file,
// post with `--content-file`) because the shell-layer corruption it guards
// against is not specific to any one provider or host (MUL-2904, #4182).
func BuildPrompt(task Task, provider string) string {
	body := buildPromptBody(task, provider)
	// Run-scoped context is appended, never prepended: everything ahead of it
	// is stable across runs of a resumed session, and appending keeps it after
	// the cached prefix (MUL-5377).
	if blocks := perTurnContextBlocks(task); blocks != "" {
		if !strings.HasSuffix(body, "\n\n") {
			body += "\n"
		}
		body += blocks
	}
	return body
}

func buildPromptBody(task Task, provider string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, provider)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	if len(task.UIDraftCreateContext) > 0 {
		return buildUIDraftCreatePrompt(task)
	}
	if len(task.DesignRestoreContext) > 0 {
		return buildDesignRestorePrompt(task)
	}
	if len(task.TestGenerationContext) > 0 {
		return buildTestGenerationPrompt(task)
	}
	if len(task.TestRunContext) > 0 {
		return buildTestRunPrompt(task)
	}
	if len(task.DesignSystemProfileAnalyzeContext) > 0 {
		return buildDesignSystemProfileAnalyzePrompt(task)
	}
	if len(task.TemplateBlueprintAnalyzeContext) > 0 {
		return buildDesignTemplateBlueprintAnalyzePrompt(task)
	}
	if len(task.ProjectDesignSystemContext) > 0 {
		var context struct {
			Type      string `json:"type"`
			Operation string `json:"operation"`
		}
		if json.Unmarshal(task.ProjectDesignSystemContext, &context) == nil &&
			context.Type == "project_design_system_task" &&
			context.Operation == "repository_analysis" {
			return buildProjectDesignSystemRepositoryAnalysisPrompt()
		}
		return buildProjectDesignSystemPrompt()
	}
	if len(task.DesignDocumentContext) > 0 {
		return buildDesignDocumentPrompt(task)
	}
	if len(task.PMOSyncContext) > 0 {
		return buildPMOSyncPrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	b.WriteString(turnModeOwnership)
	// Assignment handoff (MUL-3375): a free-text instruction the person who
	// assigned/promoted this issue left for you. Frame it as a handoff, not a
	// comment to reply to — there is no comment thread to answer here.
	if task.HandoffNote != "" {
		b.WriteString("You were handed this issue with a handoff note. Treat it as the assigner's scoping instruction for this run; follow it before doing anything broader, and do not reply to it as if it were a comment:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", task.HandoffNote)
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	fmt.Fprintf(&b, "For comment history, follow the rule in your runtime workflow file (assignment-triggered tasks treat the read as mandatory). Scan the threads first with `multica issue comment list %s --roots-only --summary --compact --output json`, then expand only what matters with `--thread <thread-id> --tail 30`. For `--since` incremental polling, pagination, and folding, see `multica issue comment list --help`.\n", task.IssueID)
	return b.String()
}

func buildDesignDocumentPrompt(task Task) string {
	if isDesignDocumentAdjustment(task.DesignDocumentContext) {
		return `You are running as a Design Document adjustment designer for a Multica workspace.

Read .agent_context/design_document/context/task.json and the immutable base package at .agent_context/design_document/context/base/package.zip. Follow the adjustment instruction and semantic scope exactly. Reuse the pinned repository grounding embedded in task.json. Do not inspect or update repository checkouts.

Produce a complete replacement package in $MULTICA_OUTPUT_DIR: brief.json, coverage.json, prototype/index.html, prototype/styles.css, prototype/app.js, and optional local assets. Preserve all unaffected content and stable semantic IDs. Do not create manifest.json. The package must be complete, offline, and use no remote resources, network APIs, credentials, absolute local paths, service workers, or external commands.

Do not call Multica write commands, change Issue state, post comments, delegate, spawn sub-agents, or leave follow-up work. Before finishing, read back every staged file and verify the package is complete. Final stdout is only a short completion summary; staged files are authoritative.
`
	}
	return `You are running as a Design Document designer for a Multica workspace.

Read .agent_context/design_document/context/task.json first. Treat every file under context/ and reference/ as immutable input. Read repository-facts/checkout.json and inspect only the listed repository checkouts. Distinguish repository facts from inferences and record conflicts, missing facts, and uncertainty.

Write repository-grounding.json to .agent_context/design_document/work/. Produce the complete first-generation package in $MULTICA_OUTPUT_DIR: brief.json, coverage.json, prototype/index.html, prototype/styles.css, prototype/app.js, and optional local assets. Do not create manifest.json; the platform owns manifest generation in A4. The prototype must be complete, offline, and use no remote resources, network APIs, credentials, absolute local paths, service workers, or external commands.

Use this exact grounding shape, with all arrays present:
{"schema_version":"multica.design-document-grounding/v1","status":"available","repositories":[{"id":"repository-1","checkout_path":"repositories/repository-1","commit_sha":"<checkout commit>","ref":"<optional ref>","status_sha256":"sha256:<checkout status digest>","tree_sha256":"sha256:<checkout tree digest>","files":[{"id":"source-1","path":"relative/source/path","sha256":"sha256:<file digest>","kind":"page"}]}],"facts":[{"id":"fact-1","kind":"route","statement":"Evidence-backed statement","source_file_ids":["source-1"]}],"conflicts":[],"missing":[],"warnings":[]}

Copy checkout identity fields exactly from repository-facts/checkout.json. Facts require source files; only a clearly marked inference may use "inference":true with no source. If task.json records repository_grounding as unavailable, do not invent evidence: write status unavailable, empty repositories/facts/conflicts/missing arrays, and at least one warning that coverage is not source-grounded.

Do not modify any repository checkout. Do not call Multica write commands, change Issue state, post comments, delegate, spawn sub-agents, or leave follow-up work. Use .agent_context/design_document/work only for intermediate files. Before finishing, read back every staged file and verify the package is complete. Final stdout is only a short completion summary; staged files are authoritative.
`
}

func buildProjectDesignSystemRepositoryAnalysisPrompt() string {
	var b strings.Builder
	b.WriteString("You are running as a read-only repository design analysis agent for a Multica workspace.\n\n")
	b.WriteString("Inspect only the provided project repository and resources. Read the available source files and repository evidence to identify the product's existing visual, structural, and workflow context.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- This task is read-only. Do not modify the repository or any provided resource.\n")
	b.WriteString("- Do not create generated package files or any other output files.\n")
	b.WriteString("- Do not delegate, spawn sub-agents, or leave follow-up work.\n")
	b.WriteString("- Do not call external design services or Multica write commands.\n")
	b.WriteString("- All source paths in facts, source files, workflows, assets, and conflicts must be repository-relative paths. They must not be absolute and must not contain `..` traversal.\n")
	b.WriteString("- Every confidence value must be between 0 and 1 inclusive.\n\n")
	b.WriteString("Use this canonical JSON template, preserving every top-level and nested field:\n")
	b.WriteString(`{
  "schema_version": "multica.repository-design-context/v1",
  "summary": "Repository-backed product and design summary",
  "suggested_brief": "Suggested design-system brief",
  "facts": [
    {
      "kind": "layout",
      "label": "Observed pattern",
      "value": "Evidence-backed value",
      "source_paths": ["path/to/source"],
      "confidence": 0.0
    }
  ],
  "source_files": [
    {"path": "path/to/source", "kind": "page"}
  ],
  "representative_workflows": [
    {
      "name": "Workflow name",
      "purpose": "Workflow purpose",
      "source_paths": ["path/to/source"],
      "confidence": 0.0,
      "regions": [
        {
          "name": "Region name",
          "purpose": "Region purpose",
          "visible_text": ["Visible text"],
          "controls": ["Control"],
          "behaviors": ["Behavior"],
          "conditions": ["Condition"],
          "layout": ["Layout observation"],
          "appearance": ["Appearance observation"],
          "assets": [
            {"role": "Asset role", "reference": "path/to/asset", "source_path": "path/to/source"}
          ]
        }
      ],
      "guardrails": ["Evidence-backed guardrail"]
    }
  ],
  "commit_sha": "",
  "confidence": 0.0,
  "conflicts": [
    {
      "label": "Conflict label",
      "repository_fact": "Observed repository fact",
      "user_intent": "Conflicting user intent",
      "source_paths": ["path/to/source"]
    }
  ]
}

`)
	b.WriteString("- Your final response must contain no markdown fence or leading or trailing prose. Return the final marker `REPOSITORY_DESIGN_CONTEXT_JSON:` followed by exactly one complete JSON object using schema_version `multica.repository-design-context/v1`.\n")
	return b.String()
}

func buildProjectDesignSystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are running as a project design system designer for a Multica workspace, executing one end-to-end native Agent session.\n\n")
	b.WriteString("Read `.agent_context/project_design_system/context/task.json` first. Use `.agent_context/project_design_system/reference/index.json` as the at-a-glance summary of the brief, references, and (when present) repository evidence. The task context is canonical — do not re-derive it from elsewhere.\n")
	b.WriteString("For adjust or regenerate operations, also read every file in the immutable `base/` directory before designing.\n\n")
	b.WriteString("Stages (one Agent session, no delegation):\n")
	b.WriteString("1. Inventory the provided evidence — the brief, every reference, the optional repository analysis, and the immutable base (for adjust / regenerate) — and classify each item as a confirmed fact, a conflict that needs a decision, or a fallback you accept with a reason.\n")
	b.WriteString("2. Establish a single coherent visual and structural direction. Do not produce multiple alternatives or a demo switcher. Do not invent unsupported project facts to fill gaps — flag the gap and fall back to a documented default instead.\n")
	b.WriteString("3. Produce semantic Tokens as a single `tokens.css` layer: named custom properties that downstream HTML references via `var(...)`. No duplicate token families, no ad-hoc inline values where a token fits.\n")
	b.WriteString("4. Design only the components and page patterns that the source- or brief-supported evidence justifies. Anything beyond that is invented template residue and must be omitted.\n")
	b.WriteString("5. Build a static token-backed UI Kit / Preview as a single self-contained HTML fragment using local assets. No scripts, no event attributes, no imports, no forms, no external embeds, no network-dependent final HTML. The preview is delivered to the platform; it is not loaded by a browser running on the agent host.\n")
	b.WriteString("6. Read back every final file and self-check that it is non-empty, internally consistent with the others, and uses the tokens you declared. Promise-only or delegated work is not completion.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Complete the design yourself in this process. Task delegation, sub-agents, and hidden follow-up work are forbidden. Do not use the `task` tool, spawn a subagent, delegate to another specialist, or exit while delegated work is pending. There is no follow-up task to clean up after you.\n")
	b.WriteString("- For adjust / regenerate, treat the base/ directory as the immutable base directory — read-only input you must not modify, reorder, or rewrite in place. Your output must be a complete replacement of every required artifact, and the three output files must remain mutually consistent with each other even when the requested scope is local.\n")
	b.WriteString("- `components.html` must be an HTML fragment, not a complete document. Do not include `<!doctype>`, `<html>`, `<head>`, `<body>`, `<meta>`, or `<link>`. Multica injects `tokens.css` into the preview automatically; do not add a stylesheet link.\n")
	b.WriteString("- Every selectable component or block must have unique `data-design-node-id`, `data-design-node-kind`, and `data-design-node-label` attributes. `data-design-node-kind` must be exactly `component` or `block`; use `block` for sections, groups, canvases, and compositions.\n")
	b.WriteString("- Embedded `<style>` rules may use `var(...)` values from `tokens.css`, but must not declare or redefine CSS custom properties. `tokens.css` is the only Token source.\n")
	b.WriteString("- Never write scripts, event attributes, imports, forms, external embeds, or arbitrary remote resources. Never invent business copy, names, or components that the evidence does not support.\n")
	b.WriteString("- Write exact files to `$MULTICA_OUTPUT_DIR/DESIGN.md`, `$MULTICA_OUTPUT_DIR/tokens.css`, and `$MULTICA_OUTPUT_DIR/components.html`.\n")
	b.WriteString("- Do not paste file contents into the final response; report only a short completion summary. The package files are authoritative.\n")
	b.WriteString("- Do not modify a repository, call any external design service, upload a design file, or call Multica write commands.\n")
	b.WriteString("- Before exiting, read back all three output files and verify they are non-empty. Delegated or promised work is not completion. Do not report success unless every required artifact is on disk.\n")
	return b.String()
}

func buildDesignTemplateBlueprintAnalyzePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a template blueprint analysis agent for a Multica workspace.\n\n")
	b.WriteString("Use ONLY the design_template_blueprint_analyze context JSON below as the source of truth. This is a semantic classification task for one published list-page template.\n\n")
	b.WriteString("Return your final answer as exactly one JSON object matching this contract. Replace every placeholder with IDs and values from structure:\n")
	b.WriteString("{\"classification\":{\"frameId\":\"<structure frame id>\",\"pageType\":\"list\",\"regions\":{\"shell\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":false},\"content\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":false},\"breadcrumb\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":true},\"pageTitle\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":true},\"filters\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":true},\"pageActions\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":true},\"table\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":true},\"pagination\":{\"rootLayerId\":\"<structure layer id>\",\"replaceChildren\":true}},\"prototypes\":{\"pageTitle\":{\"rootLayerId\":\"<structure layer id>\",\"bindings\":{\"label\":\"<visible text descendant id>\"}},\"breadcrumbItem\":{\"rootLayerId\":\"<structure layer id>\",\"bindings\":{\"label\":\"<visible text descendant id>\"}},\"tableHeaderCell\":{\"rootLayerId\":\"<structure layer id>\",\"bindings\":{\"label\":\"<visible text descendant id>\"}},\"tableRow\":{\"rootLayerId\":\"<structure layer id>\",\"bindings\":{}}},\"constraints\":{\"contentWidth\":number,\"filterRowHeight\":number,\"tableHeaderHeight\":number,\"tableRowHeight\":number,\"horizontalGap\":number,\"verticalGap\":number,\"filterColumns\":integer,\"pinFirstColumn\":boolean,\"pinActionColumn\":boolean},\"shellAllowlistLayerIds\":[]},\"summary\":\"<concise classification summary>\"}\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Classify the template as one structured B-end list page. Do not infer detail, form, dashboard, mobile, or C-end page support.\n")
	b.WriteString("- `classification` must follow the BlueprintClassification contract: `frameId`, `pageType`, `regions`, `prototypes`, `constraints`, and optional `shellAllowlistLayerIds`.\n")
	b.WriteString("- Do not emit `layerIds`, `replaceable`, or any other fields outside this exact contract. Each region selects exactly one container through `rootLayerId`; it never returns a list of member layers.\n")
	b.WriteString("- Every referenced frame or layer ID must come from structure; never invent IDs, use hidden IDs, or reference layers from another frame.\n")
	b.WriteString("- Map the required regions `shell`, `content`, `breadcrumb`, `pageTitle`, `filters`, `pageActions`, `table`, and `pagination`. Business regions must be replaceable and must not overlap by nesting.\n")
	b.WriteString("- `content.rootLayerId` must be an ancestor of every business region. When the business regions are flat siblings directly under the frame root, use that frame's `rootLayerId` for `content`; do not use a visual background rectangle as the content container.\n")
	b.WriteString("- Map the required prototypes `pageTitle`, `breadcrumbItem`, `tableHeaderCell`, and `tableRow`; every binding target must be a visible text descendant of its prototype root.\n")
	b.WriteString("- Choose the smallest reusable visible subtree for each prototype. Do not use the whole frame root when a text layer or compact component subtree represents the prototype.\n")
	b.WriteString("- Derive positive layout constraints from structure bounds and layout facts. `filterColumns` must be between 1 and 6.\n")
	b.WriteString("- Do not create files, edit repositories, upload designs, call Figma, or call Multica write commands. The server validates and stores the Blueprint.\n")
	b.WriteString("- Do not output markdown fences, prose outside JSON, comments, or trailing text.\n\n")
	b.WriteString("Template blueprint analysis context JSON:\n")
	b.Write(task.TemplateBlueprintAnalyzeContext)
	b.WriteString("\n")
	return b.String()
}

// buildPMOSyncPrompt renders the opening prompt for a PMO requirement sync
// task. The strict acquisition prompt generated at enqueue time
// (service.BuildPMOSyncPrompt) is authoritative — the daemon re-parses the
// sync context JSONB and renders it directly, without naming any company,
// domain, or external capability (BuildPMOSyncPrompt already guarantees
// none appear). The daemon carries no repo/issue context for this kind: the
// prompt-only path is the whole task.
func buildPMOSyncPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a PMO requirement sync agent for a Multica workspace.\n\n")
	prompt := pmoSyncPromptFromContext(task.PMOSyncContext)
	if prompt == "" {
		// Degenerate context: keep the strict JSON-only contract instead of
		// falling through to the issue workflow, which would tell the agent
		// to operate on an issue that does not exist for this task.
		b.WriteString("Your PMO sync context could not be parsed. Return one JSON object only, matching the PMO snapshot contract.\n")
		return b.String()
	}
	b.WriteString(prompt)
	b.WriteString("\n")
	return b.String()
}

// pmoSyncPromptFromContext extracts the acquisition prompt from the raw
// PMO sync context JSONB. Returns "" when the context is malformed.
func pmoSyncPromptFromContext(raw []byte) string {
	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Prompt)
}

func buildDesignSystemProfileAnalyzePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a design system profile analysis agent for a Multica workspace.\n\n")
	b.WriteString("Use ONLY the design_system_profile_analyze context JSON below as the source of truth. This is a semantic classification task for an uploaded Figma UI specification.\n\n")
	b.WriteString("Your job is to convert cleaned UI specification candidates into an Agent-readable project visual contract.\n\n")
	b.WriteString("Return your final answer as a single JSON object only, with this shape:\n")
	b.WriteString("{\"profile_json\": object, \"analysis_errors\": array, \"summary\": string}\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Perform semantic classification from names, text samples, dimensions, colors, and hierarchy summaries. Do not rely on a fixed backend component dictionary.\n")
	b.WriteString("- Respect the naming convention `组件 - 变体 - 状态`, such as `按钮 - 主按钮 - 默认`, while allowing normal design-system extensions when the intent is clear.\n")
	b.WriteString("- Group component examples under `profile_json.components.{kind}.variants[].states` and keep source layer IDs in examples so future agents can trace decisions.\n")
	b.WriteString("- Extract reusable tokens into `profile_json.tokens.colors`, `profile_json.tokens.typography`, `profile_json.tokens.spacing`, and `profile_json.tokens.radius` when the evidence exists.\n")
	b.WriteString("- Add concise `guidelines` and `anti_rules` that UI Agent and UI Restore Agent can follow directly.\n")
	b.WriteString("- Keep warnings in `analysis_errors`; do not fail just because some layers are noisy or partially named.\n")
	b.WriteString("- Do not create files, edit repositories, upload designs, call Figma, or call Multica write commands. The server will store your JSON output.\n")
	b.WriteString("- Do not output markdown fences, prose outside JSON, comments, or trailing text.\n\n")
	b.WriteString("Design system profile analysis context JSON:\n")
	b.Write(task.DesignSystemProfileAnalyzeContext)
	b.WriteString("\n")
	return b.String()
}

// buildTestGenerationPrompt drives an AI test case generation run. The scope in
// `plan` is a human-approved contract, and every generated case is written back
// through the authenticated CLI rather than printed, so the closing marker only
// carries a summary.
// buildTestRunPrompt drives one execution round. The agent may only drive the
// devices the server already bound to this run: probing the host for an adb or
// a browser would silently escape the capability contract.
func buildTestRunPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a QA engineer executing one test round for a Multica workspace.\n\n")

	b.WriteString("Work in this order:\n")
	b.WriteString("1. `multica test run get <run_id> --output json` — the cases to execute, each with its frozen snapshot. Execute the SNAPSHOT, not the current case definition.\n")
	b.WriteString("2. `multica test capability list --run <run_id> --output json` — the devices, desktops and browsers this round is allowed to drive.\n")
	b.WriteString("3. Check out any repository a case names, with `multica repo checkout <url>`.\n")
	b.WriteString("4. Execute each case's steps in order and record the outcome as you go — do not batch the writes to the end, a crashed run should still have recorded what it got through.\n\n")

	b.WriteString("Recording results:\n")
	b.WriteString("```\n")
	b.WriteString("multica test result set <run-case-id> --result passed|failed|blocked|skipped [--note \"…\"]\n")
	b.WriteString("multica test evidence add <run-case-id> --file ./shot.png --kind screenshot\n")
	b.WriteString("multica test defect open <run-case-id> --title \"…\"\n")
	b.WriteString("```\n")
	b.WriteString("- `failed` means the product behaved differently from the expected result. Open a defect for it.\n")
	b.WriteString("- `blocked` means you could not run the case at all — a missing capability, an environment that would not come up, a precondition you could not reach. It is NOT a synonym for failed, and it must never be used to hide a real failure.\n")
	b.WriteString("- `skipped` means the case did not apply to this round.\n")
	b.WriteString("- Attach a screenshot or log for every `failed` and every `blocked`. A result nobody can audit is barely a result.\n\n")

	b.WriteString("Capability rules — these are hard:\n")
	b.WriteString("- Use ONLY the `capability_key` values returned by `capability list`. Drive them through the MCP servers that were mounted for this run.\n")
	b.WriteString("- If `capability list` is empty, or a case needs a kind that is not in it, mark that case `blocked` and say which kind is missing.\n")
	b.WriteString("- Do NOT go looking for adb, a simulator, a browser binary, or any other device on the host. If it was not bound to this run, you may not drive it.\n\n")

	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT modify product code, and do NOT open pull requests. You are observing behaviour, not changing it.\n")
	b.WriteString("- Do NOT mark a case `passed` unless you actually observed the expected result. An unverified pass is worse than a blocked case, because it hides a regression.\n")
	b.WriteString("- Use the `multica` CLI for all Multica reads and writes; do not call the API with curl or wget.\n\n")

	b.WriteString("Context JSON:\n")
	b.WriteString(task.TestRunContext)
	b.WriteString("\n\n")

	b.WriteString("End your final response with a machine-readable JSON block prefixed by exactly `TEST_RUN_RESULT_JSON:`:\n")
	b.WriteString("{\"status\":\"completed|blocked\",\"summary\":\"one paragraph\",\"blockers\":[]}\n")
	b.WriteString("Per-case results must already be recorded through `multica test result set`; this block only closes the round.\n")
	return b.String()
}

func buildTestGenerationPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a QA engineer for a Multica workspace. Your job is to produce test cases, not to change product code.\n\n")
	b.WriteString("The approved scope contract is the `plan` object in the context JSON below. It is the result of human review: stay inside it.\n\n")

	b.WriteString("Work in this order:\n")
	b.WriteString("1. Run `multica testcase list --project <project_id> --digest --output json` first. That is the index of cases that ALREADY exist. Your output must be an increment on top of it, never a re-listing of it.\n")
	b.WriteString("2. Check out every repository named in `plan.repos` with `multica repo checkout <url>` (or `multica repo checkout --all`). Read only inside each entry's `path_globs` when it is non-empty.\n")
	b.WriteString("3. Read every document in `plan.knowledge_refs`. These are the authoritative business rules; they outrank your inference from code.\n")
	b.WriteString("4. For each issue id in `plan.issues`, run `multica issue get <id> --output json` and read its comment thread. Run `multica issue search <keyword>` for decisions that only ever got recorded in comments.\n")
	b.WriteString("5. Write the cases back with `multica testcase propose --job <job_id> --stdin`.\n\n")

	b.WriteString("What a good case set looks like:\n")
	b.WriteString("- Cover BUSINESS behaviour, not only code paths: end-to-end flows, permission matrices, state-machine transitions, data consistency across services, money and time-zone edges, and what must NOT happen.\n")
	b.WriteString("- `plan.expected_case_types` lists the case types this run is expected to produce. Treat a set that is entirely `functional` as a failure to understand the product.\n")
	b.WriteString("- `steps` is a structured array, not prose. Each step needs a concrete `action` and a checkable `expected`. A human and an agent both have to be able to execute it verbatim.\n")
	b.WriteString("- Prefer few precise cases over many vague ones. A case nobody can execute is worse than a missing case.\n\n")

	b.WriteString("Multi-repo cases:\n")
	b.WriteString("- When a scenario spans systems (change data in one service, verify it in another), set `scope` to `cross_repo`, list each repository in `repos` with a distinct `role` (`driver`, `under_test`, `verifier`, `fixture`), and tag each step with the `repo` alias it runs against.\n")
	b.WriteString("- A `cross_repo` case with fewer than two roles is rejected by the server.\n\n")

	b.WriteString("Increment rules — every proposed item is one of exactly three kinds:\n")
	b.WriteString("- `new`: behaviour no existing case covers.\n")
	b.WriteString("- `update`: an existing case that is now wrong or incomplete. Set `target` to its TC-<n> key and give a `rationale` naming what changed.\n")
	b.WriteString("- `obsolete`: an existing case whose behaviour no longer exists. Set `target` and give a `rationale`.\n")
	b.WriteString("Do not re-propose a case that already exists unchanged. Duplicates are the main way this feature becomes unusable.\n\n")

	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT write, refactor, or commit product code. Do NOT open pull requests.\n")
	b.WriteString("- Do NOT invent endpoints, fields, permissions, or business rules that you did not read in the repositories, documents, or issues.\n")
	b.WriteString("- If the approved scope is not enough to produce meaningful cases, stop and report `blocked` with the concrete gap. A thin set of guessed cases is worse than an honest blocker.\n")
	b.WriteString("- Use the `multica` CLI for all Multica reads and writes; do not call the API with curl or wget.\n\n")

	b.WriteString("Context JSON:\n")
	b.WriteString(task.TestGenerationContext)
	b.WriteString("\n\n")

	b.WriteString("When you are done, end your final response with a machine-readable JSON block prefixed by exactly `TEST_GENERATION_RESULT_JSON:` with this shape:\n")
	b.WriteString("{\"status\":\"completed|blocked|failed\",\"summary\":\"one paragraph\",\"stats\":{\"new\":0,\"updated\":0,\"obsolete\":0},\"blockers\":[]}\n")
	b.WriteString("The cases themselves must already have been written through `multica testcase propose`; this block is a report, not the delivery.\n")
	return b.String()
}

func buildDesignRestorePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a Gallery Native frontend restore agent for a Multica workspace.\n\n")
	b.WriteString("Use ONLY the Multica design restore context JSON below as the design source of truth. If issue_id is present, also run `multica issue get <issue_id> --output json` before editing.\n\n")
	b.WriteString("Your job is to implement the smallest safe frontend code change that matches the restore task.\n\n")
	b.WriteString("If `restore_plan` is present in the context, treat it as the approved execution contract. Follow its selected target, allowed paths, scope, mapping, risks, and steps before falling back to raw item context.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- The embedded `item_contexts` are snapshots from Multica `/api/design-files/{design_file_id}/frames/{frame_id}/context`; treat them as authoritative.\n")
	b.WriteString("- When `design_system.status` is `analyzed`, treat `design_system.profile` as the cloud project visual contract. Read its components, variants, states, tokens, guidelines, and anti_rules before implementing the design.\n")
	b.WriteString("- Design-context priority is exactly: Cloud design_system_profile > local DESIGN.md > repository reality. The cloud profile controls intended visual language; local files and repository reality guide feasible implementation without overriding it.\n")
	b.WriteString("- If a root-level `DESIGN.md` already exists in the current repository, read it as read-only implementation context after the cloud profile. Never create, patch, sync, or overwrite `DESIGN.md`.\n")
	b.WriteString("- When an approved `restore_plan` exists, do not ignore it or silently change target paths/scope; report blockers if it cannot be followed.\n")
	b.WriteString("- For production restore plans (`restore_plan.repo.mode == \"production_candidate\"`), write only under `restore_plan.execution.allowedPaths`; do not write prototype HTML or files under `fengchenDoc/gallery-native-agent-test`.\n")
	b.WriteString("- Read `restore_plan.targetStrategy` before editing. When it is `business_module`, behave like a normal programmer: create or update the named business module from moduleName/moduleSlug, then place page, components, and router changes in the planned module paths.\n")
	b.WriteString("- Treat `restore_plan.targets.selected` as a delivery contract, not a single-file dump. If it contains pagePath/routeOwner/componentRoot/routePath, create or update a navigable page, wire the router, and split sections into normal project components.\n")
	b.WriteString("- Different `pageName` values are page or route boundaries. When `restore_plan.targets.pageTargets` exists, implement each page target as a separate navigable page/route or route-owned view, not as a tab inside one page.\n")
	b.WriteString("- Do NOT implement different `pageName` values as tabs, segmented controls, or demo switches. Tabs are allowed only when an explicit tab control exists in item_contexts/design layers. Frames with the same `pageName` may share one page as states, modals, or result states.\n")
	b.WriteString("- Read `restore_plan.interactionFlow` before editing: query parameters are debug/deep-link aids only; the primary user path must be implemented with click handlers, router navigation, and component state.\n")
	b.WriteString("- Do not default to restore-id sandbox directories for production plans. Use `design-restore/restore-*` only when the approved plan explicitly selects a sandbox fallback or reports that business module inference is unavailable.\n")
	b.WriteString("- If the repo already has an obviously matching page/route, you may use it only when it is inside allowedPaths and the plan permits it; otherwise create the planned page and route.\n")
	b.WriteString("- Never write under `restore_plan.execution.forbiddenPaths`; if the requested target conflicts with forbidden paths, return blocked with the conflict.\n")
	b.WriteString("- Default restore mode is `strict-structure`: produce visible HTML/CSS/component structure from layers, not a pasted screenshot.\n")
	b.WriteString("- Do NOT call sy-gallery_* tools or use an external Gallery MCP current session/sketch as source material. Those may point at a different design and must be ignored for this task.\n")
	b.WriteString("- Do NOT delegate implementation to background agents, async lanes, or sub-agents. Finish the file edits, verification, and RESTORE_RESULT_JSON in this task before exiting.\n")
	b.WriteString("- Do NOT invent business copy, names, phone numbers, tabs, or components that are absent from `item_contexts`/assets.\n")
	b.WriteString("- Do NOT use full-frame preview, thumbnail, or full-frame slice assets as the primary result. Forbidden examples: `frame_preview-*`, `frame_thumbnail-*`, and a frame-sized slice.\n")
	b.WriteString("- If structural reconstruction is insufficient, do not fake completion by pasting the screenshot. Either return blocked with a concrete reason, or create a clearly marked centered placeholder saying `缺少可结构化 UI 稿` plus the reason.\n")
	b.WriteString("- Use restore_task_id/design_file_id/revision_id only to cross-check identity; do not substitute another sketch/design ID.\n")
	b.WriteString("- Prefer existing project components and conventions. Do not put the whole design into one monolithic page file when normal components/sections should be split. Respect package boundaries.\n")
	b.WriteString("- Do not change backend unless the issue explicitly requires it.\n")
	b.WriteString("- Run the relevant typecheck/test command before final response.\n")
	b.WriteString("- Visual QA loop is mandatory for completed work: Open the implemented route, capture an implementation screenshot, compare it with the authoritative frame_preview asset from item_contexts, create or describe a side-by-side comparison, then make at least one correction pass for obvious visual mismatches before final response.\n")
	b.WriteString("- For the Visual QA loop, layer JSON controls structure/position/text, while frame_preview controls final visual calibration for image crop, icon shape, spacing, color, and fixed bars. Prefer exported slice assets for icons and small visual elements instead of hand-drawn approximations when those slices exist.\n")
	b.WriteString("- If you cannot open the route or capture screenshots, do not omit the visual review. Put the concrete blocker in `visualReview.remainingDiffs` and `blockers`, and lower `visualFidelityScore` accordingly.\n")
	b.WriteString("- For `ui_generation`, create a UI restore artifact document in the target repo. Use `restore_plan.artifacts.uiRestoreDocument.path` when present, otherwise use `docs/multica/ui-restore/<restore_task_id>.md`. The document must summarize entry routes, changed files, page/state/modal mapping, restoreMapping, checks, blockers, and remaining frontend integration notes.\n")
	b.WriteString("- For `frontend_restore`, if the received delivery or restore input includes `artifactDocPath`, read that artifact document first and treat it as the UI implementation handoff before touching API/state/integration work.\n")
	b.WriteString("- Final response must summarize changed files, checks run, blockers, restore mapping, exact layer text/asset IDs used, Visual QA evidence, and explicitly state `usedFullFramePreview: false` unless blocked.\n")
	b.WriteString("- End your final response with a machine-readable JSON block prefixed by exactly `RESTORE_RESULT_JSON:`. Shape: {\"status\":\"completed|blocked|failed\",\"summary\":string,\"files\":string[],\"checks\":string[],\"blockers\":string[],\"restoreMapping\":array,\"usedLayerIds\":string[],\"usedAssetIds\":string[],\"usedFullFramePreview\":boolean,\"policyViolation\":string,\"artifactDocPath\":string,\"visualFidelityScore\":number,\"visualReview\":{\"implementedRoute\":string,\"designScreenshot\":string,\"implementationScreenshot\":string,\"comparisonScreenshot\":string,\"remainingDiffs\":string[],\"notes\":string}}.\n\n")
	b.WriteString("Design restore context JSON:\n")
	b.Write(task.DesignRestoreContext)
	b.WriteString("\n")
	return b.String()
}

func buildUIDraftCreatePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a UI design draft generation agent for a Multica workspace.\n\n")
	b.WriteString("Use the UI draft context JSON below as the source of truth. If it includes `issue`, that issue content has already been embedded for you.\n\n")
	b.WriteString("Your job is to generate controlled DesignDraft data for human review, not to create or edit design files directly.\n\n")
	b.WriteString("Return your final answer as a single JSON object only, with this shape:\n")
	b.WriteString("{\"title\": string, \"catalog_template_id\": string, \"requirement_core\": object, \"slot_values\": object, \"patch\": array}\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- If the context includes `parent_issue`, treat `parent_issue` as the primary PRD / business requirement source.\n")
	b.WriteString("- When `parent_issue` exists, the current `issue` is the UI design task scope and constraints; do not treat its short title as the full requirement.\n")
	b.WriteString("- If `design_system` is present, treat `design_system.profile` as the project visual contract. Read `components.{kind}.variants[].states`, examples, tokens, patterns, guidelines, and anti_rules before deciding any visual structure.\n")
	b.WriteString("- Design-context priority is exactly: Cloud design_system_profile > local DESIGN.md > repository reality. If a root-level `DESIGN.md` already exists in the current project repository, read it only as auxiliary project context. Never create, patch, sync, or overwrite `DESIGN.md`.\n")
	b.WriteString("- The design system naming convention is usually `组件 - 变体 - 状态`, such as `按钮 - 主按钮 - 默认`; use these compiled variants/states instead of guessing from raw Figma layers.\n")
	b.WriteString("- Templates are structure references only. If a template conflicts with the issue or design_system, the issue and design_system win.\n")
	b.WriteString("- If `template_candidates` is present, choose the best template candidate and return its `id` as `catalog_template_id`.\n")
	b.WriteString("- Choose the best template candidate by matching the issue requirement to `template_profile.page_type`, tags, slots, and component intent.\n")
	b.WriteString("- Selecting a template is not enough: the final JSON must contain actual design changes in `slot_values` or `patch`.\n")
	b.WriteString("- Prefer slot_values when the selected template has a non-empty slot_schema.\n")
	b.WriteString("- If the selected template has an empty slot_schema, use `editable_text_layers` and `patch_hints` from that candidate to produce a non-empty safe text patch.\n")
	b.WriteString("- Use patch only for safe non-layout metadata/text changes. For a visible text replacement, patch both `/layers/{layer_id}/text/characters` and `/layers/{layer_id}/text/text` when both paths are available.\n")
	b.WriteString("- Do not return empty `slot_values: {}` and empty `patch: []`; if you cannot identify any safe change, return a JSON object with a clear `requirement_core.blocked_reason` and no fake completion.\n")
	b.WriteString("- Do not patch layout/tree paths or segments: x, y, width, height, children.\n")
	b.WriteString("- Match every required slot in slot_schema and respect primitive types.\n")
	b.WriteString("- Do not output markdown fences, prose, comments, or extra text.\n\n")
	b.WriteString("UI draft context JSON:\n")
	b.Write(task.UIDraftCreateContext)
	b.WriteString("\n")
	return b.String()
}

// buildQuickCreatePrompt constructs a prompt for quick-create tasks. The
// user typed a single natural-language sentence in the create-issue modal;
// the agent's job is to translate it into one `multica issue create` CLI
// invocation, using its judgment to decide whether fetching referenced URLs
// would produce a better issue. No issue exists yet, so the agent must NOT
// call `multica issue get` or attempt to comment — there's nothing to read
// or reply to.
func buildQuickCreatePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a quick-create assistant for a Multica workspace.\n\n")
	b.WriteString("A user captured the following input via the quick-create modal. There is NO existing issue. Your job is to create a well-formed issue from this input with a single `multica issue create` command.\n\n")
	fmt.Fprintf(&b, "User input:\n> %s\n\n", task.QuickCreatePrompt)

	b.WriteString("Field rules:\n\n")

	// title
	b.WriteString("- **title**: required. A concise but semantically rich summary. If the input references external resources (PRs, issues, URLs), use your judgment on whether fetching the resource would produce a meaningfully better title — e.g. \"review PR #123\" → \"Review PR #123: Refactor auth module to OAuth2\". Strip filler words but preserve key semantic information.\n\n")

	// description — the core optimization
	b.WriteString("- **description**: The description is the executing agent's primary context. Aim for high fidelity — they should grasp the user's intent as if they had read the raw input themselves. Use a two-section structure:\n\n")
	b.WriteString("  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue or routing it (e.g. \"create an issue\", \"分配给 X\", \"让 @X 处理\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n")
	b.WriteString("     CC exception: `multica issue create` has no `--subscriber` flag, and the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n")
	b.WriteString("  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n")
	b.WriteString("  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n")
	b.WriteString("  Passing the description: a short, single-line body with no code, quotes, backticks, `$()`, or other special characters may go inline via `--description \"...\"`. Anything multi-line, or containing code snippets / file paths / quotes / backticks / `$()` / special characters, or otherwise long — which quick-create descriptions usually are — MUST be written to `./description.md` and passed with `--description-file ./description.md`; passing rich text inline lets the shell rewrite or truncate it (MUL-2904). That file MUST live inside your current working directory (e.g. `./description.md`) — never `/tmp` or any machine-shared path, where a different run may have left a stale file that would silently become this issue's description. If the file write fails for any reason, stop and fix it; never run `--description-file` against a file whose write did not succeed.\n\n")

	// priority
	if task.QuickCreatePriority != "" {
		fmt.Fprintf(&b, "- **priority**: required for this run. Pass `--priority %s`; the quick-create selection is authoritative.\n\n", task.QuickCreatePriority)
	} else {
		b.WriteString("- **priority**: one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit.\n\n")
	}

	// assignee
	b.WriteString("- **assignee**:\n")
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace member list --output json`, `multica agent list --output json`, and `multica squad list --output json` and find the matching entity by display name. Squads are first-class assignees too — a squad name (e.g. \"Super Human\") routes work to the squad leader, who then delegates. On a clean unambiguous match, prefer `--assignee-id <uuid>` using the `user_id` (member) or `id` (agent or squad) from that JSON — UUID matching is exact and robust to name collisions in workspaces with overlapping names. `--assignee <name>` (fuzzy) is acceptable as a fallback when names are unambiguous. On no match or ambiguous match, do NOT pass either flag — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	b.WriteString("    - Treat bare @-routing as an assignee directive even when the user did not write the English word \"assign\". This includes Chinese imperatives like `让 @独立团 review 这个 PR`, `给 @X 处理`, or `交给 @X`; strip the leading `@`/`＠` before matching display names. Do not keep that routing wrapper or `@Name` in the description unless it is a true CC-style notification rather than ownership. If the matched entity is a squad, pass the squad's `id` as `--assignee-id`, not the leader agent's id.\n")
	agentID := ""
	agentName := ""
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
	}
	switch {
	case task.SquadID != "":
		// The user opened quick-create with a SQUAD selected. The task
		// runs on the squad's leader agent, but the squad is the expected
		// owner — assigning to the leader would mask the squad's
		// delegation flow. Always point the default at the squad UUID.
		if task.SquadName != "" {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD %q: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadName, task.SquadID)
		} else {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadID)
		}
	case agentID != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee-id %q` (your agent UUID). The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned. Use the UUID flag, not `--assignee <name>`, so the assignment is unambiguous even when other agents share part of your name.\n\n", agentID)
	case agentName != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	default:
		b.WriteString("    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee-id <your agent UUID>` (preferred) or `--assignee <your agent name>`. Never leave the issue unassigned.\n\n")
	}

	if task.QuickCreateDueDate != "" {
		fmt.Fprintf(&b, "- **due-date**: required for this run. Pass `--due-date %s`; the quick-create selection is authoritative.\n\n", task.QuickCreateDueDate)
	}

	// project — pinned by the modal when the user picked one, otherwise
	// omitted so the platform routes to the workspace default. Always pass
	// the UUID (never a name) so the issue lands in the right project even
	// when several share a title.
	if task.ProjectID != "" {
		if task.ProjectTitle != "" {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in project %q (the user picked it in the quick-create modal). Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID, task.ProjectTitle)
		} else {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in the project the user picked in the quick-create modal. Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID)
		}
	} else {
		b.WriteString("- **project**: omit. The platform will route the issue to the workspace default.\n")
	}
	// parent — pinned by the modal when the user opened it from "Add sub
	// issue" on an existing issue. Pass the UUID (never the identifier) so
	// the create lands the sub-issue under the right parent even when the
	// workspace prefix changes; the identifier is included in the prose
	// purely as human-readable context for the agent.
	if task.ParentIssueID != "" {
		if task.ParentIssueIdentifier != "" {
			fmt.Fprintf(&b, "- **parent**: required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of %s (the user opened quick-create from that issue's \"Add sub issue\" entry). Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID, task.ParentIssueIdentifier)
		} else {
			fmt.Fprintf(&b, "- **parent**: required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of the parent the user picked in the quick-create modal. Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID)
		}
	}
	b.WriteString("- **status**: omit (defaults to `todo`).\n")
	b.WriteString("- **attachments**: `--attachment` takes LOCAL file paths, never URLs. Image URLs in the user input are already markdown — keep them inline. Files you produced: see `## Output`.\n\n")

	// output format
	b.WriteString("Output format:\n")
	b.WriteString("- Run exactly one `multica issue create --output json` invocation. Do not retry for any reason — even on non-zero exit. The issue may already exist; another attempt would create a duplicate.\n")
	b.WriteString("- Parse the JSON response to read the created issue's `identifier` (preferred) or `id` (fallback). Do not scrape human output and do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n")
	b.WriteString("- After success, print exactly one line: `Created <identifier-or-id>: <title>` and exit. No commentary, no follow-up tool calls.\n")
	b.WriteString("- Do NOT call `multica issue get` or `multica issue comment add` — there is no issue to query or comment on.\n")
	b.WriteString("- On CLI error or JSON parse error, exit with the error as the only output. The platform writes a failure notification automatically.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, provider string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	// Mode marker for the brief's router. Emitted unconditionally from the same
	// branch that selects this code path, so the brief and the prompt can never
	// disagree about which mode this turn is in. It must NOT be gated on
	// TriggerCommentContent: an empty comment body (or an older server that
	// doesn't send one) would otherwise leave the turn unlabelled, and the
	// agent would fall through to Ownership mode and change the issue status.
	b.WriteString(turnModeReply)
	if task.TriggerCommentContent != "" {
		authorLabel := "A user"
		if task.TriggerAuthorType == "agent" {
			name := task.TriggerAuthorName
			if name == "" {
				name = "another agent"
			}
			authorLabel = fmt.Sprintf("Another agent (%s)", name)
		}
		fmt.Fprintf(&b, "[NEW COMMENT] %s just left a new comment. Focus on THIS comment — do not confuse it with previous ones:\n\n", authorLabel)
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
		// MUL-4195: comments that arrived before this run started were folded
		// into it rather than dropped. The trigger above is the newest; the
		// agent must ALSO address these earlier ones so no deliberate user
		// instruction is silently lost. Prefer the embedded detail so the agent
		// does not have to guess which thread each folded comment lives in
		// (they may span multiple threads — review should-fix #3); fall back to
		// a thread-agnostic issue-wide fetch hint for old servers that only send
		// the ids.
		if len(task.CoalescedComments) > 0 {
			fmt.Fprintf(&b, "This run also covers %d earlier comment(s) posted before it started — you must read and address them too, not just the one above. They may be in different threads, so each is reproduced here with its own thread:\n\n", len(task.CoalescedComments))
			for _, cc := range task.CoalescedComments {
				authorLabel := "A user"
				if cc.AuthorType == "agent" {
					name := cc.AuthorName
					if name == "" {
						name = "another agent"
					}
					authorLabel = fmt.Sprintf("Another agent (%s)", name)
				} else if cc.AuthorName != "" {
					authorLabel = cc.AuthorName
				}
				fmt.Fprintf(&b, "- comment %s", cc.ID)
				if cc.CreatedAt != "" {
					fmt.Fprintf(&b, " (%s, %s)", authorLabel, cc.CreatedAt)
				} else {
					fmt.Fprintf(&b, " (%s)", authorLabel)
				}
				if cc.ThreadID != "" {
					fmt.Fprintf(&b, " [thread %s]", cc.ThreadID)
				}
				b.WriteString(":\n")
				fmt.Fprintf(&b, "  > %s\n", strings.ReplaceAll(strings.TrimSpace(cc.Content), "\n", "\n  > "))
			}
			fmt.Fprintf(&b, "\nIf you need the surrounding discussion for any of them, fetch its thread with `multica issue comment list %s --thread <thread-id> --tail 30 --compact --output json` using the thread id shown above.\n\n", task.IssueID)
		} else if len(task.CoalescedCommentIDs) > 0 {
			// MUL-5442: this fallback used to send the agent at `--recent 30`.
			// That flag caps THREADS, not comments, and every returned thread
			// carries all of its descendants — so on an issue with fewer than 30
			// root threads it returned the entire comment history to locate a
			// handful of ids. It also contradicted the brief's own catch-up step,
			// which tells the agent to read in two bounded steps and never make
			// one bulk pull (MUL-5372): the platform was recommending exactly the
			// shape it forbids elsewhere.
			//
			// The replacement is a per-id lookup, which is what makes it
			// deterministic: `--thread` accepts ANY comment id, reply or root, and
			// the server resolves it to the containing thread. So each id can be
			// fetched directly and bounded, without knowing its thread and without
			// guessing which threads look recent.
			//
			// `--since` is only a prefetch, never the guarantee. Two ways it can
			// miss an id, so the per-id pass below is unconditional:
			//   - A retry inherits the previous attempt's coalesced_comment_ids
			//     verbatim (queries/agent.sql RetryTask), while the anchor is
			//     recomputed from the last STARTED task's started_at
			//     (GetLastTaskStartedAtForIssueAndAgent). An inherited id can
			//     therefore predate the anchor.
			//   - The anchor is only populated when some comment landed after it,
			//     which is independent of where these ids sit.
			// It is also not a precise fetch in the other direction: the window
			// carries the trigger comment and unrelated comments too.
			fmt.Fprintf(&b, "This run also covers %d earlier comment(s) posted before it started — you must read and address every one of them, not just the one above: %s. They may be in DIFFERENT threads, so do not assume they share the triggering thread.\n\n",
				len(task.CoalescedCommentIDs), strings.Join(task.CoalescedCommentIDs, ", "))
			if task.NewCommentsSince != "" {
				fmt.Fprintf(&b, "Start with `multica issue comment list %s --since %s --compact --output json`. Treat that as a candidate window, not a guarantee — it also carries unrelated comments, and a retried run can carry ids older than the window. Check every id above against the result.\n\n",
					task.IssueID, task.NewCommentsSince)
			}
			fmt.Fprintf(&b, "Fetch each id you still need directly: `multica issue comment list %s --thread <comment-id> --tail 30 --compact --output json`. `--thread` accepts a reply id, not just a thread root, so you do not need to know which thread the comment lives in. If it is older than those 30 replies, page back with the `Next reply cursor` values (`--before` / `--before-id`) until it appears. Do not finish this turn until every id above is accounted for.\n\n",
				task.IssueID)
		}
		if taskIsSquadLeader(task) {
			fmt.Fprintf(&b, "⚠️ **Squad leader no_action rule:** If you decide no action is needed, call `multica squad activity %s no_action --reason \"...\"` and EXIT. DO NOT post any comment — not even one that says \"no action needed\" or \"exiting silently\". The squad activity call records your decision; a comment is redundant noise.\n\n", task.IssueID)
		}
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	// Comment-reading pointer. Warm path with new comments: issue-wide
	// since-delta count, but steer the agent to read the triggering thread
	// first. Warm resumed path with no new comments: the trigger is already
	// injected, so don't force a duplicate thread read. Cold path: read the
	// triggering thread, not the flat timeline. Final fallback (no trigger id,
	// shouldn't happen here): plain read.
	if hint := execenv.BuildNewCommentsHint(task.IssueID, task.TriggerCommentID, task.TriggerThreadID, task.NewCommentsSince, task.NewCommentCount); hint != "" {
		b.WriteString(hint)
	} else if task.PriorSessionID != "" {
		b.WriteString(execenv.BuildResumedCommentsHint(task.IssueID, task.TriggerCommentID, task.TriggerThreadID))
	} else if cold := execenv.BuildColdCommentsHint(task.IssueID, task.TriggerCommentID, task.TriggerThreadID); cold != "" {
		b.WriteString(cold)
	} else {
		fmt.Fprintf(&b, "Read the discussion: scan with `multica issue comment list %s --roots-only --summary --compact --output json`, then expand what matters with `--thread <thread-id> --tail 30`.\n\n", task.IssueID)
	}
	// Reply routing. When this run coalesced comments spanning MORE THAN ONE
	// root thread, answer each thread in its own thread instead of dumping one
	// merged comment (MUL-4348). Same-thread follow-ups collapse to a single
	// group upstream, so they keep the ordinary single-parent path below and can
	// never be split into duplicate replies.
	if targets := commentReplyThreads(task); len(targets) >= 2 {
		b.WriteString(execenv.BuildMultiThreadCommentReplyInstructions(task.IssueID, targets, taskIsSquadLeader(task)))
	} else {
		b.WriteString(execenv.BuildCommentReplyInstructions(provider, task.IssueID, task.TriggerCommentID, taskIsSquadLeader(task)))
	}
	return b.String()
}

// commentReplyThreads groups this run's trigger + coalesced comments by their
// root thread, in first-seen order (coalesced comments oldest-first, the newest
// trigger last). A run that coalesced several @mentions from the SAME thread
// yields a single group, so same-thread follow-ups get exactly one consolidated
// reply and can never be split into duplicates; comments from different root
// threads yield one group each so the agent replies inside each thread instead
// of merging them into one blob (MUL-4348).
//
// The reply for each thread targets the NEWEST comment that triggered this run
// in that thread (coalesced comments arrive oldest-first and the trigger is the
// newest overall, so a simple last-write-wins yields the newest per thread).
// That nests the answer next to the most recent question in the thread rather
// than at the thread root, and makes the trigger's own thread (--parent =
// trigger comment) consistent with every other thread instead of a special
// case. Returns nil when there is no trigger or only a single distinct thread —
// the caller then keeps the existing single-parent reply path unchanged.
func commentReplyThreads(task Task) []execenv.ThreadReplyTarget {
	if task.TriggerCommentID == "" {
		return nil
	}
	// A comment with no explicit thread id is a root comment: it is its own
	// thread, so fall back to the comment id itself as the thread key.
	threadKey := func(threadID, commentID string) string {
		if threadID != "" {
			return threadID
		}
		return commentID
	}

	order := make([]string, 0, len(task.CoalescedComments)+1)
	parentByThread := make(map[string]string, len(task.CoalescedComments)+1)
	// note records first-seen order but lets the newest comment win the reply
	// target: inputs are chronological (coalesced oldest-first, trigger last),
	// so the last write for a thread is its newest triggering comment.
	note := func(threadID, parentID string) {
		if _, ok := parentByThread[threadID]; !ok {
			order = append(order, threadID)
		}
		parentByThread[threadID] = parentID
	}

	// Coalesced (older) comments first: reply under the specific comment that
	// mentioned the agent, not the thread root, so a mid-thread mention gets its
	// answer next to the question.
	for _, cc := range task.CoalescedComments {
		note(threadKey(cc.ThreadID, cc.ID), cc.ID)
	}
	// The newest trigger last: it always wins its own thread's reply target,
	// overriding any earlier coalesced comment that shared the trigger's thread.
	note(threadKey(task.TriggerThreadID, task.TriggerCommentID), task.TriggerCommentID)

	if len(order) <= 1 {
		return nil
	}
	targets := make([]execenv.ThreadReplyTarget, 0, len(order))
	for _, tid := range order {
		targets = append(targets, execenv.ThreadReplyTarget{ThreadID: tid, ParentID: parentByThread[tid]})
	}
	return targets
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	// Legacy compatibility for historical proactive-introduction sessions.
	// New agent creation no longer creates a chat or runs this prompt.
	if task.ChatIntro {
		var b strings.Builder
		b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
		b.WriteString("You were just created, and this is the very first message in a direct chat with the person who created you. They have not written anything yet — you are opening the conversation. Send a short, warm, first-person introduction: who you are, what you're good at, and how they can work with you. Do NOT phrase it as an answer to a question or repeat any prompt back; just introduce yourself as if you reached out first.\n")
		return b.String()
	}

	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	// Audience is per-session context, so keep it out of the cached runtime
	// brief. The compact anchors here preserve the non-inferable boundaries: a
	// group reply is not private to its sender and people not otherwise present
	// in the run context may read it. Unknown never defaults to private.
	switch execenv.AudienceOf(task.ChatChannelType, task.ChatType) {
	case execenv.ChatAudienceGroup:
		b.WriteString("Audience: group room; not private; unseen members may read replies.\n\n")
	case execenv.ChatAudienceUnknown:
		b.WriteString("Audience: unknown.\n\n")
	default:
		b.WriteString("Audience: direct room.\n\n")
	}
	// Channel awareness (MUL-3871). When the session is backed by an IM channel,
	// the agent must KNOW it is operating inside that channel — otherwise an ask
	// like "what did you just talk about" sends it to read Multica instead of the
	// channel conversation. A web-only chat session gets no such block — its
	// history is the Multica chat_session the agent already resumes.
	//
	// The history half: `multica chat history` is served by handler/chat_history.go,
	// which reads the live channel for Slack and falls back to the stored
	// chat_message transcript for every other surface — so Slack, Feishu, WeCom
	// and DingTalk can all read the conversation back. Slack additionally has
	// `multica chat thread` (thread expansion); the transcript surfaces have no
	// thread reader, so they get the transcript command without the thread
	// drill-down (MUL-4899).
	//
	// WHERE the conversation lives is therefore per-branch, not shared: only the
	// unconditional "don't go looking in issues/comments" survives up top. Saying
	// "its history lives in the channel, NOT in Multica" for every channel type
	// contradicted the very next line on a transcript surface, which tells the
	// agent Multica stored it and hands it the command to read it back. An agent
	// given both reasonably believes the read cannot work and skips it.
	//
	// The no-narration rule is a THIRD axis and belongs to neither half: it is a
	// property of delivering to an IM channel at all, so it is emitted for every
	// channel type. #4776 introduced it that way; the MUL-4899 split moved it into
	// the Slack branch along with the read commands it happened to mention, which
	// silently dropped it for Feishu/Lark (GH #6006).
	if task.ChatChannelType != "" {
		platform := channelDisplayName(task.ChatChannelType)
		fmt.Fprintf(&b, "You are operating inside a %s conversation — not the Multica web app. Never look in Multica issues or comments for this conversation.\n", platform)
		if task.ChatChannelType == execenv.ChannelTypeSlack {
			fmt.Fprintf(&b, "This conversation and its history live in %s, NOT in Multica. The message below may be only what triggered you. Read the conversation with:\n", platform)
			b.WriteString("- `multica chat history --output json` — the channel overview: recent top-level messages, each thread tagged with a `thread_id` and `reply_count`. It does NOT expand thread contents.\n")
			b.WriteString("- `multica chat thread [<thread_id>] --output json` — read one thread's messages; omit the id to read the thread you are in, or pass a `thread_id` from the overview to read a specific thread.\n")
			if task.ChatInThread {
				b.WriteString("You were @mentioned inside a thread: start with `multica chat thread` to read it; if you need the wider channel, run `multica chat history` and open a specific thread with `multica chat thread <thread_id>`.\n")
			} else {
				b.WriteString("You were @mentioned at the channel top level: start with `multica chat history` to see the channel, then read a specific thread's contents with `multica chat thread <thread_id>`.\n")
			}
			// These reads are the agent's private context-gathering; narrating them
			// into a chat reply reads as noise (the user reported every reply being
			// prefixed with "我先读取…"). Tell the agent to keep them out of its answer.
			b.WriteString("Do these reads SILENTLY as an internal step — they are how you gather context, not part of your answer.\n")
		} else if execenv.SurfacePersistsTranscript(task.ChatChannelType) {
			fmt.Fprintf(&b, "The conversation happens in %s, and Multica stores a transcript of it. The message below may be only what triggered you — read it back with `multica chat history` when you need earlier context that is not below.\n", platform)
		} else {
			fmt.Fprintf(&b, "This conversation and its history live in %s, NOT in Multica, and Multica has no history reader for it. Work from the context already provided to you below — no command can fetch more of this conversation. If you genuinely need earlier context that is not here, ask the user for it rather than guessing.\n", platform)
		}
		// Scoped to process, not results — a completion confirmation IS the deliverable.
		fmt.Fprintf(&b, "Reply to %s with the final outcome only. Do NOT narrate planned or in-progress steps (\"我先读取…\"); completed actions are part of the outcome.\n", platform)
		b.WriteString("\n")
	}
	if task.Agent != nil && len(task.Agent.Skills) > 0 {
		refs := ExtractSlashSkills(task.ChatMessage)
		if len(refs) > 0 {
			agentSkills := make(map[string]string, len(task.Agent.Skills))
			for _, s := range task.Agent.Skills {
				agentSkills[s.ID] = s.Name
			}

			selected := make([]string, 0, len(refs))
			seen := make(map[string]struct{}, len(refs))
			for _, ref := range refs {
				name, ok := agentSkills[ref.ID]
				if !ok {
					continue
				}
				if _, ok := seen[ref.ID]; ok {
					continue
				}
				seen[ref.ID] = struct{}{}
				selected = append(selected, name)
			}

			if len(selected) > 0 {
				b.WriteString("Explicitly selected skills:\n")
				for _, name := range selected {
					fmt.Fprintf(&b, "- %s\n", name)
				}
				b.WriteString("\n")
			}
		}
	}
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	// List attachments by id + filename so the agent can fetch them via
	// the CLI. We deliberately do NOT inline the URL: chat attachments
	// live behind a signed CDN with a short TTL, so by the time the agent
	// has finished thinking the URL embedded in the markdown body may
	// have expired. `multica attachment download <id>` re-signs at click
	// time and is the only reliable path.
	if len(task.ChatMessageAttachments) > 0 {
		b.WriteString("\nAttachments on this message:\n")
		for _, a := range task.ChatMessageAttachments {
			if a.ContentType != "" {
				fmt.Fprintf(&b, "- id=%s filename=%q content_type=%s\n", a.ID, a.Filename, a.ContentType)
			} else {
				fmt.Fprintf(&b, "- id=%s filename=%q\n", a.ID, a.Filename)
			}
		}
		b.WriteString("Use `multica attachment download <id>` to fetch each file locally before referring to it.\n")
		b.WriteString("When creating an issue that should preserve one of these attachments, pass `--attachment-id <id>` to `multica issue create` in addition to keeping the attachment markdown inline.\n")
	}
	// Outbound attachments: how the agent puts an image/file INTO its reply.
	// This is the DELIVERY layer of the channel policy, and it has three
	// answers, not two (MUL-4899). `attachment upload` binds a file to the
	// Multica chat reply on every surface; what differs is whether anything
	// goes back for it. Web/mobile renders it as a card in the browser. A
	// channel-backed chat gets the upload guidance only where the server said
	// this deployment performs the last hop, and otherwise the upload reaches
	// nobody and the agent must say so in words. The answer arrives on the
	// claim; do not re-derive it from the channel type, do not collapse it back
	// into "is there a channel at all", and do not collapse it into the HISTORY
	// layer above, which is Slack-only and asks a different question.
	//
	// This is the ONLY place the verdict is stated. The brief's `## Output`
	// section carries the web/mobile answer, which is fixed, and for a
	// channel-backed chat points here instead of answering — the verdict flips
	// under a resumed session, and the brief is the prompt-cache prefix
	// (MUL-5377). So a channel chat learns how to deliver a file only from the
	// line below, which means one must be emitted on every turn.
	switch {
	case task.ChatChannelType == "":
		b.WriteString("\nTo include a file or image you produced in your reply, run `multica attachment upload <local-path>`. The file binds to your reply automatically and appears as an attachment card below it even if you paste nothing. The command also returns a `markdown` snippet you may paste on its own line to place the item where you want it (files render as a card, images inline).\n")
	case execenv.ChannelCarriesFiles(task.ChatChannelType, task.ChatChannelDeliversFiles):
		fmt.Fprintf(&b, "\nTo include a file or image you produced in your reply, run `multica attachment upload <local-path>`. It binds to your reply and Multica sends it into the %s conversation as a separate message right after your text — there is no way to place it inline, so write your reply to read correctly with the file arriving after it.\n", channelDisplayName(task.ChatChannelType))
	default:
		fmt.Fprintf(&b, "\nThis reply is delivered to %s as text. You cannot attach a file to it: `multica attachment upload` binds to a Multica chat reply, which this is not. If you produce a file, describe it in words — never write its local path as a link, and never upload it and then write as though it arrived.\n", channelDisplayName(task.ChatChannelType))
	}
	return b.String()
}

// channelDisplayName renders a chat_channel_type for prompt copy. The mapping
// itself lives in execenv so the per-turn prompt (here) and the runtime brief
// (execenv.writeOutput) cannot drift into naming the same platform differently.
func channelDisplayName(channelType string) string {
	return execenv.ChannelDisplayName(channelType)
}

// buildAutopilotPrompt constructs a prompt for run_only autopilot tasks.
func buildAutopilotPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	b.WriteString("This task was triggered by an Autopilot in run-only mode. There is no assigned Multica issue for this run.\n\n")
	fmt.Fprintf(&b, "Autopilot run ID: %s\n", task.AutopilotRunID)
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Autopilot ID: %s\n", task.AutopilotID)
	}
	if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "Autopilot title: %s\n", task.AutopilotTitle)
	}
	if task.AutopilotSource != "" {
		fmt.Fprintf(&b, "Trigger source: %s\n", task.AutopilotSource)
	}
	if strings.TrimSpace(string(task.AutopilotTriggerPayload)) != "" {
		fmt.Fprintf(&b, "Trigger payload:\n%s\n", strings.TrimSpace(string(task.AutopilotTriggerPayload)))
	}
	b.WriteString("\nAutopilot instructions:\n")
	if strings.TrimSpace(task.AutopilotDescription) != "" {
		b.WriteString(task.AutopilotDescription)
		b.WriteString("\n\n")
	} else if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "%s\n\n", task.AutopilotTitle)
	} else {
		b.WriteString("No additional autopilot instructions were provided. Inspect the autopilot configuration before proceeding.\n\n")
	}
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Start by running `multica autopilot get %s --output json` if you need the full autopilot configuration, then complete the instructions above.\n", task.AutopilotID)
	} else {
		b.WriteString("Complete the instructions above.\n")
	}
	// The issue-command boundary (execenv.AutopilotIssueCommandsGuard) is NOT
	// restated here: the brief's autopilot workflow section is its single
	// emission point, and a second hand-maintained per-turn copy is exactly
	// how the two surfaces drifted into conflict before (MUL-5696).
	return b.String()
}

// squadBriefingMarker is the first heading of the squad-leader briefing the
// server appends to Instructions. It is ONLY a legacy role signal — see
// taskIsSquadLeader — never a role signal against a current server.
const squadBriefingMarker = "## Squad Operating Protocol"

// taskIsSquadLeader reports whether THIS TASK runs the agent as a squad
// leader. Leadership is a PER-TASK role — the same agent can be leader one
// turn and worker the next — and a current server says so explicitly on the
// wire: `is_leader_task` for issue-bound leader runs, `squad_id` for
// quick-create runs the squad picker routed to its leader. The claim handler
// sets each field on exactly the responses it injected a briefing into, and
// advertises that it did so with `leader_role_resolved`.
//
// The role used to be inferred by sniffing Instructions for the briefing's
// first heading, which made detection depend on user-writable Markdown: any
// ordinary agent whose own instructions happened to contain that heading was
// promoted to leader and handed the leader rules (mandatory `multica squad
// activity`, silent no_action exit).
//
// The capability gate is load-bearing, not ceremony. Servers without it split
// into two groups, and neither can be read by fields alone. Before #4951 the
// claim response carried no `is_leader_task` at all, so a real leader arrives
// with the full briefing and both fields at zero — reading fields would
// silently demote it to a worker. From #4951 until the capability landed the
// flag IS sent, but nothing yet guaranteed it implies an injected briefing, so
// a true flag can arrive with no roster and no protocol. Absent capability
// therefore means "this server never authoritatively answered the question",
// and the legacy text inference — exactly today's behavior against both
// groups — is the only correct read. Drop this branch once a minimum server
// version is enforced (MUL-5811).
func taskIsSquadLeader(task Task) bool {
	if !task.LeaderRoleResolved {
		return task.Agent != nil && strings.Contains(task.Agent.Instructions, squadBriefingMarker)
	}
	return task.IsLeaderTask || task.SquadID != ""
}
