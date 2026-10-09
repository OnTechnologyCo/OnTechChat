package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotificationPreferencesOwnershipAndRefresh(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	bt := loginTest(t, s, bob.Username)
	at := loginTest(t, s, admin.Username)
	et := loginTest(t, s, eve.Username)
	dev := "notify-device-141"
	status, b, _ := request(t, s, bt, "POST", "/api/devices", map[string]string{"id": dev, "name": "synthetic", "token": "test-fcm-device-token-not-real"})
	expectStatus(t, status, 200, b)
	path := "/api/devices/" + dev + "/notifications"
	status, b, _ = request(t, s, bt, "GET", path, nil)
	expectStatus(t, status, 200, b)
	if decodeMap(t, b)["preview_enabled"] != false {
		t.Fatal("preview must opt in")
	}
	for name, tok := range map[string]string{"unauthenticated": "", "other": et, "administrator": at} {
		t.Run(name, func(t *testing.T) {
			status, b, _ := request(t, s, tok, "PATCH", path, map[string]bool{"preview_enabled": true})
			want := 403
			if tok == "" {
				want = 401
			}
			expectStatus(t, status, want, b)
		})
	}
	for name, data := range map[string]any{"missing": map[string]bool{}, "wrongtype": map[string]string{"preview_enabled": "yes"}, "unknown": map[string]bool{"preview_enabled": true, "auth_preview_enabled": true}} {
		t.Run(name, func(t *testing.T) { st, b, _ := request(t, s, bt, "PATCH", path, data); expectStatus(t, st, 400, b) })
	}
	status, b, _ = request(t, s, bt, "PATCH", path, map[string]bool{"preview_enabled": true})
	expectStatus(t, status, 200, b)
	if decodeMap(t, b)["auth_preview_enabled"] != false {
		t.Fatal("auth preview was allowed")
	}
	// 토큰 재등록 시 사용자가 정한 미리보기 여부를 보존합니다.
	status, b, _ = request(t, s, bt, "POST", "/api/devices", map[string]string{"id": dev, "name": "updated", "token": "test-fcm-rotated-token-not-real"})
	expectStatus(t, status, 200, b)
	_ = a.store.view(func(st *State) error {
		if !st.Devices[dev].NotificationPreview {
			t.Fatal("refresh lost preview")
		}
		return nil
	})
	status, b, _ = request(t, s, bt, "GET", "/api/devices", nil)
	expectStatus(t, status, 200, b)
	if bytes.Contains(b, []byte("test-fcm-rotated")) {
		t.Fatal("token leak")
	}

}
func TestNotificationReadReconcileIsolation(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	bt := loginTest(t, s, bob.Username)
	var m1, m2, m3, expired, deleted Message
	_ = a.store.update(func(st *State) error {
		st.Rooms["notify-room-one"] = Room{ID: "notify-room-one", Kind: "chat", OwnerID: admin.ID, Members: map[string]int64{admin.ID: 0, bob.ID: 0}}
		st.Rooms["notify-room-two"] = Room{ID: "notify-room-two", Kind: "chat", OwnerID: admin.ID, Members: map[string]int64{admin.ID: 0, bob.ID: 0, eve.ID: 0}}
		st.Devices["notify-device-one"] = Device{ID: "notify-device-one", UserID: bob.ID, Token: "fake-one"}
		st.Devices["notify-device-two"] = Device{ID: "notify-device-two", UserID: eve.ID, Token: "fake-two"}
		m1, _ = addMessage(st, Message{RoomID: "notify-room-one", UserID: admin.ID, Kind: "text", Text: "first synthetic"}, a.cfg.MaxMessages, true)
		m2, _ = addMessage(st, Message{RoomID: "notify-room-one", UserID: admin.ID, Kind: "text", Text: "newer synthetic"}, a.cfg.MaxMessages, true)
		m3, _ = addMessage(st, Message{RoomID: "notify-room-two", UserID: admin.ID, Kind: "text", Text: "other room synthetic"}, a.cfg.MaxMessages, true)
		expired, _ = addMessage(st, Message{RoomID: "notify-room-one", UserID: admin.ID, Kind: "auth", Text: "{\"code\":\"012345\"}", Expires: now() - 1}, a.cfg.MaxMessages, false)
		deleted, _ = addMessage(st, Message{RoomID: "notify-room-one", UserID: admin.ID, Kind: "deleted"}, a.cfg.MaxMessages, false)
		return nil
	})
	ids := []int64{m1.ID, m2.ID, m3.ID, expired.ID, deleted.ID, 999999, m1.ID}
	status, b, _ := request(t, s, bt, "POST", "/api/rooms/notify-room-one/read", map[string]int64{"message_id": m1.ID})
	expectStatus(t, status, 200, b)
	var rev uint64
	_ = a.store.view(func(st *State) error {
		rev = st.Revision
		for _, p := range st.Outbox {
			if p.UserID == bob.ID && p.MessageID == m1.ID {
				t.Fatal("read pending not removed")
			}
		}
		return nil
	})
	status, b, _ = request(t, s, bt, "POST", "/api/notifications/state", map[string]any{"message_ids": ids})
	expectStatus(t, status, 200, b)
	var result struct {
		IDs []int64 `json:"dismiss_message_ids"`
	}
	_ = json.Unmarshal(b, &result)
	want := map[int64]bool{m1.ID: true, expired.ID: true, deleted.ID: true, 999999: true}
	if len(result.IDs) != len(want) {
		t.Fatal(string(b))
	}
	for _, id := range result.IDs {
		if !want[id] {
			t.Fatal("unread/newer/wrong-room dismissal", id)
		}
	}
	if bytes.Contains(b, []byte("synthetic")) || bytes.Contains(b, []byte("012345")) {
		t.Fatal("response leaks contents")
	}
	_ = a.store.view(func(st *State) error {
		if rev != st.Revision || st.Rooms["notify-room-two"].Members[bob.ID] != 0 {
			t.Fatal("reconcile must not mark read")
		}
		return nil
	})
	fake := &fakePush{}
	a.push = fake
	a.deliverPush()
	if fake.calls != 3 {
		t.Fatal("only one unread room-one and two unread room-two recipients expected", fake.calls)
	}
	status, b, _ = request(t, s, "", "POST", "/api/notifications/state", map[string]any{"message_ids": ids})
	expectStatus(t, status, 401, b)
	for _, ids := range [][]int64{{-1}, {0}, make([]int64, 65)} {
		status, b, _ = request(t, s, bt, "POST", "/api/notifications/state", map[string]any{"message_ids": ids})
		expectStatus(t, status, 400, b)
	}
}
func TestNotificationPreviewBoundsAndAuthNeverExposed(t *testing.T) {
	st := emptyState()
	st.Users["sender"] = User{ID: "sender", Username: "테스트", DisplayName: "가상\n사용자"}
	for _, kind := range []string{"auth", "expired", "deleted", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			title, body := previewForMessage(&st, Message{UserID: "sender", Kind: kind, Text: "012345"})
			if title != "" || body != "" {
				t.Fatal("sensitive preview")
			}
		})
	}
	title, body := previewForMessage(&st, Message{UserID: "sender", Kind: "text", Text: strings.Repeat("가", 100) + "\nsecret"})
	if title != "가상 사용자" || len([]rune(body)) != 81 || !strings.HasSuffix(body, "…") {
		t.Fatal(title, body)
	}
	if cleanPushText("a\u202eb\n c", 80) != "a b c" {
		t.Fatal("control characters preserved")
	}
	st.Files["file"] = Attachment{ID: "file", Name: "report\n141.pdf"}
	_, body = previewForMessage(&st, Message{UserID: "sender", Kind: "file", FileID: "file"})
	if body != "파일: report 141.pdf" {
		t.Fatal(body)
	}
	p := Outbox{PreviewTitle: "not-persisted-title", PreviewBody: "not-persisted-body"}
	raw, _ := json.Marshal(p)
	if bytes.Contains(raw, []byte("not-persisted")) {
		t.Fatal("transient preview persisted")
	}
}
func TestFCMPreviewOptInAndAuthPayload(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		respond(w, 200, map[string]string{"name": "synthetic"})
	}))
	defer srv.Close()
	f := &FCM{client: srv.Client(), token: "test-token", until: time.Now().Add(time.Hour), sendURL: srv.URL}
	for _, tc := range []struct {
		name, kind    string
		enabled, want bool
	}{{"default_private", "text", false, false}, {"opt_in_text", "text", true, true}, {"opt_in_file", "file", true, true}, {"auth_private", "auth", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			d := Device{Token: "test-device", UserID: "user141", NotificationPreview: tc.enabled}
			p := Outbox{MessageID: 7, RoomID: "room141", Kind: tc.kind, Expires: now() + 100, PreviewTitle: "가상 사용자", PreviewBody: "unique-preview-141"}
			if e := f.Send(context.Background(), d, p); e != nil {
				t.Fatal(e)
			}
			m := captured["message"].(map[string]any)
			notification := m["notification"].(map[string]any)
			if (notification["body"] == "unique-preview-141") != tc.want {
				t.Fatal(notification)
			}
			data := m["data"].(map[string]any)
			if data["recipient_id"] != "user141" {
				t.Fatal("recipient missing")
			}
			raw, _ := json.Marshal(data)
			if bytes.Contains(raw, []byte("unique-preview")) {
				t.Fatal("extra data preview")
			}
			android := m["android"].(map[string]any)
			notice := android["notification"].(map[string]any)
			if notice["channel_id"] != "ontech_messages" || notice["visibility"] != "PRIVATE" || notice["tag"] != "message-7" {
				t.Fatal(notice)
			}
		})
	}
}

func TestNotificationPreferenceSurvivesStoreRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.update(func(s *State) error {
		s.Devices["persist-device-141"] = Device{ID: "persist-device-141", UserID: "synthetic-user", Token: "synthetic-non-fcm-token", NotificationPreview: true}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err = st.view(func(s *State) error {
		if !s.Devices["persist-device-141"].NotificationPreview {
			t.Fatal("preview setting lost on restart")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFCMAppDeliveryRequiresRecipientAndHidesCoordinates(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		respond(w, 200, map[string]string{"name": "test"})
	}))
	defer srv.Close()
	f := &FCM{client: srv.Client(), token: "test-token", until: time.Now().Add(time.Hour), sendURL: srv.URL}
	for _, kind := range []string{"text", "auth", "location", "location_request"} {
		t.Run(kind, func(t *testing.T) {
			d := Device{Token: "test-token-not-real", UserID: "recipient-test", NotificationPreview: true, NotificationDelivery: "app"}
			p := Outbox{MessageID: 9, RoomID: "room-test", Kind: kind, Expires: now() + 60, PreviewTitle: "Example", PreviewBody: "secret-content-37.12345"}
			if e := f.Send(context.Background(), d, p); e != nil {
				t.Fatal(e)
			}
			m := captured["message"].(map[string]any)
			if _, ok := m["notification"]; ok {
				t.Fatal("system auto-display bypasses account checks")
			}
			data := m["data"].(map[string]any)
			if data["recipient_id"] != "recipient-test" || data["delivery"] != "app" {
				t.Fatal("recipient contract")
			}
			raw, _ := json.Marshal(data)
			if kind != "text" && bytes.Contains(raw, []byte("secret-content")) {
				t.Fatal("sensitive preview")
			}
			if _, ok := m["android"].(map[string]any)["notification"]; ok {
				t.Fatal("notification options with data-only message")
			}
		})
	}
}
