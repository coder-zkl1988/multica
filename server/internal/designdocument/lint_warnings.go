package designdocument

// Anti-slop lint warnings for prototype artifacts.
//
// These rules are a Go port of the upstream Open Design artifact linter
// (apps/daemon/src/lint-artifact.ts at open-design-v0.26.1). They detect the
// recurring AI-generated design tells: default LLM accent colors, purple and
// "trust" gradients, emoji feature icons, the rounded left-accent card,
// sans-serif display faces, invented metrics, ALL CAPS without tracking and
// accent overuse. Every rule emits a DiagnosticWarning: the findings advise
// the agent and never gate the package — hasErrors counts only DiagnosticError,
// so a package carrying lint warnings still becomes a draft (DC-050).
//
// The linter is deliberately greppy, like upstream: cheap, deterministic and
// offline, with no HTML parsing. It tolerates false positives in exchange for
// catching regressed patterns, and each message quotes the matched text so a
// finding can be verified. The filler-copy and external-image rule families
// are intentionally absent: the platform already rejects those at error
// severity (template_residue_detected, prototype_css_url_unsafe).

import (
	"regexp"
	"strconv"
	"strings"
)

// lintPurpleHexes are Tailwind violet/purple and indigo stops — the original
// AI-slop palette — flagged when they appear inside a linear-gradient value.
var lintPurpleHexes = []string{
	"#a855f7", "#9333ea", "#7c3aed", "#6d28d9", "#581c87",
	"#8b5cf6", "#a78bfa", "#c4b5fd", "#ddd6fe", "#ede9fe",
	"#6366f1", "#4f46e5", "#4338ca", "#3730a3", "#312e81",
	"#818cf8", "#a5b4fc", "#c7d2fe", "#e0e7ff", "#eef2ff",
}

// lintTrustGradientBlueHexes and lintTrustGradientCyanHexes are the blue and
// cyan stops of the documented "blue→cyan two-stop trust gradient". A pure
// blue→cyan gradient has no overlap with lintPurpleHexes, so it is paired
// against these lists separately.
var lintTrustGradientBlueHexes = []string{
	"#3b82f6", "#2563eb", "#1d4ed8", "#1e40af", "#1e3a8a",
	"#60a5fa", "#93c5fd", "#bfdbfe",
	"#0ea5e9", "#0284c7", "#0369a1", "#38bdf8", "#7dd3fc",
}

var lintTrustGradientCyanHexes = []string{
	"#06b6d4", "#0891b2", "#0e7490", "#155e75", "#164e63",
	"#22d3ee", "#67e8f9", "#a5f3fc",
}

// lintAIDefaultIndigoHexes are the canonical "default LLM accent" colors:
// even a single solid use outside a design-token block is the tell. The
// documented escape hatch is an explicit --accent token; the same hex behind
// any other token name is still the default in disguise.
var lintAIDefaultIndigoHexes = []string{
	"#6366f1", "#4f46e5", "#4338ca", "#3730a3",
	"#8b5cf6", "#7c3aed", "#a855f7",
}

// lintSlopEmoji are the emoji used as feature icons in AI-generated pages.
var lintSlopEmoji = []string{
	"✨", "🚀", "🎯", "⚡", "🔥", "💡", "📈", "🎨", "🛡️", "🌟",
	"💪", "🎉", "👋", "🙌", "✅", "⭐", "🏆",
}

// lintTrackingFloorEm is the tracking floor for ALL CAPS: 0.06em of the
// element's own font size.
const lintTrackingFloorEm = 0.06

// lintAccentUseLimit is the rendered-body var(--accent) reference count above
// which the accent is considered overused on one screen.
const lintAccentUseLimit = 6

// lintVarResolveMaxDepth bounds var() resolution so a cyclic token definition
// terminates instead of looping.
const lintVarResolveMaxDepth = 4

type lintHexPattern struct {
	hex     string
	pattern *regexp.Regexp
}

// lintPurpleGradientPatterns pair each purple hex with the gradient shape it
// is scanned for, precompiled so a lint pass allocates no regexes.
var lintPurpleGradientPatterns = buildLintHexPatterns(lintPurpleHexes, `linear-gradient\([^)]*`, `[^)]*\)`)

// lintEmojiIconPatterns match each slop emoji in a structural context — a
// heading, button, list item or icon-classed span — not in body prose.
var lintEmojiIconPatterns = buildLintEmojiPatterns(lintSlopEmoji)

var lintInventedMetricPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b10×\s+(faster|better|easier)\b`),
	regexp.MustCompile(`(?i)\b100×\s+(faster|better)\b`),
	regexp.MustCompile(`(?i)\b99\.\d+%\s+uptime\b`),
	regexp.MustCompile(`(?i)\bzero[- ]downtime\b`),
	regexp.MustCompile(`(?i)\b3×\s+more\s+(productive|efficient)\b`),
}

var (
	lintGradientBodyPattern    = regexp.MustCompile(`(?i)linear-gradient\([^)]*\)`)
	lintKeywordPurpleGradient  = regexp.MustCompile(`(?i)linear-gradient\([^)]*\b(purple|violet)\b[^)]*\)`)
	lintTrustBlueKeyword       = regexp.MustCompile(`\bblue\b`)
	lintTrustCyanKeyword       = regexp.MustCompile(`\bcyan\b`)
	lintLeftAccentCardPattern  = regexp.MustCompile(`(?i)\.[a-z-]+\s*\{[^}]*border-left\s*:\s*\d+px\s+solid\s+[^;]+;[^}]*border-radius\s*:\s*[1-9]`)
	lintSansDisplayPattern     = regexp.MustCompile(`(?i)(?:h1|h2|h3|\.h-?(?:hero|xl|lg|md))[^{}]*\{[^}]*font-family\s*:\s*["']?(?:Inter|Roboto|Arial|-apple-system|system-ui|SF\s+Pro)`)
	lintHTMLCommentPattern     = regexp.MustCompile(`<!--[\s\S]*?-->`)
	lintStyleBlockPattern      = regexp.MustCompile(`(?i)(<style[^>]*>)([\s\S]*?)(</style>)`)
	lintStyleBlockWholePattern = regexp.MustCompile(`(?is)<style[^>]*>[\s\S]*?</style>`)
	lintCSSCommentPattern      = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	lintCSSRulePattern         = regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	lintUppercaseRulePattern   = regexp.MustCompile(`(?i)([^{}]*)\{([^{}]*text-transform\s*:\s*uppercase[^{}]*)\}`)
	lintUppercaseValuePattern  = regexp.MustCompile(`(?i)text-transform\s*:\s*uppercase`)
	lintInlineStyleDQPattern   = regexp.MustCompile(`(?is)(?:^|\s)style\s*=\s*"([^"]*)"`)
	lintInlineStyleSQPattern   = regexp.MustCompile(`(?is)(?:^|\s)style\s*=\s*'([^']*)'`)
	lintVarReferencePattern    = regexp.MustCompile(`var\(\s*(--[\w-]+)\s*(?:,\s*([^()]*))?\)`)
	lintTokenDeclaration       = regexp.MustCompile(`^(--[\w-]+)\s*:\s*(.+)$`)
	lintCustomPropertyPattern  = regexp.MustCompile(`^--[\w-]+\s*:`)
	lintColorSchemePattern     = regexp.MustCompile(`(?i)^color-scheme\s*:`)
	lintTagThemeScopePattern   = regexp.MustCompile(`^(?::root|html|body)(?:\[([a-zA-Z-]+)(?:[*^$|~]?=[^\]]*)?\])?$`)
	lintBareAttrScopePattern   = regexp.MustCompile(`^\[([a-zA-Z-]+)(?:[*^$|~]?=[^\]]*)?\]$`)
	lintEmTrackingValuePattern = regexp.MustCompile(`^(-?\d*\.?\d+)em$`)
)

// lintGlobalThemeAttributes are the attribute switches a global theme scope
// may use. A broader exemption would also strip arbitrary component or state
// attribute rules, which is exactly the component-local indigo laundering the
// indigo check exists to catch.
var lintGlobalThemeAttributes = map[string]struct{}{
	"data-theme": {}, "data-color-scheme": {}, "data-mode": {},
}

func buildLintHexPatterns(hexes []string, prefix, suffix string) []lintHexPattern {
	patterns := make([]lintHexPattern, 0, len(hexes))
	for _, hex := range hexes {
		patterns = append(patterns, lintHexPattern{
			hex:     hex,
			pattern: regexp.MustCompile(`(?i)` + prefix + regexp.QuoteMeta(hex) + suffix),
		})
	}
	return patterns
}

