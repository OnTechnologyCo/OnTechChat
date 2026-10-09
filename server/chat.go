package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (a *App) rooms(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	e := a.store.view(func(st *State) error {
		// One scan of the message list. Authentication codes never become list previews.
		latest := map[string]Message{}
		unread := map[string]int{}
		for _, m := range st.Messages {
			room, ok := st.Rooms[m.RoomID]
			if !ok {
				continue
			}
			last, ok := room.Members[u.ID]
			if !ok {
				continue
			}
			if m.ID <= roomClearedThrough(room, u.ID) {
				continue
			}
			if m.ID > latest[m.RoomID].ID {
				latest[m.RoomID] = m
			}
			if m.ID > last && m.UserID != u.ID && m.Kind != "deleted" && (m.Expires == 0 || m.Expires > now()) {
				unread[m.RoomID]++
			}
		}
		viewer := st.Users[u.ID]
		for _, room := range st.Rooms {
			if _, ok := room.Members[u.ID]; !ok {
				continue
			}
			if room.HiddenFor[u.ID] {
				continue
			}
			v := roomSummary(st, room, viewer, latest[room.ID])
			v["unread"] = unread[room.ID]
			out = append(out, v)
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := out[i]["pinned"].(bool), out[j]["pinned"].(bool)
		if pi != pj {
			return pi
		}
		ti, tj := out[i]["updated_at"].(int64), out[j]["updated_at"].(int64)
		if ti != tj {
			return ti > tj
		}
		mi, mj := out[i]["last_message_id"].(int64), out[j]["last_message_id"].(int64)
		if mi != mj {
			return mi > mj
		}
		return out[i]["id"].(string) < out[j]["id"].(string)
	})
	respond(w, 200, out)
}
func (a *App) createRoom(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 || len(in.Members) > 50 {
		fail(w, bad("방 이름은 1~80자이며 초대는 50명 이하입니다."))
		return
	}
	room := Room{ID: randomID(), Name: in.Name, OwnerID: u.ID, Kind: "chat", Created: now(), Members: map[string]int64{u.ID: 0}}
	e := a.store.update(func(st *State) error {
		if len(st.Rooms) >= 1000 {
			return bad("방 수 제한에 도달했습니다.")
		}
		if st.Users[u.ID].Disabled {
			return forbidden()
		}
		for _, name := range in.Members {
			v, ok := findUsername(st, strings.ToLower(strings.TrimSpace(name)))
			if !ok || v.Disabled {
				return bad("존재하지 않거나 비활성인 사용자가 있습니다.")
			}
			room.Members[v.ID] = 0
		}
		if len(room.Members) > 50 {
			return bad("방장을 포함해 한 방에는 최대 50명까지 참여할 수 있습니다.")
		}
		st.Rooms[room.ID] = room
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(room.ID)
	respond(w, 201, room)
}
func (a *App) members(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	e := a.store.view(func(st *State) error {
		room, ok := member(st, r.PathValue("room"), u.ID)
		if !ok || room.Kind == "ai" {
			return forbidden()
		}
		for id, last := range room.Members {
			v := st.Users[id]
			out = append(out, map[string]any{"id": id, "username": v.Username, "last_read_id": last, "owner": id == room.OwnerID, "disabled": v.Disabled})
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
func (a *App) addMember(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Username string `json:"username"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	id := r.PathValue("room")
	e := a.store.update(func(st *State) error {
		room, ok := member(st, id, u.ID)
		if !ok || room.OwnerID != u.ID || room.Kind != "chat" {
			return forbidden()
		}
		if len(room.Members) >= 50 {
			return bad("한 방에는 최대 50명까지 참여할 수 있습니다.")
		}
		v, ok := findUsername(st, strings.ToLower(strings.TrimSpace(in.Username)))
		if !ok || v.Disabled {
			return bad("사용자를 찾을 수 없습니다.")
		}
		if _, exists := room.Members[v.ID]; !exists {
			room.Members[v.ID] = 0
			st.Rooms[id] = room
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(id)
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) removeMember(w http.ResponseWriter, r *http.Request, u User) {
	id, target := r.PathValue("room"), r.PathValue("user")
	e := a.store.update(func(st *State) error {
		room, ok := member(st, id, u.ID)
		if !ok || room.Kind != "chat" || target == room.OwnerID || (u.ID != target && u.ID != room.OwnerID) {
			return forbidden()
		}
		delete(room.Members, target)
		st.Rooms[id] = room
		for k, p := range st.Outbox {
			if p.RoomID == id && p.UserID == target {
				delete(st.Outbox, k)
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(id, target)
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) leaveRoom(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("room")
	var newOwner string
	e := a.store.update(func(st *State) error {
		room, ok := member(st, id, u.ID)
		if !ok || room.Kind != "chat" {
			return forbidden()
		}
		if room.OwnerID == u.ID {
			candidates := []User{}
			for memberID := range room.Members {
				if memberID == u.ID {
					continue
				}
				v, exists := st.Users[memberID]
				if exists && !v.Disabled {
					candidates = append(candidates, v)
				}
			}
			if len(candidates) == 0 {
				return bad("다른 참여자가 없는 채팅방은 나갈 수 없습니다.")
			}
			sort.Slice(candidates, func(i, j int) bool { return candidates[i].Username < candidates[j].Username })
			room.OwnerID = candidates[0].ID
			newOwner = candidates[0].Username
		}
		delete(room.Members, u.ID)
		delete(room.ClearedThrough, u.ID)
		st.Rooms[id] = room
		for k, p := range st.Outbox {
			if p.RoomID == id && p.UserID == u.ID {
				delete(st.Outbox, k)
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(id, u.ID)
	out := map[string]any{"ok": true}
	if newOwner != "" {
		out["new_owner"] = newOwner
	}
	respond(w, 200, out)
}
func intQuery(r *http.Request, name string, defaultValue int64) (int64, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return defaultValue, nil
	}
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n < 0 {
		return 0, bad("잘못된 페이지 조회 값입니다.")
	}
	return n, nil
}
func messageView(m Message, st *State) map[string]any {
	if m.Kind == "location_request" {
		m.Text = currentLocationRequestText(st, m)
	}
	if m.Expires > 0 && m.Expires <= now() {
		m.Text = ""
		m.Kind = "expired"
	}
	out := map[string]any{"id": m.ID, "room_id": m.RoomID, "user_id": m.UserID, "username": m.Username, "kind": m.Kind, "text": m.Text, "created_at": m.Created, "expires_at": m.Expires}
	out["display_name"] = displayName(st.Users[m.UserID])
	if room, ok := st.Rooms[m.RoomID]; ok && room.Kind == "ai" && m.UserID == "" {
		out["display_name"] = "AI"
		out["username"] = "AI"
		out["ai"] = map[string]any{"request_id": m.AIRequestID, "model_id": m.AIModelID, "effort": m.AIEffort, "observed_model": m.AIObservedModel}
		n := 0
		for id, read := range room.Members {
			if !st.Users[id].Disabled && read < m.ID {
				n++
			}
		}
		out["unread_count"] = n
	}
	if room, ok := st.Rooms[m.RoomID]; ok && room.Kind != "auth" && m.UserID != "" {
		n := 0
		for id, read := range room.Members {
			if id != m.UserID && !st.Users[id].Disabled && read < m.ID {
				n++
			}
		}
		out["unread_count"] = n
	}
	if m.FileID != "" {
		if f, ok := st.Files[m.FileID]; ok {
			out["file"] = f
		}
	}
	return out
}
func (a *App) messages(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("room")
	after, e := intQuery(r, "after", 0)
	if e != nil {
		fail(w, e)
		return
	}
	before, e := intQuery(r, "before", 0)
	if e != nil {
		fail(w, e)
		return
	}
	limit, e := intQuery(r, "limit", 100)
	if e != nil || limit < 1 || limit > 200 || (after > 0 && before > 0) {
		fail(w, bad("limit은 1~200이며 after/before 중 하나만 사용할 수 있습니다."))
		return
	}
	out := []map[string]any{}
	hasMore := false
	var clearedThrough int64
	e = a.store.view(func(st *State) error {
		if _, ok := member(st, id, u.ID); !ok {
			return forbidden()
		}
		clearedThrough = roomClearedThrough(st.Rooms[id], u.ID)
		selected := []Message{}
		for _, m := range st.Messages {
			if m.RoomID != id || m.ID <= clearedThrough || (after > 0 && m.ID <= after) || (before > 0 && m.ID >= before) {
				continue
			}
			selected = append(selected, m)
		}
		if len(selected) > int(limit) {
			hasMore = true
			if after > 0 {
				selected = selected[:limit]
			} else {
				selected = selected[len(selected)-int(limit):]
			}
		}
		for _, m := range selected {
			out = append(out, messageView(m, st))
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]any{"messages": out, "has_more": hasMore, "cleared_through": clearedThrough})
}
func (a *App) sendMessage(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Text     string `json:"text"`
		ClientID string `json:"client_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if !utf8.ValidString(in.Text) || in.Text == "" || len(in.Text) > 16000 || !clientIDPattern.MatchString(in.ClientID) {
		fail(w, bad("메시지는 1~16000바이트이며 고유 client_id가 필요합니다."))
		return
	}
	id := r.PathValue("room")
	var roomKind string
	e := a.store.view(func(st *State) error {
		room, ok := member(st, id, u.ID)
		if !ok {
			return forbidden()
		}
		roomKind = room.Kind
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if roomKind == "ai" {
		a.sendAIMessage(w, r, u, in.Text, in.ClientID)
		return
	}
	var m Message
	e = a.store.update(func(st *State) error {
		room, ok := member(st, id, u.ID)
		if !ok || !canPost(st, room) {
			return forbidden()
		}
		var e error
		m, e = addMessage(st, Message{RoomID: id, UserID: u.ID, Username: u.Username, Kind: "text", Text: in.Text, ClientID: in.ClientID}, a.cfg.MaxMessages, a.cfg.FCM.Enabled)
		return e
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(id)
	respond(w, 201, m)
}
func (a *App) markRead(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		ID int64 `json:"message_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	id := r.PathValue("room")
	changed := false
	e := a.store.update(func(st *State) error {
		room, ok := member(st, id, u.ID)
		if !ok {
			return forbidden()
		}
		valid := in.ID == 0
		for _, m := range st.Messages {
			if m.RoomID == id && m.ID == in.ID {
				valid = true
				break
			}
		}
		if !valid {
			return bad("해당 방의 메시지 번호가 아닙니다.")
		}
		if in.ID <= room.Members[u.ID] {
			return errUnchanged
		}
		if in.ID > room.Members[u.ID] {
			room.Members[u.ID] = in.ID
			st.Rooms[id] = room
			// 읽음 확정 이후 아직 발송하지 않은 해당 사용자/대화방의 알림만 제거합니다.
			for key, job := range st.Outbox {
				if job.UserID == u.ID && job.RoomID == id && job.MessageID <= in.ID && job.State == "pending" {
					delete(st.Outbox, key)
				}
			}
			changed = true
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if changed {
		a.changed(id)
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteMessage(w http.ResponseWriter, r *http.Request, u User) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, notfound())
		return
	}
	roomID := ""
	fileID := ""
	cancelRequestID := ""
	var cancelAgentIDs []string
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	e = a.store.update(func(st *State) error {
		for i, m := range st.Messages {
			if m.ID != id {
				continue
			}
			room, ok := member(st, m.RoomID, u.ID)
			if !ok || (m.UserID != u.ID && ((room.Kind != "chat" && room.Kind != "ai") || room.OwnerID != u.ID)) {
				return forbidden()
			}
			roomID = m.RoomID
			fileID = m.FileID
			if room.Kind == "ai" {
				for qid, q := range st.AgentJobs {
					if q.SourceMessage == m.ID || q.AnswerMessage == m.ID || (m.Kind == "file" && q.RoomID == m.RoomID && !agentTerminal(q.Status)) {
						cancelAgentIDs = append(cancelAgentIDs, qid)
						delete(st.AgentJobs, qid)
					}
				}
				for qid, q := range st.AIRequests {
					if (q.UserMessageID == m.ID || (m.Kind == "file" && q.RoomID == m.RoomID)) && (q.Status == aiQueued || q.Status == aiRunning) {
						q.Status, q.ErrorCode, q.Error = aiCancelled, "AI_SOURCE_DELETED", "질문이 삭제되어 응답 생성을 중지했습니다."
						q.FinishedAt = now()
						st.AIRequests[qid] = q
						cancelRequestID = qid
					}
				}
			}
			if fileID != "" {
				delete(st.Files, fileID)
			}
			st.Messages[i].Text = ""
			st.Messages[i].Kind = "deleted"
			st.Messages[i].FileID = ""
			for k, p := range st.Outbox {
				if p.MessageID == id {
					delete(st.Outbox, k)
				}
			}
			return nil
		}
		return notfound()
	})
	if e != nil {
		fail(w, e)
		return
	}
	if fileID != "" {
		_ = os.Remove(filepath.Join(a.cfg.DataDir, "files", fileID))
	}
	for _, id := range cancelAgentIDs {
		if a.agentJobs != nil {
			a.agentJobs.cancel(id)
		}
	}
	if cancelRequestID != "" && a.aiJobs != nil {
		a.aiJobs.cancel(cancelRequestID)
	}
	a.store.discardOldBackup()
	a.changed(roomID)
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) events(w http.ResponseWriter, r *http.Request, u User) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, apiError{500, "스트리밍을 사용할 수 없습니다."})
		return
	}
	ch, unsub, ok := a.hub.subscribe(u.ID)
	if !ok {
		fail(w, apiError{429, "연결 수가 너무 많습니다."})
		return
	}
	defer unsub()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, e := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		flusher.Flush()
		return e == nil
	}
	if !send("ready", map[string]string{"version": version}) {
		return
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case room := <-ch:
			if _, e := a.identify(r); e != nil {
				send("logout", map[string]bool{"login_required": true})
				return
			}
			if !send("sync", map[string]string{"room_id": room}) {
				return
			}
		case <-ticker.C:
			if _, e := a.identify(r); e != nil {
				send("logout", map[string]bool{"login_required": true})
				return
			}
			if !send("heartbeat", map[string]int64{"time": now()}) {
				return
			}
		}
	}
}
func (a *App) status(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	a.stateMu.Lock()
	out := map[string]any{"version": version, "mail_enabled": a.cfg.Mail.Enabled, "mail_state": a.mailState, "mail_checked_at": a.mailChecked, "fcm_enabled": a.cfg.FCM.Enabled, "fcm_state": a.pushState, "storage": "AES-256-GCM encrypted snapshots", "public_url": a.cfg.PublicURL, "max_file_bytes": a.cfg.MaxFileBytes, "storage_quota_bytes": a.cfg.StorageQuotaBytes, "auth_retention_minutes": a.cfg.AuthRetentionMinutes}
	a.stateMu.Unlock()
	e := a.store.view(func(st *State) error {
		pending, failed, sent := 0, 0, 0
		for _, p := range st.Outbox {
			switch p.State {
			case "pending":
				pending++
			case "failed":
				failed++
			case "sent":
				sent++
			}
		}
		var used int64
		for _, f := range st.Files {
			used += f.Size
		}
		out["users"] = len(st.Users)
		out["rooms"] = len(st.Rooms)
		out["messages"] = len(st.Messages)
		out["file_bytes"] = used
		out["push_pending"] = pending
		out["push_failed"] = failed
		out["push_sent_requests"] = sent
		out["recovered_backup"] = a.store.recovered
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
