package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAISharedCodexHomeIsBoundToManagedExecutable(t *testing.T) {
	root := t.TempDir()
	program, data := filepath.Join(root, "공용 프로그램"), filepath.Join(root, "서버 데이터")
	exe, home := SharedCodexLocations(program, data)
	if sharedCodexHomeFor(exe, program, data) != home {
		t.Fatal("shared home mismatch")
	}
	for _, other := range []string{filepath.Join(root, "personal", "codex.exe"), filepath.Join(program, "cli", "claude.exe"), exe + ".bak"} {
		if sharedCodexHomeFor(other, program, data) != "" {
			t.Fatal("unmanaged CLI borrowed server auth")
		}
	}
	if sharedCodexHomeFor(exe, "", data) != "" {
		t.Fatal("invalid roots accepted")
	}
}
func TestAISharedCodexEnvironmentIgnoresPersonalAuth(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "personal"))
	t.Setenv("OPENAI_API_KEY", "must-not-inherit")
	t.Setenv("CODEX_ACCESS_TOKEN", "must-not-inherit")
	home := filepath.Join(t.TempDir(), "공용 인증")
	env := SharedCodexEnvironment(home)
	count := 0
	for _, v := range env {
		if strings.Contains(v, "must-not-inherit") {
			t.Fatal("personal credential inherited")
		}
		if strings.HasPrefix(strings.ToUpper(v), "CODEX_HOME=") {
			count++
			if v != "CODEX_HOME="+home {
				t.Fatal("wrong home")
			}
		}
	}
	if count != 1 {
		t.Fatal("duplicate or missing CODEX_HOME")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("environment construction must not read or create credentials")
	}
}
