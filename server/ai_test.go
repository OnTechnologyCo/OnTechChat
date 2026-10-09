package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type aiTestCall struct {
	Provider AIProviderConfig
	Model    AIModelConfig
	Effort   string
	Messages []AIWireMessage
}

type fakeAIExecutor struct {
	mu      sync.Mutex
	calls   []aiTestCall
	replies []string
	wait    <-chan struct{}
}

func (f *fakeAIExecutor) Execute(ctx context.Context, p AIProviderConfig, m AIModelConfig, effort string, messages []AIWireMessage) (AIExecutionResult, error) {
	copyMessages := append([]AIWireMessage(nil), messages...)
	f.mu.Lock()
	f.calls = append(f.calls, aiTestCall{Provider: p, Model: m, Effort: effort, Messages: copyMessages})
	index := len(f.calls) - 1
	reply := "OK"
	if index < len(f.replies) {
		reply = f.replies[index]
	}
	wait := f.wait
	f.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return AIExecutionResult{}, ctx.Err()
		}
	}
	return AIExecutionResult{Content: reply, SentEffort: effort}, nil
}

func (f *fakeAIExecutor) snapshot() []aiTestCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]aiTestCall(nil), f.calls...)
}

type fakeAIRunner struct {
	mu         sync.Mutex
	executable string
	args       []string
	stdin      string
	workDir    string
	stdout     []byte
}

func (r *fakeAIRunner) Run(_ context.Context, executable string, args []string, stdin, workDir string) ([]byte, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executable = executable
	r.args = append([]string(nil), args...)
	r.stdin = stdin
	r.workDir = workDir
	return append([]byte(nil), r.stdout...), []byte("진행 로그는 답변에 포함되지 않음"), nil
}

