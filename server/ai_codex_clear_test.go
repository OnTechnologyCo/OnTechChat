package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAICodexRejectsIncomplete(t *testing.T) {
	for _, raw := range []string{"", `{"type":"item.completed","item":{"type":"agent_message","text":"partial"}}`, `{"type":"turn.completed"}`, `{"type":"turn.failed"}`} {
		if _, err := parseCodexResponse([]byte(raw)); err == nil {
			t.Fatal("accepted incomplete output")
		}
	}
}
func TestAICodexLivePlanner(t *testing.T) {
	if os.Getenv("ONTECH_TEST_CODEX_LIVE") != "1" {
		t.Skip("opt-in actual CLI test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	turn, err := newDefaultAgentPlanner().Next(ctx, AIProviderConfig{Type: "codex_cli", Executable: "codex"}, AIModelConfig{Model: "gpt-6-astra"}, "low", AgentPlannerInput{Request: "Reply with exactly OK. No tools.", Policy: "Use no tools.", Turn: 1})
	if err != nil || turn.Final != "OK" || len(turn.ToolCalls) != 0 {
		t.Fatalf("live final=%q tools=%d err=%v", turn.Final, len(turn.ToolCalls), err)
	}
	t.Log("Codex GPT-6-Astra structured planner actual response OK")
}
func TestAIClearConversationOwnershipAndLateCompletion(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	a.cfg.AI = aiTestConfig()
	token := loginTest(t, s, "bob")
	other := loginTest(t, s, "eve")
	status, body, _ := request(t, s, token, "POST", "/api/rooms/ai", map[string]any{"model_id": "test-model"})
	expectStatus(t, status, 201, body)
	room := decodeMap(t, body)["id"].(string)
	var oldAI AIRequest
	var oldAgent AgentJob
	_ = a.store.update(func(st *State) error {
		owner := st.Rooms[room].OwnerID
		m, _ := addMessage(st, Message{RoomID: room, UserID: owner, Kind: "text", Text: "secret"}, a.cfg.MaxMessages, false)
		oldAI = AIRequest{ID: "clear-ai", OwnerID: owner, RoomID: room, Status: aiRunning, UserMessageID: m.ID}
		st.AIRequests[oldAI.ID] = oldAI
		oldAgent = AgentJob{ID: "clear-agent", OwnerID: owner, RoomID: room, Status: agentPlanning, SourceMessage: m.ID, RequestText: "secret"}
		st.AgentJobs[oldAgent.ID] = oldAgent
		return nil
	})
	endpoint := "/api/rooms/" + room + "/ai/messages"
	status, body, _ = request(t, s, other, http.MethodDelete, endpoint, nil)
	expectStatus(t, status, 403, body)
	status, body, _ = request(t, s, token, http.MethodDelete, endpoint, nil)
	expectStatus(t, status, 200, body)
	_ = (&aiJobManager{app: a}).finishSuccess(oldAI, AIExecutionResult{Content: "late"})
	_ = (&agentJobManager{app: a}).finishSuccess(oldAgent, "late")
	_ = a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.RoomID == room {
				t.Error("deleted or late message remained")
			}
		}
		if _, ok := st.AIRequests[oldAI.ID]; ok {
			t.Error("AI job remained")
		}
		if _, ok := st.AgentJobs[oldAgent.ID]; ok {
			t.Error("agent job remained")
		}
		return nil
	})
	status, body, _ = request(t, s, token, http.MethodDelete, endpoint, nil)
	expectStatus(t, status, 200, body)
}
func TestAICodexPlannerSchema(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"final": "OK", "tool_calls": []any{}})
	turn, err := decodeAgentTurn(raw)
	if err != nil || turn.Final != "OK" {
		t.Fatal(err)
	}
	if _, err = decodeAgentTurn([]byte("{\"final\":\"OK\",\"tool_calls\":[],\"command\":\"x\"}")); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestAINativeCodexNPMShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codex.cmd")
	want := filepath.Join(dir, "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "codex", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := nativeCodexExecutable(shim)
	if err != nil || got != want {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err = nativeCodexExecutable(filepath.Join(t.TempDir(), "codex.cmd")); err == nil {
		t.Fatal("missing native executable accepted")
	}
}
func TestAIDeleteAnswerOwnerOnly(t *testing.T) {
	a, s, _, bob, _ := testApp(t)
	a.cfg.AI = aiTestConfig()
	token := loginTest(t, s, "bob")
	other := loginTest(t, s, "eve")
	status, body, _ := request(t, s, token, "POST", "/api/rooms/ai", map[string]any{"model_id": "test-model"})
	expectStatus(t, status, 201, body)
	room := decodeMap(t, body)["id"].(string)
	var answer Message
	_ = a.store.update(func(st *State) error {
		var err error
		answer, err = addMessage(st, Message{RoomID: room, Kind: "text", Text: "answer", AgentJobID: "delete-answer"}, a.cfg.MaxMessages, false)
		st.AgentJobs["delete-answer"] = AgentJob{ID: "delete-answer", OwnerID: bob.ID, RoomID: room, Status: agentCompleted, AnswerMessage: answer.ID, RequestText: "private"}
		return err
	})
	endpoint := fmt.Sprintf("/api/messages/%d", answer.ID)
	status, body, _ = request(t, s, other, "DELETE", endpoint, nil)
	expectStatus(t, status, 403, body)
	status, body, _ = request(t, s, token, "DELETE", endpoint, nil)
	expectStatus(t, status, 200, body)
	_ = a.store.view(func(st *State) error {
		m, _ := findMessageByID(st, answer.ID)
		if m.Text != "" || m.Kind != "deleted" {
			t.Error("answer not deleted")
		}
		if _, ok := st.AgentJobs["delete-answer"]; ok {
			t.Error("job content remained")
		}
		return nil
	})
}
