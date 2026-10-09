package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 공용 실행 파일과 서버 전용 인증 홈. 개인 계정 경로를 사용하지 않는다.
func SharedCodexLocations(program, data string) (string, string) {
	return filepath.Join(program, "cli", "codex", "codex.exe"), filepath.Join(data, "ai-cli", "codex")
}
func sharedCodexHomeFor(executable, program, data string) string {
	if program == "" || data == "" {
		return ""
	}
	expected, home := SharedCodexLocations(program, data)
	if strings.EqualFold(filepath.Clean(executable), filepath.Clean(expected)) {
		return home
	}
	return ""
}
func SharedCodexEnvironment(home string) []string {
	env := safeAIProcessEnv()
	out := make([]string, 0, len(env)+1)
	for _, v := range env {
		if !strings.HasPrefix(strings.ToUpper(v), "CODEX_HOME=") {
			out = append(out, v)
		}
	}
	return append(out, "CODEX_HOME="+home)
}

// 설치기에서 실행 파일만 탐색한다. 사용자 인증 파일은 열거나 복사하지 않는다.
func FindCodexForSharedInstall() (string, error) {
	if path, err := exec.LookPath("codex"); err == nil && regularCLIFile(path) {
		if native, err := nativeCodexExecutable(path); err == nil {
			return native, nil
		}
	}
	if path, ok := knownWindowsCLIExecutable("codex_cli"); ok {
		if native, err := nativeCodexExecutable(path); err == nil {
			return native, nil
		}
	}
	if path := sharedCodexExecutable(); regularCLIFile(path) {
		return path, nil
	}
	return "", os.ErrNotExist
}
