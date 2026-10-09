//go:build windows

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type agentIsolationProbe struct {
	WorkspaceRead bool
	SecretRead    bool
	SecretEnv     bool
	Network       bool
	ProcessOpen   bool
}

func writeAgentProbeSource(t *testing.T, dir string) {
	t.Helper()
	src := `package main
import (
 "encoding/json"
 "net"
 "os"
 "strconv"
 "syscall"
 "time"
)
type result struct { WorkspaceRead bool; SecretRead bool; SecretEnv bool; Network bool; ProcessOpen bool }
func main() {
 if len(os.Args)>1 && os.Args[1]=="sleep" {
  if len(os.Args)>2 { go func(){ time.Sleep(1500*time.Millisecond); _=os.WriteFile(os.Args[2],[]byte("escaped"),0600) }() }
  for { time.Sleep(time.Second) }
 }
 var r result
 _,e:=os.ReadFile("allowed.txt"); r.WorkspaceRead=e==nil
 r.SecretEnv=os.Getenv("ONTECH_TEST_SECRET")!=""
 if len(os.Args)>1 { _,e=os.ReadFile(os.Args[1]); r.SecretRead=e==nil }
 if len(os.Args)>2 { c,e:=net.DialTimeout("tcp",os.Args[2],300*time.Millisecond); if e==nil { r.Network=true; c.Close() } }
 if len(os.Args)>3 { if pid,e:=strconv.Atoi(os.Args[3]); e==nil { h,e:=syscall.OpenProcess(0x0400,false,uint32(pid)); if e==nil { r.ProcessOpen=true; syscall.CloseHandle(h) } } }
 _=json.NewEncoder(os.Stdout).Encode(r)
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.go"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "allowed.txt"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
}

func agentTestEnv(workspace, goroot string) map[string]string {
	return map[string]string{
		"SystemRoot":   os.Getenv("SystemRoot"),
		"WINDIR":       os.Getenv("WINDIR"),
		"SystemDrive":  os.Getenv("SystemDrive"),
		"ComSpec":      os.Getenv("ComSpec"),
		"PATHEXT":      os.Getenv("PATHEXT"),
		"PATH":         filepath.Join(os.Getenv("SystemRoot"), "System32"),
		"ProgramFiles": os.Getenv("ProgramFiles"),
		"GOROOT":       goroot,
		"GOCACHE":      filepath.Join(workspace, "gocache"),
		"GOPATH":       filepath.Join(workspace, "gopath"),
		// SYSTEM 계정의 GetTempPath2도 샌드박스 밖 Windows 임시 폴더를 사용하지 않게 합니다.
		"GOTMPDIR":     filepath.Join(workspace, "tmp"),
		"GOENV":        "off",
		"GOFLAGS":      "-p=1",
		"CGO_ENABLED":  "0",
		"GOOS":         "windows",
		"GOARCH":       "amd64",
		"HOME":         filepath.Join(workspace, "home"),
		"USERPROFILE":  filepath.Join(workspace, "home"),
		"LOCALAPPDATA": filepath.Join(workspace, "home", "AppData", "Local"),
		"APPDATA":      filepath.Join(workspace, "home", "AppData", "Roaming"),
		"TEMP":         filepath.Join(workspace, "tmp"),
		"TMP":          filepath.Join(workspace, "tmp"),
	}
}

func prepareAgentDirs(t *testing.T, workspace string) {
	t.Helper()
	for _, name := range []string{"gocache", "gopath", "home", "home\\AppData\\Local", "home\\AppData\\Roaming", "tmp"} {
		if err := os.MkdirAll(filepath.Join(workspace, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentWindowsAppContainerBuildAndIsolation(t *testing.T) {
	t.Setenv("ONTECH_TEST_SECRET", "must-not-enter-runner")
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goroot := filepath.Join(root, "tools", "go")
	goexe := filepath.Join(goroot, "bin", "go.exe")
	if _, err = os.Stat(goexe); err != nil {
		t.Skipf("project Go toolchain unavailable: %v", err)
	}
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	secretDir := filepath.Join(base, "secret")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(secretDir, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "server-secret.txt")
	if err = os.WriteFile(secret, []byte("do-not-read"), 0600); err != nil {
		t.Fatal(err)
	}
	writeAgentProbeSource(t, workspace)
	prepareAgentDirs(t, workspace)

	cfg := defaultAgentConfig()
	cfg.TimeoutSeconds = 90
	runner := newAgentRunner(cfg)
	owner := "windows-isolation-" + randomID()
	defer deleteAppContainerProfile(owner)
	build, err := runner.Run(context.Background(), agentRunRequest{
		OwnerID: owner, ProjectID: "probe", Executable: goexe,
		Args: []string{"build", "-trimpath", "-o", "probe.exe", "probe.go"},
		Dir:  workspace, Env: agentTestEnv(workspace, goroot), ReadOnlyPaths: []string{goroot},
	})
	if err != nil {
		t.Fatalf("AppContainer Go build failed: %v stdout=%s stderr=%s", err, build.Stdout, build.Stderr)
	}
	if build.ExitCode != 0 || !build.IsAppContainer || build.AppContainerSID == "" {
		t.Fatalf("invalid build result: %+v", build)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	probeExe := filepath.Join(workspace, "probe.exe")
	run, err := runner.Run(context.Background(), agentRunRequest{
		OwnerID: owner, ProjectID: "probe", Executable: probeExe,
		Args: []string{secret, listener.Addr().String(), strconv.Itoa(os.Getpid())},
		Dir:  workspace, Env: agentTestEnv(workspace, goroot),
	})
	if err != nil {
		t.Fatalf("probe run failed: %v stderr=%s", err, run.Stderr)
	}
	if run.ExitCode != 0 || !run.IsAppContainer || run.AppContainerSID != build.AppContainerSID {
		t.Fatalf("unexpected run result: %+v", run)
	}
	var got agentIsolationProbe
	if err = json.Unmarshal([]byte(strings.TrimSpace(run.Stdout)), &got); err != nil {
		t.Fatalf("probe JSON: %v output=%q", err, run.Stdout)
	}
	if !got.WorkspaceRead || got.SecretRead || got.SecretEnv || got.Network || got.ProcessOpen {
		t.Fatalf("isolation failed: %+v", got)
	}
}

func TestAgentWindowsDistinctSIDAndJobCancellation(t *testing.T) {
	root, _ := filepath.Abs("..")
	goroot := filepath.Join(root, "tools", "go")
	goexe := filepath.Join(goroot, "bin", "go.exe")
	if _, err := os.Stat(goexe); err != nil {
		t.Skipf("project Go toolchain unavailable: %v", err)
	}
	workspace := t.TempDir()
	writeAgentProbeSource(t, workspace)
	prepareAgentDirs(t, workspace)
	cfg := defaultAgentConfig()
	cfg.TimeoutSeconds = 30
	runner := newAgentRunner(cfg)
	ownerA, ownerB := "sid-a-"+randomID(), "sid-b-"+randomID()
	defer deleteAppContainerProfile(ownerA)
	defer deleteAppContainerProfile(ownerB)
	build, err := runner.Run(context.Background(), agentRunRequest{
		OwnerID: ownerA, ProjectID: "p", Executable: goexe, Args: []string{"build", "-o", "probe.exe", "probe.go"},
		Dir: workspace, Env: agentTestEnv(workspace, goroot), ReadOnlyPaths: []string{goroot},
	})
	if err != nil || build.ExitCode != 0 {
		t.Fatalf("build failed: %v %+v", err, build)
	}
	exe := filepath.Join(workspace, "probe.exe")
	second, err := runner.Run(context.Background(), agentRunRequest{OwnerID: ownerB, ProjectID: "p", Executable: exe, Dir: workspace, Env: agentTestEnv(workspace, goroot)})
	if err != nil {
		t.Fatal(err)
	}
	if second.AppContainerSID == build.AppContainerSID {
		t.Fatal("different owners received the same AppContainer SID")
	}
	marker := filepath.Join(workspace, "escape-marker.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, agentRunRequest{OwnerID: ownerA, ProjectID: "p", Executable: exe, Args: []string{"sleep", marker}, Dir: workspace, Env: agentTestEnv(workspace, goroot)})
	if err == nil || (!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)) {
		t.Fatalf("cancel result=%v", err)
	}
	time.Sleep(1800 * time.Millisecond)
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("job child escaped cancellation: %v", err)
	}
	_ = fmt.Sprintf("%s%s", ownerA, ownerB)
}

func TestAgentWindowsExplicitEnvironment(t *testing.T) {
	workspace := t.TempDir()
	prepareAgentDirs(t, workspace)
	cfg := defaultAgentConfig()
	cfg.TimeoutSeconds = 10
	runner := newAgentRunner(cfg)
	owner := "env-" + randomID()
	defer deleteAppContainerProfile(owner)
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	env := map[string]string{
		"SystemRoot":   os.Getenv("SystemRoot"),
		"WINDIR":       os.Getenv("WINDIR"),
		"TEMP":         filepath.Join(workspace, "tmp"),
		"TMP":          filepath.Join(workspace, "tmp"),
		"USERPROFILE":  filepath.Join(workspace, "home"),
		"LOCALAPPDATA": filepath.Join(workspace, "home", "AppData", "Local"),
		"APPDATA":      filepath.Join(workspace, "home", "AppData", "Roaming"),
	}
	res, err := runner.Run(context.Background(), agentRunRequest{
		OwnerID: owner, ProjectID: "env", Executable: cmd,
		Args: []string{"/d", "/c", "exit /b 0"}, Dir: workspace, Env: env,
	})
	if err != nil {
		t.Fatalf("explicit environment failed: %v", err)
	}
	if res.ExitCode != 0 || !res.IsAppContainer {
		t.Fatalf("result=%+v", res)
	}
}

func TestAgentWindowsRejectsLinkEscapePrimitives(t *testing.T) {
	cfg := defaultAgentConfig()
	runner := newAgentRunner(cfg)
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")

	t.Run("hardlink", func(t *testing.T) {
		workspace := t.TempDir()
		original := filepath.Join(workspace, "a.txt")
		link := filepath.Join(workspace, "b.txt")
		if err := os.WriteFile(original, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(original, link); err != nil {
			t.Skipf("hard link unavailable: %v", err)
		}
		_, err := runner.Run(context.Background(), agentRunRequest{
			OwnerID: "hardlink-" + randomID(), ProjectID: "p", Executable: cmd,
			Args: []string{"/d", "/c", "exit /b 0"}, Dir: workspace,
			Env: map[string]string{"SystemRoot": os.Getenv("SystemRoot"), "WINDIR": os.Getenv("WINDIR"), "LOCALAPPDATA": filepath.Join(workspace, "local")},
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "hard") {
			t.Fatalf("hard link workspace was not rejected: %v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		workspace := t.TempDir()
		target := t.TempDir()
		link := filepath.Join(workspace, "escape")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable on this Windows configuration: %v", err)
		}
		_, err := runner.Run(context.Background(), agentRunRequest{
			OwnerID: "symlink-" + randomID(), ProjectID: "p", Executable: cmd,
			Args: []string{"/d", "/c", "exit /b 0"}, Dir: workspace,
			Env: map[string]string{"SystemRoot": os.Getenv("SystemRoot"), "WINDIR": os.Getenv("WINDIR"), "LOCALAPPDATA": filepath.Join(workspace, "local")},
		})
		if err == nil {
			t.Fatal("symlink workspace was not rejected")
		}
	})
}

func writeAgentResourceProbe(t *testing.T, dir string) {
	t.Helper()
	src := `package main
import("fmt";"os";"os/exec";"strings";"time")
func main(){
 if len(os.Args)<2{return}
 switch os.Args[1]{
 case "output":
  fmt.Print(strings.Repeat("X",8192))
 case "cpu":
  for{}
 case "memory":
  b:=make([]byte,256<<20);for i:=0;i<len(b);i+=4096{b[i]=1};fmt.Println(len(b));time.Sleep(time.Second)
 case "spawn":
  e:=exec.Command(os.Args[0],"child").Run();if e!=nil{fmt.Print("BLOCKED");return};fmt.Print("CHILD_RAN")
 case "child":
  time.Sleep(200*time.Millisecond)
 }
}
`
	if err := os.WriteFile(filepath.Join(dir, "resource_probe.go"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentWindowsResourceLimits(t *testing.T) {
	root, _ := filepath.Abs("..")
	goroot := filepath.Join(root, "tools", "go")
	goexe := filepath.Join(goroot, "bin", "go.exe")
	if _, err := os.Stat(goexe); err != nil {
		t.Skipf("project Go toolchain unavailable: %v", err)
	}
	workspace := t.TempDir()
	writeAgentResourceProbe(t, workspace)
	prepareAgentDirs(t, workspace)
	owner := "resource-" + randomID()
	defer deleteAppContainerProfile(owner)

	buildCfg := defaultAgentConfig()
	buildCfg.TimeoutSeconds = 90
	buildRunner := newAgentRunner(buildCfg)
	build, err := buildRunner.Run(context.Background(), agentRunRequest{
		OwnerID: owner, ProjectID: "resource", Executable: goexe,
		Args: []string{"build", "-trimpath", "-o", "resource_probe.exe", "resource_probe.go"},
		Dir:  workspace, Env: agentTestEnv(workspace, goroot), ReadOnlyPaths: []string{goroot},
	})
	if err != nil || build.ExitCode != 0 {
		t.Fatalf("resource probe build failed: %v %+v", err, build)
	}
	exe := filepath.Join(workspace, "resource_probe.exe")

	t.Run("output", func(t *testing.T) {
		cfg := defaultAgentConfig()
		cfg.OutputMaxBytes = 1024
		r := newAgentRunner(cfg)
		_, err := r.Run(context.Background(), agentRunRequest{
			OwnerID: owner, ProjectID: "resource-output", Executable: exe,
			Args: []string{"output"}, Dir: workspace, Env: agentTestEnv(workspace, goroot),
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "output exceeded") {
			t.Fatalf("output limit not enforced: %v", err)
		}
	})

	t.Run("process", func(t *testing.T) {
		cfg := defaultAgentConfig()
		cfg.ProcessLimit = 1
		r := newAgentRunner(cfg)
		res, err := r.Run(context.Background(), agentRunRequest{
			OwnerID: owner, ProjectID: "resource-process", Executable: exe,
			Args: []string{"spawn"}, Dir: workspace, Env: agentTestEnv(workspace, goroot),
		})
		if err != nil {
			t.Fatalf("process-limit probe failed: %v", err)
		}
		if !strings.Contains(res.Stdout, "BLOCKED") || strings.Contains(res.Stdout, "CHILD_RAN") {
			t.Fatalf("child process was not blocked: %+v", res)
		}
	})

	t.Run("cpu_time", func(t *testing.T) {
		cfg := defaultAgentConfig()
		cfg.TimeoutSeconds = 1
		r := newAgentRunner(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		started := time.Now()
		res, err := r.Run(ctx, agentRunRequest{
			OwnerID: owner, ProjectID: "resource-cpu", Executable: exe,
			Args: []string{"cpu"}, Dir: workspace, Env: agentTestEnv(workspace, goroot),
		})
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Fatalf("Job CPU limit did not terminate before context deadline: %v", err)
		}
		if time.Since(started) >= 20*time.Second || (err == nil && res.ExitCode == 0) {
			t.Fatalf("CPU time limit not enforced: err=%v result=%+v elapsed=%s", err, res, time.Since(started))
		}
	})

	t.Run("memory", func(t *testing.T) {
		cfg := defaultAgentConfig()
		cfg.MemoryMB = 64
		r := newAgentRunner(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := r.Run(ctx, agentRunRequest{
			OwnerID: owner, ProjectID: "resource-memory", Executable: exe,
			Args: []string{"memory"}, Dir: workspace, Env: agentTestEnv(workspace, goroot),
		})
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Fatalf("memory limit did not terminate before context deadline: %v", err)
		}
		if err == nil && res.ExitCode == 0 {
			t.Fatalf("memory limit not enforced: %+v", res)
		}
	})
}
