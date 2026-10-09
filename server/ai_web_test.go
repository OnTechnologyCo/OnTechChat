package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAIWebToolsRetainLocalIsolation(t *testing.T) {
	args := strings.Join(buildCodexCLIArgs("gpt-6-astra", "low"), " ")
	for _, flag := range []string{`web_search="live"`, "--sandbox read-only", "--ignore-user-config", "--disable shell_tool", "--disable plugins", "--disable computer_use", "--enable code_mode_host"} {
		if !strings.Contains(args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	claude := strings.Join(buildClaudeCLIArgs("sonnet", "low"), " ")
	for _, flag := range []string{"--tools WebSearch,WebFetch", "--allowedTools WebSearch,WebFetch", "--safe-mode", "--restricted", "--strict-mcp-config", "--permission-mode dontAsk"} {
		if !strings.Contains(claude, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	prompt, e := agentPlannerPrompt(AgentPlannerInput{Request: "최신 기술 문서를 검색해줘"})
	if e != nil || !strings.Contains(prompt, "특정 주제에 한정되지") || !strings.Contains(prompt, "http(s) URL") || !strings.Contains(prompt, "KST") {
		t.Fatal("missing general web guidance")
	}
	raw := []byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"web_search\",\"query\":\"docs\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"source https://go.dev\"}}\n{\"type\":\"turn.completed\"}\n")
	if out, e := parseCodexResponse(raw); e != nil || !strings.Contains(out, "https://go.dev") {
		t.Fatal("web events broke final response", e)
	}
}

type webObservingRunner struct {
	sawSearch bool
	sawSource bool
}

func (r *webObservingRunner) Run(ctx context.Context, exe string, args []string, stdin, dir string) ([]byte, []byte, error) {
	out, errout, e := (execAICommandRunner{}).Run(ctx, exe, args, stdin, dir)
	r.sawSearch = strings.Contains(string(out), `"type":"web_search"`) || strings.Contains(string(out), `"type":"web_search_call"`)
	r.sawSource = strings.Contains(string(out), "https://go.dev")
	return out, errout, e
}
func TestAICodexLiveWebSearch(t *testing.T) {
	if os.Getenv("ONTECH_TEST_CODEX_WEB") != "1" {
		t.Skip("opt-in actual hosted search test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	runner := &webObservingRunner{}
	p := &claudeAgentPlanner{runner: runner}
	turn, e := p.Next(ctx, AIProviderConfig{Type: "codex_cli", Executable: "codex"}, AIModelConfig{Model: "gpt-6-astra"}, "low", AgentPlannerInput{Request: "Use the native web search tool now to find the current stable Go release on go.dev. Open the official release page. Answer briefly with version and official https://go.dev source URL. Use no server/file/MCP tools.", Policy: "Public web search is allowed. Do not use server/file/MCP tools.", Turn: 1})
	if e != nil {
		t.Fatal("actual web planner failed", e)
	}
	if !runner.sawSearch || !runner.sawSource || !strings.Contains(turn.Final, "https://go.dev") || len(turn.ToolCalls) != 0 {
		t.Fatalf("actual search=%v source=%v final_has_url=%v server_tools=%d", runner.sawSearch, runner.sawSource, strings.Contains(turn.Final, "https://go.dev"), len(turn.ToolCalls))
	}
	t.Log("Actual GPT-6-Astra web search + official source + structured final passed")
}
