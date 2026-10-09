//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const trayRunKey = `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
const trayClass = "OnTechChat.Tray.Window.v1"

func trayExe(p paths) string { return p.exe }
func registerTray(p paths) error {
	value := syscall.EscapeArg(trayExe(p)) + " --tray --startup"
	if e := command(systemExe("reg.exe"), "add", trayRunKey, "/v", "OnTechChatTray", "/t", "REG_SZ", "/d", value, "/f", "/reg:64"); e != nil {
		return e
	}
	got, exists, e := registryReadText(`SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, "OnTechChatTray")
	if e != nil {
		return e
	}
	if !exists || got != value {
		return errors.New("트레이 자동 시작 등록 확인 실패")
	}
	return registerShortcuts(p)
}

// 설치기에서 브라우저나 트레이를 관리자 권한으로 실행하지 않습니다.
// 현재 세션의 Explorer 토큰으로 일반 사용자 트레이를 시작합니다.
func launchTrayAsDesktopUser(p paths) error {
	user := syscall.NewLazyDLL("user32.dll")
	hwnd, _, _ := user.NewProc("GetShellWindow").Call()
	if hwnd == 0 {
		return errors.New("현재 세션의 Explorer를 찾지 못했습니다")
	}
	var pid uint32
	user.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	process, _, e := kernel.NewProc("OpenProcess").Call(0x1000, 0, uintptr(pid))
	if process == 0 {
		return apiError("OpenProcess(Explorer)", e)
	}
	defer syscall.CloseHandle(syscall.Handle(process))
	var token syscall.Handle
	r, _, e := advapi.NewProc("OpenProcessToken").Call(process, 0xa, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return apiError("OpenProcessToken", e)
	}
	defer syscall.CloseHandle(token)
	var primary syscall.Handle
	r, _, e = advapi.NewProc("DuplicateTokenEx").Call(uintptr(token), 0xb, 0, 2, 1, uintptr(unsafe.Pointer(&primary)))
	if r == 0 {
		return apiError("DuplicateTokenEx", e)
	}
	defer syscall.CloseHandle(primary)
	userenv := syscall.NewLazyDLL("userenv.dll")
	var env uintptr
	r, _, e = userenv.NewProc("CreateEnvironmentBlock").Call(uintptr(unsafe.Pointer(&env)), uintptr(primary), 0)
	if r == 0 {
		return apiError("CreateEnvironmentBlock", e)
	}
	defer userenv.NewProc("DestroyEnvironmentBlock").Call(env)
	exe := wide(trayExe(p))
	line, e := syscall.UTF16FromString(syscall.EscapeArg(trayExe(p)) + " --tray --startup")
	if e != nil {
		return e
	}
	si := syscall.StartupInfo{Desktop: wide(`winsta0\default`), Flags: 1, ShowWindow: 0}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi syscall.ProcessInformation
	r, _, e = advapi.NewProc("CreateProcessWithTokenW").Call(uintptr(primary), 0, ptr(exe), uintptr(unsafe.Pointer(&line[0])), 0x08000400, env, ptr(wide(p.program)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(exe)
	runtime.KeepAlive(line)
	runtime.KeepAlive(si)
	if r == 0 {
		return apiError("CreateProcessWithTokenW(tray)", e)
	}
	syscall.CloseHandle(pi.Process)
	syscall.CloseHandle(pi.Thread)
	return waitForInstalledTray(p)
}
func waitForInstalledTray(p paths) error {
	user := syscall.NewLazyDLL("user32.dll")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		hwnd, _, _ := user.NewProc("FindWindowW").Call(ptr(wide(trayClass)), 0)
		if hwnd != 0 {
			var pid uint32
			user.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
			h, e := syscall.OpenProcess(0x1000, false, pid)
			if e == nil {
				image, e := processImage(h)
				syscall.CloseHandle(h)
				if e == nil && ownedRuntimePath(p, image) {
					var ready uintptr
					ok, _, _ := user.NewProc("SendMessageTimeoutW").Call(hwnd, 0x8004, 0, 0, 2, 200, uintptr(unsafe.Pointer(&ready)))
					if ok != 0 && ready == 1 {
						return nil
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("트레이 아이콘이 시작 시간 안에 등록되지 않았습니다")
}
func startTrayWithoutElevation() error {
	p, e := installPaths()
	if e != nil {
		return e
	}
	if isAdmin() {
		return launchTrayAsDesktopUser(p)
	}
	cmd := exec.Command(trayExe(p), "--tray")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e = cmd.Start(); e != nil {
		return e
	}
	return cmd.Process.Release()
}
func unregisterTray() error {
	// 이 제품의 고유 Run 값만 제거합니다. 다른 프로그램의 자동 시작은 변경하지 않습니다.
	key, err := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows\CurrentVersion\Run`)
	if err != nil {
		return err
	}
	var h syscall.Handle
	r, _, _ := advapi.NewProc("RegOpenKeyExW").Call(0x80000002, ptr(key), 0, 0x102, uintptr(unsafe.Pointer(&h)))
	if r == 2 {
		return nil
	}
	if r != 0 {
		return fmt.Errorf("RegOpenKeyExW: Windows %d", r)
	}
	defer syscall.RegCloseKey(h)
	r, _, _ = advapi.NewProc("RegDeleteValueW").Call(uintptr(h), ptr(wide("OnTechChatTray")))
	if r != 0 && r != 2 {
		return fmt.Errorf("RegDeleteValueW: Windows %d", r)
	}
	return nil
}
