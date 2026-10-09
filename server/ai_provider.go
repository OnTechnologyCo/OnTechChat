package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type AIWireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AIExecutionResult struct {
	Content       string
	ObservedModel string
	SentEffort    string
}

type AIExecutor interface {
	Execute(context.Context, AIProviderConfig, AIModelConfig, string, []AIWireMessage) (AIExecutionResult, error)
}

type AIExecutionError struct {
	Code   string
	Public string
}

func (e AIExecutionError) Error() string { return e.Public }

type defaultAIExecutor struct {
	httpClient *http.Client
	runner     aiCommandRunner
}

func newDefaultAIExecutor() AIExecutor {
	return &defaultAIExecutor{
		httpClient: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirect blocked")
		}},
		runner: execAICommandRunner{},
	}
}

func (x *defaultAIExecutor) Execute(ctx context.Context, p AIProviderConfig, m AIModelConfig, effort string, messages []AIWireMessage) (AIExecutionResult, error) {
	switch p.Type {
	case "openai_compatible":
		return x.executeOpenAI(ctx, p, m, effort, messages)
	case "claude_cli":
		resolved, ok := resolveAIConfigurationExecutable(p)
		if !ok {
			return AIExecutionResult{}, AIExecutionError{Code: "AI_CLI_EXECUTABLE_MISSING", Public: "Claude CLI 실행 파일을 자동으로 찾지 못했습니다."}
		}
		p.Executable = resolved
		return x.executeClaude(ctx, p, m, effort, messages)
	case "codex_cli":
		return x.executeCodex(ctx, p, m, effort, messages)
	default:
		return AIExecutionResult{}, AIExecutionError{Code: "AI_PROVIDER_UNAVAILABLE", Public: "AI 연결을 사용할 수 없습니다."}
	}
}

func openAIChatURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
	case strings.HasSuffix(path, "/v1"):
		path += "/chat/completions"
	default:
		path += "/v1/chat/completions"
	}
	u.Path, u.RawPath = path, ""
	return u.String(), nil
}
func (x *defaultAIExecutor) executeOpenAI(ctx context.Context, p AIProviderConfig, m AIModelConfig, effort string, messages []AIWireMessage) (AIExecutionResult, error) {
	endpoint, err := openAIChatURL(p.BaseURL)
	if err != nil {
		return AIExecutionResult{}, AIExecutionError{Code: "AI_ENDPOINT_INVALID", Public: "AI 서버 주소가 올바르지 않습니다."}
	}
	wireMessages := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		wireMessages = append(wireMessages, map[string]any{"role": msg.Role, "content": msg.Content})
	}
	if media := mediaFrom(ctx); media != nil && len(wireMessages) > 0 {
		parts := []map[string]any{{"type": "text", "text": messages[len(messages)-1].Content}}
		for _, in := range media.Inputs {
			if in.ImagePath != "" {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + strings.Split(http.DetectContentType(in.Data), ";")[0] + ";base64," + base64.StdEncoding.EncodeToString(in.Data)}})
			}
		}
		if len(parts) > 1 {
			wireMessages[len(wireMessages)-1]["content"] = parts
		}
	}
	body := map[string]any{"model": m.Model, "messages": wireMessages}
	sent := ""
	if effort != "" && effort != aiProviderDefaultEffort {
		body["reasoning_effort"] = effort
		sent = effort
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return AIExecutionResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return AIExecutionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := x.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return AIExecutionResult{}, AIExecutionError{Code: "AI_TIMEOUT", Public: "AI 응답 시간이 초과되었습니다."}
		}
		return AIExecutionResult{}, AIExecutionError{Code: "AI_CONNECTION_FAILED", Public: "AI 서버에 연결하지 못했습니다."}
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, (2<<20)+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > 2<<20 {
		return AIExecutionResult{}, AIExecutionError{Code: "AI_RESPONSE_TOO_LARGE", Public: "AI 응답 크기 제한을 초과했습니다."}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AIExecutionResult{}, AIExecutionError{Code: fmt.Sprintf("AI_HTTP_%d", resp.StatusCode), Public: "AI 서버가 요청을 처리하지 못했습니다."}
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Choices) == 0 {
		return AIExecutionResult{}, AIExecutionError{Code: "AI_RESPONSE_INVALID", Public: "AI 서버 응답 형식을 읽지 못했습니다."}
	}
	switch strings.ToLower(strings.TrimSpace(out.Choices[0].FinishReason)) {
	case "length":
		return AIExecutionResult{}, AIExecutionError{Code: "AI_RESPONSE_TRUNCATED", Public: "AI 응답이 길이 제한으로 잘려 저장하지 않았습니다."}
	case "content_filter":
		return AIExecutionResult{}, AIExecutionError{Code: "AI_RESPONSE_FILTERED", Public: "AI 제공자가 응답 생성을 제한했습니다."}
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return AIExecutionResult{}, AIExecutionError{Code: "AI_EMPTY_RESPONSE", Public: "AI가 빈 응답을 반환했습니다."}
	}
	return AIExecutionResult{Content: content, ObservedModel: out.Model, SentEffort: sent}, nil
}

