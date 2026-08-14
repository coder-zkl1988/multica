package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/opendesign"
)

type openDesignSupervisor interface {
	Run(context.Context, opendesign.SupervisorRunRequest) (opendesign.SupervisorRunResult, error)
}

type openDesignScratchPreparer func(context.Context) (*execenv.Environment, error)

type openDesignSupervisorFactory func(openDesignScratchPreparer) (openDesignSupervisor, error)

type openDesignPreparedWorker struct {
	opendesign.WorkerAPI
	prepare openDesignScratchPreparer
}

func (w *openDesignPreparedWorker) PrepareWorkspace(ctx context.Context, request opendesign.WorkerWorkspaceRequest) (opendesign.WorkerWorkspace, error) {
	if w.prepare == nil {
		return opendesign.WorkerWorkspace{}, errors.New("Open Design scratch preparer is required")
	}
	env, err := w.prepare(ctx)
	if err != nil {
		return opendesign.WorkerWorkspace{}, err
	}
	request.ScratchRoot = env.WorkDir
	return w.WorkerAPI.PrepareWorkspace(ctx, request)
}

type openDesignTaskContext struct {
	Type               string                    `json:"type"`
	Operation          string                    `json:"operation"`
	BasePackage        json.RawMessage           `json:"base_package,omitempty"`
	RepositoryAnalysis json.RawMessage           `json:"repository_analysis,omitempty"`
	OpenDesignRun      opendesign.TaskRunContext `json:"open_design_run"`
}

func (d *Daemon) newOpenDesignSupervisor(prepare openDesignScratchPreparer) (openDesignSupervisor, error) {
	if strings.TrimSpace(d.cfg.OpenDesignWorkerURL) == "" {
		return nil, errors.New("Open Design worker URL is not configured")
	}
	if strings.TrimSpace(d.cfg.OpenDesignArtifactRoot) == "" {
		return nil, errors.New("Open Design artifact root is not configured")
	}
	if strings.TrimSpace(d.cfg.OpenDesignBrowserPath) == "" {
		return nil, errors.New("Open Design Preview browser is not configured")
	}
	httpClient := &http.Client{}
	workerClient, err := opendesign.NewWorkerClient(d.cfg.OpenDesignWorkerURL, d.cfg.OpenDesignWorkerToken, httpClient)
	if err != nil {
		return nil, err
	}
	worker := &openDesignPreparedWorker{WorkerAPI: workerClient, prepare: prepare}
	probe, err := opendesign.NewProbeClient(d.cfg.OpenDesignWorkerURL, d.cfg.OpenDesignWorkerToken, httpClient)
	if err != nil {
		return nil, err
	}
	preview, err := opendesign.NewChromiumPreviewVerifier(d.cfg.OpenDesignBrowserPath)
	if err != nil {
		return nil, err
	}
	return opendesign.NewSupervisor(opendesign.SupervisorConfig{
		ArtifactRoot: d.cfg.OpenDesignArtifactRoot,
		Worker:       worker,
		Probe:        probe,
		Callbacks:    d.client,
		Preview:      preview,
	})
}