func aiTestConfig() AIConfig {
	return AIConfig{
		Enabled: true, DefaultModelID: "test-model", RequestTimeoutSeconds: 5,
		Concurrency: 2, QueueLimit: 8, PerUserPerMinute: 60, ContextMaxBytes: 64 << 10,
		SystemPrompt: "테스트 시스템 지침",
		Providers:    []AIProviderConfig{{ID: "test", Name: "테스트 API", Type: "openai_compatible", Enabled: true, TimeoutSeconds: 5, Concurrency: 2, BaseURL: "http://127.0.0.1:1/v1", apiKey: "test-key"}},
		Models:       []AIModelConfig{{ID: "test-model", ProviderID: "test", Name: "테스트 모델", Model: "wire-test", Enabled: true, DefaultEffort: aiProviderDefaultEffort, AllowedEfforts: []string{"low", "high"}, SupportedEfforts: []string{"low", "high"}}},
	}
}
func TestAIOpenAIWireModelEffortAndTruncation(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	var paths, auth []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		mu.Lock()
		bodies = append(bodies, body)
		paths = append(paths, r.URL.Path)
		auth = append(auth, r.Header.Get("Authorization"))
		call := len(bodies)
		mu.Unlock()
		finish := "stop"
		if call == 3 {
			finish = "length"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"observed-test","choices":[{"message":{"content":"OK"},"finish_reason":"`+finish+`"}]}`)
	}))
	defer server.Close()
	x := &defaultAIExecutor{httpClient: server.Client(), runner: &fakeAIRunner{}}
	p := AIProviderConfig{Type: "openai_compatible", BaseURL: server.URL + "/v1", apiKey: "secret"}
	m := AIModelConfig{Model: "wire-model"}
	result, err := x.executeOpenAI(context.Background(), p, m, "high", []AIWireMessage{{Role: "user", Content: "질문"}})
	if err != nil || result.ObservedModel != "observed-test" || result.SentEffort != "high" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err = x.executeOpenAI(context.Background(), p, m, aiProviderDefaultEffort, []AIWireMessage{{Role: "user", Content: "기본"}}); err != nil {
		t.Fatal(err)
	}
	_, err = x.executeOpenAI(context.Background(), p, m, "high", []AIWireMessage{{Role: "user", Content: "긴 답변"}})
	var ae AIExecutionError
	if !errors.As(err, &ae) || ae.Code != "AI_RESPONSE_TRUNCATED" {
		t.Fatalf("truncation error=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 || paths[0] != "/v1/chat/completions" || auth[0] != "Bearer secret" {
		t.Fatalf("wire=%v paths=%v auth=%v", bodies, paths, auth)
	}
	if bodies[0]["model"] != "wire-model" || bodies[0]["reasoning_effort"] != "high" {
		t.Fatalf("explicit wire=%v", bodies[0])
	}
	if _, ok := bodies[1]["reasoning_effort"]; ok {
		t.Fatalf("provider_default leaked to wire: %v", bodies[1])
	}
}

func TestAIClaudeArgumentsAndObservedModel(t *testing.T) {
	runner := &fakeAIRunner{stdout: []byte(`{"result":"정상 응답","is_error":false,"modelUsage":{"claude-sonnet-5":{"canonicalModel":"claude-sonnet-5"}}}`)}
	x := &defaultAIExecutor{httpClient: http.DefaultClient, runner: runner}
	result, err := x.executeClaude(context.Background(), AIProviderConfig{Executable: "claude"}, AIModelConfig{Model: "claude-test"}, "high", []AIWireMessage{{Role: "user", Content: "한글 \"따옴표\"\n둘째 줄"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "정상 응답" || result.SentEffort != "high" || result.ObservedModel != "claude-sonnet-5" {
		t.Fatalf("unexpected result %+v", result)
	}
	joined := strings.Join(runner.args, " ")
	for _, flag := range []string{"--safe-mode", "--restricted", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence", "--permission-prompts", "none", "--model", "claude-test", "--effort", "high"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("missing %s in %q", flag, joined)
		}
	}
	if !strings.Contains(runner.stdin, `한글 \"따옴표\"\n둘째 줄`) {
		t.Fatalf("stdin lost content: %s", runner.stdin)
	}
}
func TestAISettingsSecretRetentionAndClear(t *testing.T) {
	store, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &App{store: store}
	old := defaultAIConfig()
	in := aiSettingsView(old)
	for i := range in.Providers {
		if in.Providers[i].ID == "openai" {
			in.Providers[i].APIKey = "super-secret-value"
		}
	}
	next, err := a.candidateAISettings(old, in)
	if err != nil {
		t.Fatal(err)
	}
	view := aiSettingsView(next)
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "super-secret-value") || strings.Contains(string(raw), "api_key_encrypted") {
		t.Fatalf("secret exposed: %s", raw)
	}
	var encrypted string
	for _, p := range next.Providers {
		if p.ID == "openai" {
			encrypted = p.APIKeyEncrypted
		}
	}
	if encrypted == "" {
		t.Fatal("API key was not sealed")
	}
	retained, err := a.candidateAISettings(next, view)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range retained.Providers {
		if p.ID == "openai" && p.APIKeyEncrypted != encrypted {
			t.Fatal("blank key input did not retain secret")
		}
	}
	clear := aiSettingsView(retained)
	for i := range clear.Providers {
		if clear.Providers[i].ID == "openai" {
			clear.Providers[i].ClearAPIKey = true
		}
	}
	cleared, err := a.candidateAISettings(retained, clear)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cleared.Providers {
		if p.ID == "openai" && p.APIKeyEncrypted != "" {
			t.Fatal("API key was not cleared")
		}
	}
}
func TestAIContextIsolationAndCompletedPairs(t *testing.T) {
	st := emptyState()
	st.Users["owner"] = User{ID: "owner", Username: "owner"}
	st.Users["other"] = User{ID: "other", Username: "other"}
	st.Rooms["ai"] = Room{ID: "ai", Kind: "ai", OwnerID: "owner", Members: map[string]int64{"owner": 0}}
	st.Rooms["other-ai"] = Room{ID: "other-ai", Kind: "ai", OwnerID: "other", Members: map[string]int64{"other": 0}}
	st.Messages = []Message{
		{ID: 1, RoomID: "ai", UserID: "owner", Kind: "text", Text: "첫 질문"},
		{ID: 2, RoomID: "ai", UserID: "", Kind: "text", Text: "첫 답변", AIRequestID: "done"},
		{ID: 3, RoomID: "ai", UserID: "owner", Kind: "text", Text: "실패한 질문"},
		{ID: 4, RoomID: "other-ai", UserID: "other", Kind: "text", Text: "다른 사용자 비밀"},
		{ID: 5, RoomID: "ai", UserID: "owner", Kind: "text", Text: "현재 질문"},
	}
	st.AIRequests["done"] = AIRequest{ID: "done", OwnerID: "owner", RoomID: "ai", UserMessageID: 1, AnswerMessageID: 2, Status: aiCompleted}
	st.AIRequests["failed"] = AIRequest{ID: "failed", OwnerID: "owner", RoomID: "ai", UserMessageID: 3, Status: aiFailed}
	current := AIRequest{ID: "current", OwnerID: "owner", RoomID: "ai", UserMessageID: 5, Status: aiRunning}
	messages, err := buildAIConversation(&st, current, AIConfig{SystemPrompt: "관리자 지침", ContextMaxBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(messages)
	text := string(raw)
	for _, want := range []string{"관리자 지침", "첫 질문", "첫 답변", "현재 질문"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for _, forbidden := range []string{"실패한 질문", "다른 사용자 비밀"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("context leak %q in %s", forbidden, text)
		}
	}
}
func TestAIAnswerCapacityReservation(t *testing.T) {
	st := emptyState()
	st.Messages = []Message{{ID: 1, RoomID: "ai", UserID: "owner", Kind: "text", Text: "질문"}}
	st.NextMessage = 2
	st.AIRequests["req"] = AIRequest{ID: "req", RoomID: "ai", OwnerID: "owner", Status: aiRunning, UserMessageID: 1}
	if _, err := addMessage(&st, Message{RoomID: "x", UserID: "other", Kind: "text", Text: "침범"}, 2, false); err == nil {
		t.Fatal("active AI answer reservation was consumed by another message")
	}
	answer, err := addMessage(&st, Message{RoomID: "ai", UserID: "", Kind: "text", Text: "답변", ClientID: "ai-req", AIRequestID: "req"}, 2, false)
	if err != nil || answer.ID == 0 {
		t.Fatalf("reserved AI answer slot failed: %+v %v", answer, err)
	}
}

func TestAICodexExecutesCompletedResponse(t *testing.T) {
	exe, _ := os.Executable()
	runner := &fakeAIRunner{stdout: []byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"OK\"}}\n{\"type\":\"turn.completed\"}\n")}
	x := &defaultAIExecutor{runner: runner}
	result, err := x.Execute(context.Background(), AIProviderConfig{Type: "codex_cli", Executable: exe}, AIModelConfig{Model: "gpt-6-astra"}, "high", []AIWireMessage{{Role: "user", Content: "hello"}})
	if err != nil || result.Content != "OK" || result.SentEffort != "high" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	args := strings.Join(runner.args, " ")
	for _, want := range []string{"--sandbox read-only", "--ignore-user-config", "--disable shell_tool", "gpt-6-astra", "model_reasoning_effort=high"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q: %s", want, args)
		}
	}
	if runner.workDir != "" {
		t.Fatal("Codex must use disposable CWD")
	}
}

