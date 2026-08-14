package designpreview

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"strings"
)

const (
	targetMaxCount       = 64
	targetIDMaxBytes     = 128
	targetPathMaxBytes   = 4 << 10
	metricMaxCount       = 1_000_000
	dimensionMax         = 100_000
	dimensionEvidenceMax = dimensionMax + 1
	screenshotMaxBytes   = 32 << 20
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func DefaultPolicy() Policy {
	return Policy{
		ViewportWidth:         1280,
		ViewportHeight:        900,
		MinEntropy:            0.1,
		MinMaxChannelStddev:   1,
		RequireSameOrigin:     true,
		RequireConsoleClean:   true,
		RequireResourcesClean: true,
	}
}

func EvaluateCapture(capture Capture, policy Policy) TargetVerification {
	verification := TargetVerification{
		Target:                    capture.Target,
		DocumentLoaded:            capture.DocumentLoaded,
		DOMPresent:                capture.DOMPresent,
		ComputedVisibilityVisible: capture.ComputedVisibilityVisible,
		RenderedElementCount:      capture.RenderedElementCount,
		VisibleTextLength:         capture.VisibleTextLength,
		BodyWidth:                 capture.BodyWidth,
		BodyHeight:                capture.BodyHeight,
		ImageCount:                capture.ImageCount,
		FailedImageCount:          capture.FailedImageCount,
		FailedResourceCount:       capture.FailedResourceCount,
		ConsoleErrorCount:         capture.ConsoleErrorCount,
		OutboundRequestCount:      capture.OutboundRequestCount,
		InteractionRequired:       capture.InteractionRequired,
		InteractiveElementCount:   capture.InteractiveElementCount,
		InteractionChanged:        capture.InteractionChanged,
		Screenshot:                capture.Screenshot,
	}
	switch {
	case !capture.DocumentLoaded:
		verification.FailureCode = FailureDocumentNotLoaded
	case !capture.DOMPresent || capture.BodyWidth <= 0 || capture.BodyHeight <= 0:
		verification.FailureCode = FailureDOMEmpty
	case !capture.ComputedVisibilityVisible:
		verification.FailureCode = FailureComputedHidden
	case capture.RenderedElementCount <= 0:
		verification.FailureCode = FailureRenderedMissing
	case capture.BodyWidth > dimensionMax || capture.BodyHeight > dimensionMax:
		verification.FailureCode = FailurePageDimensions
	case policy.RequireSameOrigin && capture.OutboundRequestCount > 0:
		verification.FailureCode = FailureOutboundRequest
	case policy.RequireResourcesClean && (capture.FailedImageCount > 0 || capture.FailedResourceCount > 0):
		verification.FailureCode = FailureResourceLoad
	case policy.RequireConsoleClean && capture.ConsoleErrorCount > 0:
		verification.FailureCode = FailureConsoleError
	case capture.InteractionRequired && capture.InteractiveElementCount == 0:
		verification.FailureCode = FailureInteractionMissing
	case capture.InteractionRequired && !capture.InteractionChanged:
		verification.FailureCode = FailureInteractionNoEffect
	case capture.Screenshot.SHA256 == "" || capture.Screenshot.Bytes <= 0 || capture.Screenshot.Width <= 0 || capture.Screenshot.Height <= 0:
		verification.FailureCode = FailureScreenshotMissing
	case capture.Screenshot.Entropy < policy.MinEntropy || capture.Screenshot.MaxChannelStddev < policy.MinMaxChannelStddev:
		verification.FailureCode = FailureScreenshotUniform
	default:
		verification.Passed = true
	}
	return verification
}

func NewReceipt(contentDigest string, verification Verification) (Receipt, error) {
	receipt := Receipt{
		SchemaVersion: ReceiptSchemaV1,
		ContentDigest: contentDigest,
		Verification:  verification,
	}
	targets := make([]Target, 0, len(verification.Targets))
	for _, target := range verification.Targets {
		targets = append(targets, target.Target)
	}
	if err := ValidateReceipt(receipt, contentDigest, targets); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func ValidateReceipt(receipt Receipt, expectedDigest string, expectedTargets []Target) error {
	if receipt.SchemaVersion != ReceiptSchemaV1 {
		return fmt.Errorf("Design Preview receipt schema %q does not match %q", receipt.SchemaVersion, ReceiptSchemaV1)
	}
	if err := validateContentDigest(receipt.ContentDigest); err != nil {
		return err
	}
	if receipt.ContentDigest != expectedDigest {
		return errors.New("Design Preview receipt content digest does not match the expected digest")
	}
	if err := ValidateVerification(receipt.Verification, DefaultPolicy()); err != nil {
		return err
	}
	return ValidateTargetSet(receipt.Verification, expectedTargets)
}

func ValidateReceiptWithInteractions(receipt Receipt, expectedDigest string, expectedTargets []Target, required map[string]bool) error {
	if err := ValidateReceipt(receipt, expectedDigest, expectedTargets); err != nil {
		return err
	}
	for _, target := range receipt.Verification.Targets {
		want := required[target.Target.ID]
		if target.InteractionRequired != want {
			return fmt.Errorf("Design Preview target %q interaction requirement does not match", target.Target.ID)
		}
		if want && (target.InteractiveElementCount == 0 || !target.InteractionChanged) {
			return fmt.Errorf("Design Preview target %q interaction evidence did not pass", target.Target.ID)
		}
	}
	return nil
}

func ValidateVerification(verification Verification, expectedPolicy Policy) error {
	if strings.TrimSpace(verification.Browser.Name) == "" || strings.TrimSpace(verification.Browser.Version) == "" ||
		len(verification.Browser.Name) > 128 || len(verification.Browser.Version) > 256 {
		return errors.New("Design Preview browser identity is invalid")
	}
	if verification.Policy != expectedPolicy {
		return errors.New("Design Preview verification policy is not pinned")
	}
	if len(verification.Targets) == 0 || len(verification.Targets) > targetMaxCount {
		return errors.New("Design Preview verification has an invalid target count")
	}
	seen := make(map[string]struct{}, len(verification.Targets))
	allPassed := true
	for _, target := range verification.Targets {
		if err := validateTargetVerification(target, verification.Policy); err != nil {
			return err
		}
		key := target.Target.Kind + "\x00" + target.Target.ID + "\x00" + target.Target.Path
		if _, exists := seen[key]; exists {
			return errors.New("Design Preview verification repeats a target")
		}
		seen[key] = struct{}{}
		allPassed = allPassed && target.Passed
	}
	if verification.Passed != allPassed {
		return errors.New("Design Preview overall result does not match its targets")
	}
	return nil
}

func ValidateTargetSet(verification Verification, expected []Target) error {
	if len(verification.Targets) != len(expected) {
		return errors.New("Design Preview verification target count does not match the declared targets")
	}
	for index, target := range verification.Targets {
		if target.Target != expected[index] {
			return fmt.Errorf("Design Preview verification target %d does not match the declared target", index)
		}
	}
	return nil
}

func ValidateTarget(target Target) error {
	if target.Kind != "preview" && target.Kind != "ui_kit" {
		return errors.New("Design Preview target kind is invalid")
	}
	if target.ID == "" || strings.TrimSpace(target.ID) != target.ID || len(target.ID) > targetIDMaxBytes {
		return errors.New("Design Preview target id is invalid")
	}
	if len(target.Path) > targetPathMaxBytes {
		return errors.New("Design Preview target path exceeds the size limit")
	}
	if err := validatePath(target.Path); err != nil || strings.ToLower(path.Ext(target.Path)) != ".html" {
		return errors.New("Design Preview target path is invalid")
	}
	return nil
}

func validateTargetVerification(target TargetVerification, policy Policy) error {
	if err := ValidateTarget(target.Target); err != nil {
		return err
	}
	for _, value := range []int{
		target.RenderedElementCount, target.VisibleTextLength, target.ImageCount,
		target.FailedImageCount, target.FailedResourceCount, target.ConsoleErrorCount,
		target.OutboundRequestCount, target.InteractiveElementCount,
	} {
		if value < 0 || value > metricMaxCount {
			return errors.New("Design Preview target has an invalid count")
		}
	}
	if target.FailedImageCount > target.ImageCount || target.BodyWidth < 0 || target.BodyWidth > dimensionEvidenceMax ||
		target.BodyHeight < 0 || target.BodyHeight > dimensionEvidenceMax {
		return errors.New("Design Preview target has invalid dimensions or image counts")
	}
	if err := validateScreenshot(target.Screenshot, target.Passed); err != nil {
		return err
	}
	capture := Capture{
		Target:                    target.Target,
		DocumentLoaded:            target.DocumentLoaded,
		DOMPresent:                target.DOMPresent,
		ComputedVisibilityVisible: target.ComputedVisibilityVisible,
		RenderedElementCount:      target.RenderedElementCount,
		VisibleTextLength:         target.VisibleTextLength,
		BodyWidth:                 target.BodyWidth,
		BodyHeight:                target.BodyHeight,
		ImageCount:                target.ImageCount,
		FailedImageCount:          target.FailedImageCount,
		FailedResourceCount:       target.FailedResourceCount,
		ConsoleErrorCount:         target.ConsoleErrorCount,
		OutboundRequestCount:      target.OutboundRequestCount,
		InteractionRequired:       target.InteractionRequired,
		InteractiveElementCount:   target.InteractiveElementCount,
		InteractionChanged:        target.InteractionChanged,
		Screenshot:                target.Screenshot,
	}
	evaluated := EvaluateCapture(capture, policy)
	if evaluated.Passed != target.Passed || evaluated.FailureCode != target.FailureCode {
		return errors.New("Design Preview passed target does not match its visual signals")
	}
	return nil
}

func validateScreenshot(screenshot Screenshot, required bool) error {
	empty := screenshot.SHA256 == "" && screenshot.Bytes == 0 && screenshot.Width == 0 && screenshot.Height == 0 &&
		screenshot.Entropy == 0 && screenshot.MaxChannelStddev == 0
	if empty && !required {
		return nil
	}
	if err := validateContentDigest(screenshot.SHA256); err != nil {
		return errors.New("Design Preview screenshot digest is invalid")
	}
	if screenshot.Bytes <= 0 || screenshot.Bytes > screenshotMaxBytes ||
		screenshot.Width <= 0 || screenshot.Width > dimensionMax ||
		screenshot.Height <= 0 || screenshot.Height > dimensionMax ||
		math.IsNaN(screenshot.Entropy) || math.IsInf(screenshot.Entropy, 0) || screenshot.Entropy < 0 || screenshot.Entropy > 8 ||
		math.IsNaN(screenshot.MaxChannelStddev) || math.IsInf(screenshot.MaxChannelStddev, 0) || screenshot.MaxChannelStddev < 0 || screenshot.MaxChannelStddev > 128 {
		return errors.New("Design Preview screenshot metrics are invalid")
	}
	return nil
}

func validateContentDigest(contentDigest string) error {
	if !strings.HasPrefix(contentDigest, "sha256:") || !digestPattern.MatchString(strings.TrimPrefix(contentDigest, "sha256:")) {
		return errors.New("Design Preview content digest is invalid")
	}
	return nil
}

func validatePath(value string) error {
	trimmed := strings.TrimSpace(value)
	if value != trimmed || value == "" || strings.Contains(value, "\\") || !fs.ValidPath(value) {
		return errors.New("path must be a normalized relative slash path")
	}
	return nil
}
