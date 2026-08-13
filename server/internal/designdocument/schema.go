package designdocument

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func validateBinding(binding Binding) *packageError {
	identities := []struct{ name, value string }{
		{"document", binding.DocumentID}, {"revision", binding.RevisionID}, {"workspace", binding.WorkspaceID},
		{"project", binding.ProjectID}, {"task", binding.TaskID}, {"agent", binding.AgentID},
	}
	if binding.IssueID != "" {
		identities = append(identities, struct{ name, value string }{"issue", binding.IssueID})
	}
	for _, identity := range identities {
		if !validText(identity.value) {
			return newError("binding_identity_invalid", "manifest.json", fmt.Sprintf("%s identity is invalid", identity.name))
		}
	}
	if binding.TargetPlatform != "" && binding.TargetPlatform != "web" && binding.TargetPlatform != "mobile" && binding.TargetPlatform != "cross_platform" {
		return newError("binding_target_platform_invalid", "manifest.json", "target platform is invalid")
	}
	for _, digest := range []string{binding.InputSnapshotSHA256, binding.BaseContentDigest, binding.DesignSystemContentDigest} {
		if digest != "" && !validDigest(digest) {
			return newError("binding_digest_invalid", "manifest.json", "binding digest is invalid")
		}
	}
	if (binding.BaseRevisionID == "") != (binding.BaseContentDigest == "") {
		return newError("binding_base_pair_invalid", "manifest.json", "base revision and digest must be paired")
	}
	designCount := 0
	for _, value := range []string{binding.DesignSystemID, binding.DesignSystemSourceTaskID, binding.DesignSystemContentDigest} {
		if value != "" {
			designCount++
		}
	}
	if designCount != 0 && designCount != 3 {
		return newError("binding_design_system_triple_invalid", "manifest.json", "design system binding must be all or none")
	}
	return nil
}

func validText(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}
func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[7:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

type packageError struct{ code, path, message string }

func (e *packageError) Error() string { return e.code + ": " + e.message }
func newError(code, path, message string) *packageError {
	return &packageError{code: code, path: path, message: message}
}
func errorReport(code, path, message, digest string) AuditReport {
	return AuditReport{SchemaVersion: AuditSchema, Passed: false, ContentDigest: digest, Diagnostics: []Diagnostic{{Code: code, Severity: SeverityError, Path: path, Message: message}}}
}
func auditError(report AuditReport) error {
	code := "audit_failed"
	if len(report.Diagnostics) > 0 {
		code = report.Diagnostics[0].Code
	}
	return fmt.Errorf("invalid design document: %s", code)
}
