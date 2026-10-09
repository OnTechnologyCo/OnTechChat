package server

import (
	"net/http"
	"sort"
	"strings"
	"unicode"
)

// 기기 소유자만 본인 휴대전화의 미리보기 여부를 변경할 수 있습니다.
func notificationPreferences(d Device) map[string]any {
	return map[string]any{"preview_enabled": d.NotificationPreview, "auth_preview_enabled": false, "settings_version": 1}
}
func (a *App) getDeviceNotifications(w http.ResponseWriter, r *http.Request, u User) {
	var d Device
	e := a.store.view(func(st *State) error {
		v, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if v.UserID != u.ID {
			return forbidden()
		}
		d = v
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, notificationPreferences(d))
}
func (a *App) setDeviceNotifications(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Preview *bool `json:"preview_enabled"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Preview == nil {
		fail(w, bad("미리보기 사용 여부를 지정하세요."))
		return
	}
	var d Device
	e := a.store.update(func(st *State) error {
		v, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if v.UserID != u.ID {
			return forbidden()
		}
		v.NotificationPreview = *in.Preview
		v.Updated = now()
		st.Devices[v.ID] = v
		d = v
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, notificationPreferences(d))
}
func findNotificationMessage(st *State, id int64) (Message, bool) {
	// 메시지는 ID 오름차순입니다. 삭제/보관 정리로 중간 ID가 사라질 수 있습니다.
	i := sort.Search(len(st.Messages), func(i int) bool { return st.Messages[i].ID >= id })
	if i < len(st.Messages) && st.Messages[i].ID == id {
		return st.Messages[i], true
	}
	return Message{}, false
}
func notificationDismissed(st *State, m Message, uid string, at int64) bool {
	if m.Kind == "location_request" {
		for _, q := range st.LocationRequests {
			if q.MessageID == m.ID && locationRequestStatus(st, q) != "pending" {
				return true
			}
		}
	}
	room, ok := member(st, m.RoomID, uid)
	return !ok || m.ID <= roomClearedThrough(room, uid) || m.Kind == "deleted" || m.Kind == "expired" || (m.Expires > 0 && m.Expires <= at) || room.Members[uid] >= m.ID
}

// Android가 실제 게시된 알림 ID만 문의합니다. 읽음 위치를 변경하거나 메시지 본문을 반환하지 않습니다.
func (a *App) notificationState(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		IDs []int64 `json:"message_ids"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if len(in.IDs) > 64 {
		fail(w, bad("알림은 한 번에 64개까지 확인할 수 있습니다."))
		return
	}
	seen := map[int64]bool{}
	for _, id := range in.IDs {
		if id <= 0 {
			fail(w, bad("알림 메시지 ID를 확인하세요."))
			return
		}
		seen[id] = true
	}
	dismiss := []int64{}
	e := a.store.view(func(st *State) error {
		for id := range seen {
			m, ok := findNotificationMessage(st, id)
			if !ok || notificationDismissed(st, m, u.ID, now()) {
				dismiss = append(dismiss, id)
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	sort.Slice(dismiss, func(i, j int) bool { return dismiss[i] < dismiss[j] })
	respond(w, 200, map[string]any{"version": 1, "dismiss_message_ids": dismiss})
}
func cleanPushText(text string, limit int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	chars := []rune(text)
	if len(chars) > limit {
		return string(chars[:limit]) + "…"
	}
	return text
}
func previewForMessage(st *State, m Message) (string, string) {
	// 인증함/임의 kind는 미리보기 동의 여부와 무관하게 원문을 보내지 않습니다.
	if m.Kind != "text" && m.Kind != "file" {
		return "", ""
	}
	if m.UserID == "" && m.AIRequestID != "" {
		room, ok := st.Rooms[m.RoomID]
		if !ok || room.Kind != "ai" || m.Kind != "text" {
			return "", ""
		}
		return cleanPushText(room.Name, 40), cleanPushText(m.Text, 80)
	}
	sender, ok := st.Users[m.UserID]
	if !ok {
		return "", ""
	}
	title := cleanPushText(displayName(sender), 40)
	if title == "" {
		title = "온테크 채팅"
	}
	body := ""
	if m.Kind == "text" {
		body = cleanPushText(m.Text, 80)
	} else {
		body = "첨부파일을 보냈습니다."
		if f, ok := st.Files[m.FileID]; ok {
			body = "파일: " + cleanPushText(f.Name, 70)
		}
	}
	return title, body
}