func (d *Daemon) handleOpenDesignTask(ctx context.Context, task Task, provider string, slot int, taskLog *slog.Logger) bool {
	var envelope struct {
		OpenDesignRun json.RawMessage `json:"open_design_run"`
	}
	if len(task.ProjectDesignSystemContext) == 0 || json.Unmarshal(task.ProjectDesignSystemContext, &envelope) != nil || len(envelope.OpenDesignRun) == 0 {
		return false
	}

	var taskContext openDesignTaskContext
	if err := json.Unmarshal(task.ProjectDesignSystemContext, &taskContext); err != nil || taskContext.Type != "project_design_system_task" {
		d.failOpenDesignTask(ctx, task, "invalid Open Design task context", "open_design_context_invalid", "")
		return true
	}
	if adapterID, ok := opendesign.ResolveAdapter(provider); !ok || adapterID != taskContext.OpenDesignRun.Agent.AdapterID {
		d.failOpenDesignPreflight(ctx, task, taskContext.OpenDesignRun, fmt.Errorf("runtime provider %q does not match adapter %q", provider, taskContext.OpenDesignRun.Agent.AdapterID))
		return true
	}

	envRoot := execenv.PredictRootDir(d.cfg.WorkspacesRoot, task.WorkspaceID, task.ID)
	d.markActiveEnvRoot(envRoot)
	defer d.unmarkActiveEnvRoot(envRoot)

	factory := d.openDesignSupervisorFactory
	if factory == nil {
		factory = d.newOpenDesignSupervisor
	}
	var env *execenv.Environment
	supervisor, err := factory(func(prepareCtx context.Context) (*execenv.Environment, error) {
		if err := prepareCtx.Err(); err != nil {
			return nil, err
		}
		prepared, err := d.prepareOpenDesignScratch(prepareCtx, task, provider)
		if err == nil {
			env = prepared
		}
		return prepared, err
	})
	if err != nil {
		d.failOpenDesignPreflight(ctx, task, taskContext.OpenDesignRun, err)
		return true
	}

	result, runErr := supervisor.Run(ctx, opendesign.SupervisorRunRequest{
		TaskID:      task.ID,
		Context:     taskContext.OpenDesignRun,
		ScratchRoot: envRoot,
		ProjectName: openDesignProjectName(task),
		Prompt:      buildOpenDesignPrompt(),
		Provenance:  openDesignProvenance(task, taskContext.RepositoryAnalysis),
	})
	workDir := ""
	if env != nil {
		workDir = env.WorkDir
	}
	if ctx.Err() != nil {
		return true
	}
	if runErr != nil {
		d.failOpenDesignTask(ctx, task, runErr.Error(), "open_design_run_failed", workDir)
		return true
	}
	if result.Status != opendesign.RunStatusSucceeded {
		d.failOpenDesignTask(ctx, task, fmt.Sprintf("Open Design run ended in %s", result.Status), "open_design_run_incomplete", workDir)
		return true
	}
	return true
}

func (d *Daemon) prepareOpenDesignScratch(ctx context.Context, task Task, provider string) (*execenv.Environment, error) {
	if task.WorkspaceID == "" {
		return nil, errors.New("Open Design task has no workspace_id")
	}
	agentName := "agent"
	var mcpConfig json.RawMessage
	if task.Agent != nil {
		agentName = task.Agent.Name
		mcpConfig = task.Agent.McpConfig
	}
	openclawBin := ""
	if provider == "openclaw" {
		openclawBin = d.cfg.Agents[provider].Path
	}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: d.cfg.WorkspacesRoot,
		WorkspaceID:    task.WorkspaceID,
		TaskID:         task.ID,
		AgentName:      agentName,
		Provider:       provider,
		CodexVersion:   d.agentVersion("codex"),
		OpenclawBin:    openclawBin,
		McpConfig:      mcpConfig,
		Task:           taskContextForEnv(task),
	}, d.logger)
	if err != nil {
		return nil, fmt.Errorf("prepare Open Design scratch: %w", err)
	}
	meta, ok := gcMetaForTask(task)
	if !ok {
		_ = env.Cleanup(true)
		return nil, errors.New("prepare Open Design scratch: task has no GC identity")
	}
	meta.LocalDirectory = env.LocalDirectory
	if err := execenv.WriteGCMeta(env.RootDir, meta, d.logger); err != nil {
		cleanupErr := env.Cleanup(true)
		return nil, fmt.Errorf("prepare Open Design scratch GC metadata: %w", errors.Join(err, cleanupErr))
	}
	if err := d.restoreOpenDesignBaseArchive(ctx, task, env.WorkDir); err != nil {
		cleanupErr := env.Cleanup(true)
		return nil, fmt.Errorf("prepare Open Design base archive: %w", errors.Join(err, cleanupErr))
	}
	if _, err := execenv.InjectRuntimeConfig(env.WorkDir, provider, taskContextForEnv(task)); err != nil {
		return nil, fmt.Errorf("inject Open Design runtime context: %w", err)
	}
	return env, nil
}