func buildLintEmojiPatterns(emoji []string) []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(emoji))
	for _, character := range emoji {
		patterns = append(patterns, regexp.MustCompile(
			`(?i)<(?:h[1-6]|button|li|span class="[^"]*icon[^"]*")[^>]*>[^<]*`+regexp.QuoteMeta(character)))
	}
	return patterns
}

// lintFinding is one rule hit before it is bound to a package path.
type lintFinding struct {
	code    string
	message string
	fix     string
	snippet string
}

// diagnostic binds the finding to a package path as a warning that never
// reaches the audit verdict.
func (finding lintFinding) diagnostic(path string) Diagnostic {
	var message strings.Builder
	message.WriteString(finding.message)
	message.WriteString(" Fix: ")
	message.WriteString(finding.fix)
	if finding.snippet != "" {
		message.WriteString(" Found: ")
		message.WriteString(finding.snippet)
	}
	return Diagnostic{
		Code:     finding.code,
		Severity: DiagnosticWarning,
		Path:     path,
		Message:  message.String(),
	}
}

func lintFindingDiagnostics(findings []lintFinding, path string) []Diagnostic {
	if len(findings) == 0 {
		return nil
	}
	diagnostics := make([]Diagnostic, 0, len(findings))
	for _, finding := range findings {
		diagnostics = append(diagnostics, finding.diagnostic(path))
	}
	return diagnostics
}

// lintPrototypeDocument runs the whole anti-slop lint over one prototype HTML
// document. The rule order is fixed so the warning list is deterministic, and
// each rule fires at most once per document — a single corrective signal per
// rule is enough for an advisory.
func lintPrototypeDocument(source, path string) []Diagnostic {
	// Strip HTML comments first: they often carry examples that would
	// otherwise fire the pattern scans.
	text := lintHTMLCommentPattern.ReplaceAllString(source, "")
	findings := make([]lintFinding, 0, 4)
	findings = append(findings, lintGradientFindings(text)...)
	if finding, found := lintIndigoFinding(lintStripHTMLTokenBlocks(text)); found {
		findings = append(findings, finding)
	}
	if finding, found := lintEmojiIconFinding(text); found {
		findings = append(findings, finding)
	}
	if finding, found := lintLeftAccentCardFinding(text); found {
		findings = append(findings, finding)
	}
	if finding, found := lintSansDisplayFinding(text); found {
		findings = append(findings, finding)
	}
	if finding, found := lintInventedMetricFinding(text); found {
		findings = append(findings, finding)
	}
	findings = append(findings, lintAllCapsHTMLFindings(text)...)
	if finding, found := lintAccentOveruseFinding(text); found {
		findings = append(findings, finding)
	}
	return lintFindingDiagnostics(findings, path)
}

// lintPrototypeStylesheet runs the CSS-scoped subset of the lint over one
// standalone prototype stylesheet. The emoji and invented-metric rules need
// HTML structure and rendered copy, and accent-overuse counts inline usage in
// the rendered body rather than class definitions, so those rules do not
// apply to a stylesheet.
func lintPrototypeStylesheet(source, path string) []Diagnostic {
	findings := make([]lintFinding, 0, 4)
	findings = append(findings, lintGradientFindings(source)...)
	if finding, found := lintIndigoFinding(lintStripCSSTokenBlocks(source)); found {
		findings = append(findings, finding)
	}
	if finding, found := lintLeftAccentCardFinding(source); found {
		findings = append(findings, finding)
	}
	if finding, found := lintSansDisplayFinding(source); found {
		findings = append(findings, finding)
	}
	css := lintCSSCommentPattern.ReplaceAllString(source, "")
	if finding, found := lintAllCapsCSSFinding(css, lintThemeTokens(css)); found {
		findings = append(findings, finding)
	}
	return lintFindingDiagnostics(findings, path)
}

