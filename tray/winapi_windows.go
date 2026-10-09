//go:build windows

package tray

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var advapi32 = syscall.NewLazyDLL("advapi32.dll")

func w(s string) *uint16 {
	p, e := syscall.UTF16PtrFromString(s)
	if e != nil {
		panic(e)
	}
	return p
}
func wp(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }
func callError(name string, e error) error {
	var code syscall.Errno
	if errors.As(e, &code) && code != 0 {
		return fmt.Errorf("%s (Windows %d): %w", name, code, e)
	}
	return fmt.Errorf("%s failed", name)
}
func showMessage(hwnd uintptr, text string, flags uintptr) int {
	r, _, _ := user32.NewProc("MessageBoxW").Call(hwnd, wp(w(text)), wp(w("온테크 채팅 서버")), flags)
	return int(r)
}
func shellExecute(hwnd uintptr, verb, target, args string) error {
	p := w(target)
	var a *uint16
	if args != "" {
		a = w(args)
	}
	r, _, e := shell32.NewProc("ShellExecuteW").Call(hwnd, wp(w(verb)), wp(p), wp(a), 0, 1)
	runtime.KeepAlive(p)
	runtime.KeepAlive(a)
	if r <= 32 {
		return fmt.Errorf("실행 요청을 완료하지 못했습니다 (결과 %d): %v", r, e)
	}
	return nil
}
func queryNamedState(name string) serviceState {
	m, _, e := advapi32.NewProc("OpenSCManagerW").Call(0, 0, 1)
	if m == 0 {
		return serviceState{Err: callError("OpenSCManagerW", e).Error()}
	}
	defer advapi32.NewProc("CloseServiceHandle").Call(m)
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return serviceState{Err: "invalid service name"}
	}
	h, _, e := advapi32.NewProc("OpenServiceW").Call(m, wp(n), 4)
	runtime.KeepAlive(n)
	if h == 0 {
		if errors.Is(e, syscall.Errno(1060)) {
			return serviceState{}
		}
		return serviceState{Err: callError("OpenServiceW", e).Error()}
	}
	defer advapi32.NewProc("CloseServiceHandle").Call(h)
	var status struct{ Kind, State, Accept, Exit, Specific, Checkpoint, WaitHint uint32 }
	r, _, e := advapi32.NewProc("QueryServiceStatus").Call(h, uintptr(unsafe.Pointer(&status)))
	if r == 0 {
		return serviceState{Exists: true, Err: callError("QueryServiceStatus", e).Error()}
	}
	return serviceState{Exists: true, State: status.State}
}
func loadIcon(data []byte) (uintptr, error) {
	if len(data) < 22 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return 0, errors.New("invalid tray icon")
	}
	size, off := binary.LittleEndian.Uint32(data[14:18]), binary.LittleEndian.Uint32(data[18:22])
	if size == 0 || uint64(off)+uint64(size) > uint64(len(data)) {
		return 0, errors.New("invalid icon image bounds")
	}
	h, _, e := user32.NewProc("CreateIconFromResourceEx").Call(uintptr(unsafe.Pointer(&data[off])), uintptr(size), 1, 0x30000, 32, 32, 0)
	runtime.KeepAlive(data)
	if h == 0 {
		return 0, callError("CreateIconFromResourceEx", e)
	}
	return h, nil
}
func executablePaths() (exe, program, origin string, err error) {
	exe, err = os.Executable()
	if err != nil {
		return
	}
	program = filepath.Dir(filepath.Dir(exe))
	data, e := os.ReadFile(filepath.Join(filepath.Dir(exe), endpointFilename))
	if e != nil {
		err = fmt.Errorf("트레이 접속 주소 파일을 읽지 못했습니다. 새 설치 파일로 업데이트하세요: %w", e)
		return
	}
	origin, err = decodeEndpoint(data)
	return
}
func copyClipboard(hwnd uintptr, s string) error {
	u, e := syscall.UTF16FromString(s)
	if e != nil {
		return e
	}
	h, _, e := kernel32.NewProc("GlobalAlloc").Call(0x42, uintptr(len(u)*2))
	if h == 0 {
		return callError("GlobalAlloc", e)
	}
	owned := true
	defer func() {
		if owned {
			kernel32.NewProc("GlobalFree").Call(h)
		}
	}()
	p, _, e := kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 {
		return callError("GlobalLock", e)
	}
	kernel32.NewProc("RtlMoveMemory").Call(p, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)*2))
	runtime.KeepAlive(u)
	kernel32.NewProc("GlobalUnlock").Call(h)
	r, _, e := user32.NewProc("OpenClipboard").Call(hwnd)
	if r == 0 {
		return callError("OpenClipboard", e)
	}
	defer user32.NewProc("CloseClipboard").Call()
	r, _, e = user32.NewProc("EmptyClipboard").Call()
	if r == 0 {
		return callError("EmptyClipboard", e)
	}
	r, _, e = user32.NewProc("SetClipboardData").Call(13, h)
	if r == 0 {
		return callError("SetClipboardData", e)
	}
	owned = false
	return nil
}
