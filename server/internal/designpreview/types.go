package designpreview

import "context"

const ReceiptSchemaV1 = "multica.design-preview-receipt/v1"

const (
	FailureDocumentNotLoaded   = "document_not_loaded"
	FailureDOMEmpty            = "dom_empty"
	FailureComputedHidden      = "computed_visibility_hidden"
	FailureRenderedMissing     = "rendered_content_not_visible"
	FailurePageDimensions      = "page_dimensions_exceeded"
	FailureResourceLoad        = "resource_load_failed"
	FailureOutboundRequest     = "outbound_request_blocked"
	FailureConsoleError        = "console_error"
	FailureScreenshotMissing   = "screenshot_missing"
	FailureScreenshotUniform   = "screenshot_uniform"
	FailureInteractionMissing  = "interaction_control_missing"
	FailureInteractionNoEffect = "interaction_no_effect"
)

type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

type TargetURL struct {
	Target             Target
	URL                string
	RequireInteraction bool
}

type Verifier interface {
	Verify(context.Context, []TargetURL) (Verification, error)
}

type BrowserIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Policy struct {
	ViewportWidth         int     `json:"viewport_width"`
	ViewportHeight        int     `json:"viewport_height"`
	MinEntropy            float64 `json:"min_entropy"`
	MinMaxChannelStddev   float64 `json:"min_max_channel_stddev"`
	RequireSameOrigin     bool    `json:"require_same_origin"`
	RequireConsoleClean   bool    `json:"require_console_clean"`
	RequireResourcesClean bool    `json:"require_resources_clean"`
}

type Screenshot struct {
	SHA256           string  `json:"sha256,omitempty"`
	Bytes            int     `json:"bytes"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Entropy          float64 `json:"entropy"`
	MaxChannelStddev float64 `json:"max_channel_stddev"`
}

type Capture struct {
	Target                    Target
	DocumentLoaded            bool
	DOMPresent                bool
	ComputedVisibilityVisible bool
	RenderedElementCount      int
	VisibleTextLength         int
	BodyWidth                 int
	BodyHeight                int
	ImageCount                int
	FailedImageCount          int
	FailedResourceCount       int
	ConsoleErrorCount         int
	OutboundRequestCount      int
	InteractionRequired       bool
	InteractiveElementCount   int
	InteractionChanged        bool
	Screenshot                Screenshot
}

type TargetVerification struct {
	Target                    Target     `json:"target"`
	Passed                    bool       `json:"passed"`
	FailureCode               string     `json:"failure_code,omitempty"`
	DocumentLoaded            bool       `json:"document_loaded"`
	DOMPresent                bool       `json:"dom_present"`
	ComputedVisibilityVisible bool       `json:"computed_visibility_visible"`
	RenderedElementCount      int        `json:"rendered_element_count"`
	VisibleTextLength         int        `json:"visible_text_length"`
	BodyWidth                 int        `json:"body_width"`
	BodyHeight                int        `json:"body_height"`
	ImageCount                int        `json:"image_count"`
	FailedImageCount          int        `json:"failed_image_count"`
	FailedResourceCount       int        `json:"failed_resource_count"`
	ConsoleErrorCount         int        `json:"console_error_count"`
	OutboundRequestCount      int        `json:"outbound_request_count"`
	InteractionRequired       bool       `json:"interaction_required,omitempty"`
	InteractiveElementCount   int        `json:"interactive_element_count,omitempty"`
	InteractionChanged        bool       `json:"interaction_changed,omitempty"`
	Screenshot                Screenshot `json:"screenshot"`
}

type Verification struct {
	Browser BrowserIdentity      `json:"browser"`
	Policy  Policy               `json:"policy"`
	Targets []TargetVerification `json:"targets"`
	Passed  bool                 `json:"passed"`
}

type Receipt struct {
	SchemaVersion string       `json:"schema_version"`
	ContentDigest string       `json:"content_digest"`
	Verification  Verification `json:"verification"`
}
