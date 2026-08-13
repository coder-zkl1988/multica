package service

import (
	"context"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestResolveTaskWorkspaceIDProjectDesignSystemContext(t *testing.T) {
	tests := []struct {
		name    string
		context string
	}{
		{
			name:    "existing quick create context",
			context: `{"type":"quick_create","workspace_id":"00000000-0000-0000-0000-000000000001"}`,
		},
		{
			name:    "project design system context",
			context: `{"type":"project_design_system_task","workspace_id":"00000000-0000-0000-0000-000000000001"}`,
		},
		{
			name:    "Design Document context",
			context: `{"type":"design_document_task","workspace_id":"00000000-0000-0000-0000-000000000001"}`,
		},
	}

	service := &TaskService{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := db.AgentTaskQueue{Context: []byte(tt.context)}
			if got := service.ResolveTaskWorkspaceID(context.Background(), task); got != "00000000-0000-0000-0000-000000000001" {
				t.Fatalf("ResolveTaskWorkspaceID() = %q, want workspace from task context", got)
			}
		})
	}
}
