package server

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"time"
)

type aiJobManager struct {
	app             *App
	wakeCh          chan struct{}
	mu              sync.Mutex
	runningGlobal   int
	runningProvider map[string]int
	cancels         map[string]context.CancelFunc
	jobs            sync.WaitGroup
}

func newAIJobManager(a *App) *aiJobManager {
	return &aiJobManager{
		app: a, wakeCh: make(chan struct{}, 1),
		runningProvider: map[string]int{},
		cancels:         map[string]context.CancelFunc{},
	}
}

func (m *aiJobManager) wake() {
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

func (m *aiJobManager) cancel(id string) {
	m.mu.Lock()
	cancel := m.cancels[id]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (m *aiJobManager) wait() { m.jobs.Wait() }

func (m *aiJobManager) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer m.cancelAll()
	m.wake()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wakeCh:
			m.dispatch()
		case <-ticker.C:
			m.dispatch()
		}
	}
}

func (m *aiJobManager) cancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, cancel := range m.cancels {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
func (m *aiJobManager) queuedRequests() []AIRequest {
	out := []AIRequest{}
	_ = m.app.store.view(func(st *State) error {
		for _, q := range st.AIRequests {
			if q.Status == aiQueued {
				out = append(out, q)
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (m *aiJobManager) hasCapacity(provider AIProviderConfig) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningGlobal >= m.app.cfg.AI.Concurrency {
		return false
	}
	return m.runningProvider[provider.ID] < provider.Concurrency
}
func (m *aiJobManager) dispatch() {
	for {
		candidates := m.queuedRequests()
		if len(candidates) == 0 {
			return
		}
		progress := false
		for _, q := range candidates {
			provider, pok := findAIProvider(m.app.cfg.AI, q.ProviderID)
			model, mok := findAIModel(m.app.cfg.AI, q.ModelID)
			if !pok || !mok || model.Model != q.Model {
				m.failQueued(q, "AI_CONFIGURATION_CHANGED", "AI 설정이 변경되어 요청을 실행할 수 없습니다.")
				progress = true
				continue
			}
			if ready, _ := aiProviderRuntimeState(provider); !ready {
				m.failQueued(q, "AI_PROVIDER_UNAVAILABLE", "선택한 AI 연결을 현재 사용할 수 없습니다.")
				progress = true
				continue
			}
			if !m.hasCapacity(provider) {
				continue
			}
			if m.start(q, provider, model) {
				progress = true
				break
			}
		}
		if !progress {
			return
		}
	}
}

func (m *aiJobManager) failQueued(q AIRequest, code, message string) {
	_ = m.app.store.update(func(st *State) error {
		current, ok := st.AIRequests[q.ID]
		if !ok || current.Status != aiQueued {
			return errUnchanged
		}
		current.Status, current.ErrorCode, current.Error = aiFailed, code, message
		current.FinishedAt = now()
		st.AIRequests[current.ID] = current
		return nil
	})
	m.app.changed(q.RoomID)
}
func (m *aiJobManager) start(q AIRequest, provider AIProviderConfig, model AIModelConfig) bool {
	started, handled := false, false
	err := m.app.store.update(func(st *State) error {
		current, ok := st.AIRequests[q.ID]
		if !ok || current.Status != aiQueued {
			return errUnchanged
		}
		if sourceErr := validateAIRequestSource(st, current); sourceErr != nil {
			var ae AIExecutionError
			code, message := "AI_SOURCE_UNAVAILABLE", "질문 메시지가 없어 응답을 생성할 수 없습니다."
			if errors.As(sourceErr, &ae) {
				code, message = ae.Code, ae.Public
			}
			current.Status, current.ErrorCode, current.Error = aiFailed, code, message
			current.FinishedAt = now()
			st.AIRequests[current.ID] = current
			q, handled = current, true
			return nil
		}
		current.Status = aiRunning
		current.StartedAt = now()
		st.AIRequests[current.ID] = current
		q = current
		started = true
		return nil
	})
	if err != nil {
		return false
	}
	if !started {
		if handled {
			m.app.changed(q.RoomID)
		}
		return handled
	}
	timeout := provider.TimeoutSeconds
	if timeout <= 0 {
		timeout = m.app.cfg.AI.RequestTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(m.app.ctx, time.Duration(timeout)*time.Second)
	m.mu.Lock()
	m.runningGlobal++
	m.runningProvider[provider.ID]++
	m.cancels[q.ID] = cancel
	m.mu.Unlock()
	m.jobs.Add(1)
	go m.execute(ctx, q, provider, model)
	m.app.changed(q.RoomID)
	return true
}

func (m *aiJobManager) release(q AIRequest) {
	m.mu.Lock()
	if cancel := m.cancels[q.ID]; cancel != nil {
		cancel()
		delete(m.cancels, q.ID)
	}
	if m.runningGlobal > 0 {
		m.runningGlobal--
	}
	if m.runningProvider[q.ProviderID] > 0 {
		m.runningProvider[q.ProviderID]--
	}
	m.mu.Unlock()
	m.jobs.Done()
	m.wake()
}
func (m *aiJobManager) execute(ctx context.Context, q AIRequest, provider AIProviderConfig, model AIModelConfig) {
	defer m.release(q)
	var messages []AIWireMessage
	err := m.app.store.view(func(st *State) error {
		current, ok := st.AIRequests[q.ID]
		if !ok || current.Status != aiRunning {
			return errUnchanged
		}
		var e error
		messages, e = buildAIConversation(st, current, m.app.cfg.AI)
		return e
	})
	if err != nil {
		if err != errUnchanged {
			m.finishError(q, err)
		}
		return
	}
	media, err := m.app.prepareAIMedia(q.OwnerID, q.RoomID, q.UserMessageID)
	if err != nil {
		m.finishError(q, err)
		return
	}
	defer os.RemoveAll(media.Dir)
	ctx = withAIMedia(ctx, media)
	if len(messages) > 0 {
		messages[len(messages)-1].Content += media.Text
	}
	result, err := m.app.aiExecutor.Execute(ctx, provider, model, q.ResolvedEffort, messages)
	if err != nil {
		m.finishError(q, err)
		return
	}
	if len(result.Content) == 0 || len(result.Content) > 256<<10 {
		m.finishError(q, AIExecutionError{Code: "AI_RESPONSE_TOO_LARGE", Public: "AI 응답 크기 제한을 초과했습니다."})
		return
	}
	if err := m.finishSuccess(q, result, media); err != nil {
		m.finishError(q, AIExecutionError{Code: "AI_STORAGE_FAILED", Public: "AI 답변을 저장하지 못했습니다."})
		return
	}
	m.app.changed(q.RoomID)
}

func (m *aiJobManager) finishSuccess(q AIRequest, result AIExecutionResult, inputs ...*aiMedia) error {
	media := firstAIMedia(inputs)
	return m.app.commitAIMedia(q.OwnerID, q.RoomID, media, func(st *State, created *[]string) error {
		current, ok := st.AIRequests[q.ID]
		if !ok || current.Status != aiRunning {
			return errUnchanged
		}
		if sourceErr := validateAIRequestSource(st, current); sourceErr != nil {
			var ae AIExecutionError
			code, message := "AI_SOURCE_UNAVAILABLE", "질문 메시지가 없어 응답 결과를 저장하지 않았습니다."
			if errors.As(sourceErr, &ae) {
				code, message = ae.Code, ae.Public
			}
			current.Status, current.ErrorCode, current.Error = aiFailed, code, message
			current.FinishedAt = now()
			st.AIRequests[current.ID] = current
			return nil
		}
		message, err := addMessage(st, Message{
			RoomID: q.RoomID, UserID: "", Username: "AI", Kind: "text",
			Text: result.Content, ClientID: "ai-" + q.ID, AIRequestID: q.ID,
			AIModelID: q.ModelID, AIEffort: q.ResolvedEffort,
			AIObservedModel: result.ObservedModel,
		}, m.app.cfg.MaxMessages, m.app.cfg.FCM.Enabled)
		if err != nil {
			return err
		}
		if media != nil {
			if e := m.app.saveAIImages(st, q.OwnerID, q.RoomID, "", q.ID, media.Generated, created); e != nil {
				return e
			}
		}
		current.Status = aiCompleted
		current.AnswerMessageID = message.ID
		current.SentEffort = result.SentEffort
		current.ObservedModel = result.ObservedModel
		current.ErrorCode, current.Error = "", ""
		current.FinishedAt = now()
		st.AIRequests[current.ID] = current
		return nil
	})
}

func (m *aiJobManager) finishError(q AIRequest, runErr error) {
	status := aiFailed
	code, message := "AI_REQUEST_FAILED", "AI 응답 생성에 실패했습니다."
	if errors.Is(runErr, context.Canceled) {
		status = aiInterrupted
		code, message = "AI_SERVER_RESTARTED", "서버 상태 변경으로 응답 생성이 중단되었습니다."
	} else if errors.Is(runErr, context.DeadlineExceeded) {
		code, message = "AI_TIMEOUT", "AI 응답 시간이 초과되었습니다."
	} else {
		var ae AIExecutionError
		if errors.As(runErr, &ae) {
			code, message = ae.Code, ae.Public
		}
	}
	changed := false
	_ = m.app.store.update(func(st *State) error {
		current, ok := st.AIRequests[q.ID]
		if !ok || current.Status != aiRunning {
			return errUnchanged
		}
		current.Status, current.ErrorCode, current.Error = status, code, message
		current.FinishedAt = now()
		st.AIRequests[current.ID] = current
		changed = true
		return nil
	})
	if changed {
		m.app.changed(q.RoomID)
	}
}
