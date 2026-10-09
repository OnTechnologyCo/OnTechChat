//go:build windows

package main

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func testDACL(t *testing.T, path string) string {
	t.Helper()
	var needed uint32
	get := advapi.NewProc("GetFileSecurityW")
	get.Call(ptr(wide(path)), 4, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		t.Fatal("security descriptor size missing")
	}
	raw := make([]byte, needed)
	ok, _, err := get.Call(ptr(wide(path)), 4, uintptr(unsafe.Pointer(&raw[0])), uintptr(len(raw)), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		t.Fatal(err)
	}
	var out *uint16
	ok, _, err = advapi.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW").Call(uintptr(unsafe.Pointer(&raw[0])), 1, 4, uintptr(unsafe.Pointer(&out)), 0)
	if ok == 0 {
		t.Fatal(err)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(unsafe.Pointer(out))))
	units := unsafe.Slice(out, 4096)
	n := 0
	for n < len(units) && units[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(units[:n])
}
func TestSharedCodexACLSeparatesEngineAndAuth(t *testing.T) {
	root := t.TempDir()
	owner, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, rel := range []string{"auth", filepath.Join("program", "cli"), filepath.Join("program", "cli", "codex"), filepath.Join("program", "cli", "codex", "codex.exe")} {
			path := filepath.Join(root, rel)
			if e := applyPathDACL(path, "D:P(A;OICI;FA;;;"+owner.Uid+")"); e != nil && !os.IsNotExist(e) {
				t.Error(e)
			}
			_ = os.Chmod(path, 0700)
		}
	})
	sid := "S-1-5-80-1-2-3-4-5"
	auth := filepath.Join(root, "auth")
	if err := os.Mkdir(auth, 0700); err != nil {
		t.Fatal(err)
	}
	if err := protectData(auth, sid, true); err != nil {
		t.Fatal(err)
	}
	dacl := testDACL(t, auth)
	if !strings.Contains(dacl, sid) || strings.Contains(dacl, ";;;BU)") || strings.Contains(dacl, ";;;AU)") || strings.Contains(dacl, ";;;WD)") {
		t.Fatal("auth permissions too broad")
	}
	p := paths{program: filepath.Join(root, "program")}
	dir := filepath.Join(p.program, "cli", "codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex.exe"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := protectSharedCodexProgram(p, sid); err != nil {
		t.Fatal(err)
	}
	dacl = testDACL(t, filepath.Join(dir, "codex.exe"))
	if !strings.Contains(dacl, sid) || !strings.Contains(dacl, ";;;BU)") {
		t.Fatal("shared engine cannot be read by users/service")
	}
	if strings.Contains(dacl, "FA;;;BU)") || strings.Contains(dacl, "FA;;;"+sid+")") {
		t.Fatal("engine is writable by non-admin")
	}
}
