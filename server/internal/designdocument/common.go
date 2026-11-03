package designdocument

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

// ErrInvalidPackage is returned whenever a package fails collection,
// structural validation or audit.
var ErrInvalidPackage = errors.New("design document package is invalid")

// semanticIDPattern is the stable ID shape shared by brief objects, coverage
// references and Preview target IDs. Lowercase and URL safe so an ID can be
// used directly in a route, a locator and a Preview receipt.
var semanticIDPattern = regexp.MustCompile("^[a-z0-9][a-z0-9._:-]{0,127}$")

func validSemanticID(value string) bool {
	return semanticIDPattern.MatchString(value)
}

func errorDiagnostic(code, path, message string) Diagnostic {
	return Diagnostic{Code: code, Severity: DiagnosticError, Path: path, Message: message}
}

func hasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == DiagnosticError {
			return true
		}
	}
	return false
}

// ErrorDiagnostics returns only the findings that block the package, in report
// order. Warnings advise the agent and never gate anything, so every consumer
// that reports why a package failed must read this rather than Diagnostics —
// a warning in a failure payload would claim a rule that did not actually
// reject the run.
func ErrorDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	errors := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == DiagnosticError {
			errors = append(errors, diagnostic)
		}
	}
	return errors
}

func nonNilDiagnostics(values []Diagnostic) []Diagnostic {
	if values == nil {
		return []Diagnostic{}
	}
	return values
}

func nonNilFiles(values []ArtifactIndexEntry) []ArtifactIndexEntry {
	if values == nil {
		return []ArtifactIndexEntry{}
	}
	return values
}

func nonNilPreviewTargets(values []PreviewTarget) []PreviewTarget {
	if values == nil {
		return []PreviewTarget{}
	}
	return values
}

func nonNilPages(values []PageIndexEntry) []PageIndexEntry {
	if values == nil {
		return []PageIndexEntry{}
	}
	return values
}

func nonNilFlows(values []FlowIndexEntry) []FlowIndexEntry {
	if values == nil {
		return []FlowIndexEntry{}
	}
	return values
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func sha256String(value []byte) string {
	return "sha256:" + sha256Hex(value)
}

func validSHA256Reference(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}
