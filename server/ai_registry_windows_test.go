//go:build windows

package server

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestAIWindowsRegistryUnicodePaths(t *testing.T) {
	for _, want := range []string{"C:\\Users\\다른 사용자\\AppData\\Local", "D:\\Profiles\\测试 이름\\Tools", "D:\\Users\\User 🛠\\bin"} {
		units := append(utf16.Encode([]rune(want)), 0)
		raw := make([]byte, len(units)*2)
		for i, n := range units {
			binary.LittleEndian.PutUint16(raw[2*i:], n)
		}
		if got := decodeRegistryUTF16(raw); got != want {
			t.Fatalf("Unicode path lost: got=%q want=%q", got, want)
		}
	}
	if decodeRegistryUTF16([]byte{1}) != "" {
		t.Fatal("odd UTF16 accepted")
	}
}
func TestAIWindowsNativeRegistryProfiles(t *testing.T) {
	profiles := nativeWindowsCLIProfiles()
	if len(profiles) == 0 {
		t.Fatal("native ProfileList returned no profiles")
	}
	for _, p := range profiles {
		if !strings.HasPrefix(p.SID, "S-1-") || !filepath.IsAbs(p.Path) {
			t.Fatal("invalid profile record")
		}
	}
}
func TestAICodexUnicodeInstallAndExplicitPath(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "한글 사용자")
	exe := filepath.Join(profile, "AppData", "Local", "Programs", "OpenAI", "Codex", "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("USERPROFILE", profile)
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("APPDATA", "")
	p := AIProviderConfig{Type: "codex_cli", Enabled: true, Executable: "codex"}
	got, ok := resolveAIConfigurationExecutable(p)
	if !ok || got != exe {
		t.Fatalf("Unicode install unresolved: %q", got)
	}
	// 서비스처럼 HOME/PATH가 달라도 명시 경로는 계정명 가정 없이 우선한다.
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "service-profile"))
	p.Executable = "\"" + exe + "\""
	if got, ok = resolveAIConfigurationExecutable(p); !ok || got != exe {
		t.Fatalf("explicit path unresolved: %q", got)
	}
}
func TestAICLIDiagnosticsContainsNoCredentials(t *testing.T) {
	p := AIProviderConfig{Type: "codex_cli", Executable: "codex", apiKey: "secret"}
	d := aiCLIDiagnostics(p)
	if len(d) != 4 || d["server_account"] == "" || d["server_host"] == "" || d["server_version"] != version {
		t.Fatal("missing execution identity")
	}
	for _, v := range d {
		if strings.Contains(v, "secret") {
			t.Fatal("credential exposed")
		}
	}
	if aiCLIDiagnostics(AIProviderConfig{Type: "openai_compatible"}) != nil {
		t.Fatal("API returned CLI diagnostics")
	}
}
