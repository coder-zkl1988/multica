package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPMOSnapshotBytes     = 2 << 20
	maxPMOKeyBytes          = 256
	maxPMOTitleBytes        = 500
	maxPMODescriptionBytes  = 64 << 10
	maxPMOStatusBytes       = 128
	maxPMOChildren          = 2000
	maxPMOTasksPerContainer = 5000
	maxPMOTasksTotal        = 10000
)

var (
	ErrIncompletePMOSnapshot = errors.New("pmo snapshot is incomplete")
	ErrPMOSnapshotTooLarge   = errors.New("pmo snapshot is too large")

	validPMOProjectStatuses = map[string]struct{}{
		"planned": {}, "in_progress": {}, "paused": {}, "completed": {}, "cancelled": {},
	}
	validPMOIssueStatuses = map[string]struct{}{
		"backlog": {}, "todo": {}, "in_progress": {}, "in_review": {},
		"done": {}, "blocked": {}, "cancelled": {},
	}
)

type PMOSnapshot struct {
	SchemaVersion    string           `json:"schema_version"`
	SnapshotComplete bool             `json:"snapshot_complete"`
	Parent           PMORequirement   `json:"parent_requirement"`
	Children         []PMORequirement `json:"child_requirements"`
	Tasks            []PMOTask        `json:"tasks"`
}

type PMORequirement struct {
	Key           string            `json:"key"`
	DisplayNumber string            `json:"display_number"`
	NumericID     int64             `json:"numeric_id"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	SourceStatus  string            `json:"source_status"`
	Status        string            `json:"status"`
	Owner         *PMOExternalOwner `json:"owner"`
	StartDate     *string           `json:"start_date"`
	DueDate       *string           `json:"due_date"`
	Workload      *float64          `json:"workload"`
	// No omitempty: the stored normalized snapshot must stay round-trip
	// re-validatable (tasks arrays are required), so empty arrays marshal
	// explicitly rather than disappearing.
	Tasks []PMOTask `json:"tasks"`
}

type PMOTask struct {
	TaskID       string            `json:"task_id"`
	SchemeID     string            `json:"scheme_id"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	SourceStatus string            `json:"source_status"`
	Status       string            `json:"status"`
	Owner        *PMOExternalOwner `json:"owner"`
	StartDate    *string           `json:"start_date"`
	DueDate      *string           `json:"due_date"`
	Workload     *float64          `json:"workload"`
	UpdatedAt    *string           `json:"updated_at"`
}

type PMOExternalOwner struct {
	ExternalID  string `json:"external_id"`
	DisplayName string `json:"display_name"`
}

func ParsePMOSnapshot(output string) (PMOSnapshot, error) {
	if len(output) > maxPMOSnapshotBytes {
		return PMOSnapshot{}, ErrPMOSnapshotTooLarge
	}
	if !utf8.ValidString(output) {
		return PMOSnapshot{}, errors.New("pmo snapshot is not valid UTF-8")
	}

	raw := output
	// Agents wrap the JSON in prose or a Markdown fence often enough that
	// strict-only parsing turns a cosmetic deviation into a hard run failure
	// ("decode pmo snapshot: invalid character 'D' looking for beginning of
	// value"). Only when the output does not already begin with '{' do we fall
	// back to extracting the first balanced JSON object; output that starts as
	// JSON stays strict, so a valid snapshot followed by a trailing JSON blob
	// is still rejected (and cannot smuggle content past the parser).
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		extracted, err := extractPMOSnapshotJSONObject(output)
		if err != nil {
			return PMOSnapshot{}, err
		}
		raw = extracted
	}

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var snapshot PMOSnapshot
	if err := dec.Decode(&snapshot); err != nil {
		return PMOSnapshot{}, fmt.Errorf("decode pmo snapshot: %w", err)
	}
	if err := requirePMOJSONEOF(dec); err != nil {
		return PMOSnapshot{}, err
	}
	if err := snapshot.validate(); err != nil {
		return PMOSnapshot{}, err
	}
	return snapshot.normalize(), nil
}

// extractPMOSnapshotJSONObject returns the first balanced JSON object in the
// agent output, skipping any leading prose or Markdown fence. The content
// after the object must not contain another '{' (a second JSON blob), so the
// one-object contract holds outside the strict-prefix path too.
func extractPMOSnapshotJSONObject(output string) (string, error) {
	start := strings.IndexByte(output, '{')
	if start < 0 {
		return "", errors.New("pmo snapshot output contains no JSON object")
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(output); i++ {
		c := output[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if strings.Contains(output[i+1:], "{") {
					return "", errors.New("pmo snapshot contains trailing JSON")
				}
				return output[start : i+1], nil
			}
		}
	}
	return "", errors.New("pmo snapshot JSON object is unbalanced")
}

func requirePMOJSONEOF(dec *json.Decoder) error {
	var trailing any
	err := dec.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing pmo snapshot content: %w", err)
	}
	return errors.New("pmo snapshot contains trailing JSON")
}

