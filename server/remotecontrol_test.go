package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentRemoteControlRoleAndSecretBoundary(t *testing.T) {
	a, s, super, bob, _ := testApp(t)
	c, e := a.candidateRemoteControl(RemoteControlConfig{}, RemoteControlSettings{Enabled: true, Host: "127.0.0.1", Port: 5901, Username: "remote-test", Password: "fixture-secret-only"})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c)
	view, _ := json.Marshal(remoteControlSettings(c))
	if bytes.Contains(raw, []byte("fixture-secret-only")) || bytes.Contains(view, []byte("fixture-secret-only")) || bytes.Contains(view, []byte("password_encrypted")) {
		t.Fatal("secret leaked")
	}
	kept, e := a.candidateRemoteControl(c, *remoteControlSettings(c))
	if e != nil || kept.PasswordEncrypted != c.PasswordEncrypted {
		t.Fatal("blank did not retain")
	}
	cfg := defaults()
	cfg.RemoteControl = c
	if e = unlockConfiguration(&cfg, a.store); e != nil || cfg.RemoteControl.password != "fixture-secret-only" {
		t.Fatal("secret reload failed")
	}
	a.cfg.RemoteControl = cfg.RemoteControl
	mgr := &agentJobManager{app: a}
	if !mgr.remoteControlAvailable(super.ID) || mgr.remoteControlAvailable(bob.ID) {
		t.Fatal("role visibility incorrect")
	}
	res := mgr.executeRemoteControl(context.Background(), AgentJob{OwnerID: bob.ID}, AgentToolCall{Name: "remotecontrol.list"})
	if res.OK || !strings.Contains(res.Summary, "관리자") {
		t.Fatal("ordinary user reached MCP")
	}
	if e = a.store.update(func(st *State) error { u := st.Users[super.ID]; u.Role = roleUser; st.Users[super.ID] = u; return nil }); e != nil {
		t.Fatal(e)
	}
	if mgr.remoteControlAvailable(super.ID) {
		t.Fatal("revoked admin retains MCP")
	}
	code, b, _ := request(t, s, loginTest(t, s, "bob"), "POST", "/api/settings/test-remotecontrol", map[string]any{})
	expectStatus(t, code, 403, b)
	disabled := super
	disabled.Disabled = true
	if remoteControlAllowed(disabled) {
		t.Fatal("disabled user allowed")
	}
}
func TestAgentRemoteControlValidationAndPortableInstall(t *testing.T) {
	for _, c := range []RemoteControlConfig{
		{Enabled: true, Host: "8.8.8.8", Port: 5901, Username: "a", password: "b"},
		{Enabled: true, Host: "localhost/evil", Port: 5901, Username: "a", password: "b"},
		{Enabled: true, Host: "127.0.0.1", Port: 0, Username: "a", password: "b"},
	} {
		if validateRemoteControl(c) == nil {
			t.Fatal("unsafe target accepted")
		}
	}
	for _, call := range []AgentToolCall{
		{Name: "remotecontrol.list", Content: "{}"},
		{Name: "remotecontrol.call", ProgramID: "pc_get_status", Content: "[]"},
		{Name: "remotecontrol.call", ProgramID: "shell", Content: "{}"},
		{Name: "remotecontrol.call", ProgramID: "pc_get_status", Content: "{}", Path: "C:\\secret"},
	} {
		if validateAgentToolCall(call) == nil {
			t.Fatal("invalid MCP call accepted")
		}
	}
	good := AgentToolCall{ID: "m", Name: "remotecontrol.call", ProgramID: "pc_get_status", Content: "{}"}
	if e := validateAgentToolCall(good); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(AgentPlannerTurn{ToolCalls: []AgentToolCall{good}})
	if _, e := decodeAgentTurn(raw); e != nil {
		t.Fatal(e)
	}
	base := filepath.Join(t.TempDir(), "다른 PC")
	plugins := filepath.Join(base, "RemoteControl", "plugins")
	if e := os.MkdirAll(plugins, 0700); e != nil {
		t.Fatal(e)
	}
	exe := filepath.Join(base, "RemoteControl", "RemoteControl.exe")
	if e := os.WriteFile(exe, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("ProgramW6432", base)
	t.Setenv("ProgramFiles", base)
	if got, e := remoteControlExecutable(RemoteControlConfig{}); e != nil || got != exe {
		t.Fatal("portable install resolution", got, e)
	}
	t.Setenv("RC_MCP_PASSWORD", "inherited-wrong")
	env := strings.Join(remoteControlEnv(RemoteControlConfig{password: "server-secret"}), "\n")
	if strings.Contains(env, "inherited-wrong") || strings.Count(env, "RC_MCP_PASSWORD=") != 1 {
		t.Fatal("wrong inherited profile")
	}
}
func TestAgentRemoteControlProtocol(t *testing.T) {
	replies := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{"tools":{}}}}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"pc_get_status","inputSchema":{"type":"object"}}],"nextCursor":"page2"}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"untrusted_shell","inputSchema":{}}]}}`,
		`{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"대상 PC 정상"}],"isError":false}}`,
	}, "\n")
	var sent bytes.Buffer
	client := &remoteMCPClient{scan: bufio.NewScanner(strings.NewReader(replies)), out: &sent}
	if e := client.initialize(); e != nil {
		t.Fatal(e)
	}
	tools, e := client.tools()
	if e != nil || len(tools) != 1 || tools[0].Name != "pc_get_status" {
		t.Fatal("catalog/pagination failed", e)
	}
	raw, e := client.request("tools/call", map[string]any{"name": "pc_get_status", "arguments": map[string]any{}})
	if e != nil {
		t.Fatal(e)
	}
	out, e := remoteMCPCallResult(raw)
	if e != nil || !strings.Contains(out, "대상 PC 정상") {
		t.Fatal("result failed")
	}
	if !strings.Contains(sent.String(), "notifications/initialized") || !strings.Contains(sent.String(), `"cursor":"page2"`) {
		t.Fatal("handshake or paging absent")
	}
	if _, e = remoteMCPCallResult([]byte(`{"isError":true,"content":[{"type":"text","text":"secret"}]}`)); e == nil {
		t.Fatal("error became success")
	}
}
func TestAgentRemoteControlSettingsRoundTrip(t *testing.T) {
	a, s, token, _ := settingsTestApp(t)
	snap, settings := getSettingMap(t, s, token)
	settings.RemoteControl = &RemoteControlSettings{Enabled: true, Host: "127.0.0.1", Port: 5901, Username: "test", Password: "fixture-pass"}
	code, body := settingSave(t, s, token, snap["revision"].(string), settings, false)
	expectStatus(t, code, 200, body)
	if strings.Contains(string(body), "fixture-pass") {
		t.Fatal("API secret exposed")
	}
	saved, _, e := a.readSavedSettings()
	if e != nil || saved.RemoteControl.PasswordEncrypted == "" {
		t.Fatal("save failed", e)
	}
	if e = unlockConfiguration(&saved, a.store); e != nil || saved.RemoteControl.password != "fixture-pass" {
		t.Fatal("reload failed", e)
	}
	snap, settings = getSettingMap(t, s, token)
	settings.RemoteControl.Enabled = false
	settings.RemoteControl.ClearPassword = true
	code, body = settingSave(t, s, token, snap["revision"].(string), settings, false)
	expectStatus(t, code, 200, body)
	saved, _, e = a.readSavedSettings()
	if e != nil || saved.RemoteControl.PasswordEncrypted != "" {
		t.Fatal("clear failed")
	}
}
