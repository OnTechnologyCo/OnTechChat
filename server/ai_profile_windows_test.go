//go:build windows

package server

import (
	"path/filepath"
	"testing"
)

func TestAICLIProfileMatchesOnlyCurrentSID(t *testing.T) {
	profiles := []windowsProfileEntry{
		{SID: "S-1-5-21-1001", Path: `D:\Profiles\다른 사용자`},
		{SID: "S-1-5-21-1002", Path: `E:\WindowsUsers\선택 계정`},
	}
	if got := cliProfileForSID("S-1-5-21-1002", profiles); got != profiles[1].Path {
		t.Fatalf("wrong profile: %q", got)
	}
	if got := cliProfileForSID("S-1-5-19", profiles); got != "" {
		t.Fatal("borrowed another user's profile")
	}
	if cliProfileForSID("S-1-5-21-1002", []windowsProfileEntry{{SID: "S-1-5-21-1002", Path: "relative"}}) != "" {
		t.Fatal("relative profile accepted")
	}
}
func TestAICLIProfileEnvironment(t *testing.T) {
	profile := `E:\WindowsUsers\선택 계정`
	env := cliProfileEnvironment(profile)
	if env["HOME"] != profile || env["USERPROFILE"] != profile {
		t.Fatal("home mismatch")
	}
	if env["LOCALAPPDATA"] != filepath.Join(profile, "AppData", "Local") || env["APPDATA"] != filepath.Join(profile, "AppData", "Roaming") {
		t.Fatal("app data mismatch")
	}
	if env["HOMEDRIVE"] != "E:" || env["HOMEPATH"] != `\WindowsUsers\선택 계정` {
		t.Fatal("drive hardcoded")
	}
	if len(env) != 6 {
		t.Fatal("unexpected authentication override")
	}
	if cliProfileEnvironment("") != nil || cliProfileEnvironment("relative") != nil {
		t.Fatal("invalid profile accepted")
	}
}