func (s PMOSnapshot) validate() error {
	if strings.TrimSpace(s.SchemaVersion) != "1" {
		return fmt.Errorf("unsupported pmo schema version %q", s.SchemaVersion)
	}
	if !s.SnapshotComplete {
		return ErrIncompletePMOSnapshot
	}
	if s.Children == nil || s.Tasks == nil {
		return errors.New("pmo snapshot must include child_requirements and tasks arrays")
	}
	if len(s.Children) > maxPMOChildren {
		return fmt.Errorf("pmo snapshot has too many child requirements: %d", len(s.Children))
	}
	if len(s.Tasks) > maxPMOTasksPerContainer {
		return fmt.Errorf("pmo snapshot has too many parent tasks: %d", len(s.Tasks))
	}

	requirementKeys := make(map[string]struct{}, len(s.Children)+1)
	numericIDs := make(map[int64]struct{}, len(s.Children)+1)
	if err := validatePMORequirement(s.Parent, "parent_requirement", validPMOProjectStatuses, false); err != nil {
		return err
	}
	if err := addPMORequirementIdentity(requirementKeys, numericIDs, s.Parent, "parent_requirement"); err != nil {
		return err
	}

	taskIDs := make(map[string]struct{})
	taskCount := 0
	for i, task := range s.Tasks {
		if err := validatePMOTask(task, fmt.Sprintf("tasks[%d]", i)); err != nil {
			return err
		}
		if err := addPMOTaskIdentity(taskIDs, task, fmt.Sprintf("tasks[%d]", i)); err != nil {
			return err
		}
		taskCount++
	}

	for i, child := range s.Children {
		path := fmt.Sprintf("child_requirements[%d]", i)
		if err := validatePMORequirement(child, path, validPMOIssueStatuses, true); err != nil {
			return err
		}
		if err := addPMORequirementIdentity(requirementKeys, numericIDs, child, path); err != nil {
			return err
		}
		for j, task := range child.Tasks {
			taskPath := fmt.Sprintf("%s.tasks[%d]", path, j)
			if err := validatePMOTask(task, taskPath); err != nil {
				return err
			}
			if err := addPMOTaskIdentity(taskIDs, task, taskPath); err != nil {
				return err
			}
			taskCount++
		}
	}
	if taskCount > maxPMOTasksTotal {
		return fmt.Errorf("pmo snapshot has too many tasks: %d", taskCount)
	}
	return nil
}

func validatePMORequirement(requirement PMORequirement, path string, statuses map[string]struct{}, tasksRequired bool) error {
	if err := validatePMOIdentity(requirement.Key, path+".key"); err != nil {
		return err
	}
	if err := validatePMOIdentity(requirement.DisplayNumber, path+".display_number"); err != nil {
		return err
	}
	if requirement.NumericID <= 0 {
		return fmt.Errorf("%s.numeric_id must be positive", path)
	}
	if err := validatePMOText(requirement.Title, path+".title", maxPMOTitleBytes, true); err != nil {
		return err
	}
	if err := validatePMOText(requirement.Description, path+".description", maxPMODescriptionBytes, false); err != nil {
		return err
	}
	if err := validatePMOText(requirement.SourceStatus, path+".source_status", maxPMOStatusBytes, true); err != nil {
		return err
	}
	if _, ok := statuses[strings.TrimSpace(requirement.Status)]; !ok {
		return fmt.Errorf("%s.status is invalid: %q", path, requirement.Status)
	}
	if err := validatePMOOwner(requirement.Owner, path+".owner"); err != nil {
		return err
	}
	if err := validatePMODates(requirement.StartDate, requirement.DueDate, path); err != nil {
		return err
	}
	if err := validatePMOWorkload(requirement.Workload, path+".workload"); err != nil {
		return err
	}
	if tasksRequired {
		if requirement.Tasks == nil {
			return fmt.Errorf("%s.tasks is required", path)
		}
		if len(requirement.Tasks) > maxPMOTasksPerContainer {
			return fmt.Errorf("%s has too many tasks: %d", path, len(requirement.Tasks))
		}
	} else if len(requirement.Tasks) != 0 {
		return errors.New("parent_requirement.tasks must be empty; use top-level tasks")
	}
	return nil
}

func validatePMOTask(task PMOTask, path string) error {
	if err := validatePMOIdentity(task.TaskID, path+".task_id"); err != nil {
		return err
	}
	if err := validatePMOIdentity(task.SchemeID, path+".scheme_id"); err != nil {
		return err
	}
	if err := validatePMOText(task.Title, path+".title", maxPMOTitleBytes, true); err != nil {
		return err
	}
	if err := validatePMOText(task.Description, path+".description", maxPMODescriptionBytes, false); err != nil {
		return err
	}
	if err := validatePMOText(task.SourceStatus, path+".source_status", maxPMOStatusBytes, true); err != nil {
		return err
	}
	if _, ok := validPMOIssueStatuses[strings.TrimSpace(task.Status)]; !ok {
		return fmt.Errorf("%s.status is invalid: %q", path, task.Status)
	}
	if err := validatePMOOwner(task.Owner, path+".owner"); err != nil {
		return err
	}
	if err := validatePMODates(task.StartDate, task.DueDate, path); err != nil {
		return err
	}
	if err := validatePMOWorkload(task.Workload, path+".workload"); err != nil {
		return err
	}
	if task.UpdatedAt != nil {
		value := strings.TrimSpace(*task.UpdatedAt)
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return fmt.Errorf("%s.updated_at must be RFC3339: %w", path, err)
		}
	}
	return nil
}

