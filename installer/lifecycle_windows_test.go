//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRemovalFailureAndDataChoice(t *testing.T) {
	for _, tc := range []struct {
		name, fail string
		purge      bool
		want       []string
	}{
		{"keep", "", false, []string{"firewall", "protocol", "startup", "shortcuts", "stop", "service", "program", "registry"}},
		{"purge", "", true, []string{"firewall", "protocol", "startup", "shortcuts", "stop", "service", "data", "program", "registry"}},
		{"partial", "firewall", true, []string{"firewall", "protocol", "startup", "shortcuts", "stop", "service"}},
		{"running", "stop", true, []string{"firewall", "protocol", "startup", "shortcuts", "stop"}},
		{"dataLocked", "data", true, []string{"firewall", "protocol", "startup", "shortcuts", "stop", "service", "data"}},
		{"programLocked", "program", false, []string{"firewall", "protocol", "startup", "shortcuts", "stop", "service", "program"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			step := func(name string) func() error {
				return func() error {
					calls = append(calls, name)
					if name == tc.fail {
						return errors.New("injected failure")
					}
					return nil
				}
			}
			e := runRemoval(tc.purge, removalSteps{step("firewall"), step("protocol"), step("startup"), step("shortcuts"), step("stop"), step("service"), step("data"), step("program"), step("registry")})
			if (e != nil) != (tc.fail != "") || !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("error=%v steps=%v", e, calls)
			}
		})
	}
}
func TestProgramReplacementRollback(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "program")
	stage := filepath.Join(root, "stage")
	if e := os.Mkdir(dst, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dst, "old"), []byte("previous"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := replaceProgram(stage, dst); e == nil {
		t.Fatal("missing stage accepted")
	}
	if b, e := os.ReadFile(filepath.Join(dst, "old")); e != nil || string(b) != "previous" {
		t.Fatal("previous version lost", e)
	}
	if e := os.Mkdir(stage, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(stage, "new"), []byte("new"), 0600); e != nil {
		t.Fatal(e)
	}
	backup, e := replaceProgram(stage, dst)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dst, "new")); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(backup, "old")); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dst, "old")); !os.IsNotExist(e) {
		t.Fatal("stale output copied")
	}
	if e = removeTree(backup); e != nil {
		t.Fatal(e)
	}
}
func TestUnicodeShortcutAndTargetCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "한글 ' 폴더")
	if e := os.Mkdir(root, 0755); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(root, "OnTechChat.Server.exe")
	if e := os.WriteFile(target, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	own := filepath.Join(root, "이름 변경.lnk")
	other := filepath.Join(root, "other.lnk")
	if e := createShortcut(own, target, "--tray", target); e != nil {
		t.Fatal(e)
	}
	if e := createShortcut(other, systemExe("notepad.exe"), "", systemExe("notepad.exe")); e != nil {
		t.Fatal(e)
	}
	p := paths{program: root, exe: target}
	if e := removeShortcutRoots(p, []string{root}, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(own); !os.IsNotExist(e) {
		t.Fatal("owned shortcut remains")
	}
	if _, e := os.Stat(other); e != nil {
		t.Fatal("unrelated shortcut removed", e)
	}
}
func TestLockedRemovalRetainsOwnership(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "owned")
	if e := os.Mkdir(dir, 0755); e != nil {
		t.Fatal(e)
	}
	p := paths{program: dir}
	if e := writeRecord(filepath.Join(dir, "install-record.json"), p); e != nil {
		t.Fatal(e)
	}
	locked := filepath.Join(dir, "locked")
	h, e := syscall.CreateFile(wide(locked), syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.CREATE_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		t.Fatal(e)
	}
	e = removeTree(dir)
	syscall.CloseHandle(h)
	if e == nil {
		t.Fatal("locked removal reported success")
	}
	if e = ownedDirectory(dir); e != nil {
		t.Fatal("retry marker missing", e)
	}
	if e = removeTree(dir); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("folder remains")
	}
}
func TestOwnedProcessFixture(t *testing.T) {
	ready := os.Getenv("ONTECH_PROCESS_FIXTURE")
	if ready == "" {
		return
	}
	if e := os.WriteFile(ready, []byte("ready"), 0600); e != nil {
		os.Exit(2)
	}
	time.Sleep(60 * time.Second)
	os.Exit(0)
}
func TestStopOnlyExactInstalledProcesses(t *testing.T) {
	root := t.TempDir()
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	var cmds []*exec.Cmd
	var done []chan error
	for _, folder := range []string{"installed", "unrelated"} {
		dir := filepath.Join(root, folder)
		if e = os.Mkdir(dir, 0755); e != nil {
			t.Fatal(e)
		}
		exe := filepath.Join(dir, "OnTechChat.Server.exe")
		if e = copyFile(self, exe); e != nil {
			t.Fatal(e)
		}
		c := exec.Command(exe, "-test.run=^TestOwnedProcessFixture$")
		ready := filepath.Join(dir, "ready")
		c.Env = append(os.Environ(), "ONTECH_PROCESS_FIXTURE="+ready)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if e = c.Start(); e != nil {
			t.Fatal(e)
		}
		ch := make(chan error, 1)
		go func() { ch <- c.Wait() }()
		t.Cleanup(func() { _ = c.Process.Kill() })
		cmds = append(cmds, c)
		done = append(done, ch)
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, e = os.Stat(ready); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fixture startup timeout")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	p := paths{program: filepath.Join(root, "installed"), exe: cmds[0].Path}
	if e = stopOwnedProcesses(p); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done[0]:
	case <-time.After(10 * time.Second):
		t.Fatal("owned process survived")
	}
	select {
	case <-done[1]:
		t.Fatal("unrelated same-name process was stopped")
	default:
	}
	if e = waitParent(cmds[0].Process.Pid); e != nil {
		t.Fatal("already-exited parent rejected", e)
	}
	_ = cmds[1].Process.Kill()
	<-done[1]
	if ownedRuntimePath(p, strings.ReplaceAll(p.exe, "installed", "installed-other")) {
		t.Fatal("prefix path accepted")
	}
}