type aiCommandRunner interface {
	Run(context.Context, string, []string, string, string) ([]byte, []byte, error)
}

type execAICommandRunner struct{}

type boundedCommandBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedCommandBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.limit <= 0 {
		return n, nil
	}
	left := b.limit - b.buf.Len()
	if left > 0 {
		if left > len(p) {
			left = len(p)
		}
		_, _ = b.buf.Write(p[:left])
	}
	if len(p) > left {
		b.overflow = true
	}
	return n, nil
}

func safeAIProcessEnv() []string {
	keys := []string{
		"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP",
		"USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA", "CODEX_HOME",
		"HOMEDRIVE", "HOMEPATH", "ProgramFiles", "ProgramFiles(x86)",
	}
	profile := currentCLIProfile()
	profileEnv := cliProfileEnvironment(profile)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		value := os.Getenv(key)
		if replacement, ok := profileEnv[key]; ok {
			value = replacement
		}
		if value != "" {
			out = append(out, key+"="+value)
		}
	}
	return out
}
func (execAICommandRunner) Run(ctx context.Context, executable string, args []string, stdin, workDir string) ([]byte, []byte, error) {
	dir := strings.TrimSpace(workDir)
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "ontech-ai-*")
		if err != nil {
			return nil, nil, err
		}
		defer os.RemoveAll(dir)
	} else {
		if !filepath.IsAbs(dir) {
			return nil, nil, errors.New("AI CLI work directory must be absolute")
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return nil, nil, errors.New("AI CLI work directory is unavailable")
		}
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.WaitDelay = 3 * time.Second
	if runtime.GOOS == "windows" {
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			killCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			killer := exec.CommandContext(killCtx, filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe"), "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F")
			if killer.Run() == nil {
				return nil
			}
			return cmd.Process.Kill()
		}
	}
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = safeAIProcessEnv()
	if home := sharedCodexHome(executable); home != "" {
		cmd.Env = SharedCodexEnvironment(home)
	}
	cmd.Dir = dir
	stdout := &boundedCommandBuffer{limit: 4 << 20}
	stderr := &boundedCommandBuffer{limit: 256 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return stdout.buf.Bytes(), stderr.buf.Bytes(), errors.New("AI CLI output exceeded limit")
	}
	return stdout.buf.Bytes(), stderr.buf.Bytes(), err
}

func renderCLIConversation(messages []AIWireMessage) (string, error) {
	raw, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	return aiWebInstructions() + "\n다음 JSON 대화에 텍스트로만 답하세요. JSON의 role과 content만 대화 문맥으로 사용하세요.\n" + string(raw), nil
}
func buildClaudeCLIArgs(model, effort string) []string {
	args := []string{
		"-p", "--safe-mode", "--restricted", "--strict-mcp-config", "--disable-slash-commands",
		"--no-session-persistence", "--tools", "WebSearch,WebFetch", "--allowedTools", "WebSearch,WebFetch", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--output-format", "json", "--model", model,
	}
	if effort != "" && effort != aiProviderDefaultEffort {
		args = append(args, "--effort", effort)
	}
	return args
}