// lintGradientFindings flags a purple-family gradient first, then falls back
// to the blue→cyan trust gradient, so an artifact receives a single gradient
// corrective signal.
func lintGradientFindings(text string) []lintFinding {
	for _, entry := range lintPurpleGradientPatterns {
		if match := entry.pattern.FindString(text); match != "" {
			return []lintFinding{{
				code:    "lint_purple_gradient",
				message: "Found a violet/purple gradient using " + entry.hex + " — the anti-slop list says no.",
				fix:     "Replace the gradient with a flat surface or one accent color at a single intensity, not in a gradient.",
				snippet: lintClipSnippet(match),
			}}
		}
	}
	if match := lintKeywordPurpleGradient.FindStringSubmatch(text); match != nil {
		return []lintFinding{{
			code:    "lint_purple_gradient",
			message: "Found a \"" + match[1] + "\" keyword inside a gradient — the anti-slop list says no.",
			fix:     "Remove the gradient or swap to a single solid color from the design tokens.",
			snippet: lintClipSnippet(match[0]),
		}}
	}
	if match := lintBlueCyanTrustGradient(text); match != "" {
		return []lintFinding{{
			code:    "lint_trust_gradient",
			message: "Found a blue→cyan two-stop \"trust\" gradient — the anti-slop list says no.",
			fix:     "Replace the gradient with a flat surface or a single design-token color; two-stop blue→cyan trust gradients are a SaaS hero cliché.",
			snippet: lintClipSnippet(match),
		}}
	}
	return nil
}

// lintBlueCyanTrustGradient returns the first linear-gradient whose stop list
// pairs a Tailwind blue/sky token (or the blue keyword) with a cyan token (or
// the cyan keyword), or "" when none matches.
func lintBlueCyanTrustGradient(text string) string {
	for _, gradient := range lintGradientBodyPattern.FindAllString(text, -1) {
		lowered := strings.ToLower(gradient)
		hasBlue := lintContainsHex(lowered, lintTrustGradientBlueHexes) || lintTrustBlueKeyword.MatchString(lowered)
		hasCyan := lintContainsHex(lowered, lintTrustGradientCyanHexes) || lintTrustCyanKeyword.MatchString(lowered)
		if hasBlue && hasCyan {
			return gradient
		}
	}
	return ""
}

func lintContainsHex(lowered string, hexes []string) bool {
	for _, hex := range hexes {
		if strings.Contains(lowered, hex) {
			return true
		}
	}
	return false
}

// lintIndigoFinding flags a solid use of the default LLM accent palette. The
// caller strips design-token blocks first: an explicit --accent declaration is
// the documented escape hatch, and indigo laundered through any other token
// name stays in scope.
func lintIndigoFinding(text string) (lintFinding, bool) {
	lowered := strings.ToLower(text)
	for _, hex := range lintAIDefaultIndigoHexes {
		if strings.Contains(lowered, hex) {
			return lintFinding{
				code:    "lint_ai_default_indigo",
				message: "Found a default LLM accent color (" + hex + ") — this is the most-reported AI design tell.",
				fix:     "Replace it with the design system's accent token; if the brief truly requires indigo, encode it as the explicit --accent token so it reads as intentional, not default.",
				snippet: hex,
			}, true
		}
	}
	return lintFinding{}, false
}

func lintEmojiIconFinding(text string) (lintFinding, bool) {
	for index, emoji := range lintSlopEmoji {
		if !strings.Contains(text, emoji) {
			continue
		}
		if match := lintEmojiIconPatterns[index].FindString(text); match != "" {
			return lintFinding{
				code:    "lint_emoji_icon",
				message: "Emoji \"" + emoji + "\" used as a UI icon — the anti-slop list says SVG monoline only.",
				fix:     "Replace it with a small inline SVG icon (1.6–1.8px stroke, currentColor) or remove the icon entirely.",
				snippet: lintClipSnippet(match),
			}, true
		}
	}
	return lintFinding{}, false
}

func lintLeftAccentCardFinding(text string) (lintFinding, bool) {
	match := lintLeftAccentCardPattern.FindString(text)
	if match == "" {
		return lintFinding{}, false
	}
	return lintFinding{
		code:    "lint_left_accent_card",
		message: "Rounded card with a coloured left border — the canonical AI-slop card pattern.",
		fix:     "Drop either the border-radius (set 0) or the border-left; keep card borders uniform.",
		snippet: lintClipSnippet(match),
	}, true
}

func lintSansDisplayFinding(text string) (lintFinding, bool) {
	match := lintSansDisplayPattern.FindString(text)
	if match == "" {
		return lintFinding{}, false
	}
	return lintFinding{
		code:    "lint_sans_display",
		message: "A heading rule uses Inter / Roboto / system-sans as the display face.",
		fix:     "Bind headings to the design system's display face and override it only for a tech/utility or modern-minimal direction.",
		snippet: lintClipSnippet(match),
	}, true
}

