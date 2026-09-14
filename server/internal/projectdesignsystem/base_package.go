package projectdesignsystem

import (
	"errors"
	"strings"
)

// BasePackageReferenceSchema mirrors Open Design's reference-first base flow:
// the task carries immutable identity only, while the daemon downloads and
// revalidates the complete archive before the Agent starts.
const BasePackageReferenceSchema = "multica.project-design-system-base-package-reference/v1"

const (
	BasePackageArchiveContentType = "application/zip"
	BasePackageDigestHeader       = "X-Multica-Design-Package-Digest"
	BasePackageSlotHeader         = "X-Multica-Design-Package-Slot"
	BasePackageSourceTaskHeader   = "X-Multica-Design-Package-Source-Task-ID"
	MaxArchiveBytes               = maxV2ArchiveBytes
)

type BasePackageReference struct {
	Schema        string         `json:"schema"`
	Slot          string         `json:"slot"`
	ContentDigest string         `json:"content_digest"`
	SourceTaskID  string         `json:"source_task_id"`
	Binding       PackageBinding `json:"binding"`
}

func ValidateBasePackageReference(reference BasePackageReference) error {
	if reference.Schema != BasePackageReferenceSchema {
		return errors.New("project design system base package reference schema is invalid")
	}
	if reference.Slot != "draft" && reference.Slot != "saved" {
		return errors.New("project design system base package reference slot is invalid")
	}
	if !validSHA256Reference(reference.ContentDigest) {
		return errors.New("project design system base package reference digest is invalid")
	}
	if strings.TrimSpace(reference.SourceTaskID) == "" || reference.SourceTaskID != reference.Binding.TaskID {
		return errors.New("project design system base package source task is invalid")
	}
	if err := validateV2Binding(reference.Binding); err != nil {
		return err
	}
	return nil
}
