package designdocument

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// templateResidueMarkers are placeholder texts that mean the agent shipped a
// template instead of a design. The scan is deliberately narrow so it cannot
// fire on real product copy.
var templateResidueMarkers = []string{"lorem ipsum", "todo:", "fixme:"}

// packageAudit is the audit verdict plus the deck slide projection the manifest
// carries. The projection stays out of auditResult so a non-deck package never
// gains the manifest key.
type packageAudit struct {
	auditResult
	slides []SlideIndexEntry
}

// auditPackage is the single place that decides whether a design document
// package may become a draft revision.
//
// The coverage document carries an agent self-assessment. Nothing in that
// self-assessment reaches this verdict: only checks the platform can perform
// itself, such as reference resolution, uniqueness, digest binding and the
// static prototype safety rules, can change Passed.
func auditPackage(
	files map[string][]byte,
	index []ArtifactIndexEntry,
	binding PackageBinding,
	contentDigest string,
	previewTargets []PreviewTarget,
) packageAudit {
	result := packageAudit{}
	diagnostics := make([]Diagnostic, 0)
	for _, required := range []string{briefPath, coveragePath, prototypeEntryPath} {
		if len(bytes.TrimSpace(files[required])) == 0 {
			diagnostics = append(diagnostics, errorDiagnostic("artifact_missing", required, required+" must be present and non-empty"))
		}
	}
	if hasErrors(diagnostics) {
		result.report = report(contentDigest, diagnostics)
		return result
	}

	artifactByPath := make(map[string]ArtifactIndexEntry, len(index))
	for _, entry := range index {
		artifactByPath[entry.Path] = entry
	}

	// A deck is a single-file prototype: the deck contract lives on the
	// prototype entry, and every other page in the package stays a plain page.
	deck := IsDeckDocument(files[prototypeEntryPath])

	brief, briefIdentity, briefDiagnostics := auditBrief(files[briefPath])
	diagnostics = append(diagnostics, briefDiagnostics...)
	diagnostics = append(diagnostics, auditCoverage(files[coveragePath], briefIdentity, binding)...)
	// Optional. When present it must be a well-formed critique report; its
	// contents never reach the verdict (DC-050).
	if raw, present := files[critiquePath]; present {
		diagnostics = append(diagnostics, auditCritique(raw)...)
	}
	if briefIdentity != nil {
		diagnostics = append(diagnostics, auditPrototypeIdentity(briefIdentity, previewTargets)...)
	}

	for _, entry := range index {
		contents := files[entry.Path]
		// Only the prototype entry can be a deck document, so every deck
		// exemption is scoped to that single entry. A linked .js file and any
		// other page keep the plain prototype rules.
		deckEntry := deck && entry.Role == "prototype_entry"
		switch entry.Role {
		case "prototype_entry", "prototype_page":
			diagnostics = append(diagnostics, auditMarkup(contents, entry.Path, deckEntry, artifactByPath)...)
			if deckEntry {
				slides, deckDiagnostics := auditDeck(contents, entry.Path, brief)
				result.slides = slides
				diagnostics = append(diagnostics, deckDiagnostics...)
			}
			// Anti-slop lint findings are warning-level advice (DC-050): they
			// ride along in the report and never reach the verdict, because
			// hasErrors counts DiagnosticError only.
			diagnostics = append(diagnostics, lintPrototypeDocument(string(contents), entry.Path)...)
		case "prototype_style":
			diagnostics = append(diagnostics, auditStyle(string(contents), entry.Path, false, artifactByPath)...)
			diagnostics = append(diagnostics, lintPrototypeStylesheet(string(contents), entry.Path)...)
		case "prototype_script":
			diagnostics = append(diagnostics, auditScript(contents, entry.Path, false, artifactByPath)...)
		case "asset":
			if entry.MediaType == "image/svg+xml" {
				diagnostics = append(diagnostics, auditSVGAsset(entry.Path, contents, artifactByPath)...)
			}
		}
		diagnostics = append(diagnostics, auditTemplateResidue(entry, contents, deckEntry)...)
	}

	if !hasErrors(diagnostics) {
		result.pages = briefPageIndex(brief)
		result.flows = briefFlowIndex(brief)
	}
	result.report = report(contentDigest, diagnostics)
	return result
}

