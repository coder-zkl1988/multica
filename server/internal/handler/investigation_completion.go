package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const investigationResultMarker = "INVESTIGATION_RESULT_JSON:"

type investigationResult struct {
	Kind            string          `json:"kind"`
	Question        string          `json:"question"`
	RootCause       string          `json:"root_cause"`
	Evidence        json.RawMessage `json:"evidence"`
	Confidence      string          `json:"confidence"`
	Category        string          `json:"category"`
	Recommendations []string        `json:"recommendations"`
	OpenQuestions   []string        `json:"open_questions"`
}

func parseInvestigationResult(output string) (investigationResult, error) {
	idx := strings.LastIndex(output, investigationResultMarker)
	if idx < 0 {
		return investigationResult{}, errors.New("missing investigation result")
	}
	tail := strings.TrimSpace(output[idx+len(investigationResultMarker):])
	tail = strings.TrimPrefix(tail, "```json")
	tail = strings.TrimPrefix(tail, "```")
	if fence := strings.Index(tail, "```"); fence >= 0 {
		tail = tail[:fence]
	}
	var result investigationResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(tail)), &result); err != nil {
		return investigationResult{}, errors.New("invalid investigation result JSON")
	}
	switch result.Kind {
	case "needs_input":
		if strings.TrimSpace(result.Question) == "" {
			return investigationResult{}, errors.New("investigation input request is missing a question")
		}
	case "conclusion":
		var evidence []json.RawMessage
		if strings.TrimSpace(result.RootCause) == "" || json.Unmarshal(result.Evidence, &evidence) != nil || len(evidence) == 0 {
			return investigationResult{}, errors.New("investigation conclusion is incomplete")
		}
		switch result.Confidence {
		case "confirmed", "provisional", "unverified":
		default:
			return investigationResult{}, errors.New("invalid investigation confidence")
		}
	default:
		return investigationResult{}, errors.New("invalid investigation result kind")
	}
	return result, nil
}

func persistInvestigationResult(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, result investigationResult) error {
	contextValue, ok := service.ParseInvestigationTaskContext(task)
	if !ok {
		return errors.New("task context is no longer an investigation")
	}
	workspaceID := parseUUID(contextValue.WorkspaceID)
	if result.Kind == "needs_input" {
		if _, err := q.UpdateInvestigationStatus(ctx, db.UpdateInvestigationStatusParams{
			ID: task.InvestigationID, WorkspaceID: workspaceID, Status: "needs_input",
		}); err != nil {
			return err
		}
		_, err := q.CreateInvestigationComment(ctx, db.CreateInvestigationCommentParams{
			WorkspaceID: workspaceID, InvestigationID: task.InvestigationID,
			AuthorType: "agent", AuthorID: task.AgentID, Content: strings.TrimSpace(result.Question),
			Type: "input_request", TaskID: task.ID,
		})
		return err
	}

	recommendations, _ := json.Marshal(result.Recommendations)
	openQuestions, _ := json.Marshal(result.OpenQuestions)
	if _, err := q.UpdateInvestigationConclusion(ctx, db.UpdateInvestigationConclusionParams{
		ID: task.InvestigationID, WorkspaceID: workspaceID,
		RootCause: pgtype.Text{String: strings.TrimSpace(result.RootCause), Valid: true},
		Evidence:  result.Evidence, Confidence: pgtype.Text{String: result.Confidence, Valid: true},
		Category:        pgtype.Text{String: strings.TrimSpace(result.Category), Valid: strings.TrimSpace(result.Category) != ""},
		Recommendations: recommendations, OpenQuestions: openQuestions,
	}); err != nil {
		return err
	}
	_, err := q.CreateInvestigationComment(ctx, db.CreateInvestigationCommentParams{
		WorkspaceID: workspaceID, InvestigationID: task.InvestigationID,
		AuthorType: "agent", AuthorID: task.AgentID, Content: strings.TrimSpace(result.RootCause),
		Type: "conclusion", TaskID: task.ID,
	})
	return err
}
