package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type remoteMCPClient struct {
	scan *bufio.Scanner
	out  io.Writer
	seq  int
}

func (c *remoteMCPClient) send(v any) error { return json.NewEncoder(c.out).Encode(v) }
func (c *remoteMCPClient) request(method string, params any) (json.RawMessage, error) {
	c.seq++
	id := c.seq
	if e := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); e != nil {
		return nil, e
	}
	for count := 0; count < 100 && c.scan.Scan(); count++ {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(c.scan.Bytes(), &msg) != nil {
			return nil, errors.New("invalid MCP response")
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 {
				_ = c.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "Unsupported client request"}})
			}
			continue
		}
		if string(msg.ID) != strconv.Itoa(id) {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return nil, errors.New("MCP request failed")
		}
		if len(msg.Result) == 0 {
			return nil, errors.New("empty MCP response")
		}
		return msg.Result, nil
	}
	return nil, errors.New("MCP response missing or exceeds limit")
}
func (c *remoteMCPClient) initialize() error {
	raw, e := c.request("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "OnTechChat", "version": version}})
	if e != nil {
		return e
	}
	var result struct {
		Protocol string `json:"protocolVersion"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return errors.New("invalid MCP initialization")
	}
	switch result.Protocol {
	case "2024-11-05", "2025-03-26", "2025-06-18":
	default:
		return errors.New("unsupported MCP protocol")
	}
	return c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

type remoteMCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (c *remoteMCPClient) tools() ([]remoteMCPTool, error) {
	var all []remoteMCPTool
	cursor := ""
	for page := 0; page < 8; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, e := c.request("tools/list", params)
		if e != nil {
			return nil, e
		}
		var result struct {
			Tools      []remoteMCPTool `json:"tools"`
			NextCursor string          `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return nil, errors.New("invalid MCP tools")
		}
		for _, tool := range result.Tools {
			if strings.HasPrefix(tool.Name, "pc_") && len(tool.Name) <= 100 {
				all = append(all, tool)
			}
		}
		if len(all) > 100 {
			return nil, errors.New("too many MCP tools")
		}
		cursor = result.NextCursor
		if cursor == "" {
			return all, nil
		}
	}
	return nil, errors.New("MCP pagination limit")
}
func remoteControlExecutable(c RemoteControlConfig) (string, error) {
	dirs := []string{c.PluginsDir}
	if c.PluginsDir == "" {
		for _, key := range []string{"ProgramW6432", "ProgramFiles"} {
			if base := os.Getenv(key); base != "" {
				dirs = append(dirs, filepath.Join(base, "RemoteControl", "plugins"))
			}
		}
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if !strings.EqualFold(filepath.Base(filepath.Clean(dir)), "plugins") {
			continue
		}
		// Use the installed engine beside the supplied plugins folder, never execute archive entries or plugin commands.
		exe := filepath.Join(filepath.Dir(filepath.Clean(dir)), "RemoteControl.exe")
		if info, e := os.Stat(dir); e == nil && info.IsDir() && regularCLIFile(exe) && validateConfigTarget(exe) == nil && validateConfigTarget(dir) == nil {
			return exe, nil
		}
	}
	return "", errors.New("이 서버 PC에서 RemoteControl plugins 폴더와 RemoteControl.exe를 찾지 못했습니다. 설치 경로를 확인하세요.")
}
func remoteControlEnv(c RemoteControlConfig) []string {
	env := []string{}
	for _, v := range safeAIProcessEnv() {
		if !strings.HasPrefix(strings.ToUpper(v), "RC_MCP_") {
			env = append(env, v)
		}
	}
	return append(env, "RC_MCP_HOST="+c.Host, "RC_MCP_PORT="+strconv.Itoa(c.Port), "RC_MCP_USER="+c.Username, "RC_MCP_PASSWORD="+c.password)
}
func withRemoteMCP(ctx context.Context, c RemoteControlConfig, fn func(*remoteMCPClient) (string, error)) (string, error) {
	if !c.Enabled {
		return "", errors.New("RemoteControl MCP 연결이 꺼져 있습니다.")
	}
	if e := validateRemoteControl(c); e != nil {
		return "", e
	}
	if c.password == "" {
		return "", errors.New("RemoteControl 비밀번호가 준비되지 않았습니다.")
	}
	exe, e := remoteControlExecutable(c)
	if e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--mcp-client")
	cmd.Env = remoteControlEnv(c)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	in, e := cmd.StdinPipe()
	if e != nil {
		return "", e
	}
	defer in.Close()
	out, e := cmd.StdoutPipe()
	if e != nil {
		return "", e
	}
	if e = cmd.Start(); e != nil {
		return "", errors.New("RemoteControl MCP 실행 권한 또는 설치 상태를 확인하세요.")
	}
	defer func() { in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scan := bufio.NewScanner(io.LimitReader(out, 4<<20))
	scan.Buffer(make([]byte, 4096), 1<<20)
	client := &remoteMCPClient{scan: scan, out: in}
	if e = client.initialize(); e != nil {
		return "", errors.New("RemoteControl MCP 초기화에 실패했습니다.")
	}
	text, e := fn(client)
	if e != nil {
		if ctx.Err() != nil {
			return "", errors.New("RemoteControl MCP 요청이 취소되었거나 제한 시간을 초과했습니다.")
		}
		return "", errors.New("RemoteControl MCP 연결/도구 실행에 실패했습니다. 대상 PC의 LAN MCP, 허용 IP, 포트와 Remote ID/PW를 확인하세요.")
	}
	// Never propagate the configured password back into a model, API response, or stored result.
	return strings.ReplaceAll(text, c.password, "[비밀번호 숨김]"), nil
}
func remoteMCPCallResult(raw json.RawMessage) (string, error) {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IsError {
		return "", errors.New("MCP tool failed")
	}
	var b strings.Builder
	for _, item := range result.Content {
		if item.Type == "text" {
			b.WriteString(item.Text)
			b.WriteByte('\n')
		} else {
			b.WriteString("[텍스트 외 MCP 결과: " + item.Type + " · 이 대화에서 내용 미해석]\n")
		}
	}
	if b.Len() > 128<<10 {
		return "", errors.New("MCP output exceeds limit")
	}
	return b.String(), nil
}
func runRemoteControl(ctx context.Context, c RemoteControlConfig, call AgentToolCall) (string, error) {
	return withRemoteMCP(ctx, c, func(client *remoteMCPClient) (string, error) {
		tools, e := client.tools()
		if e != nil {
			return "", e
		}
		if call.Name == "remotecontrol.list" {
			raw, e := json.Marshal(tools)
			if len(raw) > 128<<10 {
				return "", errors.New("MCP catalog too large")
			}
			return string(raw), e
		}
		found := false
		for _, tool := range tools {
			if tool.Name == call.ProgramID {
				found = true
				break
			}
		}
		if !found {
			return "", errors.New("MCP tool not offered")
		}
		var args map[string]any
		if json.Unmarshal([]byte(call.Content), &args) != nil || args == nil {
			return "", errors.New("invalid MCP arguments")
		}
		raw, e := client.request("tools/call", map[string]any{"name": call.ProgramID, "arguments": args})
		if e != nil {
			return "", e
		}
		return remoteMCPCallResult(raw)
	})
}
