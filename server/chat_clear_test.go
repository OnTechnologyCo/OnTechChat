package server

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestChatClearIsPrivateAndNewMessagesRemain(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	_, b, _ := request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": bob.ID})
	room := decodeMap(t, b)["id"].(string)
	status, b, _ := request(t, s, bt, "POST", "/api/rooms/"+room+"/messages", map[string]string{"text": "old private history", "client_id": "clear-chat-message-001"})
	expectStatus(t, status, 201, b)
	mid := int64(decodeMap(t, b)["id"].(float64))
	_ = a.store.update(func(st *State) error {
		st.Outbox["clear-self"] = Outbox{ID: "clear-self", RoomID: room, UserID: admin.ID, MessageID: mid}
		st.Outbox["keep-peer"] = Outbox{ID: "keep-peer", RoomID: room, UserID: bob.ID, MessageID: mid}
		return nil
	})
	status, b, _ = request(t, s, et, "DELETE", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, at, "DELETE", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, b)
	cutoff := int64(decodeMap(t, b)["cleared_through"].(float64))
	if cutoff < mid {
		t.Fatal("invalid cutoff")
	}
	for _, query := range []string{"", "?before=999999", "?after=1"} {
		status, b, _ = request(t, s, at, "GET", "/api/rooms/"+room+"/messages"+query, nil)
		expectStatus(t, status, 200, b)
		out := decodeMap(t, b)
		if len(out["messages"].([]any)) != 0 || out["cleared_through"] != float64(cutoff) {
			t.Fatal("cleared history returned")
		}
	}
	status, b, _ = request(t, s, bt, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, b)
	if len(decodeMap(t, b)["messages"].([]any)) != 1 {
		t.Fatal("peer history removed")
	}
	status, b, _ = request(t, s, at, "GET", "/api/rooms", nil)
	expectStatus(t, status, 200, b)
	var rooms []map[string]any
	if err := json.Unmarshal(b, &rooms); err != nil {
		t.Fatal(err)
	}
	for _, r := range rooms {
		if r["id"] == room && (r["unread"] != float64(0) || r["last_message_id"] != float64(0)) {
			t.Fatal("preview or badge leaked old messages")
		}
	}
	_ = a.store.view(func(st *State) error {
		if _, ok := st.Outbox["clear-self"]; ok {
			t.Fatal("old own notification retained")
		}
		if _, ok := st.Outbox["keep-peer"]; !ok {
			t.Fatal("peer notification removed")
		}
		if !notificationDismissed(st, st.Messages[len(st.Messages)-1], admin.ID, now()) {
			t.Fatal("notification not dismissed")
		}
		encoded, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		var restored State
		if json.Unmarshal(encoded, &restored) != nil || roomClearedThrough(restored.Rooms[room], admin.ID) != cutoff {
			t.Fatal("cutoff not persistent")
		}
		return nil
	})
	status, b, _ = request(t, s, bt, "POST", "/api/rooms/"+room+"/messages", map[string]string{"text": "new history", "client_id": "clear-chat-message-002"})
	expectStatus(t, status, 201, b)
	secondSession := loginTest(t, s, admin.Username)
	status, b, _ = request(t, s, secondSession, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, b)
	msgs := decodeMap(t, b)["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["text"] != "new history" {
		t.Fatal("new message missing or old history resurrected")
	}
}
func TestChatClearGroupAttachmentsAndUnsupportedRooms(t *testing.T) {
	a, s, _, bob, _ := testApp(t)
	at, bt := loginTest(t, s, "admin"), loginTest(t, s, "bob")
	room := createTestRoom(t, s, at)
	id := "cannot-clear-ai"
	if err := a.store.update(func(st *State) error {
		st.Rooms[id] = Room{ID: id, Kind: "ai", OwnerID: bob.ID, Members: map[string]int64{bob.ID: 0}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	code, body, _ := request(t, s, bt, "DELETE", "/api/rooms/"+id+"/messages", nil)
	expectStatus(t, code, 403, body)
	status, b := uploadTest(t, s, at, room, "clear-file-001", "retained.txt", []byte("peer keeps file"))
	expectStatus(t, status, 201, b)
	file := decodeMap(t, b)["file"].(map[string]any)["id"].(string)
	status, b, _ = request(t, s, bt, "DELETE", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, bt, "GET", "/api/files/"+file, nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, at, "GET", "/api/files/"+file, nil)
	expectStatus(t, status, 200, b)
	if string(b) != "peer keeps file" {
		t.Fatal("shared attachment damaged")
	}
	status, b, _ = request(t, s, bt, "DELETE", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, bt, "DELETE", fmt.Sprintf("/api/rooms/%s/messages", "missing"), nil)
	expectStatus(t, status, 403, b)
}

func TestAuthInboxClearRemovesPrivateMailAndAllowsNewMail(t *testing.T) {
	a, s, admin, _, _ := testApp(t)
	token := loginTest(t, s, admin.Username)
	room := authRoomID(admin.ID)
	_ = a.store.update(func(st *State) error {
		m, e := addMessage(st, Message{RoomID: room, Kind: "auth", Text: `{"subject":"old","body":"old body"}`, ClientID: "auth-clear-old"}, a.cfg.MaxMessages, false)
		if e != nil {
			return e
		}
		st.Outbox["auth-clear-outbox"] = Outbox{ID: "auth-clear-outbox", RoomID: room, UserID: admin.ID, MessageID: m.ID}
		if st.MailDelivered == nil {
			st.MailDelivered = map[string]int64{}
		}
		st.MailDelivered["mail-ledger-test"] = now() + 3600
		return nil
	})
	status, body, _ := request(t, s, token, "DELETE", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, body)
	_ = a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.RoomID == room {
				t.Fatal("auth message not physically removed")
			}
		}
		if _, ok := st.Outbox["auth-clear-outbox"]; ok {
			t.Fatal("auth outbox retained")
		}
		if st.MailDelivered["mail-ledger-test"] == 0 {
			t.Fatal("mail dedup ledger removed")
		}
		return nil
	})
	_ = a.store.update(func(st *State) error {
		_, e := addMessage(st, Message{RoomID: room, Kind: "auth", Text: `{"subject":"new","body":"new body"}`, ClientID: "auth-clear-new"}, a.cfg.MaxMessages, false)
		return e
	})
	status, body, _ = request(t, s, token, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, body)
	msgs := decodeMap(t, body)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatal("new auth mail not visible after clear")
	}
}
