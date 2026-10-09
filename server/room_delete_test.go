package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func assertRoomListed(t *testing.T, s *httptest.Server, token, id string, want bool) {
	t.Helper()
	code, body, _ := request(t, s, token, "GET", "/api/rooms", nil)
	expectStatus(t, code, 200, body)
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r["id"] == id {
			found = true
		}
	}
	if found != want {
		t.Fatalf("room %s listed=%v want=%v", id, found, want)
	}
}

func TestRoomDeleteSharedScopeAndPersistence(t *testing.T) {
	for _, kind := range []string{"direct", "chat"} {
		t.Run(kind, func(t *testing.T) {
			a, s, admin, bob, _ := testApp(t)
			at, bt, et := loginTest(t, s, "admin"), loginTest(t, s, "bob"), loginTest(t, s, "eve")
			var code int
			var b []byte
			if kind == "direct" {
				code, b, _ = request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": bob.ID})
			} else {
				code, b, _ = request(t, s, at, "POST", "/api/rooms", map[string]any{"name": "delete test", "members": []string{"bob"}})
			}
			expectStatus(t, code, 201, b)
			id := decodeMap(t, b)["id"].(string)
			code, b, _ = request(t, s, bt, "POST", "/api/rooms/"+id+"/messages", map[string]string{"text": "keep for peer", "client_id": "delete-list-old-001"})
			expectStatus(t, code, 201, b)
			endpoint := "/api/rooms/" + id
			code, b, _ = request(t, s, "", "DELETE", endpoint, nil)
			expectStatus(t, code, 401, b)
			code, b, _ = request(t, s, et, "DELETE", endpoint, nil)
			expectStatus(t, code, 403, b)
			code, b, _ = request(t, s, at, "DELETE", endpoint, nil)
			expectStatus(t, code, 200, b)
			assertRoomListed(t, s, at, id, false)
			assertRoomListed(t, s, bt, id, true)
			assertRoomListed(t, s, loginTest(t, s, "admin"), id, false)
			if err := a.store.view(func(st *State) error {
				raw, e := json.Marshal(st)
				if e != nil {
					return e
				}
				var copy State
				if e = json.Unmarshal(raw, &copy); e != nil {
					return e
				}
				if !copy.Rooms[id].HiddenFor[admin.ID] {
					t.Error("hidden flag not persistent")
				}
				if len(copy.Rooms[id].Members) != 2 {
					t.Error("deletion left shared membership")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			code, b, _ = request(t, s, at, "GET", endpoint+"/messages", nil)
			expectStatus(t, code, 200, b)
			if len(decodeMap(t, b)["messages"].([]any)) != 0 {
				t.Fatal("old history returned")
			}
			code, b, _ = request(t, s, bt, "GET", endpoint+"/messages", nil)
			expectStatus(t, code, 200, b)
			if len(decodeMap(t, b)["messages"].([]any)) != 1 {
				t.Fatal("peer history deleted")
			}
			code, b, _ = request(t, s, at, "DELETE", endpoint, nil)
			expectStatus(t, code, 200, b)
			code, b, _ = request(t, s, bt, "POST", endpoint+"/messages", map[string]string{"text": "new message", "client_id": "delete-list-new-002"})
			expectStatus(t, code, 201, b)
			assertRoomListed(t, s, at, id, true)
			code, b, _ = request(t, s, at, "GET", endpoint+"/messages", nil)
			rows := decodeMap(t, b)["messages"].([]any)
			if len(rows) != 1 || rows[0].(map[string]any)["text"] != "new message" {
				t.Fatal("old history restored or new message missing")
			}
			code, b, _ = request(t, s, at, "DELETE", endpoint, nil)
			expectStatus(t, code, 200, b)
			if kind == "direct" {
				code, b, _ = request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": bob.ID})
				expectStatus(t, code, 200, b)
				assertRoomListed(t, s, at, id, true)
			}
		})
	}
}

func TestRoomDeleteAuthDedupAndNewMail(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	at, bt := loginTest(t, s, "admin"), loginTest(t, s, "bob")
	cfg := a.cfg.Mail
	cfg.RecipientUsernames = []string{"admin", "bob"}
	p := &ParsedAuthMail{Subject: "synthetic title", Body: "synthetic body"}
	if _, _, e := a.deliverMail(cfg, "delete-test", 7, 1, p, time.Now(), false); e != nil {
		t.Fatal(e)
	}
	id := authRoomID(admin.ID)
	code, b, _ := request(t, s, bt, "DELETE", "/api/rooms/"+id, nil)
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, at, "DELETE", "/api/rooms/"+id, nil)
	expectStatus(t, code, 200, b)
	assertRoomListed(t, s, at, id, false)
	assertRoomListed(t, s, bt, authRoomID(bob.ID), true)
	if added, duplicates, e := a.deliverMail(cfg, "delete-test", 7, 1, p, time.Now(), false); e != nil || added != 0 || duplicates != 2 {
		t.Fatal("old mail redelivered", added, duplicates, e)
	}
	assertRoomListed(t, s, at, id, false)
	if _, _, e := a.deliverMail(cfg, "delete-test", 7, 2, p, time.Now(), false); e != nil {
		t.Fatal(e)
	}
	assertRoomListed(t, s, at, id, true)
}

func TestRoomDeleteAIStopsLateResults(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	a.cfg.AI = aiTestConfig()
	token, other := loginTest(t, s, "bob"), loginTest(t, s, "eve")
	code, b, _ := request(t, s, token, "POST", "/api/rooms/ai", map[string]any{"model_id": "test-model"})
	expectStatus(t, code, 201, b)
	id := decodeMap(t, b)["id"].(string)
	var oldAI AIRequest
	var oldAgent AgentJob
	if e := a.store.update(func(st *State) error {
		owner := st.Rooms[id].OwnerID
		m, e := addMessage(st, Message{RoomID: id, UserID: owner, Kind: "text", Text: "old context"}, a.cfg.MaxMessages, false)
		if e != nil {
			return e
		}
		oldAI = AIRequest{ID: "delete-ai", OwnerID: owner, RoomID: id, Status: aiRunning, UserMessageID: m.ID}
		st.AIRequests[oldAI.ID] = oldAI
		oldAgent = AgentJob{ID: "delete-agent", OwnerID: owner, RoomID: id, Status: agentPlanning, SourceMessage: m.ID}
		st.AgentJobs[oldAgent.ID] = oldAgent
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	code, b, _ = request(t, s, other, "DELETE", "/api/rooms/"+id, nil)
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, token, "DELETE", "/api/rooms/"+id, nil)
	expectStatus(t, code, 200, b)
	if e := (&aiJobManager{app: a}).finishSuccess(oldAI, AIExecutionResult{Content: "late"}); e != nil {
		t.Fatal(e)
	}
	if e := (&agentJobManager{app: a}).finishSuccess(oldAgent, "late"); e != nil {
		t.Fatal(e)
	}
	assertRoomListed(t, s, token, id, false)
	if e := a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.RoomID == id {
				t.Error("deleted AI history reappeared")
			}
		}
		if _, ok := st.AIRequests[oldAI.ID]; ok {
			t.Error("AI request survived")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	code, b, _ = request(t, s, token, "DELETE", "/api/rooms/"+id, nil)
	expectStatus(t, code, 200, b)
}

func TestRoomDeleteEmptyInboxRemainsHidden(t *testing.T) {
	_, s, _, bob, _ := testApp(t)
	token := loginTest(t, s, "bob")
	id := authRoomID(bob.ID)
	code, b, _ := request(t, s, token, "DELETE", "/api/rooms/"+id, nil)
	expectStatus(t, code, 200, b)
	assertRoomListed(t, s, token, id, false)
	code, b, _ = request(t, s, token, "DELETE", "/api/rooms/missing", nil)
	expectStatus(t, code, 403, b)
}