func lintInventedMetricFinding(text string) (lintFinding, bool) {
	for _, pattern := range lintInventedMetricPatterns {
		if match := pattern.FindString(text); match != "" {
			return lintFinding{
				code:    "lint_invented_metric",
				message: "Suspected invented metric \"" + match + "\" — no numbers without a real source.",
				fix:     "Remove the claim or replace it with a labelled placeholder until a real number exists.",
				snippet: lintClipSnippet(match),
			}, true
		}
	}
	return lintFinding{}, false
}

// lintAccentOveruseFinding counts var(--accent) references in the rendered
// body — outside <style> blocks, i.e. inline usage in the DOM. A stylesheet
// legitimately defines the accent on many selectors that never render on one
// page; what the user actually sees is what can be overused.
func lintAccentOveruseFinding(text string) (lintFinding, bool) {
	body := lintStyleBlockWholePattern.ReplaceAllString(text, "")
	uses := strings.Count(body, "var(--accent)")
	if uses <= lintAccentUseLimit {
		return lintFinding{}, false
	}
	return lintFinding{
		code:    "lint_accent_overuse",
		message: "var(--accent) used " + strconv.Itoa(uses) + " times in the rendered body — likely overused per screen.",
		fix:     "Cap accent usage at 2 visible uses per screen (one eyebrow + one CTA, or one accent card + one tab) and demote the rest to neutral text colors.",
	}, true
}

// lintAllCapsHTMLFindings checks every <style> block for uppercase rules
// without adequate tracking, then falls back to inline style attributes so
// both the stylesheet and the DOM are covered. The token map is harvested once
// from all style blocks so a rule like `letter-spacing: var(--caps-tracking)`
// is judged by the token's literal value.
func lintAllCapsHTMLFindings(text string) []lintFinding {
	blocks := make([]string, 0)
	for _, match := range lintStyleBlockPattern.FindAllStringSubmatch(text, -1) {
		blocks = append(blocks, match[2])
	}
	tokens := lintThemeTokens(strings.Join(blocks, "\n"))
	for _, block := range blocks {
		css := lintCSSCommentPattern.ReplaceAllString(block, "")
		if finding, found := lintAllCapsCSSFinding(css, tokens); found {
			return []lintFinding{finding}
		}
	}
	for _, pattern := range []*regexp.Regexp{lintInlineStyleDQPattern, lintInlineStyleSQPattern} {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			declaration := match[1]
			if !lintUppercaseValuePattern.MatchString(declaration) {
				continue
			}
			if lintUppercaseTrackingVerdict(declaration, tokens) == lintTrackingInadequate {
				return []lintFinding{{
					code:    "lint_all_caps_no_tracking",
					message: "Inline style sets text-transform: uppercase without sufficient letter-spacing (>= 0.06em).",
					fix:     "Add letter-spacing: 0.08em (typical) to the same inline style; ALL CAPS without tracking looks cramped.",
					snippet: lintClipSnippet(declaration),
				}}
			}
		}
	}
	return nil
}

// lintAllCapsCSSFinding returns the first uppercase rule whose tracking is
// inadequate, or false when every uppercase rule passes or is not judged.
func lintAllCapsCSSFinding(css string, tokens map[string]string) (lintFinding, bool) {
	for _, match := range lintUppercaseRulePattern.FindAllStringSubmatch(css, -1) {
		selector := strings.TrimSpace(match[1])
		body := match[2]
		if lintUppercaseTrackingVerdict(body, tokens) != lintTrackingInadequate {
			continue
		}
		return lintFinding{
			code: "lint_all_caps_no_tracking",
			message: "Selector \"" + lintClipRunes(selector, 60) +
				"\" sets text-transform: uppercase without sufficient letter-spacing (>= 0.06em).",
			fix:     "Add letter-spacing: 0.08em (typical) to the same rule; ALL CAPS without tracking looks cramped.",
			snippet: lintClipSnippet(selector + " { " + strings.TrimSpace(body) + " }"),
		}, true
	}
	return lintFinding{}, false
}

// lintTrackingVerdict is the verdict of the uppercase tracking check.
type lintTrackingVerdict int

const (
	// lintTrackingInadequate means the rule renders ALL CAPS without the
	// tracking floor and the lint fires.
	lintTrackingInadequate lintTrackingVerdict = iota
	// lintTrackingAdequate means the tracking floor is met.
	lintTrackingAdequate
	// lintTrackingNotJudged means the tracking is declared in a unit the
	// pure-em subset cannot judge (px, rem, an unresolved var) and the lint
	// stays silent rather than risking a false positive.
	lintTrackingNotJudged
)