func (d *Daemon) restoreOpenDesignBaseArchive(ctx context.Context, task Task, workDir string) error {
	var taskContext struct {
		Operation   string          `json:"operation"`
		BasePackage json.RawMessage `json:"base_package"`
	}
	if err := json.Unmarshal(task.ProjectDesignSystemContext, &taskContext); err != nil {
		return fmt.Errorf("decode Open Design task context: %w", err)
	}
	if taskContext.Operation != "adjust" && taskContext.Operation != "regenerate" {
		return nil
	}
	var discriminator struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(taskContext.BasePackage, &discriminator); err != nil {
		return fmt.Errorf("decode Open Design base package: %w", err)
	}
	if discriminator.Schema == "" {
		return nil
	}
	if discriminator.Schema != opendesign.BasePackageReferenceSchema {
		return fmt.Errorf("unsupported Open Design base package schema %q", discriminator.Schema)
	}
	var reference opendesign.BasePackageReference
	if err := json.Unmarshal(taskContext.BasePackage, &reference); err != nil {
		return fmt.Errorf("decode Open Design base package reference: %w", err)
	}
	if err := opendesign.ValidateBasePackageReference(reference); err != nil {
		return fmt.Errorf("validate Open Design base package reference: %w", err)
	}
	if d.client == nil {
		return errors.New("Open Design base archive client is unavailable")
	}
	archive, err := d.client.DownloadOpenDesignBaseArchive(ctx, task.ID, reference)
	if err != nil {
		return fmt.Errorf("download Open Design base archive: %w", err)
	}
	if err := opendesign.ExtractProjectArchive(archive, reference.ContentDigest, workDir); err != nil {
		return fmt.Errorf("extract Open Design base archive: %w", err)
	}
	return nil
}

func taskContextForEnv(task Task) execenv.TaskContextForEnv {
	agentName := "agent"
	var agentID, instructions string
	var skills []SkillData
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
		instructions = task.Agent.Instructions
		skills = task.Agent.Skills
	}
	return execenv.TaskContextForEnv{
		IssueID:                           task.IssueID,
		TriggerCommentID:                  task.TriggerCommentID,
		NewCommentCount:                   task.NewCommentCount,
		NewCommentsSince:                  task.NewCommentsSince,
		PriorSessionResumed:               task.PriorSessionID != "",
		AgentID:                           agentID,
		AgentName:                         agentName,
		AgentInstructions:                 instructions,
		AgentSkills:                       convertSkillsForEnv(skills),
		Repos:                             convertReposForEnv(task.Repos),
		ProjectID:                         task.ProjectID,
		ProjectTitle:                      task.ProjectTitle,
		ProjectResources:                  convertProjectResourcesForEnv(task.ProjectResources),
		ChatSessionID:                     task.ChatSessionID,
		AutopilotRunID:                    task.AutopilotRunID,
		AutopilotID:                       task.AutopilotID,
		AutopilotTitle:                    task.AutopilotTitle,
		AutopilotDescription:              task.AutopilotDescription,
		AutopilotSource:                   task.AutopilotSource,
		AutopilotTriggerPayload:           strings.TrimSpace(string(task.AutopilotTriggerPayload)),
		QuickCreatePrompt:                 task.QuickCreatePrompt,
		UIDraftCreateContext:              strings.TrimSpace(string(task.UIDraftCreateContext)),
		DesignRestoreContext:              strings.TrimSpace(string(task.DesignRestoreContext)),
		DesignSystemProfileAnalyzeContext: strings.TrimSpace(string(task.DesignSystemProfileAnalyzeContext)),
		ProjectDesignSystemContext:        strings.TrimSpace(string(task.ProjectDesignSystemContext)),
		IsSquadLeader:                     strings.Contains(instructions, "## Squad Operating Protocol"),
		RequestingUserName:                task.RequestingUserName,
		RequestingUserProfileDescription:  task.RequestingUserProfileDescription,
		WorkspaceContext:                  task.WorkspaceContext,
	}
}

