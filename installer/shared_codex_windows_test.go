//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	serverapp "ontechdev.com/ontechchat/server"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedCodexCopiesOnlyEngineFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "개인 계정")
	destination := filepath.Join(root, "공용 프로그램")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(source, "codex.exe")
	if err = copyFile(self, exe); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth.json", "config.toml", "private.txt"} {
		if err = os.WriteFile(filepath.Join(source, name), []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = copySharedCodex(exe, destination); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "codex.exe" {
		t.Fatal("copied user state")
	}
	src, _ := os.ReadFile(exe)
	dst, _ := os.ReadFile(filepath.Join(destination, "codex.exe"))
	if sha256.Sum256(src) != sha256.Sum256(dst) {
		t.Fatal("engine copy damaged")
	}
	invalid := filepath.Join(root, "invalid.exe")
	if err = os.WriteFile(invalid, []byte("not a PE"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = copySharedCodex(invalid, filepath.Join(root, "rejected")); err == nil {
		t.Fatal("invalid executable accepted")
	}
}
func TestSharedCodexNativeEngineUsesDedicatedHome(t *testing.T) {
	source, err := serverapp.FindCodexForSharedInstall()
	if err != nil {
		t.Skip("native Codex not installed")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "공용 CLI")
	if err = copySharedCodex(source, dir); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "서버 인증")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe := filepath.Join(dir, "codex.exe")
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Dir = home
		cmd.Env = serverapp.SharedCodexEnvironment(home)
		return cmd.CombinedOutput()
	}
	out, err := run("--version")
	if err != nil || !bytes.Contains(out, []byte("codex")) {
		t.Fatalf("copied engine failed: %v", err)
	}
	// 네트워크 호출 없이 개인 로그인과 공용 홈이 분리되는지 실제 CLI로 확인한다.
	out, err = run("-c", `cli_auth_credentials_store="file"`, "login", "status")
	if err == nil {
		t.Fatal("empty server home unexpectedly used personal login")
	}
	if !strings.Contains(strings.ToLower(string(out)), "not logged") {
		t.Fatalf("unexpected login status error: %v", err)
	}
	if _, err = os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("smoke check created credentials")
	}
	t.Log("copied native Codex executes; empty shared home does not reuse personal credentials")
}