// lintUppercaseTrackingVerdict judges one declaration body against the
// tracking floor. This port intentionally covers only the pure-em subset of
// the upstream rule: a letter-spacing declared as an em value (directly or
// through a resolvable token) is compared to 0.06em, a missing declaration or
// the normal keyword is inadequate, and any other unit is left unjudged —
// converting those to the per-element em floor needs the font-size resolution
// machinery upstream carries, which this port drops in exchange for staying
// free of false positives. Custom property declarations are skipped while
// parsing, so a token named --letter-spacing cannot satisfy the rule, and the
// LAST letter-spacing declaration wins, mirroring the CSS cascade.
func lintUppercaseTrackingVerdict(body string, tokens map[string]string) lintTrackingVerdict {
	value := ""
	found := false
	for _, declaration := range lintParseDeclarations(body) {
		if declaration.property == "letter-spacing" {
			found = true
			value = declaration.value
		}
	}
	if !found {
		return lintTrackingInadequate
	}
	resolved := strings.ToLower(strings.TrimSpace(lintResolveCSSVars(value, tokens)))
	if resolved == "normal" {
		return lintTrackingInadequate
	}
	match := lintEmTrackingValuePattern.FindStringSubmatch(resolved)
	if match == nil {
		return lintTrackingNotJudged
	}
	parsed, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return lintTrackingNotJudged
	}
	if parsed >= lintTrackingFloorEm {
		return lintTrackingAdequate
	}
	return lintTrackingInadequate
}

type lintDeclaration struct {
	property string
	value    string
}

// lintParseDeclarations splits a declaration body into property/value pairs,
// lowercasing the property name and skipping custom properties so a token
// declaration cannot collide with a real property name.
func lintParseDeclarations(body string) []lintDeclaration {
	declarations := make([]lintDeclaration, 0)
	for _, raw := range strings.Split(body, ";") {
		colon := strings.Index(raw, ":")
		if colon < 0 {
			continue
		}
		property := strings.ToLower(strings.TrimSpace(raw[:colon]))
		if property == "" || strings.HasPrefix(property, "--") {
			continue
		}
		value := strings.TrimSpace(raw[colon+1:])
		if value == "" {
			continue
		}
		declarations = append(declarations, lintDeclaration{property: property, value: value})
	}
	return declarations
}

// lintResolveCSSVars replaces var(--name) references (with their one-level
// fallbacks) by the literal token value, iterating a few times so a token
// whose value is another reference resolves through one or two hops. Unresolved
// references without a fallback stay in place.
func lintResolveCSSVars(body string, tokens map[string]string) string {
	resolved := body
	for depth := 0; depth < lintVarResolveMaxDepth; depth++ {
		next := lintVarReferencePattern.ReplaceAllStringFunc(resolved, func(reference string) string {
			match := lintVarReferencePattern.FindStringSubmatch(reference)
			if match == nil {
				return reference
			}
			if value, ok := tokens[match[1]]; ok {
				return value
			}
			if match[2] != "" {
				return strings.TrimSpace(match[2])
			}
			return reference
		})
		if next == resolved {
			return resolved
		}
		resolved = next
	}
	return resolved
}

// lintThemeTokens collects CSS custom properties declared in global theme
// scopes so a var() reference inside a letter-spacing declaration is judged by
// the value the browser renders. Component-local token definitions are
// ignored: the tracking check must still catch under-tracking laundered
// through a local variable. Within a rule body the last declaration wins and
// later scopes overwrite earlier ones, mirroring the CSS cascade.
func lintThemeTokens(css string) map[string]string {
	tokens := make(map[string]string)
	cleaned := lintCSSCommentPattern.ReplaceAllString(css, "")
	for _, match := range lintCSSRulePattern.FindAllStringSubmatch(cleaned, -1) {
		selector := strings.TrimSpace(match[1])
		if !lintSelectorListIsGlobalThemeScope(selector) {
			continue
		}
		for _, declaration := range lintSplitDeclarations(match[2]) {
			if token := lintTokenDeclaration.FindStringSubmatch(declaration); token != nil {
				tokens[token[1]] = strings.TrimSpace(token[2])
			}
		}
	}
	return tokens
}

