package server

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func cliCommandName(providerType string) string {
	switch providerType {
	case "codex_cli":
		return "codex"
	case "claude_cli":
		return "claude"
	}
	return ""
}

func aiCLIExecutableState(p AIProviderConfig) (bool, string) {
	if _, ok := resolveAIConfigurationExecutable(p); ok {
		return true, "configured"
	}
	raw := strings.Trim(strings.TrimSpace(p.Executable), "\"")
	candidates := []string{}
	if filepath.IsAbs(raw) {
		candidates = append(candidates, raw)
	}
	candidates = append(candidates, windowsCLIExecutableCandidates(p.Type)...)
	for _, path := range candidates {
		if _, err := os.Stat(path); errors.Is(err, os.ErrPermission) {
			return false, "executable_access_denied"
		}
	}
	return false, "executable_missing"
}

// 관리자 설정 검사에만 제공한다. 인증 파일/토큰/전체 환경 변수는 반환하지 않는다.
func aiCLIDiagnostics(p AIProviderConfig) map[string]string {
	if cliCommandName(p.Type) == "" {
		return nil
	}
	host, _ := os.Hostname()
	account := ""
	if current, err := user.Current(); err == nil {
		account = current.Username
	}
	return map[string]string{"server_host": host, "server_account": account, "server_version": version, "configured_executable": p.Executable}
}

func cliProfileEnvironment(profile string) map[string]string {
	if profile == "" || !filepath.IsAbs(profile) {
		return nil
	}
	volume := filepath.VolumeName(profile)
	return map[string]string{
		"USERPROFILE": profile, "HOME": profile,
		"APPDATA":      filepath.Join(profile, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(profile, "AppData", "Local"),
		"HOMEDRIVE":    volume, "HOMEPATH": strings.TrimPrefix(profile, volume),
	}
}
