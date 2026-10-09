package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func displayName(u User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}
func avatarColor(u User) string {
	switch u.AvatarColor {
	case "blue", "mint", "rose", "violet", "gold", "slate":
		return u.AvatarColor
	}
	return "blue"
}
func publicProfile(u User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "display_name": displayName(u), "status_message": u.StatusMessage, "role": u.Role, "avatar_color": avatarColor(u), "avatar_revision": u.AvatarRevision, "disabled": u.Disabled}
}
func (a *App) directoryChanged() {
	a.invalidateConversationHelp()
	ids := []string{}
	_ = a.store.view(func(st *State) error {
		for id := range st.Users {
			ids = append(ids, id)
		}
		return nil
	})
	a.hub.publish(ids, "")
}
func (a *App) friends(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	e := a.store.view(func(st *State) error {
		viewer := st.Users[u.ID]
		for _, v := range sortedUsers(st) {
			if v.Disabled {
				continue
			}
			p := publicProfile(v)
			p["is_self"] = v.ID == u.ID
			p["favorite"] = viewer.Favorites[v.ID]
			out = append(out, p)
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
func (a *App) getProfile(w http.ResponseWriter, r *http.Request, u User) {
	respond(w, 200, publicProfile(u))
}
func validProfileText(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			return false
		}
	}
	return true
}
func normalizedAvatar(s string) (string, error) {
	if len(s) > 60<<10 {
		return "", bad("프로필 이미지는 45KB 이하의 JPEG/PNG를 사용하세요.")
	}
	raw, e := base64.StdEncoding.DecodeString(s)
	if e != nil || len(raw) > 45<<10 {
		return "", bad("프로필 이미지가 올바르지 않습니다.")
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 256 || cfg.Height > 256 {
		return "", bad("프로필 사진은 가로·세로 256픽셀 이하의 JPEG/PNG여야 합니다.")
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return "", bad("프로필 사진을 읽지 못했습니다.")
	}
	var buf bytes.Buffer
	if e = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); e != nil || buf.Len() > 45<<10 {
		return "", bad("프로필 사진을 더 작게 선택해 주세요.")
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
func (a *App) updateProfile(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		DisplayName   string  `json:"display_name"`
		StatusMessage string  `json:"status_message"`
		AvatarColor   string  `json:"avatar_color"`
		AvatarImage   *string `json:"avatar_image"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.StatusMessage = strings.TrimSpace(in.StatusMessage)
	if !validProfileText(in.DisplayName, 40) || !validProfileText(in.StatusMessage, 120) {
		fail(w, bad("표시 이름은 40자, 상태메시지는 120자 이하입니다. 제어 문자는 사용할 수 없습니다."))
		return
	}
	if avatarColor(User{AvatarColor: in.AvatarColor}) != in.AvatarColor {
		fail(w, bad("잘못된 프로필 색상입니다."))
		return
	}
	img := ""
	if in.AvatarImage != nil && *in.AvatarImage != "" {
		var e error
		img, e = normalizedAvatar(*in.AvatarImage)
		if e != nil {
			fail(w, e)
			return
		}
	}
	var result User
	e := a.store.update(func(st *State) error {
		v, ok := st.Users[u.ID]
		if !ok || v.Disabled {
			return forbidden()
		}
		v.DisplayName = in.DisplayName
		v.StatusMessage = in.StatusMessage
		v.AvatarColor = in.AvatarColor
		if in.AvatarImage != nil {
			v.AvatarImage = img
			v.AvatarRevision = ""
			if img != "" {
				v.AvatarRevision = digest(img)[:16]
			}
		}
		st.Users[u.ID] = v
		result = v
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.directoryChanged()
	respond(w, 200, publicProfile(result))
}
func (a *App) avatar(w http.ResponseWriter, r *http.Request, u User) {
	var data []byte
	e := a.store.view(func(st *State) error {
		v, ok := st.Users[r.PathValue("id")]
		if !ok || v.Disabled || v.AvatarImage == "" {
			return notfound()
		}
		var e error
		data, e = base64.StdEncoding.DecodeString(v.AvatarImage)
		return e
	})
	if e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func (a *App) favoriteFriend(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Favorite bool `json:"favorite"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	id := r.PathValue("id")
	e := a.store.update(func(st *State) error {
		v, ok := st.Users[id]
		if !ok || v.Disabled || id == u.ID {
			return bad("즐겨찾기할 친구를 선택하세요.")
		}
		me := st.Users[u.ID]
		if me.Disabled {
			return forbidden()
		}
		if me.Favorites == nil {
			me.Favorites = map[string]bool{}
		}
		if in.Favorite {
			me.Favorites[id] = true
		} else {
			delete(me.Favorites, id)
		}
		st.Users[u.ID] = me
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.hub.publish([]string{u.ID}, "")
	respond(w, 200, map[string]bool{"ok": true})
}
func directKey(a, b string) string {
	ids := []string{a, b}
	sort.Strings(ids)
	return digest(ids[0] + "\x00" + ids[1])
}
func canPost(st *State, r Room) bool {
	if r.Kind == "chat" {
		return true
	}
	if r.Kind != "direct" || len(r.Members) != 2 {
		return false
	}
	for id := range r.Members {
		v, ok := st.Users[id]
		if !ok || v.Disabled {
			return false
		}
	}
	return true
}
func (a *App) openDirect(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		UserID string `json:"user_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.UserID == u.ID {
		fail(w, bad("나와 다른 친구를 선택하세요."))
		return
	}
	var room Room
	created := false
	e := a.store.update(func(st *State) error {
		peer, ok := st.Users[in.UserID]
		me, mok := st.Users[u.ID]
		if !ok || !mok || me.Disabled || peer.Disabled {
			return bad("대화할 수 있는 사용자가 아닙니다.")
		}
		key := directKey(u.ID, in.UserID)
		for _, v := range st.Rooms {
			if v.Kind == "direct" && v.DirectKey == key {
				if len(v.Members) != 2 {
					return bad("1:1 대화방 상태를 확인해 주세요.")
				}
				if _, ok := v.Members[u.ID]; !ok {
					return forbidden()
				}
				if _, ok := v.Members[in.UserID]; !ok {
					return forbidden()
				}
				room = v
				if room.HiddenFor[u.ID] {
					delete(room.HiddenFor, u.ID)
					st.Rooms[room.ID] = room
					return nil
				}
				return errUnchanged
			}
		}
		if len(st.Rooms) >= 1000 {
			return bad("방 수 제한에 도달했습니다.")
		}
		room = Room{ID: randomID(), DirectKey: key, Name: "1:1 대화", Kind: "direct", OwnerID: u.ID, Created: now(), Members: map[string]int64{u.ID: 0, in.UserID: 0}}
		st.Rooms[room.ID] = room
		created = true
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(room.ID)
	status := 200
	if created {
		status = 201
	}
	respond(w, status, map[string]any{"id": room.ID, "kind": "direct", "created": created})
}
func (a *App) roomPreference(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Pinned bool `json:"pinned"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	id := r.PathValue("room")
	e := a.store.update(func(st *State) error {
		if _, ok := member(st, id, u.ID); !ok {
			return forbidden()
		}
		me := st.Users[u.ID]
		if me.PinnedRooms == nil {
			me.PinnedRooms = map[string]bool{}
		}
		if in.Pinned {
			me.PinnedRooms[id] = true
		} else {
			delete(me.PinnedRooms, id)
		}
		st.Users[u.ID] = me
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.hub.publish([]string{u.ID}, "")
	respond(w, 200, map[string]bool{"ok": true})
}
func roomSummary(st *State, r Room, u User, m Message) map[string]any {
	title := r.Name
	var peer any
	if r.Kind == "direct" {
		for id := range r.Members {
			if id != u.ID {
				p := st.Users[id]
				title = displayName(p)
				peer = publicProfile(p)
			}
		}
	}
	preview := "대화를 시작해 보세요"
	updated := r.Created
	if m.ID > 0 {
		updated = m.Created
		switch {
		case m.Expires > 0 && m.Expires <= now():
			preview = "보관 기간이 지난 메시지"
		case m.Kind == "deleted":
			preview = "삭제된 메시지"
		case m.Kind == "auth":
			preview = "새 인증번호가 도착했습니다"
		case m.Kind == "location_request":
			preview = "현재 위치 확인 요청"
		case m.Kind == "location":
			preview = "위치를 공유했습니다"
		case m.Kind == "file":
			preview = "첨부파일 · " + m.Text
		default:
			preview = m.Text
		}
		chars := []rune(strings.ReplaceAll(preview, "\n", " "))
		if len(chars) > 100 {
			preview = string(chars[:100]) + "…"
		}
	}
	canSend := canPost(st, r) || (r.Kind == "ai" && r.OwnerID == u.ID && len(r.Members) == 1)
	out := map[string]any{"id": r.ID, "name": title, "kind": r.Kind, "owner_id": r.OwnerID, "created_at": r.Created, "member_count": len(r.Members), "last_read_id": r.Members[u.ID], "last_message_id": m.ID, "last_message": preview, "updated_at": updated, "peer": peer, "pinned": u.PinnedRooms[r.ID], "can_send": canSend}
	if r.Kind == "ai" {
		out["ai_model_id"] = r.AIModelID
		out["ai_effort"] = r.AIEffort
		var latest AIRequest
		found := false
		for _, q := range st.AIRequests {
			if q.RoomID == r.ID && q.OwnerID == u.ID && (!found || q.CreatedAt > latest.CreatedAt || (q.CreatedAt == latest.CreatedAt && q.ID > latest.ID)) {
				latest, found = q, true
			}
		}
		if found {
			out["ai_request"] = publicAIRequest(latest)
		}
		var latestAgent AgentJob
		agentFound := false
		for _, q := range st.AgentJobs {
			if q.RoomID == r.ID && q.OwnerID == u.ID &&
				(!agentFound || q.CreatedAt > latestAgent.CreatedAt || (q.CreatedAt == latestAgent.CreatedAt && q.ID > latestAgent.ID)) {
				latestAgent, agentFound = q, true
			}
		}
		if agentFound {
			out["agent_job"] = publicAgentJob(latestAgent)
		}
	}
	return out
}