// lintStripHTMLTokenBlocks removes design-token rule blocks from every <style>
// block of an HTML document before the indigo scan.
func lintStripHTMLTokenBlocks(text string) string {
	return lintStyleBlockPattern.ReplaceAllStringFunc(text, func(block string) string {
		match := lintStyleBlockPattern.FindStringSubmatch(block)
		if match == nil {
			return block
		}
		return match[1] + lintStripCSSTokenBlocks(match[2]) + match[3]
	})
}

// lintStripCSSTokenBlocks removes rules that look like design-token
// definitions from one CSS text. A rule is removed only when every selector in
// its list is a global theme scope (:root, html, body, optionally suffixed
// with a known global-theme attribute), the body is token-shaped (custom
// properties plus color-scheme metadata) and no token launders an AI-default
// indigo hex through a non-accent name. Component-local token definitions and
// renamed indigo tokens stay in scope so the scan still catches indigo hidden
// behind an arbitrary variable.
func lintStripCSSTokenBlocks(css string) string {
	cleaned := lintCSSCommentPattern.ReplaceAllString(css, "")
	return lintCSSRulePattern.ReplaceAllStringFunc(cleaned, func(rule string) string {
		match := lintCSSRulePattern.FindStringSubmatch(rule)
		if match == nil {
			return rule
		}
		selector := strings.TrimSpace(match[1])
		if !lintSelectorListIsGlobalThemeScope(selector) {
			return rule
		}
		declarations := lintSplitDeclarations(match[2])
		if len(declarations) == 0 {
			return rule
		}
		for _, declaration := range declarations {
			if !lintIsTokenShapedDeclaration(declaration) {
				return rule
			}
		}
		for _, declaration := range declarations {
			if lintDeclarationLaunderIndigo(declaration) {
				return rule
			}
		}
		return ""
	})
}

// lintSplitDeclarations splits a rule body into trimmed declaration strings.
func lintSplitDeclarations(body string) []string {
	parts := strings.Split(body, ";")
	declarations := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			declarations = append(declarations, trimmed)
		}
	}
	return declarations
}

// lintIsTokenShapedDeclaration reports whether a declaration is a CSS custom
// property or the color-scheme metadata that legitimately accompanies tokens
// in a :root block and cannot smuggle a visible color.
func lintIsTokenShapedDeclaration(declaration string) bool {
	return lintCustomPropertyPattern.MatchString(declaration) || lintColorSchemePattern.MatchString(declaration)
}

// lintDeclarationLaunderIndigo reports whether a token declaration hides an
// AI-default indigo hex behind a name other than --accent. The escape hatch is
// --accent only: an arbitrary token name carrying the default color is still
// the LLM default in disguise.
func lintDeclarationLaunderIndigo(declaration string) bool {
	token := lintTokenDeclaration.FindStringSubmatch(declaration)
	if token == nil || strings.ToLower(token[1]) == "--accent" {
		return false
	}
	return lintContainsHex(strings.ToLower(token[2]), lintAIDefaultIndigoHexes)
}

func lintSelectorListIsGlobalThemeScope(selector string) bool {
	parts := strings.Split(selector, ",")
	for _, part := range parts {
		if !lintIsGlobalThemeScopeSelector(strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

func lintIsGlobalThemeScopeSelector(selector string) bool {
	// :root / html / body, optionally suffixed with a single attribute
	// selector. The bare form is always a global theme scope; the suffixed
	// form only when the attribute names a known global-theme switch, so a
	// component/state suffix keeps the rule in scope of the indigo scan.
	if match := lintTagThemeScopePattern.FindStringSubmatch(selector); match != nil {
		if match[1] == "" {
			return true
		}
		_, known := lintGlobalThemeAttributes[strings.ToLower(match[1])]
		return known
	}
	// Bare attribute selectors are scopes only for known global-theme switches.
	if match := lintBareAttrScopePattern.FindStringSubmatch(selector); match != nil {
		_, known := lintGlobalThemeAttributes[strings.ToLower(match[1])]
		return known
	}
	return false
}

// lintClipSnippet collapses whitespace and clips a snippet so a message stays
// one readable line.
func lintClipSnippet(value string) string {
	collapsed := strings.Join(strings.Fields(value), " ")
	const snippetLimit = 160
	if len(collapsed) <= snippetLimit {
		return collapsed
	}
	return lintClipRunes(collapsed, snippetLimit-1) + "…"
}

// lintClipRunes truncates to a rune-safe prefix so a clipped snippet cannot
// cut a multi-byte character in half.
func lintClipRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