func validatePMOIdentity(value, path string) error {
	return validatePMOText(value, path, maxPMOKeyBytes, true)
}

func validatePMOText(value, path string, maxBytes int, required bool) error {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return fmt.Errorf("%s is required", path)
	}
	if len(trimmed) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", path, maxBytes)
	}
	return nil
}

func validatePMOOwner(owner *PMOExternalOwner, path string) error {
	if owner == nil {
		return nil
	}
	if err := validatePMOIdentity(owner.ExternalID, path+".external_id"); err != nil {
		return err
	}
	return validatePMOText(owner.DisplayName, path+".display_name", maxPMOTitleBytes, true)
}

func validatePMODates(start, due *string, path string) error {
	startTime, err := parsePMODate(start, path+".start_date")
	if err != nil {
		return err
	}
	dueTime, err := parsePMODate(due, path+".due_date")
	if err != nil {
		return err
	}
	if startTime != nil && dueTime != nil && dueTime.Before(*startTime) {
		return fmt.Errorf("%s.due_date must not precede start_date", path)
	}
	return nil
}

func parsePMODate(value *string, path string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*value))
	if err != nil {
		return nil, fmt.Errorf("%s must be YYYY-MM-DD: %w", path, err)
	}
	return &parsed, nil
}

func validatePMOWorkload(value *float64, path string) error {
	if value != nil && (*value < 0 || math.IsInf(*value, 0) || math.IsNaN(*value)) {
		return fmt.Errorf("%s must be a non-negative finite number", path)
	}
	return nil
}

func addPMORequirementIdentity(keys map[string]struct{}, numericIDs map[int64]struct{}, requirement PMORequirement, path string) error {
	key := strings.TrimSpace(requirement.Key)
	if _, exists := keys[key]; exists {
		return fmt.Errorf("%s.key is duplicated: %q", path, key)
	}
	keys[key] = struct{}{}
	if _, exists := numericIDs[requirement.NumericID]; exists {
		return fmt.Errorf("%s.numeric_id is duplicated: %d", path, requirement.NumericID)
	}
	numericIDs[requirement.NumericID] = struct{}{}
	return nil
}

func addPMOTaskIdentity(ids map[string]struct{}, task PMOTask, path string) error {
	id := strings.TrimSpace(task.TaskID)
	if _, exists := ids[id]; exists {
		return fmt.Errorf("%s.task_id is duplicated: %q", path, id)
	}
	ids[id] = struct{}{}
	return nil
}

func (s PMOSnapshot) normalize() PMOSnapshot {
	s.SchemaVersion = strings.TrimSpace(s.SchemaVersion)
	s.Parent = s.Parent.normalize()
	for i := range s.Children {
		s.Children[i] = s.Children[i].normalize()
	}
	for i := range s.Tasks {
		s.Tasks[i] = s.Tasks[i].normalize()
	}
	return s
}

func (r PMORequirement) normalize() PMORequirement {
	r.Key = strings.TrimSpace(r.Key)
	r.DisplayNumber = strings.TrimSpace(r.DisplayNumber)
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	r.SourceStatus = strings.TrimSpace(r.SourceStatus)
	r.Status = strings.TrimSpace(r.Status)
	r.Owner = normalizePMOOwner(r.Owner)
	r.StartDate = normalizePMOString(r.StartDate)
	r.DueDate = normalizePMOString(r.DueDate)
	for i := range r.Tasks {
		r.Tasks[i] = r.Tasks[i].normalize()
	}
	return r
}

func (t PMOTask) normalize() PMOTask {
	t.TaskID = strings.TrimSpace(t.TaskID)
	t.SchemeID = strings.TrimSpace(t.SchemeID)
	t.Title = strings.TrimSpace(t.Title)
	t.Description = strings.TrimSpace(t.Description)
	t.SourceStatus = strings.TrimSpace(t.SourceStatus)
	t.Status = strings.TrimSpace(t.Status)
	t.Owner = normalizePMOOwner(t.Owner)
	t.StartDate = normalizePMOString(t.StartDate)
	t.DueDate = normalizePMOString(t.DueDate)
	t.UpdatedAt = normalizePMOString(t.UpdatedAt)
	return t
}

func normalizePMOOwner(owner *PMOExternalOwner) *PMOExternalOwner {
	if owner == nil {
		return nil
	}
	normalized := *owner
	normalized.ExternalID = strings.TrimSpace(normalized.ExternalID)
	normalized.DisplayName = strings.TrimSpace(normalized.DisplayName)
	return &normalized
}

func normalizePMOString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	return &normalized
}