func buildCodexCLIArgs(model, effort string) []string {
	args := []string{
		"exec", "-", "--skip-git-repo-check", "--ephemeral",
		"--ignore-rules", "--ignore-user-config", "--sandbox", "read-only", "--json", "-m", model,
		"-c", `approval_policy="never"`, "-c", `web_search="live"`,
		"--disable", "shell_tool", "--disable", "unified_exec", "--disable", "apps",
		"--disable", "plugins", "--disable", "hooks", "--disable", "browser_use",
		"--disable", "computer_use", "--disable", "multi_agent", "--enable", "code_mode_host", "--enable", "image_generation",
	}
	if effort != "" && effort != aiProviderDefaultEffort {
		args = append(args, "-c", "model_reasoning_effort="+effort)
	}
	return args
}
func (x *defaultAIExecutor) executeClaude(ctx context.Context, p AIProviderConfig, m AIModelConfig, effort string, messages []AIWireMessage) (AIExecutionResult, error) {
	if e := requireCLIImageSupport(ctx, p); e != nil {
		return AIExecutionResult{}, e
	}
	prompt, err := renderCLIConversationForProvider(p, messages)
	if err != nil {
		return AIExecutionResult{}, err
	}
	out, _, err := x.runner.Run(ctx, p.Executable, claudeConversationArgs(p, m.Model, effort), prompt, p.WorkDir)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return AIExecutionResult{}, AIExecutionError{Code: "AI_TIMEOUT", Public: "AI 응답 시간이 초과되었습니다."}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return AIExecutionResult{}, ctx.Err()
		}
		return AIExecutionResult{}, AIExecutionError{Code: "AI_CLI_FAILED", Public: "AI CLI 실행에 실패했습니다. 관리자 설정에서 연결 상태를 확인하세요."}
	}
	var result struct {
		Result     string `json:"result"`
		IsError    bool   `json:"is_error"`
		ModelUsage map[string]struct {
			CanonicalModel string `json:"canonicalModel"`
		} `json:"modelUsage"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.IsError {
		return AIExecutionResult{}, AIExecutionError{Code: "AI_RESPONSE_INVALID", Public: "AI CLI 응답을 읽지 못했습니다."}
	}
	content := strings.TrimSpace(result.Result)
	if content == "" {
		return AIExecutionResult{}, AIExecutionError{Code: "AI_EMPTY_RESPONSE", Public: "AI가 빈 응답을 반환했습니다."}
	}
	sent := ""
	if effort != "" && effort != aiProviderDefaultEffort {
		sent = effort
	}
	observed := ""
	if len(result.ModelUsage) == 1 {
		for key, info := range result.ModelUsage {
			observed = strings.TrimSpace(info.CanonicalModel)
			if observed == "" {
				observed = strings.TrimSpace(key)
			}
		}
	}
	return AIExecutionResult{Content: content, ObservedModel: observed, SentEffort: sent}, nil
}

func regularCLIFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func expandWindowsProfileValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacements := map[string]string{
		"%SystemDrive%": os.Getenv("SystemDrive"),
		"%SYSTEMDRIVE%": os.Getenv("SystemDrive"),
		"%SystemRoot%":  os.Getenv("SystemRoot"),
		"%SYSTEMROOT%":  os.Getenv("SystemRoot"),
	}
	for old, newValue := range replacements {
		if newValue != "" {
			value = strings.ReplaceAll(value, old, newValue)
		}
	}
	return filepath.Clean(value)
}

func windowsRegistryProfiles() []string {
	var profiles []string
	for _, entry := range windowsRegistryProfileEntries() {
		profiles = append(profiles, entry.Path)
	}
	return profiles
}

func windowsRegistryLineValue(line string) (string, bool) {
	idx := strings.Index(line, "REG_")
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimSpace(line[idx:])
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	return value, value != ""
}

type windowsProfileEntry struct {
	SID  string
	Path string
}

func windowsRegistryProfileEntries() []windowsProfileEntry { return nativeWindowsCLIProfiles() }

func windowsRegistryUserEnvValues(valueName string) map[string]string {
	return nativeWindowsCLIUserEnv(valueName)
}

func expandWindowsUserPathValue(value, profile string) string {
	value = strings.Trim(strings.TrimSpace(value), string(rune(34)))
	if value == "" {
		return ""
	}
	volume := filepath.VolumeName(profile)
	homePath := strings.TrimPrefix(profile, volume)
	replacements := map[string]string{
		"%USERPROFILE%":       profile,
		"%LOCALAPPDATA%":      filepath.Join(profile, "AppData", "Local"),
		"%APPDATA%":           filepath.Join(profile, "AppData", "Roaming"),
		"%HOMEDRIVE%":         volume,
		"%HOMEPATH%":          homePath,
		"%PROGRAMFILES%":      os.Getenv("ProgramFiles"),
		"%PROGRAMFILES(X86)%": os.Getenv("ProgramFiles(x86)"),
		"%PROGRAMDATA%":       os.Getenv("ProgramData"),
		"%SYSTEMROOT%":        os.Getenv("SystemRoot"),
		"%SYSTEMDRIVE%":       os.Getenv("SystemDrive"),
	}
	for key, replacement := range replacements {
		for _, variant := range []string{key, strings.ToLower(key), strings.Title(strings.ToLower(key))} {
			if replacement != "" {
				value = strings.ReplaceAll(value, variant, replacement)
			}
		}
	}
	if strings.Contains(value, "%") || !filepath.IsAbs(value) {
		return ""
	}
	return filepath.Clean(value)
}

func windowsUserEnvCandidateDirsFrom(profiles []windowsProfileEntry, paths, pnpm, npmPrefix map[string]string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" {
			return
		}
		key := strings.ToLower(filepath.Clean(path))
		if seen[key] {
			return
		}
		seen[key] = true
		dirs = append(dirs, path)
	}
	for _, profile := range profiles {
		for _, raw := range strings.Split(paths[profile.SID], ";") {
			add(expandWindowsUserPathValue(raw, profile.Path))
		}
		for _, raw := range []string{pnpm[profile.SID], npmPrefix[profile.SID]} {
			add(expandWindowsUserPathValue(raw, profile.Path))
		}
	}
	return dirs
}

func windowsUserEnvCandidateDirs() []string {
	return windowsUserEnvCandidateDirsFrom(
		windowsRegistryProfileEntries(),
		windowsRegistryUserEnvValues("Path"),
		windowsRegistryUserEnvValues("PNPM_HOME"),
		windowsRegistryUserEnvValues("NPM_CONFIG_PREFIX"),
	)
}

func firstRegularCLICandidate(candidates []string) (string, bool) {
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(candidate))
		if seen[key] {
			continue
		}
		seen[key] = true
		if regularCLIFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func windowsCLIExecutableCandidates(providerType string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	var candidates []string
	addProfile := func(profile string) {
		if strings.TrimSpace(profile) == "" {
			return
		}
		local := filepath.Join(profile, "AppData", "Local")
		roaming := filepath.Join(profile, "AppData", "Roaming")
		switch providerType {
		case "codex_cli":
			candidates = append(candidates,
				filepath.Join(local, "Programs", "OpenAI", "Codex", "bin", "codex.exe"),
				filepath.Join(local, "Programs", "OpenAI", "Codex", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Links", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Links", "codex.cmd"),
				filepath.Join(local, "Microsoft", "WindowsApps", "codex.exe"),
				filepath.Join(local, "Microsoft", "WindowsApps", "codex.cmd"),
				filepath.Join(local, "pnpm", "codex.exe"),
				filepath.Join(local, "pnpm", "codex.cmd"),
				filepath.Join(roaming, "npm", "codex.exe"),
				filepath.Join(roaming, "npm", "codex.cmd"),
				filepath.Join(profile, "scoop", "shims", "codex.exe"),
				filepath.Join(profile, "scoop", "shims", "codex.cmd"),
				filepath.Join(profile, ".local", "bin", "codex.exe"),
				filepath.Join(profile, ".codex", "bin", "codex.exe"),
			)
		case "claude_cli":
			candidates = append(candidates,
				filepath.Join(roaming, "npm", "claude.exe"),
				filepath.Join(roaming, "npm", "claude.cmd"),
				filepath.Join(local, "pnpm", "claude.exe"),
				filepath.Join(local, "pnpm", "claude.cmd"),
				filepath.Join(profile, "scoop", "shims", "claude.exe"),
				filepath.Join(profile, "scoop", "shims", "claude.cmd"),
			)
		}
	}
	addProfile(os.Getenv("USERPROFILE"))
	addProfile(currentCLIProfile())
	for _, profile := range windowsRegistryProfiles() {
		addProfile(profile)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		if providerType == "codex_cli" {
			candidates = append(candidates,
				filepath.Join(local, "Programs", "OpenAI", "Codex", "bin", "codex.exe"),
				filepath.Join(local, "Programs", "OpenAI", "Codex", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Links", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Links", "codex.cmd"),
				filepath.Join(local, "Microsoft", "WindowsApps", "codex.exe"),
				filepath.Join(local, "Microsoft", "WindowsApps", "codex.cmd"),
				filepath.Join(local, "pnpm", "codex.exe"),
				filepath.Join(local, "pnpm", "codex.cmd"),
			)
			for _, pattern := range []string{
				filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "bin", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "*", "codex.exe"),
				filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "*", "bin", "codex.exe"),
			} {
				if matches, err := filepath.Glob(pattern); err == nil {
					candidates = append(candidates, matches...)
				}
			}
		}
	}
	if roaming := os.Getenv("APPDATA"); roaming != "" {
		switch providerType {
		case "codex_cli":
			candidates = append(candidates, filepath.Join(roaming, "npm", "codex.exe"), filepath.Join(roaming, "npm", "codex.cmd"))
		case "claude_cli":
			candidates = append(candidates, filepath.Join(roaming, "npm", "claude.exe"), filepath.Join(roaming, "npm", "claude.cmd"))
		}
	}
	for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if pf == "" {
			continue
		}
		switch providerType {
		case "codex_cli":
			candidates = append(candidates,
				filepath.Join(pf, "OpenAI", "Codex", "bin", "codex.exe"),
				filepath.Join(pf, "OpenAI", "Codex", "codex.exe"),
			)
		}
	}
	if chocolatey := os.Getenv("ChocolateyInstall"); chocolatey != "" {
		switch providerType {
		case "codex_cli":
			candidates = append(candidates, filepath.Join(chocolatey, "bin", "codex.exe"), filepath.Join(chocolatey, "bin", "codex.cmd"))
		case "claude_cli":
			candidates = append(candidates, filepath.Join(chocolatey, "bin", "claude.exe"), filepath.Join(chocolatey, "bin", "claude.cmd"))
		}
	}
	systemDrive := strings.TrimSpace(os.Getenv("SystemDrive"))
	if systemDrive == "" {
		systemDrive = filepath.VolumeName(os.Getenv("SystemRoot"))
	}
	if systemDrive == "" {
		systemDrive = "C:"
	}
	usersRoot := filepath.Join(systemDrive+string(os.PathSeparator), "Users")
	if entries, err := os.ReadDir(usersRoot); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := strings.ToLower(entry.Name())
			switch name {
			case "all users", "default", "default user", "public", "defaultaccount", "wdagutilityaccount":
				continue
			}
			addProfile(filepath.Join(usersRoot, entry.Name()))
		}
	}
	var userCandidates []string
	for _, dir := range windowsUserEnvCandidateDirs() {
		switch providerType {
		case "codex_cli":
			userCandidates = append(userCandidates,
				filepath.Join(dir, "codex.exe"),
				filepath.Join(dir, "codex.cmd"),
				filepath.Join(dir, "node_modules", ".bin", "codex.exe"),
				filepath.Join(dir, "node_modules", ".bin", "codex.cmd"),
			)
		case "claude_cli":
			userCandidates = append(userCandidates,
				filepath.Join(dir, "claude.exe"),
				filepath.Join(dir, "claude.cmd"),
				filepath.Join(dir, "node_modules", ".bin", "claude.exe"),
				filepath.Join(dir, "node_modules", ".bin", "claude.cmd"),
			)
		}
	}
	return append(candidates, userCandidates...)
}

func knownWindowsCLIExecutable(providerType string) (string, bool) {
	return firstRegularCLICandidate(windowsCLIExecutableCandidates(providerType))
}

func resolveAIConfigurationExecutable(p AIProviderConfig) (string, bool) {
	if p.Type == "codex_cli" {
		if common := sharedCodexExecutable(); regularCLIFile(common) {
			return common, true
		}
	}
	raw := strings.Trim(strings.TrimSpace(p.Executable), "\"")
	if raw == "" {
		raw = cliCommandName(p.Type)
	}
	if raw != "" {
		if filepath.IsAbs(raw) {
			if _, err := os.Stat(raw); errors.Is(err, os.ErrPermission) {
				return "", false
			}
			if regularCLIFile(raw) {
				return raw, true
			}
		} else if found, err := exec.LookPath(raw); err == nil && regularCLIFile(found) {
			return found, true
		}
	}
	return knownWindowsCLIExecutable(p.Type)
}

func aiProviderConfigurationState(p AIProviderConfig) (bool, string) {
	if !p.Enabled {
		return false, "disabled"
	}
	switch p.Type {
	case "openai_compatible":
		if p.apiKey == "" {
			return false, "credential_missing"
		}
		return true, "configured"
	case "claude_cli":
		if ok, reason := aiCLIExecutableState(p); !ok {
			return false, reason
		}
		return true, "configured"
	case "codex_cli":
		if ok, reason := aiCLIExecutableState(p); !ok {
			return false, reason
		}
		if exe, ok := resolveAIConfigurationExecutable(p); ok {
			if home := sharedCodexHome(exe); home != "" && !regularCLIFile(filepath.Join(home, "auth.json")) {
				return false, "server_login_required"
			}
		}
		return true, "configured"
	default:
		return false, "unsupported"
	}
}

func aiProviderRuntimeState(p AIProviderConfig) (bool, string) {
	if !p.Enabled {
		return false, "disabled"
	}
	switch p.Type {
	case "openai_compatible":
		if p.apiKey == "" {
			return false, "credential_missing"
		}
		return true, "ready"
	case "claude_cli":
		if ok, reason := aiCLIExecutableState(p); !ok {
			return false, reason
		}
		return true, "ready"
	case "codex_cli":
		if ok, reason := aiCLIExecutableState(p); !ok {
			return false, reason
		}
		return true, "ready"
	default:
		return false, "unsupported"
	}
}
