package server

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAIAutomaticHelpConsentIsolationAndDedup(t *testing.T) {
	a, s, at, bt, et, room := helpFixture(t)
	f := &fakeAIExecutor{replies: []string{"자동 요약 [메시지 #1]\n[답장 초안]\n확인했습니다."}}
	a.aiExecutor = f
	path := "/api/rooms/" + room + "/ai-help"
	code, b, _ := request(t, s, at, "PUT", path, map[string]any{"enabled": true})
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, bt, "PUT", path, map[string]any{"enabled": true, "timezone": "Asia/Seoul"})
	expectStatus(t, code, 200, b)
	a.autoHelpTick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		code, b, _ = request(t, s, bt, "GET", path, nil)
		if decodeMap(t, b)["status"] == "ready" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if decodeMap(t, b)["status"] != "ready" {
		t.Fatalf("no automatic result: %s", b)
	}
	calls := f.snapshot()
	if len(calls) != 1 || !calls[0].Provider.analysisOnly {
		t.Fatal("missing isolated automatic call")
	}
	a.autoHelpTick()
	if len(f.snapshot()) != 1 {
		t.Fatal("unchanged messages reanalyzed")
	}
	code, b, _ = request(t, s, et, "GET", path, nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["enabled"] != false || strings.Contains(string(b), "자동 요약") {
		t.Fatal("another member saw private assistance")
	}
	code, b, _ = request(t, s, bt, "PUT", path, map[string]any{"enabled": false})
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["status"] != "off" || strings.Contains(string(b), "자동 요약") {
		t.Fatal("disabled result remained")
	}
}
func TestAIAutomaticHelpDisabledInFlight(t *testing.T) {
	a, s, _, bt, _, room := helpFixture(t)
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	a.aiExecutor = aiExecutorFunc(func(ctx context.Context, p AIProviderConfig, m AIModelConfig, e string, msg []AIWireMessage) (AIExecutionResult, error) {
		started <- struct{}{}
		<-gate
		return AIExecutionResult{Content: "late private answer"}, nil
	})
	path := "/api/rooms/" + room + "/ai-help"
	code, b, _ := request(t, s, bt, "PUT", path, map[string]any{"enabled": true})
	expectStatus(t, code, 200, b)
	a.autoHelpTick()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		close(gate)
		t.Fatal("did not run")
	}
	code, b, _ = request(t, s, bt, "PUT", path, map[string]any{"enabled": false})
	expectStatus(t, code, 200, b)
	close(gate)
	time.Sleep(30 * time.Millisecond)
	code, b, _ = request(t, s, bt, "GET", path, nil)
	expectStatus(t, code, 200, b)
	if strings.Contains(string(b), "late private answer") {
		t.Fatal("revoked result resurrected")
	}
}

type aiExecutorFunc func(context.Context, AIProviderConfig, AIModelConfig, string, []AIWireMessage) (AIExecutionResult, error)

func (f aiExecutorFunc) Execute(c context.Context, p AIProviderConfig, m AIModelConfig, e string, v []AIWireMessage) (AIExecutionResult, error) {
	return f(c, p, m, e, v)
}
