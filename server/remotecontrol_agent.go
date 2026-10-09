package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func validateRemoteControlCall(call AgentToolCall) error {
	if call.Path != "" || len(call.Args) != 0 || call.Output != "" || call.Action != "" {
		return bad("MCP 인자가 올바르지 않습니다.")
	}
	switch call.Name {
	case "remotecontrol.list":
		if call.ProgramID != "" || call.Content != "" {
			return bad("MCP 목록 인자를 비워 두세요.")
		}
	case "remotecontrol.call":
		var args map[string]any
		if !strings.HasPrefix(call.ProgramID, "pc_") || len(call.ProgramID) > 100 || len(call.Content) > 64<<10 || json.Unmarshal([]byte(call.Content), &args) != nil || args == nil {
			return bad("MCP 도구 이름과 JSON 인자를 확인하세요.")
		}
	default:
		return bad("허용되지 않은 MCP 도구입니다.")
	}
	return nil
}
func (m *agentJobManager) remoteControlAvailable(userID string) bool {
	allowed := false
	_ = m.app.store.view(func(st *State) error {
		u, ok := st.Users[userID]
		allowed = ok && remoteControlAllowed(u) && m.app.cfg.RemoteControl.Enabled
		return nil
	})
	return allowed
}
func (m *agentJobManager) executeRemoteControl(ctx context.Context, q AgentJob, call AgentToolCall) AgentToolResult {
	result := AgentToolResult{ID: call.ID, Name: call.Name}
	// Check current persisted role on every call, including after planning and revocation.
	if m.checkPermission(q) != nil || !m.remoteControlAvailable(q.OwnerID) {
		result.Summary = "RemoteControl MCP는 연결이 활성화된 관리자만 사용할 수 있습니다."
		return result
	}
	if e := validateRemoteControlCall(call); e != nil {
		result.Summary = "MCP 인자가 올바르지 않습니다."
		return result
	}
	text, e := runRemoteControl(ctx, m.app.cfg.RemoteControl, call)
	if e != nil {
		result.Summary = e.Error()
		return result
	}
	result.OK = true
	result.Summary = "RemoteControl MCP 실행 완료"
	result.Output = text
	return result
}
func (a *App) testRemoteControlSettings(w http.ResponseWriter, r *http.Request, u User) {
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	var in struct {
		Revision      string                `json:"revision"`
		RemoteControl RemoteControlSettings `json:"remotecontrol"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	a.settingsMu.Lock()
	saved, revision, e := a.readSavedSettings()
	if e == nil && in.Revision != revision {
		e = apiError{409, "설정이 변경되었습니다. 다시 읽어 주세요."}
	}
	if e == nil {
		e = unlockConfiguration(&saved, a.store)
	}
	if e == nil {
		saved.RemoteControl, e = a.candidateRemoteControl(saved.RemoteControl, in.RemoteControl)
	}
	a.settingsMu.Unlock()
	if e != nil {
		fail(w, e)
		return
	}
	_, e = runRemoteControl(r.Context(), saved.RemoteControl, AgentToolCall{Name: "remotecontrol.call", ProgramID: "pc_get_status", Content: "{}"})
	if e != nil {
		respond(w, 200, map[string]any{"ok": false, "message": e.Error()})
		return
	}
	respond(w, 200, map[string]any{"ok": true, "message": "MCP 초기화·도구 조회·대상 PC 상태 조회 성공. 저장 후 적용하면 관리자 AI 대화에서 사용할 수 있습니다."})
}
