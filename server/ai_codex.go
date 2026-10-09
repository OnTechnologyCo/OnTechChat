package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func codexFailure(ctx context.Context, diagnostic []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	text := strings.ToLower(string(diagnostic))
	code, message := "AI_CLI_FAILED", "Codex CLI 호출에 실패했습니다. 서버 실행 계정에서 같은 모델로 codex exec를 확인하세요."
	switch {
	case strings.Contains(text, "401"), strings.Contains(text, "unauthorized"), strings.Contains(text, "not logged"), strings.Contains(text, "login"), strings.Contains(text, "authentication"):
		code, message = "AI_CLI_AUTH_REQUIRED", "서버용 Codex 로그인이 필요합니다. 서버 PC 트레이의 서버용 Codex 로그인에서 로그인한 뒤 다시 시도하세요."
	case strings.Contains(text, "unexpected argument"), strings.Contains(text, "unknown option"), strings.Contains(text, "unknown field"):
		code, message = "AI_CLI_VERSION_UNSUPPORTED", "설치 PC의 Codex CLI가 실행 옵션을 지원하지 않습니다. CLI를 업데이트한 뒤 실제 호출을 다시 확인하세요."
	case strings.Contains(text, "model"), strings.Contains(text, "reasoning"):
		code, message = "AI_CLI_MODEL_REJECTED", "Codex가 선택한 모델 또는 추론 설정을 거부했습니다. 서버 실행 계정의 모델 사용 권한과 CLI 버전을 확인하세요."
	case strings.Contains(text, "sandbox"):
		code, message = "AI_CLI_SANDBOX_FAILED", "Codex의 읽기 전용 실행 환경을 시작하지 못했습니다. 설치 PC에서 Codex Windows 샌드박스 설정을 확인하세요."
	}
	return AIExecutionError{Code: code, Public: message}
}

// Only completed final assistant messages are user-visible; reasoning and tool logs are excluded.
func parseCodexResponse(out []byte) (string, error) {
	completed, failed := false, false
	answer := ""
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(line, &event) != nil {
			return "", AIExecutionError{Code: "AI_CLI_INVALID_RESPONSE", Public: "Codex 응답 형식을 읽지 못했습니다."}
		}
		switch event.Type {
		case "item.completed":
			if event.Item.Type == "agent_message" {
				answer = event.Item.Text
			}
		case "turn.completed":
			completed = true
		case "turn.failed":
			failed = true
		}
	}
	if failed {
		return "", codexFailure(context.Background(), out)
	}
	if !completed || strings.TrimSpace(answer) == "" {
		return "", AIExecutionError{Code: "AI_CLI_INCOMPLETE", Public: "Codex가 완성된 답변을 반환하지 않았습니다."}
	}
	return strings.TrimSpace(answer), nil
}