// auditDeck applies the DC-068 producer contract to one deck document. Every
// diagnostic it returns is error level: each rule describes something the host
// bridge, the presenter, the PDF stitching or the skeleton contract depends on,
// so a violation must stop the package. It returns the slide projection even
// when a rule fails, because the manifest reports what the document declares.
func auditDeck(source []byte, path string, brief Brief) ([]SlideIndexEntry, []Diagnostic) {
	diagnostics := make([]Diagnostic, 0)
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, []Diagnostic{errorDiagnostic("deck_slide_missing", path, "deck document cannot be parsed as HTML")}
	}
	slides, slidesErr := DeckSlides(source)
	if slidesErr != nil {
		slides = nil
	}

	sections := deckSlideSections(document)
	if len(sections) == 0 {
		diagnostics = append(diagnostics, errorDiagnostic("deck_slide_missing", path, "deck document declares no section.slide"))
	} else {
		active := 0
		labelInvalid := false
		numbers := make(map[int]struct{}, len(sections))
		for position, section := range sections {
			if hasClass(section, "active") {
				active++
			}
			label := attributeText(section, "data-screen-label")
			match := DeckScreenLabelPattern.FindStringSubmatch(label)
			if match == nil {
				labelInvalid = true
				continue
			}
			number, convErr := strconv.Atoi(match[1])
			if convErr != nil || number != position+1 {
				labelInvalid = true
			}
			if _, duplicate := numbers[number]; duplicate {
				labelInvalid = true
			}
			numbers[number] = struct{}{}
		}
		if active != 1 {
			diagnostics = append(diagnostics, errorDiagnostic("deck_slide_active_invalid", path,
				"deck document has "+strconv.Itoa(active)+" active slides, want exactly 1"))
		}
		if labelInvalid {
			diagnostics = append(diagnostics, errorDiagnostic("deck_slide_label_invalid", path,
				"every slide needs a data-screen-label numbered 1..N consecutively and uniquely"))
		}
	}

	requiredIDs := []string{"deck-stage", "deck-prev", "deck-next", "deck-cur", "deck-total"}
	missingIDs := make([]string, 0, len(requiredIDs))
	for _, id := range requiredIDs {
		if findFirstElement(document, func(node *html.Node) bool { return attributeText(node, "id") == id }) == nil {
			missingIDs = append(missingIDs, "#"+id)
		}
	}
	if len(missingIDs) > 0 {
		diagnostics = append(diagnostics, errorDiagnostic("deck_chrome_missing", path,
			"deck is missing required chrome "+strings.Join(missingIDs, ", ")))
	}

	scriptText := deckInlineScriptText(document)
	missingLiterals := make([]string, 0, 3)
	for _, literal := range []string{DeckProtocolReadyMessageType, DeckProtocolStateMessageType, DeckProtocolNavigateMessageType} {
		if !strings.Contains(scriptText, literal) {
			missingLiterals = append(missingLiterals, literal)
		}
	}
	if len(missingLiterals) > 0 {
		diagnostics = append(diagnostics, errorDiagnostic("deck_protocol_missing", path,
			"deck inline script is missing protocol literals "+strings.Join(missingLiterals, ", ")))
	}

	frameworkStyle := findFirstElement(document, func(node *html.Node) bool { return node.Data == "style" })
	if frameworkStyle == nil {
		diagnostics = append(diagnostics, errorDiagnostic("deck_skeleton_drift", path, "deck framework <style> is missing"))
	} else if !deckFrameworkStyleAllowed(sha256Hex([]byte(normalizeDeckFrameworkStyle(elementText(frameworkStyle))))) {
		diagnostics = append(diagnostics, errorDiagnostic("deck_skeleton_drift", path,
			"deck framework <style> does not match a reviewed framework version"))
	}

	if len(brief.Pages) != 1 || brief.Pages[0].Entry != prototypeEntryPath {
		diagnostics = append(diagnostics, errorDiagnostic("deck_page_count_invalid", briefPath,
			"a deck brief must declare exactly one page whose entry is "+prototypeEntryPath))
	}

	if _, notesErr := ExtractSpeakerNotes(string(source), len(slides)); notesErr != nil {
		switch {
		case errors.Is(notesErr, ErrSpeakerNotesNotFound):
			diagnostics = append(diagnostics, errorDiagnostic("deck_notes_missing", path,
				"deck document has no speaker-notes block"))
		case errors.Is(notesErr, ErrSpeakerNotesInvalid):
			diagnostics = append(diagnostics, errorDiagnostic("deck_notes_invalid", path,
				"deck speaker-notes payload is invalid"))
		}
	}

	entries := make([]SlideIndexEntry, 0, len(slides))
	for _, slide := range slides {
		entries = append(entries, SlideIndexEntry{Index: slide.Index, Label: slide.Label, ID: slide.ID})
	}
	return entries, diagnostics
}

