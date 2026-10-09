package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMessengerDirectoryAndProfile(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	st, b, _ := request(t, s, "", "GET", "/api/friends", nil)
	expectStatus(t, st, 401, b)
	st, b, _ = request(t, s, bt, "GET", "/api/friends", nil)
	expectStatus(t, st, 200, b)
	var rows []map[string]any
	_ = json.Unmarshal(b, &rows)
	if len(rows) != 3 {
		t.Fatal(string(b))
	}
	found := false
	for _, u := range rows {
		if u["id"] == admin.ID {
			found = u["role"] == admin.Role
		}
		if _, ok := u["password_hash"]; ok {
			t.Fatal("hash leaked")
		}
		if _, ok := u["avatar_image"]; ok {
			t.Fatal("photo leaked in directory")
		}
	}
	if !found {
		t.Fatal("administrator missing")
	}
	st, b, _ = request(t, s, bt, "PATCH", "/api/profile", map[string]any{"display_name": "도윤", "status_message": "오늘도 좋은 하루", "avatar_color": "mint"})
	expectStatus(t, st, 200, b)
	st, b, _ = request(t, s, bt, "GET", "/api/me", nil)
	if decodeMap(t, b)["display_name"] != "도윤" {
		t.Fatal(string(b))
	}
	st, b, _ = request(t, s, at, "PATCH", "/api/profile", map[string]any{"id": bob.ID, "display_name": "forged", "avatar_color": "rose"})
	expectStatus(t, st, 400, b)
	st, b, _ = request(t, s, bt, "PATCH", "/api/profile", map[string]any{"display_name": "bad\nname", "avatar_color": "mint"})
	expectStatus(t, st, 400, b)
	st, b, _ = request(t, s, bt, "PATCH", "/api/profile", map[string]any{"display_name": "valid", "status_message": strings.Repeat("가", 121), "avatar_color": "blue"})
	expectStatus(t, st, 400, b)
	st, b, _ = request(t, s, bt, "PATCH", "/api/friends/"+admin.ID, map[string]bool{"favorite": true})
	expectStatus(t, st, 200, b)
	st, b, _ = request(t, s, at, "PATCH", "/api/users/"+eve.ID, map[string]bool{"disabled": true})
	expectStatus(t, st, 200, b)
	st, b, _ = request(t, s, bt, "GET", "/api/friends", nil)
	_ = json.Unmarshal(b, &rows)
	if len(rows) != 2 {
		t.Fatal("disabled users included", string(b))
	}
	for _, u := range rows {
		if u["id"] == admin.ID && u["favorite"] != true {
			t.Fatal("favorite not set")
		}
	}
	_ = a.store.view(func(st *State) error {
		if len(st.Users[admin.ID].Favorites) != 0 {
			t.Fatal("favorite changed for another user")
		}
		if st.Users[bob.ID].Hash != bob.Hash {
			t.Fatal("password changed")
		}
		return nil
	})
}
func TestMessengerAvatarSafety(t *testing.T) {
	_, s, admin, bob, _ := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	im.Set(0, 0, color.RGBA{100, 80, 30, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, im)
	st, b, _ := request(t, s, bt, "PATCH", "/api/profile", map[string]any{"display_name": "Bob", "avatar_color": "blue", "avatar_image": base64.StdEncoding.EncodeToString(buf.Bytes())})
	expectStatus(t, st, 200, b)
	if decodeMap(t, b)["avatar_revision"] == "" {
		t.Fatal("no revision")
	}
	st, b, h := request(t, s, at, "GET", "/api/users/"+bob.ID+"/avatar", nil)
	expectStatus(t, st, 200, b)
	if h.Get("Content-Type") != "image/jpeg" {
		t.Fatal("image not normalized")
	}
	if _, _, e := image.Decode(bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	st, b, _ = request(t, s, "", "GET", "/api/users/"+bob.ID+"/avatar", nil)
	expectStatus(t, st, 401, b)
	for _, raw := range []string{"not base64", base64.StdEncoding.EncodeToString([]byte("<svg onload='x()'/>")), strings.Repeat("A", 61<<10)} {
		st, b, _ = request(t, s, bt, "PATCH", "/api/profile", map[string]any{"avatar_color": "blue", "avatar_image": raw})
		expectStatus(t, st, 400, b)
	}
	st, b, _ = request(t, s, bt, "PATCH", "/api/profile", map[string]any{"avatar_color": "blue", "avatar_image": ""})
	expectStatus(t, st, 200, b)
	st, b, _ = request(t, s, at, "GET", "/api/users/"+bob.ID+"/avatar", nil)
	expectStatus(t, st, 404, b)
}
func TestMessengerDirectConcurrencyIsolation(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	et := loginTest(t, s, eve.Username)
	ids := make(chan string, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token, target := at, bob.ID
			if i%2 == 1 {
				token, target = bt, admin.ID
			}
			st, b, _ := request(t, s, token, "POST", "/api/rooms/direct", map[string]string{"user_id": target})
			if st != 200 && st != 201 {
				t.Error(st, string(b))
				return
			}
			ids <- decodeMap(t, b)["id"].(string)
		}(i)
	}
	wg.Wait()
	close(ids)
	id := ""
	for v := range ids {
		if id != "" && v != id {
			t.Fatal("duplicate direct room")
		}
		id = v
	}
	count := 0
	_ = a.store.view(func(st *State) error {
		for _, r := range st.Rooms {
			if r.Kind == "direct" {
				count++
			}
		}
		return nil
	})
	if count != 1 {
		t.Fatal("room count", count)
	}
	st, b, _ := request(t, s, et, "GET", "/api/rooms/"+id+"/messages", nil)
	expectStatus(t, st, 403, b)
	st, b, _ = request(t, s, at, "POST", "/api/rooms/"+id+"/members", map[string]string{"username": eve.Username})
	expectStatus(t, st, 403, b)
	st, b, _ = request(t, s, bt, "DELETE", "/api/rooms/"+id+"/members/"+bob.ID, nil)
	expectStatus(t, st, 403, b)
	st, b, _ = request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": admin.ID})
	expectStatus(t, st, 400, b)
	st, b, _ = request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": "not-existing"})
	expectStatus(t, st, 400, b)
}
func TestMessengerMessagesReceiptsAndPreference(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	et := loginTest(t, s, eve.Username)
	_, b, _ := request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": bob.ID})
	id := decodeMap(t, b)["id"].(string)
	st, b, _ := request(t, s, bt, "POST", "/api/rooms/"+id+"/messages", map[string]string{"text": "안녕하세요", "client_id": "direct-message-test-001"})
	expectStatus(t, st, 201, b)
	mid := int64(decodeMap(t, b)["id"].(float64))
	st, b, _ = request(t, s, at, "GET", "/api/rooms", nil)
	expectStatus(t, st, 200, b)
	var rooms []map[string]any
	_ = json.Unmarshal(b, &rooms)
	var summary map[string]any
	for _, r := range rooms {
		if r["id"] == id {
			summary = r
		}
	}
	if summary["last_message"] != "안녕하세요" || summary["unread"] != float64(1) || summary["name"] != bob.Username {
		t.Fatal(string(b))
	}
	st, b, _ = request(t, s, bt, "GET", "/api/rooms/"+id+"/messages", nil)
	m := decodeMap(t, b)["messages"].([]any)[0].(map[string]any)
	if m["unread_count"] != float64(1) {
		t.Fatal("read receipt absent")
	}
	ch, unsub, _ := a.hub.subscribe(bob.ID)
	defer unsub()
	st, b, _ = request(t, s, at, "POST", "/api/rooms/"+id+"/read", map[string]int64{"message_id": mid})
	expectStatus(t, st, 200, b)
	select {
	case r := <-ch:
		if r != id {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("missing read event")
	}
	st, b, _ = request(t, s, at, "POST", "/api/rooms/"+id+"/read", map[string]int64{"message_id": mid})
	expectStatus(t, st, 200, b)
	select {
	case <-ch:
		t.Fatal("duplicate read event loop")
	default:
	}
	st, b, _ = request(t, s, bt, "GET", "/api/rooms/"+id+"/messages", nil)
	m = decodeMap(t, b)["messages"].([]any)[0].(map[string]any)
	if m["unread_count"] != float64(0) {
		t.Fatal("read receipt not updated")
	}
	st, b, _ = request(t, s, et, "PATCH", "/api/rooms/"+id+"/preference", map[string]bool{"pinned": true})
	expectStatus(t, st, 403, b)
	st, b, _ = request(t, s, bt, "PATCH", "/api/rooms/"+id+"/preference", map[string]bool{"pinned": true})
	expectStatus(t, st, 200, b)
	st, b, _ = request(t, s, bt, "GET", "/api/rooms", nil)
	_ = json.Unmarshal(b, &rooms)
	if rooms[0]["id"] != id || rooms[0]["pinned"] != true {
		t.Fatal("pin not applied")
	}
	st, b, _ = request(t, s, at, "DELETE", "/api/messages/"+jsonNumber(mid), nil)
	expectStatus(t, st, 403, b)
	st, b, _ = request(t, s, bt, "DELETE", "/api/messages/"+jsonNumber(mid), nil)
	expectStatus(t, st, 200, b)
}
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }
func TestMessengerDirectFilesAndDisable(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	_, b, _ := request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": bob.ID})
	id := decodeMap(t, b)["id"].(string)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "대화파일.txt")
	_, _ = part.Write([]byte("direct file test"))
	_ = writer.Close()
	req, _ := http.NewRequest("POST", s.URL+"/api/rooms/"+id+"/files", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+bt)
	req.Header.Set("X-OnTech-Request", "1")
	req.Header.Set("X-Client-ID", "direct-file-test-001")
	resp, e := s.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatal("upload status", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	fid := payload["file"].(map[string]any)["id"].(string)
	st, b, _ := request(t, s, at, "GET", "/api/files/"+fid, nil)
	expectStatus(t, st, 200, b)
	if string(b) != "direct file test" {
		t.Fatal("file mismatch")
	}
	st, b, _ = request(t, s, at, "PATCH", "/api/users/"+bob.ID, map[string]bool{"disabled": true})
	expectStatus(t, st, 200, b)
	st, b, _ = request(t, s, at, "POST", "/api/rooms/"+id+"/messages", map[string]string{"text": "blocked", "client_id": "disabled-peer-test"})
	expectStatus(t, st, 403, b)
	st, b, _ = request(t, s, bt, "GET", "/api/friends", nil)
	expectStatus(t, st, 401, b)
	_ = a.store.view(func(st *State) error {
		if st.Rooms[id].Kind != "direct" {
			t.Fatal("wrong room kind")
		}
		return nil
	})
}
func TestMessengerAuthPreviewNeverLeaksCode(t *testing.T) {
	a, s, admin, _, _ := testApp(t)
	at := loginTest(t, s, admin.Username)
	_ = a.store.update(func(st *State) error {
		_, e := addMessage(st, Message{RoomID: authRoomID(admin.ID), Kind: "auth", Text: `{"service":"Synthetic","code":"037592"}`, Expires: now() + 600}, 100, false)
		return e
	})
	st, b, _ := request(t, s, at, "GET", "/api/rooms", nil)
	expectStatus(t, st, 200, b)
	if bytes.Contains(b, []byte("037592")) || bytes.Contains(b, []byte("Synthetic")) {
		t.Fatal("authentication data leaked to list")
	}
}
func TestMessengerLegacyStoreAndPersistence(t *testing.T) {
	dir := t.TempDir()
	store, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	u := User{ID: "legacy-user", Username: "legacy", Role: "user"}
	if e = store.update(func(st *State) error { addUser(st, u); return nil }); e != nil {
		t.Fatal(e)
	}
	_ = store.Close()
	store, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.update(func(st *State) error {
		v := st.Users[u.ID]
		if v.DisplayName != "" || v.Favorites != nil {
			t.Fatal("legacy defaults")
		}
		v.DisplayName = "기존 사용자"
		v.Favorites = map[string]bool{"other": true}
		v.PinnedRooms = map[string]bool{"room": true}
		st.Users[u.ID] = v
		st.Rooms["direct-room"] = Room{ID: "direct-room", Kind: "direct", DirectKey: directKey("a", "b"), Members: map[string]int64{"a": 3, "b": 0}}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	_ = store.Close()
	store, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	_ = store.view(func(st *State) error {
		v := st.Users[u.ID]
		if v.DisplayName != "기존 사용자" || !v.Favorites["other"] || !v.PinnedRooms["room"] || st.Rooms["direct-room"].DirectKey == "" {
			t.Fatal("new fields not persisted")
		}
		return nil
	})
}
