package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// taskModelSelectionResolution keeps the saved request distinct from the
// selection the daemon has actually accepted for launch. Effective is nil on
// rejection, so task evidence cannot accidentally present an ignored invalid
// override as if it ran.
type taskModelSelectionResolution struct {
	Requested                      taskModelSelection
	Launch                         taskModelSelection
	Effective                      *taskModelSelection
	ValidateResolvedModelSelection func(string) error
}

// resolveTaskModelSelectionForTask adds strict rejection only for ordinary
// issue runs. Older task shapes and specialized workflows keep the historical
// drop behavior until their provider-specific launch contracts can be
// validated independently; they drop an unsupported capability override
// silently instead of emitting the user-facing thinking-level warning.
func resolveTaskModelSelectionForTask(
	ctx context.Context,
	task Task,
	provider string,
	runtimeCmd agent.Command,
	requested taskModelSelection,
	taskLog *slog.Logger,
) (taskModelSelectionResolution, error) {
	result := taskModelSelectionResolution{Requested: requested}
	if !ordinaryIssueUsesStrictModelSelection(task) {
		effective := resolveTaskModelSelection(ctx, provider, runtimeCmd, requested, taskLog)
		// Legacy task shapes and the specialized workflows (chat, autopilot,
		// quick create, Testing Center runs, Design Center, PMO sync) keep the
		// historical silent drop: an unsupported level degrades to the runtime
		// default without surfacing a transcript message, because these runs
		// carry the warning text straight into agent-visible output. Only the
		// resolver's own diagnostics (taskLog) report the drop.
		effective.ThinkingLevelWarning = ""
		result.Launch = effective
		result.Effective = &effective
		return result, nil
	}

	// An unset Codex model is resolved from the task's actual config at
	// startup. A catalog default is only display metadata and must not stand in
	// for that model. Preserve the requested overrides and let the startup
	// preflight call validateResolvedCodexModelSelection once it knows the ID.
	if provider == "codex" && requested.Model == "" &&
		(requested.ThinkingLevel != "" || requested.ServiceTier != "") {
		effective := requested
		result.Launch = effective
		catalog, catalogErr := listModels(ctx, provider, runtimeCmd)
		result.ValidateResolvedModelSelection = func(actualModel string) error {
			if actualModel == "" {
				return fmt.Errorf("model selection rejected: selected model could not be resolved by Codex config/read; select an explicit model or clear thinking_level and service_tier")
			}
			if catalogErr != nil || catalog.Fallback {
				return nil
			}
			return validateResolvedCodexModelSelection(catalog, actualModel, requested)
		}
		return result, nil
	}

	read := false
	var (
		catalog    agent.Catalog
		catalogErr error
	)
	loadCatalog := func() (agent.Catalog, error) {
		if !read {
			read = true
			catalog, catalogErr = listModels(ctx, provider, runtimeCmd)
		}
		return catalog, catalogErr
	}

	effective := requested
	capabilityChecksPending := effective.ThinkingLevel != "" || effective.ServiceTier != ""
	effective.Model = qualifyTaskModel(provider, effective.Model, capabilityChecksPending, loadCatalog, taskLog)

	// An explicit model may be present only in Catalog.Unavailable, so strict
	// ordinary runs consult the same lazy catalog even when no capability
	// override was requested. The memoized loader still caps this at one read.
	if effective.Model != "" || capabilityChecksPending {
		catalog, catalogErr = loadCatalog()
		if catalogErr != nil {
			result.Launch = effective
			result.Effective = &effective
			return result, nil
		}
		if catalog.Fallback {
			result.Launch = effective
			result.Effective = &effective
			return result, nil
		}
		if err := validateKnownTaskModelSelection(provider, catalog, effective); err != nil {
			return result, err
		}
	}

	result.Launch = effective
	result.Effective = &effective
	return result, nil
}

// validateResolvedCodexModelSelection is the handoff contract for Codex
// startup: after config resolution identifies the real model, validate the
// deferred request against that exact model. It deliberately does not infer a
// default and treats fallback or unknown catalogs as non-authoritative.
func validateResolvedCodexModelSelection(catalog agent.Catalog, actualModel string, requested taskModelSelection) error {
	if actualModel == "" {
		return nil
	}
	resolved := requested
	resolved.Model = actualModel
	return validateKnownTaskModelSelection("codex", catalog, resolved)
}

func validateKnownTaskModelSelection(provider string, catalog agent.Catalog, selection taskModelSelection) error {
	if catalog.Fallback {
		return nil
	}
	for _, unavailable := range catalog.Unavailable {
		if unavailable.ID == selection.Model {
			return fmt.Errorf("model selection rejected: selected model %q is unavailable for provider %q; choose an available model or update the runtime", selection.Model, provider)
		}
	}

	model := catalogSelectionModel(provider, catalog, selection.Model)
	if model == nil {
		return nil
	}
	loadCatalog := func() (agent.Catalog, error) { return catalog, nil }
	if selection.ServiceTier != "" && modelServiceTierCapabilitiesKnown(*model) {
		ok, err := agent.ValidateServiceTierWith(loadCatalog, provider, selection.Model, selection.ServiceTier)
		if err != nil {
			return nil
		}
		if !ok {
			return fmt.Errorf("model selection rejected: service_tier %q is not supported by selected model %q for provider %q; choose a supported tier or model", selection.ServiceTier, selection.Model, provider)
		}
	}
	if selection.ThinkingLevel != "" && model.Thinking != nil && len(model.Thinking.SupportedLevels) > 0 {
		ok, err := agent.ValidateThinkingLevelWith(loadCatalog, provider, selection.Model, selection.ThinkingLevel)
		if err != nil {
			return nil
		}
		if !ok {
			return fmt.Errorf("model selection rejected: thinking_level %q is not supported by selected model %q for provider %q; choose a supported level or model", selection.ThinkingLevel, selection.Model, provider)
		}
	}
	return nil
}

func catalogSelectionModel(provider string, catalog agent.Catalog, model string) *agent.Model {
	if model == "" {
		if provider == "opencode" {
			return nil
		}
		for i := range catalog.Models {
			candidate := &catalog.Models[i]
			if candidate.Default {
				return candidate
			}
		}
		return nil
	}
	for i := range catalog.Models {
		candidate := &catalog.Models[i]
		if candidate.ID == model {
			return candidate
		}
	}
	return nil
}

func modelServiceTierCapabilitiesKnown(model agent.Model) bool {
	return len(model.ServiceTiers) > 0 || model.SupportsExplicitStandardServiceTier
}

func ordinaryIssueUsesStrictModelSelection(task Task) bool {
	return task.IssueID != "" &&
		task.ChatSessionID == "" &&
		task.AutopilotRunID == "" &&
		task.QuickCreatePrompt == "" &&
		task.RegenerateQuickActionsFor == "" &&
		len(task.UIDraftCreateContext) == 0 &&
		len(task.DesignRestoreContext) == 0 &&
		!testingContextPresent(task.TestGenerationContext) &&
		!testingContextPresent(task.TestRunContext) &&
		len(task.DesignSystemProfileAnalyzeContext) == 0 &&
		len(task.TemplateBlueprintAnalyzeContext) == 0 &&
		len(task.ProjectDesignSystemContext) == 0 &&
		len(task.DesignDocumentContext) == 0 &&
		len(task.DesignDeliveryContext) == 0 &&
		len(task.PMOSyncContext) == 0 &&
		!taskIsSquadLeader(task)
}
