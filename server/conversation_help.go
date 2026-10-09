package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
	_ "time/tzdata"
)

type conversationHelpScope struct {
	Owner, RoomID, Hash, Timezone string
	IDs                           []int64
	Expires                       int64
	Total                         int
	Truncated                     bool
	Text, Action                  string
}
type conversationHelpState struct {
	mu        sync.Mutex
	entries   map[string]conversationHelpScope
	running   map[string]bool
	automatic map[string]autoHelpEntry
}
type conversationHelpMessage struct {
	ID      int64  `json:"id"`
	Sender  string `json:"sender"`
	Created int64  `json:"created_at"`
	Text    string `json:"text"`
}
type conversationHelpSelection struct {
	RoomID   string `json:"room_id"`
	Limit    int    `json:"limit"`
	From     int64  `json:"from,omitempty"`
	To       int64  `json:"to,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}
type conversationReadArgs struct {
	Limit           int
	After, FriendID string
	Selection       *conversationHelpSelection
}

var conversationReadTools = map[string]func(*State, User, conversationReadArgs) (any, error){
	"friends.list":           readHelpFriends,
	"conversations.list":     readHelpConversations,
	"conversations.messages": readHelpMessages,
}

func helpViewer(st *State, u User) (User, error) {
	v, ok := st.Users[u.ID]
	if !ok || v.Disabled {
		return User{}, forbidden()
	}
	return v, nil
}
func helpRoom(st *State, u User, id string) (Room, error) {
	r, ok := member(st, id, u.ID)
	if !ok || (r.Kind != "direct" && r.Kind != "chat") {
		return Room{}, forbidden()
	}
	return r, nil
}
func helpName(u User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}
func readHelpFriends(st *State, u User, in conversationReadArgs) (any, error) {
	if _, e := helpViewer(st, u); e != nil {
		return nil, e
	}
	items := []map[string]string{}
	for _, v := range st.Users {
		if !v.Disabled && v.ID != u.ID && v.ID > in.After {
			items = append(items, map[string]string{"id": v.ID, "display_name": helpName(v)})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["id"] < items[j]["id"] })
	next := ""
	if len(items) > in.Limit {
		items = items[:in.Limit]
		next = items[len(items)-1]["id"]
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}
func readHelpConversations(st *State, u User, in conversationReadArgs) (any, error) {
	if _, e := helpViewer(st, u); e != nil {
		return nil, e
	}
	if in.FriendID != "" {
		v, ok := st.Users[in.FriendID]
		if !ok || v.Disabled || v.ID == u.ID {
			return nil, forbidden()
		}
	}
	items := []map[string]string{}
	for _, r := range st.Rooms {
		if r.ID <= in.After {
			continue
		}
		if _, e := helpRoom(st, u, r.ID); e != nil {
			continue
		}
		if in.FriendID != "" {
			if _, ok := r.Members[in.FriendID]; !ok {
				continue
			}
		}
		name := r.Name
		if r.Kind == "direct" {
			for id := range r.Members {
				if id != u.ID {
					name = helpName(st.Users[id])
				}
			}
		}
		items = append(items, map[string]string{"id": r.ID, "name": name, "kind": r.Kind})
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["id"] < items[j]["id"] })
	next := ""
	if len(items) > in.Limit {
		items = items[:in.Limit]
		next = items[len(items)-1]["id"]
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}
func selectedHelpMessages(st *State, u User, in conversationHelpSelection) ([]conversationHelpMessage, int, error) {
	room, e := helpRoom(st, u, in.RoomID)
	if e != nil {
		return nil, 0, e
	}
	if in.Limit < 1 || in.Limit > 200 || in.From < 0 || in.To < 0 || (in.From > 0 && in.To > 0 && in.From > in.To) {
		return nil, 0, bad("대화 범위와 개수(1~200)를 확인하세요.")
	}
	all := []conversationHelpMessage{}
	for _, m := range st.Messages {
		if m.RoomID != room.ID || m.Kind != "text" || m.ID <= roomClearedThrough(room, u.ID) || (m.Expires > 0 && m.Expires <= now()) || (in.From > 0 && m.Created < in.From) || (in.To > 0 && m.Created > in.To) {
			continue
		}
		all = append(all, conversationHelpMessage{m.ID, helpName(st.Users[m.UserID]), m.Created, m.Text})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	total := len(all)
	if len(all) > in.Limit {
		all = all[len(all)-in.Limit:]
	}
	size, start := 0, len(all)
	for start > 0 {
		raw, _ := json.Marshal(all[start-1])
		if size+len(raw) > 96<<10 {
			break
		}
		size += len(raw)
		start--
	}
	return all[start:], total, nil
}
func readHelpMessages(st *State, u User, in conversationReadArgs) (any, error) {
	if in.Selection == nil {
		return nil, bad("명시적으로 선택한 대화 범위가 필요합니다.")
	}
	msgs, total, e := selectedHelpMessages(st, u, *in.Selection)
	if e != nil {
		return nil, e
	}
	return map[string]any{"messages": msgs, "total_matching": total, "truncated": total > len(msgs)}, nil
}
func helpFingerprint(messages []conversationHelpMessage) string {
	b, _ := json.Marshal(messages)
	return digest(string(b))
}
func helpSnapshot(st *State, u User, s conversationHelpScope) ([]conversationHelpMessage, error) {
	if s.Owner != u.ID || s.Expires <= now() {
		return nil, apiError{409, "미리보기 또는 분석 결과가 만료되었습니다. 다시 선택하세요."}
	}
	room, e := helpRoom(st, u, s.RoomID)
	if e != nil {
		return nil, e
	}
	ids := map[int64]bool{}
	for _, id := range s.IDs {
		ids[id] = true
	}
	msgs := []conversationHelpMessage{}
	for _, m := range st.Messages {
		if ids[m.ID] && m.RoomID == room.ID && m.Kind == "text" && m.ID > roomClearedThrough(room, u.ID) && (m.Expires == 0 || m.Expires > now()) {
			msgs = append(msgs, conversationHelpMessage{m.ID, helpName(st.Users[m.UserID]), m.Created, m.Text})
		}
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID })
	if len(msgs) != len(s.IDs) || helpFingerprint(msgs) != s.Hash {
		return nil, apiError{409, "원문이 삭제되거나 변경되었습니다. 다시 미리보기 하세요."}
	}
	return msgs, nil
}
func (a *App) helpPut(s conversationHelpScope) string {
	h := a.conversationHelp
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for id, v := range h.entries {
		if v.Expires <= now() {
			delete(h.entries, id)
		} else if v.Owner == s.Owner {
			count++
		}
	}
	if count >= 12 {
		for id, v := range h.entries {
			if v.Owner == s.Owner {
				delete(h.entries, id)
				break
			}
		}
	}
	if len(h.entries) >= 1000 {
		return ""
	}
	id := randomID()
	h.entries[id] = s
	return id
}
func (a *App) helpGet(u User, id string) (conversationHelpScope, []conversationHelpMessage, error) {
	h := a.conversationHelp
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.entries[id]
	if !ok || s.Owner != u.ID {
		return s, nil, notfound()
	}
	var msgs []conversationHelpMessage
	e := a.store.view(func(st *State) error { var e error; msgs, e = helpSnapshot(st, u, s); return e })
	if e != nil {
		delete(h.entries, id)
	}
	return s, msgs, e
}
func (a *App) invalidateConversationHelp() {
	if a.conversationHelp == nil {
		return
	}
	h := a.conversationHelp
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = a.store.view(func(st *State) error {
		for id, s := range h.entries {
			if _, e := helpSnapshot(st, User{ID: s.Owner}, s); e != nil {
				delete(h.entries, id)
			}
		}
		return nil
	})
}
func (a *App) conversationHelpMaintenance(ctxDone <-chan struct{}) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctxDone:
			return
		case <-tick.C:
			a.invalidateConversationHelp()
			a.autoHelpTick()
		}
	}
}
func noHelpCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
}
func (a *App) helpList(w http.ResponseWriter, r *http.Request, u User, tool string) {
	noHelpCache(w)
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 100 {
			fail(w, bad("목록 개수는 1~100입니다."))
			return
		}
		limit = n
	}
	args := conversationReadArgs{Limit: limit, After: r.URL.Query().Get("after"), FriendID: r.URL.Query().Get("friend_id")}
	var out any
	e := a.store.view(func(st *State) error { var e error; out, e = conversationReadTools[tool](st, u, args); return e })
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
func (a *App) helpFriends(w http.ResponseWriter, r *http.Request, u User) {
	a.helpList(w, r, u, "friends.list")
}
func (a *App) helpConversations(w http.ResponseWriter, r *http.Request, u User) {
	a.helpList(w, r, u, "conversations.list")
}
func (a *App) helpPreview(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var in conversationHelpSelection
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !a.limiter.allow("help-preview:"+u.ID, 30, time.Minute) {
		fail(w, apiError{429, "잠시 후 다시 시도하세요."})
		return
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Seoul"
	}
	if len(in.Timezone) > 64 {
		fail(w, bad("시간대를 확인하세요."))
		return
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil {
		fail(w, bad("IANA 시간대를 확인하세요."))
		return
	}
	var msgs []conversationHelpMessage
	var total int
	name := ""
	e := a.store.view(func(st *State) error {
		out, e := conversationReadTools["conversations.messages"](st, u, conversationReadArgs{Selection: &in})
		if e != nil {
			return e
		}
		v := out.(map[string]any)
		msgs = v["messages"].([]conversationHelpMessage)
		total = v["total_matching"].(int)
		name = st.Rooms[in.RoomID].Name
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	s := conversationHelpScope{Owner: u.ID, RoomID: in.RoomID, Hash: helpFingerprint(msgs), Timezone: in.Timezone, Expires: now() + 900, Total: total, Truncated: total > len(msgs)}
	for _, m := range msgs {
		s.IDs = append(s.IDs, m.ID)
	}
	id := a.helpPut(s)
	if id == "" {
		fail(w, apiError{429, "분석 대기 한도를 초과했습니다."})
		return
	}
	if _, current, e := a.helpGet(u, id); e != nil {
		fail(w, e)
		return
	} else {
		msgs = current
	}
	respond(w, 200, map[string]any{"preview_token": id, "room_id": in.RoomID, "room_name": name, "messages": msgs, "count": len(msgs), "total_matching": total, "truncated": s.Truncated, "timezone": s.Timezone, "expires_at": s.Expires})
}
func (a *App) helpCheck(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var in struct {
		PreviewToken string `json:"preview_token"`
		ResultID     string `json:"result_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	id := in.ResultID
	if id == "" {
		id = in.PreviewToken
	}
	if _, _, e := a.helpGet(u, id); e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]bool{"valid": true})
}
func (a *App) helpResult(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	id := r.PathValue("id")
	s, msgs, e := a.helpGet(u, id)
	if e != nil {
		fail(w, e)
		return
	}
	if s.Text == "" {
		fail(w, notfound())
		return
	}
	respond(w, 200, helpResultView(id, s, msgs))
}
func helpResultView(id string, s conversationHelpScope, msgs []conversationHelpMessage) map[string]any {
	return map[string]any{"result_id": id, "text": s.Text, "sources": msgs, "room_id": s.RoomID, "action": s.Action, "count": len(msgs), "truncated": s.Truncated, "timezone": s.Timezone, "expires_at": s.Expires}
}
func (a *App) helpSource(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, notfound())
		return
	}
	var out conversationHelpMessage
	e = a.store.view(func(st *State) error {
		room, e := helpRoom(st, u, r.PathValue("room"))
		if e != nil {
			return e
		}
		for _, m := range st.Messages {
			if m.ID == id && m.RoomID == room.ID && m.Kind == "text" && m.ID > roomClearedThrough(room, u.ID) && (m.Expires == 0 || m.Expires > now()) {
				out = conversationHelpMessage{m.ID, helpName(st.Users[m.UserID]), m.Created, m.Text}
				return nil
			}
		}
		return notfound()
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