func buildOpenDesignPrompt() string {
	return strings.Join([]string{
		"Create or update the project design system in this Open Design orchestrator-scratch workspace.",
		"Read .agent_context/project_design_system/task.json first and use its operation, brief, repository analysis, references, and base package as the source of truth.",
		"For adjust operations, the verified base archive is already restored into the scratch root. Modify that package in place and preserve its identity, structure, and valid source-backed assets. Preserve files and behavior outside the requested adjustment; do not replace the package with an unrelated redesign.",
		"For adjust operations that change visual styles, enumerate every distinct rendered component variant affected by the request across the UI Kit and preview cards. Verify the final computed styles for a representative of each variant, not only the edited file contents. Inspect the CSS cascade and selector specificity so existing rules cannot override the requested result. Do not treat changed tokens or source declarations as proof of visual completion. If any representative resolves to an old or mismatched requested property, repair every mismatched representative before completion and repeat the deterministic rendered-style check.",
		"This is a non-interactive automation run and the task inputs are already confirmed. Do not ask questions, emit confirmation forms, or wait for user input; make conservative defaults and execute the work immediately.",
		"You must complete the work yourself as the primary agent. Do not delegate to, spawn, or invoke designer, task, or other sub-agents; the supervisor observes only this primary run.",
		"Run Package Audit exactly as: \"$OD_NODE_BIN\" \"$OD_BIN\" tools connectors design-system-package-audit --path . --fail-on-warnings. This fixed worker command is the package-contract authority; do not improvise another Audit command. Do not read, glob, grep, or inspect the pinned checkout outside the current scratch workspace; the agent sandbox intentionally denies that access. Do not fetch package guidance from upstream `main` or other Open Design versions.",
		"Use Open Design's native design-system package conventions and tools. Produce one coherent, complete package in the current workspace.",
		"Before the first Package Audit, create the pinned v0.16.1 package shape with root-level `DESIGN.md`, `README.md`, `SKILL.md`, `colors_and_type.css`, and `tokens.css`.",
		"`DESIGN.md` must contain source-backed sections covering context/product, color/palette, typography/type, spacing/layout, components, motion/interaction, voice/brand, and anti-patterns.",
		"`colors_and_type.css` plus root-level `tokens.css` must define at least 12 CSS custom properties, at least 4 concrete color values, and font, radius, and spacing or gap tokens. Do not place the only companion token file under a nested directory because the pinned Audit reads root-level `tokens.css`.",
		"Create at least 6 focused, complete HTML cards under `preview/`, including `preview/colors-*.html`, `preview/typography-specimens.html`, `preview/spacing-*.html`, and `preview/components-*.html`. Every card must contain a complete HTML document, real layout markup, and a non-empty embedded `<style>` block; linked shared stylesheets alone do not satisfy the pinned Preview fidelity contract.",
		"`SKILL.md` must use YAML frontmatter with `name`, `description`, and `user-invocable`, and substantive sections named What is inside, Source Context, When to use, How to use, and Design System Highlights.",
		"`README.md` must include a Product Overview or Product Context of at least 180 substantive characters plus Package Contents, Source Context or Source Evidence, Review or Reuse Workflow, and Preview Manifest sections. The overview must identify the product and use a capability verb such as supports, provides, includes, enables, or offers. Use the exact heading `## Reuse Workflow` instead of combining both alternatives in one heading, and provide a concrete reuse or review workflow there. It must name `DESIGN.md`, `colors_and_type.css`, `preview/`, `ui_kits/app/`, and source/context references. Document preserved-artifact namespaces with concrete audit-compatible paths such as `assets/source-backed/`, `fonts/source-backed/`, and `build/runtime/`. When source evidence contains no such artifacts, state truthfully that these namespaces contain no preserved files; do not invent or copy placeholder files.",
		"`ui_kits/app/index.html` must load `../../colors_and_type.css`, render a real composed interface, and load or import at least 3 modular files under `ui_kits/app/components/` when repository evidence contains reusable components. Use source-derived component names; do not invent chat-specific roles for a non-chat product merely to satisfy examples. For the pinned v0.16.1 reusable-kit contract, create and document at least 3 actual browser-ready `components/*.js` paths.",
		"The entry document must contain a direct executable runtime bootstrap statement that mounts the composed interface. Use an audit-compatible direct statement such as `document.getElementById(\"app\").replaceChildren(rootElement)` or an equivalent direct `append`, `appendChild`, `replaceChildren`, or `innerHTML =` call. Calls that exist only inside imported component files or helper functions do not satisfy the pinned Audit because the entry surface itself must prove that it mounts the kit.",
		"The UI Kit and every `preview/*.html` card must render with all outbound network access blocked. Use package-local scripts, styles, fonts, images, and runtime files only. Do not use CDN scripts, remote URLs, or protocol-relative URLs. In particular, do not load raw `.jsx` or `.tsx` files in the browser and do not depend on remote React, ReactDOM, Babel, web fonts, or images. Preserve at least 3 truthful `components/<source-derived-name>.jsx` paths as reusable source components and provide compiled browser-ready `.js` counterparts; make `index.html` load only those local compiled runtime files. For this Multica execution, ignore any direct-JSX CDN skeleton in upstream skill guidance because the supervisor's same-origin Preview gate intentionally blocks it.",
		"`ui_kits/app/README.md` must be at least 350 characters and use the exact headings `## Structure`, `## Usage`, and `## Design Notes`. It must name `index.html`, `components/`, at least 3 truthful JSX source-component paths, and their browser-ready JavaScript counterparts. The Usage section must contain a concrete copy, compose, import, use, build, or create workflow. Design Notes must describe the source basis plus layout, colors, typography, or tokens.",
		"After the package is implemented, run Package Audit, repair its findings, and make at most 3 Package Audit executions in total. Continue repairing while the complete finding set improves. Stop early only when two consecutive executions return exactly the same complete set of error and warning codes and paths. Do not stop merely because one finding persists while other findings improve. If the final allowed execution still fails, end the run without claiming success so the supervisor can record the audit failure.",
		"Do not modify the source repository, do not write to a Multica legacy output directory, and do not report completion while required package work remains.",
	}, "\n\n")
}

