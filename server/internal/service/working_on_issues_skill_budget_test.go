package service

import (
	"strings"
	"testing"
)

func TestWorkingOnIssuesSkillKeepsCoreSmallAndDetailsLoadable(t *testing.T) {
	const (
		issuesPath        = "references/issues.md"
		maxCoreLines      = 110
		maxCoreBytes      = 6_500
		// The reference tracks upstream's own growth: this budget is a guard
		// against runaway prose, not a freeze. Raise it deliberately, as here
		// for the reworked PR-linking and comment-editing sections, rather than
		// trimming contracts to fit.
		maxReferenceLines = 470
		maxReferenceBytes = 24_500
	)

	skill, ok := findSkill(t, PlatformSkillName)
	if !ok {
		return
	}
	_, core, _ := splitFrontmatter(skill.Content)
	issues := supportingFileContent(t, skill, issuesPath)

	assertTextBudget(t, "multica-platform/SKILL.md body", core, maxCoreLines, maxCoreBytes)
	assertTextBudget(t, issuesPath, issues, maxReferenceLines, maxReferenceBytes)

	for _, want := range []string{
		"open the reference(s) your task actually needs",
		"Do not read them all",
		issuesPath,
		"Writes are real",
		"`--no-start` when you are only recording",
		"Comment reads stay bounded",
	} {
		if !containsUnwrapped(core, want) {
			t.Errorf("compact platform core missing routing or safety contract %q", want)
		}
	}

	for _, want := range []string{
		"Default for code-changing issue work",
		"open or update a PR before posting the final Multica issue comment",
		"if no code changed, say no PR is needed",
		"user explicitly asked for a local-only change or no PR",
		"report that blocker instead of pretending the run is complete",
		"include the PR URL when a PR exists",
		"MULTICA_ISSUE_OUTCOME_FILE",
		"managed completion contract for final status and delivery",
		"do not duplicate it with a CLI comment",
		"available for deliberate state changes",
		"successful process exit alone does not establish review readiness",
		"the PR **title**, the **branch name**, and the **body right after a closing keyword**",
		"title or body only",
		"never the branch",
		"a passing reference and links nothing",
		"put the key in the title, the branch, or after a closing keyword",
		"multica issue pull-requests <issue-id> --output json",
		"snapshot_available == true",
		"Only then does `checks_rollup == null` mean \"no checks\"",
		"There is no separate `draft` or `merged` boolean",
		"**`backlog`** parks an agent-assigned issue",
		"does **not** stop tasks already in flight",
		"no active task / retry remains",
		"custom terminal status can still enqueue at creation",
		"not automatic side effects of a task starting or finishing",
		"delivered the issue's own ask",
		"produces none of the issue's own deliverable",
		"dispatching members is not delivery",
		"multica issue assign <issue-id> --to-id <agent-id> --no-start",
		"multica issue runs <issue-id> --siblings --output json",
		"Nothing here reserves an issue or serialises anything",
		"todo starts work now, backlog parks it",
		"when a whole stage finishes",
		"one implicit stage",
		"Advancement is agent-driven",
		"multica issue status <stage-2-child-id> todo",
		"status_category",
	} {
		if !containsUnwrapped(issues, want) {
			t.Errorf("%s missing operational contract %q", issuesPath, want)
		}
	}
}

func supportingFileContent(t *testing.T, skill AgentSkillData, path string) string {
	t.Helper()
	for _, file := range skill.Files {
		if file.Path == path {
			return file.Content
		}
	}
	t.Fatalf("built-in skill %q missing supporting file %q", skill.Name, path)
	return ""
}

func assertTextBudget(t *testing.T, name, content string, maxLines, maxBytes int) {
	t.Helper()
	lines := strings.Count(content, "\n") + 1
	if lines > maxLines {
		t.Errorf("%s is %d lines, over %d-line loading budget", name, lines, maxLines)
	}
	if bytes := len([]byte(content)); bytes > maxBytes {
		t.Errorf("%s is %d bytes, over %d-byte loading budget", name, bytes, maxBytes)
	}
}
