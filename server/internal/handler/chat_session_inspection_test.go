package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestListChatSessionTasksAndTaskMessages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "ChatSessionInspectionAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := insertPendingChatTask(t, agentID, sessionID, "completed")

	if _, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
		TaskID:  util.MustParseUUID(taskID),
		Seq:     1,
		Type:    "tool_use",
		Tool:    pgtype.Text{String: "shell", Valid: true},
		Content: pgtype.Text{String: "running command", Valid: true},
		Input:   []byte(`{"cmd":"date"}`),
	}); err != nil {
		t.Fatalf("seed task message: %v", err)
	}

	tasksW := httptest.NewRecorder()
	tasksReq := chatPendingCtxAs(t,
		withURLParam(newRequestAs(testUserID, "GET", "/api/chat/sessions/"+sessionID+"/tasks", nil), "sessionId", sessionID),
		testUserID,
	)
	testHandler.ListChatSessionTasks(tasksW, tasksReq)
	if tasksW.Code != http.StatusOK {
		t.Fatalf("ListChatSessionTasks: expected 200, got %d: %s", tasksW.Code, tasksW.Body.String())
	}
	var tasks []AgentTaskResponse
	if err := json.Unmarshal(tasksW.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != taskID || tasks[0].ChatSessionID != sessionID {
		t.Fatalf("unexpected tasks response: %+v", tasks)
	}

	messagesW := httptest.NewRecorder()
	messagesReq := chatPendingCtxAs(t,
		withURLParam(newRequestAs(testUserID, "GET", "/api/chat/sessions/"+sessionID+"/task-messages?task="+taskID, nil), "sessionId", sessionID),
		testUserID,
	)
	testHandler.ListChatSessionTaskMessages(messagesW, messagesReq)
	if messagesW.Code != http.StatusOK {
		t.Fatalf("ListChatSessionTaskMessages: expected 200, got %d: %s", messagesW.Code, messagesW.Body.String())
	}
	var messages []protocol.TaskMessagePayload
	if err := json.Unmarshal(messagesW.Body.Bytes(), &messages); err != nil {
		t.Fatalf("decode task messages: %v", err)
	}
	if len(messages) != 1 || messages[0].TaskID != taskID || messages[0].Type != "tool_use" || messages[0].Tool != "shell" {
		t.Fatalf("unexpected task messages response: %+v", messages)
	}
	if got := messages[0].Input["cmd"]; got != "date" {
		t.Fatalf("task message input cmd = %#v, want date", got)
	}
}

func TestChatSessionInspectionPrivateAgentForbidsAfterAccessRevoked(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, _, memberID := privateAgentTestFixture(t)
	sessionID := insertChatSessionAs(t, agentID, memberID)
	insertPendingChatTask(t, agentID, sessionID, "completed")

	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(memberID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load plain member row: %v", err)
	}

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"tasks":         testHandler.ListChatSessionTasks,
		"task-messages": testHandler.ListChatSessionTaskMessages,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequestAs(memberID, "GET", "/api/chat/sessions/"+sessionID+"/"+name, nil)
			req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
			req = withURLParam(req, "sessionId", sessionID)
			call(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s on stale session: expected 403, got %d: %s", name, w.Code, w.Body.String())
			}
		})
	}
}