func waitAIRequest(t *testing.T, a *App, id, want string) AIRequest {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var q AIRequest
		_ = a.store.view(func(st *State) error { q = st.AIRequests[id]; return nil })
		if q.Status == want {
			return q
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("AI request %s did not reach %s", id, want)
	return AIRequest{}
}
func TestAIAPIDuplicateOwnershipCancelAndRetry(t *testing.T) {
	a, server, _, _, _ := testApp(t)
	a.cfg.AI = aiTestConfig()
	bobToken := loginTest(t, server, "bob")
	eveToken := loginTest(t, server, "eve")
	status, body, _ := request(t, server, bobToken, http.MethodPost, "/api/rooms/ai", map[string]any{"name": "개인 AI", "model_id": "test-model", "effort": "low"})
	expectStatus(t, status, http.StatusCreated, body)
	roomID := decodeMap(t, body)["id"].(string)
	messageBody := map[string]any{"text": "중복 없이 실행", "client_id": "client-ai-0001"}
	status, body, _ = request(t, server, bobToken, http.MethodPost, "/api/rooms/"+roomID+"/messages", messageBody)
	expectStatus(t, status, http.StatusAccepted, body)
	first := decodeMap(t, body)
	firstRequest := first["request"].(map[string]any)
	requestID := firstRequest["id"].(string)
	messageID := first["message"].(map[string]any)["id"].(float64)
	status, body, _ = request(t, server, bobToken, http.MethodPost, "/api/rooms/"+roomID+"/messages", messageBody)
	expectStatus(t, status, http.StatusAccepted, body)
	duplicate := decodeMap(t, body)
	if duplicate["request"].(map[string]any)["id"] != requestID || duplicate["message"].(map[string]any)["id"].(float64) != messageID {
		t.Fatal("duplicate request created new state")
	}
	status, body, _ = request(t, server, bobToken, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]any{"text": "두 번째", "client_id": "client-ai-0002"})
	expectStatus(t, status, http.StatusConflict, body)
	status, body, _ = request(t, server, eveToken, http.MethodGet, "/api/rooms/"+roomID+"/ai/requests/"+requestID, nil)
	expectStatus(t, status, http.StatusForbidden, body)
	status, body, _ = request(t, server, eveToken, http.MethodPost, "/api/rooms/"+roomID+"/ai/requests/"+requestID+"/cancel", map[string]any{})
	expectStatus(t, status, http.StatusForbidden, body)
	status, body, _ = request(t, server, bobToken, http.MethodPost, "/api/rooms/"+roomID+"/members", map[string]any{"username": "eve"})
	expectStatus(t, status, http.StatusForbidden, body)
	status, body, _ = request(t, server, bobToken, http.MethodPost, "/api/rooms/"+roomID+"/ai/requests/"+requestID+"/cancel", map[string]any{})
	expectStatus(t, status, http.StatusOK, body)
	status, body, _ = request(t, server, bobToken, http.MethodPost, "/api/rooms/"+roomID+"/ai/requests/"+requestID+"/retry", map[string]any{"client_id": "client-ai-retry-0001"})
	expectStatus(t, status, http.StatusAccepted, body)
	var messageCount, requestCount int
	_ = a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.RoomID == roomID {
				messageCount++
			}
		}
		for _, q := range st.AIRequests {
			if q.RoomID == roomID {
				requestCount++
			}
		}
		return nil
	})
	if messageCount != 1 || requestCount != 2 {
		t.Fatalf("unexpected persisted state messages=%d requests=%d", messageCount, requestCount)
	}
}
func TestAIWorkerPersistsAnswerAndCarriesCompletedContext(t *testing.T) {
	a, server, _, _, _ := testApp(t)
	a.cfg.AI = aiTestConfig()
	fake := &fakeAIExecutor{replies: []string{"첫 답변", "둘째 답변"}}
	a.aiExecutor = fake
	a.startWorkers()
	token := loginTest(t, server, "bob")
	status, body, _ := request(t, server, token, http.MethodPost, "/api/rooms/ai", map[string]any{"model_id": "test-model", "effort": "high"})
	expectStatus(t, status, http.StatusCreated, body)
	roomID := decodeMap(t, body)["id"].(string)
	status, body, _ = request(t, server, token, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]any{"text": "첫 질문", "client_id": "worker-client-0001"})
	expectStatus(t, status, http.StatusAccepted, body)
	firstID := decodeMap(t, body)["request"].(map[string]any)["id"].(string)
	first := waitAIRequest(t, a, firstID, aiCompleted)
	if first.AnswerMessageID == 0 || first.SentEffort != "high" {
		t.Fatalf("first request not persisted: %+v", first)
	}
	status, body, _ = request(t, server, token, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]any{"text": "둘째 질문", "client_id": "worker-client-0002"})
	expectStatus(t, status, http.StatusAccepted, body)
	secondID := decodeMap(t, body)["request"].(map[string]any)["id"].(string)
	waitAIRequest(t, a, secondID, aiCompleted)
	calls := fake.snapshot()
	if len(calls) != 2 {
		t.Fatalf("provider calls=%d", len(calls))
	}
	secondMessages := calls[1].Messages
	if len(secondMessages) != 4 {
		t.Fatalf("second context=%+v", secondMessages)
	}
	want := []AIWireMessage{{Role: "system", Content: "테스트 시스템 지침"}, {Role: "user", Content: "첫 질문"}, {Role: "assistant", Content: "첫 답변"}, {Role: "user", Content: "둘째 질문"}}
	for i := range want {
		if secondMessages[i] != want[i] {
			t.Fatalf("context[%d]=%+v want %+v", i, secondMessages[i], want[i])
		}
	}
	var aiAnswers int
	_ = a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.RoomID == roomID && m.UserID == "" && m.AIRequestID != "" {
				aiAnswers++
			}
		}
		return nil
	})
	if aiAnswers != 2 {
		t.Fatalf("persisted AI answers=%d", aiAnswers)
	}
}
func TestAISourceDeletionCancelsRunningResult(t *testing.T) {
	a, server, _, _, _ := testApp(t)
	a.cfg.AI = aiTestConfig()
	block := make(chan struct{})
	fake := &fakeAIExecutor{replies: []string{"늦은 답변"}, wait: block}
	a.aiExecutor = fake
	a.startWorkers()
	token := loginTest(t, server, "bob")
	status, body, _ := request(t, server, token, http.MethodPost, "/api/rooms/ai", map[string]any{"model_id": "test-model", "effort": "low"})
	expectStatus(t, status, http.StatusCreated, body)
	roomID := decodeMap(t, body)["id"].(string)
	status, body, _ = request(t, server, token, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]any{"text": "삭제할 질문", "client_id": "delete-client-0001"})
	expectStatus(t, status, http.StatusAccepted, body)
	accepted := decodeMap(t, body)
	requestID := accepted["request"].(map[string]any)["id"].(string)
	messageID := int64(accepted["message"].(map[string]any)["id"].(float64))
	waitAIRequest(t, a, requestID, aiRunning)
	status, body, _ = request(t, server, token, http.MethodDelete, "/api/messages/"+fmt.Sprint(messageID), nil)
	expectStatus(t, status, http.StatusOK, body)
	waitAIRequest(t, a, requestID, aiCancelled)
	close(block)
	time.Sleep(50 * time.Millisecond)
	var answers int
	_ = a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.RoomID == roomID && m.AIRequestID == requestID {
				answers++
			}
		}
		return nil
	})
	if answers != 0 {
		t.Fatalf("late AI result was persisted: %d", answers)
	}
}