func openDesignProjectName(task Task) string {
	if name := strings.TrimSpace(task.ProjectTitle); name != "" {
		return name + " design system"
	}
	return "Project design system"
}

func openDesignProvenance(task Task, repositoryAnalysis json.RawMessage) opendesign.WorkerWorkspaceProvenance {
	var analysis struct {
		CommitSHA string `json:"commit_sha"`
	}
	_ = json.Unmarshal(repositoryAnalysis, &analysis)
	commit := strings.TrimSpace(analysis.CommitSHA)
	return opendesign.WorkerWorkspaceProvenance{
		SourceLabel:  "multica-project:" + task.ProjectID,
		SourceRef:    commit,
		BaseRevision: commit,
	}
}

func (d *Daemon) failOpenDesignPreflight(ctx context.Context, task Task, runContext opendesign.TaskRunContext, cause error) {
	report := opendesign.PreflightReport{
		Schema:     opendesign.PreflightSchema,
		Engine:     runContext.Engine,
		AdapterID:  runContext.Agent.AdapterID,
		Model:      runContext.Agent.Model,
		Binary:     opendesign.ProbeResult{Status: opendesign.ProbeFailed, Message: cause.Error()},
		Auth:       opendesign.ProbeResult{Status: opendesign.ProbeUnknown},
		ModelProbe: opendesign.ProbeResult{Status: opendesign.ProbeFailed},
		Plugins:    opendesign.PluginPreflight{Policy: opendesign.PluginsDisabled},
	}
	_ = d.client.ReportOpenDesignPreflight(context.WithoutCancel(ctx), task.ID, report)
	d.failOpenDesignTask(context.WithoutCancel(ctx), task, cause.Error(), "open_design_preflight_failed", "")
}

func (d *Daemon) failOpenDesignTask(ctx context.Context, task Task, message, reason, workDir string) {
	_ = d.client.FailTask(ctx, task.ID, message, "", workDir, "", reason, false, "")
}
