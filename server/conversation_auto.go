package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

type autoHelpSetting struct {
	Revision string `json:"revision"`
	Timezone string `json:"timezone"`
}
type autoHelpEntry struct {
	Revision, Hash, ResultID, Error string
	Started                         int64
	Running                         bool
}

func autoHelpKey(owner, room string) string { return owner + "/" + room }
func (a *App) autoHelpSet(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var in struct {
		Enabled  bool   `json:"enabled"`
		Timezone string `json:"timezone"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Seoul"
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil {
		fail(w, bad("시간대를 확인하세요."))
		return
	}
	id := r.PathValue("room")
	e := a.store.update(func(st *State) error {
		room, e := helpRoom(st, u, id)
		if e != nil {
			return e
		}
		if room.AutoHelp == nil {
			room.AutoHelp = map[string]autoHelpSetting{}
		}
		if in.Enabled {
			room.AutoHelp[u.ID] = autoHelpSetting{randomID(), in.Timezone}
		} else {
			delete(room.AutoHelp, u.ID)
		}
		st.Rooms[id] = room
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	h := a.conversationHelp
	h.mu.Lock()
	delete(h.automatic, autoHelpKey(u.ID, id))
	for key, s := range h.entries {
		if s.Owner == u.ID && s.RoomID == id && s.Action == "automatic" {
			delete(h.entries, key)
		}
	}
	h.mu.Unlock()
	a.autoHelpStatus(w, r, u)
}
func (a *App) autoHelpStatus(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var setting autoHelpSetting
	var messages []conversationHelpMessage
	id := r.PathValue("room")
	e := a.store.view(func(st *State) error {
		room, e := helpRoom(st, u, id)
		if e != nil {
			return e
		}
		setting = room.AutoHelp[u.ID]
		messages, _, e = selectedHelpMessages(st, u, conversationHelpSelection{RoomID: id, Limit: 50})
		return e
	})
	if e != nil {
		fail(w, e)
		return
	}
	out := map[string]any{"enabled": setting.Revision != "", "status": "off"}
	if setting.Revision != "" {
		out["status"] = "waiting"
		h := a.conversationHelp
		h.mu.Lock()
		entry := h.automatic[autoHelpKey(u.ID, id)]
		h.mu.Unlock()
		if len(messages) == 0 {
			out["status"] = "empty"
		} else if entry.Revision == setting.Revision {
			if entry.Running {
				out["status"] = "running"
			} else if entry.Error != "" {
				out["status"] = "error"
				out["error"] = entry.Error
			}
			if entry.ResultID != "" {
				if s, msgs, e := a.helpGet(u, entry.ResultID); e == nil {
					out["result"] = helpResultView(entry.ResultID, s, msgs)
					out["status"] = "ready"
				}
			}
		}
	}
	respond(w, 200, out)
}

// Opt-in is persisted per member, but source text and results remain transient.
func (a *App) autoHelpTick() {
	if !a.cfg.AI.Enabled || a.ctx.Err() != nil {
		return
	}
	type candidate struct {
		User     User
		Room     string
		Setting  autoHelpSetting
		Messages []conversationHelpMessage
		Total    int
	}
	candidates := []candidate{}
	_ = a.store.view(func(st *State) error {
		for _, room := range st.Rooms {
			for owner, setting := range room.AutoHelp {
				u, ok := st.Users[owner]
				if !ok || u.Disabled {
					continue
				}
				msgs, total, e := selectedHelpMessages(st, u, conversationHelpSelection{RoomID: room.ID, Limit: 50})
				if e != nil || len(msgs) == 0 {
					continue
				}
				candidates = append(candidates, candidate{u, room.ID, setting, msgs, total})
			}
		}
		return nil
	})
	h := a.conversationHelp
	h.mu.Lock()
	if h.automatic == nil {
		h.automatic = map[string]autoHelpEntry{}
	}
	// Evict removed/revoked subscriptions; never retain unbounded per-room caches.
	valid := map[string]bool{}
	for _, c := range candidates {
		valid[autoHelpKey(c.User.ID, c.Room)] = true
	}
	for key, entry := range h.automatic {
		if !valid[key] && !entry.Running {
			delete(h.automatic, key)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return h.automatic[autoHelpKey(candidates[i].User.ID, candidates[i].Room)].Started < h.automatic[autoHelpKey(candidates[j].User.ID, candidates[j].Room)].Started
	})
	h.mu.Unlock()
	for _, c := range candidates {
		key, hash := autoHelpKey(c.User.ID, c.Room), helpFingerprint(c.Messages)
		h.mu.Lock()
		entry := h.automatic[key]
		limit := a.cfg.AI.Concurrency
		if limit < 1 {
			limit = 1
		}
		if limit > 2 {
			limit = 2
		}
		currentResult := h.entries[entry.ResultID]
		if h.running[c.User.ID] || len(h.running) >= limit || len(h.automatic) >= 1000 && entry.Revision == "" || entry.Running || now()-entry.Started < 30 || (entry.Revision == c.Setting.Revision && entry.Hash == hash && currentResult.Expires > now()) {
			h.mu.Unlock()
			continue
		}
		if !a.limiter.allow("conversation-help:"+c.User.ID, 6, time.Minute) {
			h.mu.Unlock()
			continue
		}
		h.running[c.User.ID] = true
		h.automatic[key] = autoHelpEntry{Revision: c.Setting.Revision, Hash: hash, Started: now(), Running: true}
		h.mu.Unlock()
		a.wg.Add(1)
		go func(c candidate) {
			defer a.wg.Done()
			a.runAutoHelp(c.User, c.Room, c.Setting, c.Messages, c.Total)
		}(c)
	}
}
func (a *App) runAutoHelp(u User, room string, setting autoHelpSetting, msgs []conversationHelpMessage, total int) {
	h := a.conversationHelp
	key := autoHelpKey(u.ID, room)
	resultID, publicError := "", ""
	defer func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.running, u.ID)
		entry := h.automatic[key]
		if entry.Revision == setting.Revision {
			entry.Running = false
			entry.ResultID = resultID
			entry.Error = publicError
			h.automatic[key] = entry
		}
	}()
	provider, model, effort, e := a.resolveAIRuntime("", "")
	if e != nil {
		publicError = "AI 연결을 확인하세요."
		return
	}
	provider.analysisOnly = true
	system := helpAnalysisSystem("summary", setting.Timezone) + "\n현재 도움을 받는 사용자: " + helpName(u) + "\n자동 대화 도움: 핵심 요약, 약속·할 일(담당자/기한), 확인이 필요한 질문을 간결하게 제시하세요. 답장이 유용하면 마지막에 [답장 초안] 제목 아래 초안 한 개를 쓰세요. 발신자가 누구인지 구분하며 자동 전송하지 마세요."
	budget := a.cfg.AI.ContextMaxBytes
	if budget < 4096 {
		budget = 4096
	}
	if budget > 96<<10 {
		budget = 96 << 10
	}
	for len(msgs) > 1 && helpWireSize(helpWire(system, "", msgs, nil)) > budget {
		msgs = msgs[1:]
	}
	if helpWireSize(helpWire(system, "", msgs, nil)) > budget {
		publicError = "최근 메시지가 너무 길어 자동 분석할 수 없습니다."
		return
	}
	scope := conversationHelpScope{Owner: u.ID, RoomID: room, Hash: helpFingerprint(msgs), Timezone: setting.Timezone, Expires: now() + 900, Total: total, Truncated: total > len(msgs), Action: "automatic"}
	for _, m := range msgs {
		scope.IDs = append(scope.IDs, m.ID)
	}
	validate := func() error {
		return a.store.view(func(st *State) error {
			r, e := helpRoom(st, u, room)
			if e != nil {
				return e
			}
			if r.AutoHelp[u.ID].Revision != setting.Revision {
				return forbidden()
			}
			_, e = helpSnapshot(st, u, scope)
			return e
		})
	}
	if e = validate(); e != nil {
		return
	}
	timeout := provider.TimeoutSeconds
	if timeout < 5 {
		timeout = 120
	}
	if timeout > 300 {
		timeout = 300
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	// Cancel promptly when consent/source access is revoked during a model call.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if validate() != nil {
					cancel()
					return
				}
			}
		}
	}()
	result, e := a.aiExecutor.Execute(ctx, provider, model, effort, helpWire(system, "", msgs, nil))
	if e != nil || ctx.Err() != nil {
		publicError = "AI 분석에 실패했습니다. 잠시 후 자동으로 재시도합니다."
		return
	}
	if validate() != nil {
		return
	}
	if strings.TrimSpace(result.Content) == "" {
		publicError = "AI가 빈 결과를 반환했습니다."
		return
	}
	scope.Text = helpValidCitations(helpBoundedText(result.Content, 32<<10), msgs)
	resultID = a.helpPut(scope)
	if validate() != nil {
		h.mu.Lock()
		delete(h.entries, resultID)
		h.mu.Unlock()
		resultID = ""
	}
}