func TestAISelectionAndPublicCatalogBoundaries(t *testing.T) {
	cfg := aiTestConfig()
	provider, model, effort, err := resolveAISelection(cfg, "test-model", "high")
	if err != nil || provider.ID != "test" || model.Model != "wire-test" || effort != "high" {
		t.Fatalf("selection=%+v %+v %q err=%v", provider, model, effort, err)
	}
	if _, _, _, err = resolveAISelection(cfg, "test-model", "xhigh"); err == nil {
		t.Fatal("unsupported reasoning effort accepted")
	}
	catalog := publicAICatalog(cfg)
	raw, _ := json.Marshal(catalog)
	text := string(raw)
	for _, secret := range []string{"test-key", cfg.Providers[0].BaseURL, cfg.SystemPrompt, "wire-test"} {
		if strings.Contains(text, secret) {
			t.Fatalf("public catalog leaked %q: %s", secret, text)
		}
	}
	models := catalog["models"].([]map[string]any)
	if len(models) != 1 {
		t.Fatalf("catalog models=%v", models)
	}
	efforts := models[0]["efforts"].([]string)
	for _, want := range []string{aiProviderDefaultEffort, "low", "high"} {
		found := false
		for _, got := range efforts {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing effort %s in %v", want, efforts)
		}
	}
	cfg.Models[0].SupportedEfforts = []string{"low"}
	if _, _, _, err = resolveAISelection(cfg, "test-model", "high"); err == nil {
		t.Fatal("admin allowed effort escaped provider capability")
	}
}
func TestAIHTTPFailureHandlingAndRedirectBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/unauthorized/"):
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasPrefix(r.URL.Path, "/limited/"):
			w.WriteHeader(http.StatusTooManyRequests)
		case strings.HasPrefix(r.URL.Path, "/broken/"):
			_, _ = io.WriteString(w, "not-json")
		case strings.HasPrefix(r.URL.Path, "/empty/"):
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`)
		case strings.HasPrefix(r.URL.Path, "/large/"):
			_, _ = io.WriteString(w, strings.Repeat("x", (2<<20)+20))
		default:
			_, _ = io.WriteString(w, `{"model":"ok","choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`)
		}
	}))
	defer server.Close()
	x := newDefaultAIExecutor().(*defaultAIExecutor)
	m := AIModelConfig{Model: "wire-model"}
	cases := []struct{ path, code string }{{"unauthorized", "AI_HTTP_401"}, {"limited", "AI_HTTP_429"}, {"broken", "AI_RESPONSE_INVALID"}, {"empty", "AI_EMPTY_RESPONSE"}, {"large", "AI_RESPONSE_TOO_LARGE"}}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			_, err := x.executeOpenAI(context.Background(), AIProviderConfig{BaseURL: server.URL + "/" + tc.path, apiKey: "secret"}, m, aiProviderDefaultEffort, []AIWireMessage{{Role: "user", Content: "x"}})
			var ae AIExecutionError
			if !errors.As(err, &ae) || ae.Code != tc.code {
				t.Fatalf("error=%v code=%q", err, ae.Code)
			}
		})
	}
	targetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetHit = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := x.executeOpenAI(context.Background(), AIProviderConfig{BaseURL: redirect.URL, apiKey: "secret"}, m, aiProviderDefaultEffort, []AIWireMessage{{Role: "user", Content: "x"}})
	var ae AIExecutionError
	if !errors.As(err, &ae) || ae.Code != "AI_CONNECTION_FAILED" || targetHit {
		t.Fatalf("redirect was not blocked: err=%v targetHit=%v", err, targetHit)
	}
}
func TestAITimeoutAndLegacySettingsPresence(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"late"}}]}`)
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	x := newDefaultAIExecutor().(*defaultAIExecutor)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := x.executeOpenAI(ctx, AIProviderConfig{BaseURL: slow.URL, apiKey: "secret"}, AIModelConfig{Model: "wire"}, aiProviderDefaultEffort, []AIWireMessage{{Role: "user", Content: "x"}})
	var ae AIExecutionError
	if !errors.As(err, &ae) || ae.Code != "AI_TIMEOUT" {
		t.Fatalf("timeout error=%v", err)
	}

	store, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := defaults()
	cfg.AI = aiTestConfig()
	a := &App{store: store, cfg: cfg}
	in := settingsEditable(cfg)
	in.AI = nil
	before, _ := json.Marshal(cfg.AI)
	next, err := a.candidateSettings(cfg, in)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(next.AI)
	if string(before) != string(after) {
		t.Fatalf("legacy settings request changed AI config\nbefore=%s\nafter=%s", before, after)
	}
}

func TestAICodexConfigurationCheckSeparateFromRuntime(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := AIProviderConfig{ID: "codex-cli", Type: "codex_cli", Enabled: true, Executable: exe}
	if ok, reason := aiProviderConfigurationState(p); !ok || reason != "configured" {
		t.Fatalf("codex configuration check=%v %q", ok, reason)
	}
	if ok, reason := aiProviderRuntimeState(p); !ok || reason != "ready" {
		t.Fatalf("codex runtime check=%v %q", ok, reason)
	}
}

func TestAICodexWindowsInstallDiscoveryWithoutPath(t *testing.T) {
	t.Setenv("PATH", "")
	p := AIProviderConfig{ID: "codex-cli", Type: "codex_cli", Enabled: true, Executable: "codex"}
	resolved, ok := resolveAIConfigurationExecutable(p)
	if !ok {
		t.Skip("Codex standard Windows installation is not present on this test machine")
	}
	if !filepath.IsAbs(resolved) || !regularCLIFile(resolved) {
		t.Fatalf("invalid discovered Codex executable: %q", resolved)
	}
	if ok, reason := aiProviderConfigurationState(p); !ok || reason != "configured" {
		t.Fatalf("configuration state after discovery=%v %q path=%q", ok, reason, resolved)
	}
}

func TestAICodexDiscoveryFallsBackFromStaleAbsolutePath(t *testing.T) {
	profile := t.TempDir()
	local := filepath.Join(profile, "AppData", "Local")
	roaming := filepath.Join(profile, "AppData", "Roaming")
	want := filepath.Join(local, "Programs", "OpenAI", "Codex", "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("USERPROFILE", profile)
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	p := AIProviderConfig{ID: "codex-cli", Type: "codex_cli", Enabled: true, Executable: `Z:\old-machine\codex.exe`}
	got, ok := resolveAIConfigurationExecutable(p)
	if !ok || !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("stale absolute path did not fall back: got=%q want=%q ok=%v", got, want, ok)
	}
}

func TestAICodexDiscoveryFindsWinGetLink(t *testing.T) {
	profile := t.TempDir()
	local := filepath.Join(profile, "AppData", "Local")
	want := filepath.Join(local, "Microsoft", "WinGet", "Links", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("USERPROFILE", profile)
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", filepath.Join(profile, "AppData", "Roaming"))
	p := AIProviderConfig{ID: "codex-cli", Type: "codex_cli", Enabled: true, Executable: "codex"}
	got, ok := resolveAIConfigurationExecutable(p)
	if !ok || !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("WinGet Codex link not found: got=%q want=%q ok=%v", got, want, ok)
	}
}

func TestAIClaudeExecutionUsesAutomaticDiscovery(t *testing.T) {
	profile := t.TempDir()
	claude := filepath.Join(profile, "AppData", "Roaming", "npm", "claude.cmd")
	if err := os.MkdirAll(filepath.Dir(claude), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("USERPROFILE", profile)
	t.Setenv("LOCALAPPDATA", filepath.Join(profile, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(profile, "AppData", "Roaming"))
	runner := &fakeAIRunner{stdout: []byte(`{"result":"OK","is_error":false,"modelUsage":{"claude":{"canonicalModel":"claude"}}}`)}
	x := &defaultAIExecutor{httpClient: http.DefaultClient, runner: runner}
	_, err := x.Execute(context.Background(), AIProviderConfig{Type: "claude_cli", Enabled: true, Executable: `Z:\old-machine\claude.exe`}, AIModelConfig{Model: "sonnet"}, "high", []AIWireMessage{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(runner.executable), filepath.Clean(claude)) {
		t.Fatalf("Claude did not use discovered executable: got=%q want=%q", runner.executable, claude)
	}
}

func TestAICodexDiscoveryFindsWindowsAppsAlias(t *testing.T) {
	profile := t.TempDir()
	local := filepath.Join(profile, "AppData", "Local")
	want := filepath.Join(local, "Microsoft", "WindowsApps", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("USERPROFILE", profile)
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", filepath.Join(profile, "AppData", "Roaming"))
	p := AIProviderConfig{ID: "codex-cli", Type: "codex_cli", Enabled: true, Executable: "codex"}
	got, ok := resolveAIConfigurationExecutable(p)
	if !ok || !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("WindowsApps Codex alias not found: got=%q want=%q ok=%v", got, want, ok)
	}
}

func TestAICodexDiscoveryServiceLikeEnvironment(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only discovery")
	}
	t.Setenv("PATH", "")
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "systemprofile"))
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("APPDATA", "")
	p := AIProviderConfig{ID: "codex-cli", Type: "codex_cli", Enabled: true, Executable: "Z:\\stale-machine\\codex.exe"}
	got, ok := resolveAIConfigurationExecutable(p)
	if !ok {
		t.Skip("No Codex installation found through Registry ProfileList/system user scan")
	}
	if !filepath.IsAbs(got) || !regularCLIFile(got) {
		t.Fatalf("service-like discovery returned invalid path: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "systemprofile") {
		t.Fatalf("service-like discovery incorrectly stayed in system profile: %q", got)
	}
}

func TestAIWindowsUserRegistryPathExpansion(t *testing.T) {
	profile := t.TempDir()
	want := filepath.Join(profile, "Tools", "Codex")
	profiles := []windowsProfileEntry{{SID: "S-1-5-21-test", Path: profile}}
	paths := map[string]string{"S-1-5-21-test": `%USERPROFILE%\Tools\Codex;%LOCALAPPDATA%\Programs\Other`}
	pnpm := map[string]string{"S-1-5-21-test": `%USERPROFILE%\pnpm`}
	npmPrefix := map[string]string{"S-1-5-21-test": `%USERPROFILE%\npm-prefix`}
	dirs := windowsUserEnvCandidateDirsFrom(profiles, paths, pnpm, npmPrefix)
	found := false
	for _, dir := range dirs {
		if strings.EqualFold(filepath.Clean(dir), filepath.Clean(want)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("custom user PATH directory not expanded: want=%q got=%v", want, dirs)
	}
}

func TestAICodexConfigurationReflectsDiscovery(t *testing.T) {
	p := AIProviderConfig{Type: "codex_cli", Enabled: true}
	_, found := resolveAIConfigurationExecutable(p)
	ok, reason := aiProviderConfigurationState(p)
	if ok != found || (!found && reason != "executable_missing") {
		t.Fatalf("found=%v ok=%v reason=%s", found, ok, reason)
	}
}