func runCodex(ctx context.Context, runner aiCommandRunner, p AIProviderConfig, model, effort, prompt, schema string) (string, error) {
	executable, ok := resolveAIConfigurationExecutable(p)
	if !ok {
		if _, reason := aiCLIExecutableState(p); reason == "executable_access_denied" {
			return "", AIExecutionError{Code: "AI_CLI_ACCESS_DENIED", Public: "서버 실행 계정이 Codex 경로에 접근할 수 없습니다. 최신 설치 파일을 다시 실행해 공용 Codex를 준비한 뒤 트레이의 서버용 Codex 로그인에서 로그인하세요."}
		}
		return "", AIExecutionError{Code: "AI_CLI_EXECUTABLE_MISSING", Public: "이 서버 PC에서 Codex CLI를 찾지 못했습니다. 현재 PC에 설치하거나 CLI 경로를 확인하세요."}
	}
	executable, launchErr := nativeCodexExecutable(executable)
	if launchErr != nil {
		return "", launchErr
	}
	if home := sharedCodexHome(executable); home != "" && !regularCLIFile(filepath.Join(home, "auth.json")) {
		return "", AIExecutionError{Code: "AI_CLI_AUTH_REQUIRED", Public: "공용 Codex는 준비되었습니다. 서버 PC 트레이의 서버용 Codex 로그인에서 한 번 로그인하세요."}
	}
	args := buildCodexCLIArgs(model, effort)
	media := mediaFrom(ctx)
	if media != nil && !p.analysisOnly {
		for _, in := range media.Inputs {
			if in.ImagePath != "" {
				args = append(args, "--image", in.ImagePath)
			}
		}
	}
	if p.analysisOnly {
		args = append(args, "-c", `web_search="disabled"`, "--disable", "code_mode_host", "--disable", "image_generation", "--disable", "view_image")
	}
	if sharedCodexHome(executable) != "" {
		args = append(args, "-c", `cli_auth_credentials_store="file"`)
	}
	if schema != "" {
		f, err := os.CreateTemp("", "ontech-codex-schema-*.json")
		if err != nil {
			return "", err
		}
		name := f.Name()
		defer os.Remove(name)
		_, writeErr := f.WriteString(schema)
		closeErr := f.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			return "", err
		}
		args = append(args, "--output-schema", name)
	}
	// Never give the model the server source or user workspace as its native-tool CWD.
	// Workspace changes are structured requests checked by the server's Agent tools.
	out, diagnostic, err := runner.Run(ctx, executable, args, prompt, "")
	if err != nil {
		if media != nil && !p.analysisOnly {
			_ = collectCodexImages(out, codexMediaHome(executable), &aiMedia{})
		}
		if errors.Is(err, os.ErrPermission) {
			return "", AIExecutionError{Code: "AI_CLI_ACCESS_DENIED", Public: "서버 실행 계정에 Codex 실행 권한이 없습니다. 최신 설치 파일을 다시 실행해 공용 Codex를 준비한 뒤 트레이의 서버용 Codex 로그인에서 로그인하세요."}
		}
		return "", codexFailure(ctx, append(diagnostic, out...))
	}
	text, err := parseCodexResponse(out)
	if !p.analysisOnly {
		if err == nil {
			err = collectCodexImages(out, codexMediaHome(executable), media)
		} else if media != nil {
			_ = collectCodexImages(out, codexMediaHome(executable), &aiMedia{})
		}
	}
	return text, err
}

func (x *defaultAIExecutor) executeCodex(ctx context.Context, p AIProviderConfig, m AIModelConfig, effort string, messages []AIWireMessage) (AIExecutionResult, error) {
	prompt, err := renderCLIConversationForProvider(p, messages)
	if err != nil {
		return AIExecutionResult{}, err
	}
	text, err := runCodex(ctx, x.runner, p, m.Model, effort, prompt, "")
	if errors.Is(err, context.DeadlineExceeded) {
		err = AIExecutionError{Code: "AI_TIMEOUT", Public: "AI 응답 시간이 초과되었습니다."}
	}
	result := AIExecutionResult{Content: text}
	if effort != aiProviderDefaultEffort {
		result.SentEffort = effort
	}
	return result, err
}

func nativeCodexExecutable(executable string) (string, error) {
	ext := strings.ToLower(filepath.Ext(executable))
	if ext != ".cmd" && ext != ".bat" && ext != ".ps1" {
		return executable, nil
	}
	dir := filepath.Dir(executable)
	patterns := []string{
		filepath.Join(dir, "node_modules", "@openai", "codex*", "vendor", "*", "codex", "codex.exe"),
		filepath.Join(dir, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex*", "vendor", "*", "codex", "codex.exe"),
		filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.exe"),
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			if regularCLIFile(path) {
				return path, nil
			}
		}
	}
	return "", AIExecutionError{Code: "AI_CLI_NATIVE_EXECUTABLE_MISSING", Public: "Codex 실행 스크립트는 찾았지만 네이티브 실행 파일이 없습니다. 설치 PC에서 Codex를 다시 설치하거나 codex.exe 경로를 지정하세요."}
}
