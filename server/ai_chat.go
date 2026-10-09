package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	aiQueued      = "queued"
	aiRunning     = "running"
	aiCompleted   = "completed"
	aiFailed      = "failed"
	aiCancelled   = "cancelled"
	aiInterrupted = "interrupted"
)

type AIRequest struct {
	ID              string `json:"id"`
	OwnerID         string `json:"owner_id"`
	RoomID          string `json:"room_id"`
	ClientID        string `json:"client_id"`
	RetryOf         string `json:"retry_of,omitempty"`
	UserMessageID   int64  `json:"user_message_id"`
	AnswerMessageID int64  `json:"answer_message_id,omitempty"`
	Status          string `json:"status"`
	ProviderID      string `json:"provider_id"`
	ModelID         string `json:"model_id"`
	Model           string `json:"model"`
	RequestedEffort string `json:"requested_effort"`
	ResolvedEffort  string `json:"resolved_effort"`
	SentEffort      string `json:"sent_effort,omitempty"`
	ObservedModel   string `json:"observed_model,omitempty"`
	SettingsRev     string `json:"settings_revision"`
	ErrorCode       string `json:"error_code,omitempty"`
	Error           string `json:"error,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	StartedAt       int64  `json:"started_at,omitempty"`
	FinishedAt      int64  `json:"finished_at,omitempty"`
}
type codedAPIError struct {
	status  int
	code    string
	message string
	data    map[string]any
}

func (e codedAPIError) Error() string { return e.message }

func aiAPIError(status int, code, message string) error {
	return codedAPIError{status: status, code: code, message: message}
}

func aiTerminal(status string) bool {
	switch status {
	case aiCompleted, aiFailed, aiCancelled, aiInterrupted:
		return true
	}
	return false
}

func publicAIRequest(q AIRequest) map[string]any {
	return map[string]any{
		"id": q.ID, "room_id": q.RoomID, "client_id": q.ClientID,
		"retry_of": q.RetryOf, "user_message_id": q.UserMessageID,
		"answer_message_id": q.AnswerMessageID, "status": q.Status,
		"model_id": q.ModelID, "requested_effort": q.RequestedEffort,
		"resolved_effort": q.ResolvedEffort, "sent_effort": q.SentEffort,
		"observed_model": q.ObservedModel, "error_code": q.ErrorCode,
		"error": q.Error, "created_at": q.CreatedAt, "started_at": q.StartedAt,
		"finished_at": q.FinishedAt,
	}
}
func findMessageByID(st *State, id int64) (Message, bool) {
	i := sort.Search(len(st.Messages), func(i int) bool { return st.Messages[i].ID >= id })
	if i < len(st.Messages) && st.Messages[i].ID == id {
		return st.Messages[i], true
	}
	return Message{}, false
}

func activeAIRequest(st *State, roomID string) (AIRequest, bool) {
	var best AIRequest
	found := false
	for _, q := range st.AIRequests {
		if q.RoomID == roomID && (q.Status == aiQueued || q.Status == aiRunning) {
			if !found || q.CreatedAt < best.CreatedAt {
				best, found = q, true
			}
		}
	}
	return best, found
}

func findAIRequestByClient(st *State, roomID, ownerID, clientID string) (AIRequest, bool) {
	for _, q := range st.AIRequests {
		if q.RoomID == roomID && q.OwnerID == ownerID && q.ClientID == clientID {
			return q, true
		}
	}
	return AIRequest{}, false
}
func validateAIRequestSource(st *State, q AIRequest) error {
	owner, ok := st.Users[q.OwnerID]
	if !ok || owner.Disabled {
		return AIExecutionError{Code: "AI_OWNER_UNAVAILABLE", Public: "AI 대화 소유자 계정을 사용할 수 없습니다."}
	}
	room, ok := st.Rooms[q.RoomID]
	if !ok || room.Kind != "ai" || room.OwnerID != q.OwnerID || len(room.Members) != 1 {
		return AIExecutionError{Code: "AI_ROOM_UNAVAILABLE", Public: "AI 대화방을 더 이상 사용할 수 없습니다."}
	}
	if _, ok := room.Members[q.OwnerID]; !ok {
		return AIExecutionError{Code: "AI_ROOM_UNAVAILABLE", Public: "AI 대화방을 더 이상 사용할 수 없습니다."}
	}
	source, ok := findMessageByID(st, q.UserMessageID)
	if !ok || source.RoomID != q.RoomID || source.UserID != q.OwnerID || source.Kind != "text" {
		return AIExecutionError{Code: "AI_SOURCE_UNAVAILABLE", Public: "질문 메시지가 없어 응답을 생성할 수 없습니다."}
	}
	return nil
}
func interruptAIRequests(s *Store) error {
	changed := false
	err := s.update(func(st *State) error {
		for id, q := range st.AIRequests {
			if q.Status != aiQueued && q.Status != aiRunning {
				continue
			}
			q.Status = aiInterrupted
			q.ErrorCode = "AI_SERVER_RESTARTED"
			q.Error = "서버가 다시 시작되어 응답 생성이 중단되었습니다."
			q.FinishedAt = now()
			st.AIRequests[id] = q
			changed = true
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
	return err
}

func activeAISettingsRevision(c Config) string {
	b, err := configBytes(c)
	if err != nil {
		return ""
	}
	return digest(string(b))
}
func publicAICatalog(ai AIConfig) map[string]any {
	models := []map[string]any{}
	available := map[string]bool{}
	for _, m := range ai.Models {
		if !m.Enabled {
			continue
		}
		p, ok := findAIProvider(ai, m.ProviderID)
		if !ok {
			continue
		}
		ready, _ := aiProviderRuntimeState(p)
		if !ready {
			continue
		}
		models = append(models, map[string]any{
			"id": m.ID, "name": m.Name, "provider_name": p.Name,
			"default_effort": m.DefaultEffort, "efforts": effectiveAIEfforts(m),
		})
		available[m.ID] = true
	}
	defaultID := ai.DefaultModelID
	if !available[defaultID] {
		defaultID = ""
	}
	return map[string]any{
		"enabled": ai.Enabled, "default_model_id": defaultID,
		"default_unavailable": ai.Enabled && ai.DefaultModelID != "" && defaultID == "",
		"models":              models,
	}
}

func (a *App) aiCatalog(w http.ResponseWriter, r *http.Request, u User) {
	respond(w, 200, publicAICatalog(a.cfg.AI))
}

func (a *App) resolveAIRuntime(modelID, effort string) (AIProviderConfig, AIModelConfig, string, error) {
	if !a.cfg.AI.Enabled {
		return AIProviderConfig{}, AIModelConfig{}, "", aiAPIError(503, "AI_DISABLED", "AI 대화 기능이 비활성화되어 있습니다.")
	}
	p, m, resolved, err := resolveAISelection(a.cfg.AI, modelID, effort)
	if err != nil {
		return AIProviderConfig{}, AIModelConfig{}, "", err
	}
	if ready, _ := aiProviderRuntimeState(p); !ready {
		return AIProviderConfig{}, AIModelConfig{}, "", aiAPIError(503, "AI_PROVIDER_UNAVAILABLE", "선택한 AI 연결을 현재 사용할 수 없습니다.")
	}
	return p, m, resolved, nil
}
func (a *App) createAIRoom(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Name    string `json:"name"`
		ModelID string `json:"model_id"`
		Effort  string `json:"effort"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	_, model, effort, err := a.resolveAIRuntime(strings.TrimSpace(in.ModelID), strings.TrimSpace(in.Effort))
	if err != nil {
		fail(w, err)
		return
	}
	if _, err = a.ensureDefaultAgentProject(u); err != nil {
		fail(w, apiError{503, "AI 작업공간을 자동으로 준비하지 못했습니다."})
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "AI · " + model.Name
	}
	if len([]rune(name)) > 80 {
		fail(w, bad("AI 대화 이름은 80자 이하입니다."))
		return
	}
	room := Room{
		ID: randomID(), Name: name, OwnerID: u.ID, Kind: "ai", Created: now(),
		Members: map[string]int64{u.ID: 0}, AIProfileID: defaultAIProfileID,
		AIModelID: model.ID, AIEffort: effort,
	}
	err = a.store.update(func(st *State) error {
		if len(st.Rooms) >= 1000 || st.Users[u.ID].Disabled {
			return forbidden()
		}
		st.Rooms[room.ID] = room
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(room.ID)
	respond(w, 201, map[string]any{"id": room.ID, "kind": room.Kind})
}
func (a *App) updateAIRoom(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		ModelID string `json:"model_id"`
		Effort  string `json:"effort"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	_, model, effort, err := a.resolveAIRuntime(strings.TrimSpace(in.ModelID), strings.TrimSpace(in.Effort))
	if err != nil {
		fail(w, err)
		return
	}
	roomID := r.PathValue("room")
	err = a.store.update(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || room.Kind != "ai" || room.OwnerID != u.ID || len(room.Members) != 1 {
			return forbidden()
		}
		if q, active := activeAIRequest(st, roomID); active {
			return codedAPIError{
				status: 409, code: "AI_REQUEST_ACTIVE",
				message: "응답 생성 중에는 모델이나 추론 설정을 바꿀 수 없습니다.",
				data:    map[string]any{"request": publicAIRequest(q)},
			}
		}
		room.AIModelID, room.AIEffort = model.ID, effort
		st.Rooms[roomID] = room
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(roomID)
	respond(w, 200, map[string]any{"ok": true, "model_id": model.ID, "effort": effort})
}
func (a *App) sendAIMessage(w http.ResponseWriter, r *http.Request, u User, text, clientID string) {
	roomID := r.PathValue("room")
	var room Room
	var duplicate AIRequest
	var duplicateMessage Message
	foundDuplicate := false
	err := a.store.view(func(st *State) error {
		v, ok := member(st, roomID, u.ID)
		if !ok || v.Kind != "ai" || v.OwnerID != u.ID || len(v.Members) != 1 {
			return forbidden()
		}
		room = v
		if q, ok := findAIRequestByClient(st, roomID, u.ID, clientID); ok {
			m, mok := findMessageByID(st, q.UserMessageID)
			if !mok || m.Text != text {
				return aiAPIError(409, "AI_CLIENT_ID_CONFLICT", "같은 client_id로 다른 메시지를 보낼 수 없습니다.")
			}
			duplicate, duplicateMessage, foundDuplicate = q, m, true
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if foundDuplicate {
		respond(w, 202, map[string]any{"message": duplicateMessage, "request": publicAIRequest(duplicate)})
		return
	}
	if _, workspaceErr := a.ensureDefaultAgentProject(u); workspaceErr != nil {
		fail(w, apiError{503, "AI 작업공간을 자동으로 준비하지 못했습니다."})
		return
	}
	if !a.cfg.AI.Enabled {
		fail(w, aiAPIError(503, "AI_DISABLED", "AI 대화 기능이 비활성화되어 있습니다."))
		return
	}
	provider, _, _, runtimeErr := a.resolveAIRuntime(room.AIModelID, room.AIEffort)
	if runtimeErr != nil {
		fail(w, runtimeErr)
		return
	}
	if provider.Type == "claude_cli" || provider.Type == "codex_cli" {
		a.sendAutomaticAgentMessage(w, r, u, room, text, clientID)
		return
	}
	if !a.limiter.allow("ai:"+u.ID, a.cfg.AI.PerUserPerMinute, time.Minute) {
		fail(w, apiError{429, "AI 요청 횟수가 너무 많습니다. 잠시 후 다시 시도하세요."})
		return
	}
	var msg Message
	var request AIRequest
	created := false
	settingsRev := activeAISettingsRevision(a.cfg)

	err = a.store.update(func(st *State) error {
		v, ok := member(st, roomID, u.ID)
		if !ok || v.Kind != "ai" || v.OwnerID != u.ID || len(v.Members) != 1 {
			return forbidden()
		}
		if q, ok := findAIRequestByClient(st, roomID, u.ID, clientID); ok {
			m, mok := findMessageByID(st, q.UserMessageID)
			if !mok || m.Text != text {
				return aiAPIError(409, "AI_CLIENT_ID_CONFLICT", "같은 client_id로 다른 메시지를 보낼 수 없습니다.")
			}
			request, msg = q, m
			return errUnchanged
		}
		if agentJob, active := activeAgentJobForRoom(st, roomID); active {
			return codedAPIError{
				status: 409, code: "AGENT_REQUEST_ACTIVE",
				message: "이 AI 대화방에서 작업 모드를 실행하고 있습니다.",
				data:    map[string]any{"agent_job": publicAgentJob(agentJob)},
			}
		}
		if q, active := activeAIRequest(st, roomID); active {
			return codedAPIError{
				status: 409, code: "AI_REQUEST_ACTIVE",
				message: "이 AI 대화방에서 이미 응답을 생성하고 있습니다.",
				data:    map[string]any{"request": publicAIRequest(q)},
			}
		}
		queued := 0
		for _, q := range st.AIRequests {
			if q.Status == aiQueued {
				queued++
			}
		}
		if queued >= a.cfg.AI.QueueLimit {
			return aiAPIError(503, "AI_QUEUE_FULL", "AI 요청 대기열이 가득 찼습니다.")
		}
		if !aiMessageCapacityAvailable(st, a.cfg.MaxMessages, 2) {
			return aiAPIError(503, "AI_MESSAGE_CAPACITY", "AI 질문과 답변을 저장할 공간이 부족합니다. 오래된 메시지를 정리하거나 최대 메시지 수를 늘리세요.")
		}
		provider, model, resolved, e := resolveAISelection(a.cfg.AI, v.AIModelID, v.AIEffort)
		if e != nil {
			return e
		}
		msg, e = addMessage(st, Message{
			RoomID: roomID, UserID: u.ID, Username: u.Username,
			Kind: "text", Text: text, ClientID: clientID,
		}, a.cfg.MaxMessages, false)
		if e != nil {
			return e
		}
		request = AIRequest{
			ID: randomID(), OwnerID: u.ID, RoomID: roomID, ClientID: clientID,
			UserMessageID: msg.ID, Status: aiQueued, ProviderID: provider.ID,
			ModelID: model.ID, Model: model.Model, RequestedEffort: v.AIEffort,
			ResolvedEffort: resolved, SettingsRev: settingsRev, CreatedAt: now(),
		}
		st.AIRequests[request.ID] = request
		created = true
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(roomID)
	if created && a.aiJobs != nil {
		a.aiJobs.wake()
	}
	respond(w, 202, map[string]any{"message": msg, "request": publicAIRequest(request)})
}

func (a *App) getAIRequest(w http.ResponseWriter, r *http.Request, u User) {
	roomID, requestID := r.PathValue("room"), r.PathValue("request")
	var q AIRequest
	err := a.store.view(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || room.Kind != "ai" || room.OwnerID != u.ID {
			return forbidden()
		}
		var found bool
		q, found = st.AIRequests[requestID]
		if !found || q.RoomID != roomID || q.OwnerID != u.ID {
			return notfound()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, publicAIRequest(q))
}
func (a *App) cancelAIRequest(w http.ResponseWriter, r *http.Request, u User) {
	roomID, requestID := r.PathValue("room"), r.PathValue("request")
	cancelRunning := false
	var out AIRequest
	err := a.store.update(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || room.Kind != "ai" || room.OwnerID != u.ID {
			return forbidden()
		}
		q, ok := st.AIRequests[requestID]
		if !ok || q.RoomID != roomID || q.OwnerID != u.ID {
			return notfound()
		}
		if q.Status == aiCancelled {
			out = q
			return errUnchanged
		}
		if q.Status != aiQueued && q.Status != aiRunning {
			return aiAPIError(409, "AI_REQUEST_TERMINAL", "이미 종료된 AI 요청은 중지할 수 없습니다.")
		}
		cancelRunning = q.Status == aiRunning
		q.Status = aiCancelled
		q.ErrorCode = "AI_CANCELLED"
		q.Error = "응답 생성을 중지했습니다."
		q.FinishedAt = now()
		st.AIRequests[q.ID] = q
		out = q
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if cancelRunning && a.aiJobs != nil {
		a.aiJobs.cancel(requestID)
	}
	a.changed(roomID)
	respond(w, 200, publicAIRequest(out))
}
func (a *App) retryAIRequest(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		ClientID string `json:"client_id"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if !clientIDPattern.MatchString(in.ClientID) {
		fail(w, bad("재시도에는 새 client_id가 필요합니다."))
		return
	}
	roomID, requestID := r.PathValue("room"), r.PathValue("request")
	var room Room
	err := a.store.view(func(st *State) error {
		v, ok := member(st, roomID, u.ID)
		if !ok || v.Kind != "ai" || v.OwnerID != u.ID {
			return forbidden()
		}
		q, ok := st.AIRequests[requestID]
		if !ok || q.OwnerID != u.ID || q.RoomID != roomID {
			return notfound()
		}
		if q.Status == aiCompleted || !aiTerminal(q.Status) {
			return aiAPIError(409, "AI_RETRY_NOT_ALLOWED", "이 AI 요청은 재시도할 수 없습니다.")
		}
		room = v
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if _, _, _, err = a.resolveAIRuntime(room.AIModelID, room.AIEffort); err != nil {
		fail(w, err)
		return
	}
	if !a.limiter.allow("ai:"+u.ID, a.cfg.AI.PerUserPerMinute, time.Minute) {
		fail(w, apiError{429, "AI 요청 횟수가 너무 많습니다. 잠시 후 다시 시도하세요."})
		return
	}
	var out AIRequest
	created := false
	settingsRev := activeAISettingsRevision(a.cfg)
	err = a.store.update(func(st *State) error {
		v, ok := member(st, roomID, u.ID)
		if !ok || v.Kind != "ai" || v.OwnerID != u.ID {
			return forbidden()
		}
		original, ok := st.AIRequests[requestID]
		if !ok || original.OwnerID != u.ID || original.RoomID != roomID {
			return notfound()
		}
		if original.Status == aiCompleted || !aiTerminal(original.Status) {
			return aiAPIError(409, "AI_RETRY_NOT_ALLOWED", "이 AI 요청은 재시도할 수 없습니다.")
		}
		if existing, ok := findAIRequestByClient(st, roomID, u.ID, in.ClientID); ok {
			if existing.RetryOf == original.ID {
				out = existing
				return errUnchanged
			}
			return aiAPIError(409, "AI_CLIENT_ID_CONFLICT", "이미 사용한 client_id입니다.")
		}
		if q, active := activeAIRequest(st, roomID); active {
			return codedAPIError{status: 409, code: "AI_REQUEST_ACTIVE", message: "이미 응답을 생성하고 있습니다.", data: map[string]any{"request": publicAIRequest(q)}}
		}
		if source, ok := findMessageByID(st, original.UserMessageID); !ok || source.Kind != "text" || source.UserID != u.ID {
			return aiAPIError(409, "AI_SOURCE_UNAVAILABLE", "원본 메시지가 없어 재시도할 수 없습니다.")
		}
		queued := 0
		for _, q := range st.AIRequests {
			if q.Status == aiQueued {
				queued++
			}
		}
		if queued >= a.cfg.AI.QueueLimit {
			return aiAPIError(503, "AI_QUEUE_FULL", "AI 요청 대기열이 가득 찼습니다.")
		}
		if !aiMessageCapacityAvailable(st, a.cfg.MaxMessages, 1) {
			return aiAPIError(503, "AI_MESSAGE_CAPACITY", "AI 답변을 저장할 공간이 부족합니다. 오래된 메시지를 정리하거나 최대 메시지 수를 늘리세요.")
		}
		provider, model, resolved, e := resolveAISelection(a.cfg.AI, v.AIModelID, v.AIEffort)
		if e != nil {
			return e
		}
		out = AIRequest{
			ID: randomID(), OwnerID: u.ID, RoomID: roomID, ClientID: in.ClientID,
			RetryOf: original.ID, UserMessageID: original.UserMessageID,
			Status: aiQueued, ProviderID: provider.ID, ModelID: model.ID,
			Model: model.Model, RequestedEffort: v.AIEffort,
			ResolvedEffort: resolved, SettingsRev: settingsRev, CreatedAt: now(),
		}
		st.AIRequests[out.ID] = out
		created = true
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(roomID)
	if created && a.aiJobs != nil {
		a.aiJobs.wake()
	}
	respond(w, 202, publicAIRequest(out))
}
func buildAIConversation(st *State, q AIRequest, cfg AIConfig) ([]AIWireMessage, error) {
	if err := validateAIRequestSource(st, q); err != nil {
		return nil, err
	}
	current, _ := findMessageByID(st, q.UserMessageID)
	type pair struct {
		user   Message
		answer Message
	}
	pairs := []pair{}
	for _, prior := range st.AIRequests {
		if prior.RoomID != q.RoomID || prior.OwnerID != q.OwnerID {
			continue
		}
		if prior.Status != aiCompleted || prior.AnswerMessageID == 0 || prior.UserMessageID >= q.UserMessageID {
			continue
		}

		userMsg, uok := findMessageByID(st, prior.UserMessageID)
		answerMsg, aok := findMessageByID(st, prior.AnswerMessageID)
		if !uok || !aok {
			continue
		}
		if userMsg.Kind != "text" || answerMsg.Kind != "text" ||
			userMsg.UserID != q.OwnerID || answerMsg.UserID != "" ||
			answerMsg.AIRequestID != prior.ID {
			continue
		}
		pairs = append(pairs, pair{user: userMsg, answer: answerMsg})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].user.ID == pairs[j].user.ID {
			return pairs[i].answer.ID < pairs[j].answer.ID
		}
		return pairs[i].user.ID < pairs[j].user.ID
	})
	cost := len(current.Text) + 32
	if cfg.SystemPrompt != "" {
		cost += len(cfg.SystemPrompt) + 32
	}
	if cost > cfg.ContextMaxBytes {
		return nil, AIExecutionError{Code: "AI_CONTEXT_TOO_LARGE", Public: "질문이 AI 문맥 제한을 초과했습니다."}
	}
	from := len(pairs)
	for i := len(pairs) - 1; i >= 0; i-- {
		pairCost := len(pairs[i].user.Text) + len(pairs[i].answer.Text) + 64
		if cost+pairCost > cfg.ContextMaxBytes {
			break
		}
		cost += pairCost
		from = i
	}
	messages := make([]AIWireMessage, 0, 2+(len(pairs)-from)*2)
	if cfg.SystemPrompt != "" {
		messages = append(messages, AIWireMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	for _, p := range pairs[from:] {
		messages = append(messages,
			AIWireMessage{Role: "user", Content: p.user.Text},
			AIWireMessage{Role: "assistant", Content: p.answer.Text},
		)
	}
	messages = append(messages, AIWireMessage{Role: "user", Content: current.Text})
	return messages, nil
}
func (a *App) testAISettings(w http.ResponseWriter, r *http.Request, u User) {
	if err := a.requireSettingsAdmin(r, u); err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Revision string      `json:"revision"`
		AI       *AISettings `json:"ai,omitempty"`
		ModelID  string      `json:"model_id"`
		Effort   string      `json:"effort"`
		Live     bool        `json:"live"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	a.settingsMu.Lock()
	saved, revision, err := a.readSavedSettings()
	if err == nil && in.Revision != revision {
		err = apiError{409, "다른 관리자 또는 설정 작업으로 내용이 변경되었습니다. 다시 읽고 시도하세요."}
	}
	if err == nil {
		err = unlockConfiguration(&saved, a.store)
	}
	if err == nil && in.AI != nil {
		saved.AI, err = a.candidateAISettings(saved.AI, *in.AI)
	}
	a.settingsMu.Unlock()
	if err != nil {
		fail(w, err)
		return
	}
	provider, model, effort, err := resolveAISelection(saved.AI, in.ModelID, in.Effort)
	if err != nil {
		fail(w, err)
		return
	}
	diagnostics := aiCLIDiagnostics(provider)
	configured, configReason := aiProviderConfigurationState(provider)
	if !configured {
		respond(w, 200, map[string]any{
			"ok": false, "stage": "configuration", "reason": configReason, "cli_diagnostics": diagnostics,
			"provider_id": provider.ID, "model_id": model.ID,
			"saved": in.AI == nil, "live": false,
		})
		return
	}
	resolvedExecutable := ""
	if provider.Type == "claude_cli" || provider.Type == "codex_cli" {
		if found, ok := resolveAIConfigurationExecutable(provider); ok {
			resolvedExecutable = found
		}
	}
	runtimeReady, runtimeReason := aiProviderRuntimeState(provider)
	if !in.Live {
		respond(w, 200, map[string]any{
			"ok": true, "stage": "local_check", "reason": "configured", "cli_diagnostics": diagnostics,
			"provider_id": provider.ID, "model_id": model.ID,
			"effort": effort, "saved": in.AI == nil,
			"live": false, "cost_may_apply": false,
			"runtime_ready": runtimeReady, "runtime_reason": runtimeReason,
			"resolved_executable": resolvedExecutable, "executable_found": resolvedExecutable != "",
		})
		return
	}
	if !runtimeReady {
		respond(w, 200, map[string]any{
			"ok": false, "stage": "runtime", "reason": runtimeReason, "cli_diagnostics": diagnostics,
			"provider_id": provider.ID, "model_id": model.ID,
			"saved": in.AI == nil, "live": true,
		})
		return
	}
	timeout := provider.TimeoutSeconds
	if timeout <= 0 {
		timeout = saved.AI.RequestTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
	defer cancel()
	result, runErr := a.aiExecutor.Execute(ctx, provider, model, effort, []AIWireMessage{
		{Role: "user", Content: "다음 문자열만 답하세요: OK"},
	})
	if runErr != nil {
		code, message := "AI_TEST_FAILED", "AI 실제 호출 테스트에 실패했습니다."
		var ae AIExecutionError
		if errors.As(runErr, &ae) {
			code, message = ae.Code, ae.Public
		}
		respond(w, 200, map[string]any{
			"ok": false, "stage": "live_call", "error_code": code, "cli_diagnostics": diagnostics,
			"error": message, "provider_id": provider.ID, "model_id": model.ID,
			"saved": in.AI == nil, "live": true, "cost_may_apply": true,
		})
		return
	}
	respond(w, 200, map[string]any{
		"ok": true, "stage": "live_call", "provider_id": provider.ID, "cli_diagnostics": diagnostics,
		"model_id": model.ID, "requested_effort": effort,
		"sent_effort": result.SentEffort, "observed_model": result.ObservedModel,
		"saved": in.AI == nil, "live": true, "cost_may_apply": true,
	})
}