// deckSlideSections collects section.slide in the same scope DeckSlides uses:
// below #deck-stage when present, the whole document otherwise. A slide nested
// inside another slide is not collected twice.
func deckSlideSections(document *html.Node) []*html.Node {
	scope := findFirstElement(document, func(node *html.Node) bool { return attributeText(node, "id") == "deck-stage" })
	if scope == nil {
		scope = document
	}
	sections := make([]*html.Node, 0)
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "section" && hasClass(node, "slide") {
			sections = append(sections, node)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(scope)
	return sections
}

// deckInlineScriptText concatenates every inline script body. External scripts
// cannot carry the protocol literals this contract requires.
func deckInlineScriptText(document *html.Node) string {
	var text strings.Builder
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "script" {
			if _, hasSource := attributeValue(node, "src"); !hasSource {
				text.WriteString(elementText(node))
				text.WriteString("\n")
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return text.String()
}

// normalizeDeckFrameworkStyle folds line endings to LF before digesting. The
// HTML parser already normalizes CRLF, so this only guards a caller that
// extracts the style from raw source.
func normalizeDeckFrameworkStyle(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func deckFrameworkStyleAllowed(digest string) bool {
	for _, allowed := range DeckFrameworkStyleSHA256 {
		if digest == allowed {
			return true
		}
	}
	return false
}

// auditPrototypeIdentity keeps the brief and the prototype in agreement: every
// declared page renders a real prototype document, and every prototype document
// is claimed by a declared page.
func auditPrototypeIdentity(index *briefIndex, previewTargets []PreviewTarget) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	targets := make(map[string]struct{}, len(previewTargets))
	for _, target := range previewTargets {
		targets[target.Path] = struct{}{}
	}
	entries := make([]string, 0, len(index.entries))
	for entry := range index.entries {
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	for _, entry := range entries {
		if _, exists := targets[entry]; !exists {
			diagnostics = append(diagnostics, errorDiagnostic("brief_page_entry_unresolved", briefPath,
				"brief page entry "+entry+" is not a prototype document in this package"))
		}
	}
	for _, target := range previewTargets {
		if _, claimed := index.entries[target.Path]; !claimed {
			diagnostics = append(diagnostics, errorDiagnostic("prototype_page_undeclared", target.Path,
				"prototype document "+target.Path+" is not declared by any brief page"))
		}
	}
	return diagnostics
}

// auditTemplateResidue is the platform side of the template residue check. It
// never consults the agent self-report in coverage.json — and it does not scan
// that report either: coverage.json is where the agent is asked to state that
// no placeholder text remains, so its findings name the very markers this
// scan looks for ("No lorem ipsum ... remain."), and scanning it rejected a
// finished package for describing the check it had passed.
//
// In the deck prototype entry the speaker-notes block is authored presenter
// copy rather than prototype content, so its body is removed before the scan: a
// note that says "TODO: tighten the opening" is legitimate and must not fail
// the package.
func auditTemplateResidue(entry ArtifactIndexEntry, contents []byte, deckEntry bool) []Diagnostic {
	switch entry.Role {
	case "brief", "prototype_entry", "prototype_page", "prototype_style", "prototype_script":
	default:
		return nil
	}
	scanned := contents
	if deckEntry && entry.Role == "prototype_entry" {
		scanned = withoutSpeakerNotesBlock(contents)
	}
	lowered := strings.ToLower(string(scanned))
	for _, marker := range templateResidueMarkers {
		if strings.Contains(lowered, marker) {
			return []Diagnostic{errorDiagnostic("template_residue_detected", entry.Path,
				"template placeholder text "+marker+" is still present")}
		}
	}
	return nil
}

// withoutSpeakerNotesBlock re-renders the document with the speaker-notes
// script removed. Re-rendering keeps every other text node, attribute and
// script body, so the residue scan sees the same content the raw source holds.
// A parse failure returns the source untouched; the markup audit reports that
// separately.
func withoutSpeakerNotesBlock(source []byte) []byte {
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return source
	}
	script := findFirstElement(document, func(node *html.Node) bool {
		return node.Data == "script" && strings.EqualFold(attributeText(node, "id"), "speaker-notes")
	})
	if script == nil {
		return source
	}
	if script.Parent != nil {
		script.Parent.RemoveChild(script)
	}
	var rendered bytes.Buffer
	if err := html.Render(&rendered, document); err != nil {
		return source
	}
	return rendered.Bytes()
}

func report(contentDigest string, diagnostics []Diagnostic) AuditReport {
	return AuditReport{
		SchemaVersion: AuditSchemaV1,
		Passed:        !hasErrors(diagnostics),
		ContentDigest: contentDigest,
		Diagnostics:   nonNilDiagnostics(diagnostics),
	}
}
