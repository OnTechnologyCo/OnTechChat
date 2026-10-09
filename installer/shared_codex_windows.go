//go:build windows

package main

import (
	"context"
	"debug/pe"
	"errors"
	"fmt"
	serverapp "ontechdev.com/ontechchat/server"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// 실행 엔진만 복사한다. auth.json/config.toml/사용자 작업 파일은 복사하지 않는다.
var sharedCodexFiles = []string{"codex.exe", "codex-code-mode-host.exe", "codex-command-runner.exe", "codex-windows-sandbox-setup.exe", "rg.exe"}

func validateCodexPE(path string) error {
	f, err := pe.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("Codex CLI must be Windows x64")
	}
	return nil
}

func copySharedCodex(source, destination string) error {
	resolved, err := resolvedCodexEnginePath(source)
	if err != nil {
		return err
	}
	source = resolved
	if err := denyReparse(source); err != nil {
		return err
	}
	if err := validateCodexPE(source); err != nil {
		return err
	}
	if err := denyReparse(destination); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	for _, name := range sharedCodexFiles {
		src := filepath.Join(filepath.Dir(source), name)
		if name == "codex.exe" {
			src = source
		}
		info, err := os.Stat(src)
		if os.IsNotExist(err) && name != "codex.exe" {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 768<<20 {
			return errors.New("invalid Codex engine file")
		}
		src, err = resolvedCodexEnginePath(src)
		if err != nil {
			return err
		}
		if err = denyReparse(src); err != nil {
			return err
		}
		if err = copyFile(src, filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	return nil
}
func stageSharedCodex(stage string, p paths) (bool, error) {
	source, err := serverapp.FindCodexForSharedInstall()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	target, _ := serverapp.SharedCodexLocations(stage, p.data)
	if err = copySharedCodex(source, filepath.Dir(target)); err != nil {
		return false, err
	}
	return true, nil
}

func sharedCodexServiceSID() (string, error) {
	sid, _, _, err := syscall.LookupSID("", `NT SERVICE\`+serviceName)
	if err != nil {
		return "", err
	}
	return sid.String()
}
func protectSharedCodexHome(p paths) error {
	_, home := serverapp.SharedCodexLocations(p.program, p.data)
	if err := denyReparse(home); err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	sid, err := sharedCodexServiceSID()
	if err != nil {
		return err
	}
	return filepath.WalkDir(filepath.Join(p.data, "ai-cli"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = denyReparse(path); e != nil {
			return e
		}
		return protectData(path, sid, true)
	})
}
func protectSharedCodexProgram(p paths, sid string) error {
	exe, _ := serverapp.SharedCodexLocations(p.program, p.data)
	root := filepath.Dir(filepath.Dir(exe))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = denyReparse(path); e != nil {
			return e
		}
		return applyPathDACL(path, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)(A;OICI;0x1200a9;;;"+sid+")")
	})
}

func sharedCodexLogin(p paths) error {
	if err := checkOwned(p); err != nil {
		return err
	}
	exe, _ := serverapp.SharedCodexLocations(p.program, p.data)
	if err := denyReparse(exe); err != nil {
		return err
	}
	if err := validateCodexPE(exe); err != nil {
		return errors.New("공용 Codex CLI가 준비되지 않았습니다. 이 PC에 Codex를 설치한 뒤 OnTechChat 설치 파일을 다시 실행하세요.")
	}
	if err := protectSharedCodexHome(p); err != nil {
		return err
	}
	note := "서버용 Codex 로그인을 진행합니다.\n\n열리는 CLI 창에서 브라우저 로그인 또는 기기 코드 로그인을 선택하세요.\n채팅 사용자의 AI 요청은 여기서 연결한 AI 계정의 사용 한도를 함께 사용합니다.\n\nWindows 계정/암호 설정은 필요 없습니다. 개인 Codex 로그인은 변경하지 않습니다.\n인증 정보는 관리자와 서버 서비스만 접근하는 전용 폴더에 저장합니다."
	if message(note+"\n\n로그인을 시작할까요?", 0x24) != 6 {
		return nil
	}
	return sharedCodexLoginConsole(p)
}
func sharedCodexLoginConsole(p paths) error {
	if err := checkOwned(p); err != nil {
		return err
	}
	exe, home := serverapp.SharedCodexLocations(p.program, p.data)
	if err := denyReparse(exe); err != nil {
		return err
	}
	if err := validateCodexPE(exe); err != nil {
		return err
	}
	if err := protectSharedCodexHome(p); err != nil {
		return err
	}
	kernel.NewProc("AllocConsole").Call()
	defer kernel.NewProc("FreeConsole").Call()
	kernel.NewProc("SetConsoleOutputCP").Call(65001)
	kernel.NewProc("SetConsoleCP").Call(65001)
	kernel.NewProc("SetConsoleTitleW").Call(ptr(wide("OnTechChat 서버용 Codex 로그인")))
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer out.Close()
	in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	fmt.Fprintln(out, "OnTechChat 서버용 Codex 로그인\n1: 브라우저 로그인 (기본)\n2: 기기 코드 로그인\n번호 입력 후 Enter:")
	var choice string
	fmt.Fscanln(in, &choice)
	args := []string{"-c", `cli_auth_credentials_store="file"`, "login"}
	if strings.TrimSpace(choice) == "2" {
		args = append(args, "--device-auth")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = serverapp.SharedCodexEnvironment(home)
	cmd.Dir = home
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out
	if err = cmd.Run(); err != nil {
		fmt.Fprintln(out, "\n로그인이 완료되지 않았습니다. 위 오류를 확인한 뒤 Enter를 누르세요.")
		fmt.Fscanln(in, &choice)
		return errors.New("Codex 로그인이 완료되지 않았습니다. CLI 창의 오류를 확인하고 트레이의 서버용 Codex 로그인에서 다시 시도하세요.")
	}
	if err = protectSharedCodexHome(p); err != nil {
		return err
	}
	auth, err := os.Stat(filepath.Join(home, "auth.json"))
	if err != nil || !auth.Mode().IsRegular() {
		return errors.New("서버용 인증 파일이 생성되지 않았습니다. 조직의 인증 저장 정책을 확인하세요.")
	}
	message("서버용 Codex 로그인 완료.\n서비스 계정 변경이나 서버 재시작 없이 AI 설정의 실제 호출을 확인하세요.", 0x40)
	return nil
}

// 표준 Codex 설치기의 bin 링크는 열린 파일 핸들로 실제 로컬 엔진 경로를 구한다.
// 목적지에는 여전히 링크/정션을 허용하지 않는다.
func resolvedCodexEnginePath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]uint16, 32768)
	n, _, e := kernel.NewProc("GetFinalPathNameByHandleW").Call(f.Fd(), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
	if n == 0 || n >= uintptr(len(b)) {
		return "", fmt.Errorf("resolve Codex engine: %w", e)
	}
	resolved := syscall.UTF16ToString(b[:n])
	if strings.HasPrefix(resolved, `\\?\UNC\`) {
		return "", errors.New("Codex engine must be on a local disk")
	}
	resolved = strings.TrimPrefix(resolved, `\\?\`)
	if !filepath.IsAbs(resolved) {
		return "", errors.New("invalid Codex engine path")
	}
	return resolved, nil
}
