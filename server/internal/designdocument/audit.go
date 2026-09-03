package designdocument

import (
	"bytes"
	"sort"
	"strings"
)

// templateResidueMarkers are placeholder texts that mean the agent shipped a
// template instead of a design. The scan is deliberately narrow so it cannot
// fire on real product copy.
var templateResidueMarkers = []string{"lorem ipsum", "todo:", "fixme:"}

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
) auditResult {
	result := auditResult{}
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
		switch entry.Role {
		case "prototype_entry", "prototype_page":
			diagnostics = append(diagnostics, auditMarkup(contents, entry.Path, artifactByPath)...)
		case "prototype_style":
			diagnostics = append(diagnostics, auditStyle(string(contents), entry.Path, false, artifactByPath)...)
		case "prototype_script":
			diagnostics = append(diagnostics, auditScript(contents, entry.Path, artifactByPath)...)
		case "asset":
			if entry.MediaType == "image/svg+xml" {
				diagnostics = append(diagnostics, auditSVGAsset(entry.Path, contents, artifactByPath)...)
			}
		}
		diagnostics = append(diagnostics, auditTemplateResidue(entry, contents)...)
	}

	if !hasErrors(diagnostics) {
		result.pages = briefPageIndex(brief)
		result.flows = briefFlowIndex(brief)
	}
	result.report = report(contentDigest, diagnostics)
	return result
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
func auditTemplateResidue(entry ArtifactIndexEntry, contents []byte) []Diagnostic {
	switch entry.Role {
	case "brief", "prototype_entry", "prototype_page", "prototype_style", "prototype_script":
	default:
		return nil
	}
	lowered := strings.ToLower(string(contents))
	for _, marker := range templateResidueMarkers {
		if strings.Contains(lowered, marker) {
			return []Diagnostic{errorDiagnostic("template_residue_detected", entry.Path,
				"template placeholder text "+marker+" is still present")}
		}
	}
	return nil
}

func report(contentDigest string, diagnostics []Diagnostic) AuditReport {
	return AuditReport{
		SchemaVersion: AuditSchemaV1,
		Passed:        !hasErrors(diagnostics),
		ContentDigest: contentDigest,
		Diagnostics:   nonNilDiagnostics(diagnostics),
	}
}
