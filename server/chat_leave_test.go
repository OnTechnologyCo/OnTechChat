package server

import (
	"encoding/json"
	"testing"
)

func TestRoomLeaveAndOwnerTransfer(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	_, body, _ := request(t, s, at, "POST", "/api/rooms", map[string]any{
		"name":    "leave-test",
		"members": []string{bob.Username, eve.Username},
	})
	room := decodeMap(t, body)
	id := room["id"].(string)
	code, body, _ := request(t, s, at, "POST", "/api/rooms/"+id+"/leave", map[string]any{})
	expectStatus(t, code, 200, body)
	result := decodeMap(t, body)
	if result["new_owner"] == "" {
		t.Fatal("owner transfer missing")
	}
	_ = a.store.view(func(st *State) error {
		r := st.Rooms[id]
		if _, ok := r.Members[admin.ID]; ok {
			t.Fatal("leaving owner still member")
		}
		if r.OwnerID != bob.ID {
			t.Fatalf("unexpected new owner: %s", r.OwnerID)
		}
		return nil
	})
	code, body, _ = request(t, s, at, "GET", "/api/rooms", nil)
	expectStatus(t, code, 200, body)
	var rooms []map[string]any
	if e := json.Unmarshal(body, &rooms); e != nil {
		t.Fatal(e)
	}
	for _, r := range rooms {
		if r["id"] == id {
			t.Fatal("left room still visible")
		}
	}
	code, body, _ = request(t, s, bt, "POST", "/api/rooms/"+id+"/leave", map[string]any{})
	expectStatus(t, code, 200, body)
	_ = a.store.view(func(st *State) error {
		if st.Rooms[id].OwnerID != eve.ID {
			t.Fatal("second owner transfer failed")
		}
		return nil
	})
}
