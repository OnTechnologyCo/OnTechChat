//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var advapi = syscall.NewLazyDLL("advapi32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var shell = syscall.NewLazyDLL("shell32.dll")

func wide(s string) *uint16 {
	p, e := syscall.UTF16PtrFromString(s)
	if e != nil {
		panic(e)
	}
	return p
}
func ptr(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }
func message(text string, flags uintptr) int {
	r, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, ptr(wide(text)), ptr(wide("온테크 채팅 서버 설치 / "+installerVersion)), flags)
	return int(r)
}
func folder(id uintptr) (string, error) {
	var b [260]uint16
	r, _, _ := shell.NewProc("SHGetFolderPathW").Call(0, id, 0, 0, uintptr(unsafe.Pointer(&b[0])))
	if int32(r) < 0 {
		return "", fmt.Errorf("Windows folder lookup: %x", r)
	}
	return syscall.UTF16ToString(b[:]), nil
}
func systemExe(name string) string {
	var b [260]uint16
	r, _, _ := kernel.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&b[0])), 260)
	if r == 0 {
		panic("GetSystemDirectoryW failed")
	}
	return filepath.Join(syscall.UTF16ToString(b[:]), name)
}
func isAdmin() bool {
	sid, e := syscall.StringToSid("S-1-5-32-544")
	if e != nil {
		return false
	}
	var yes uint32
	r, _, _ := advapi.NewProc("CheckTokenMembership").Call(0, uintptr(unsafe.Pointer(sid)), uintptr(unsafe.Pointer(&yes)))
	return r != 0 && yes != 0
}
func elevateSelf(args []string) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}
	r, _, e := shell.NewProc("ShellExecuteW").Call(0, ptr(wide("runas")), ptr(wide(exe)), ptr(wide(strings.Join(quoted, " "))), 0, 1)
	if r <= 32 {
		return fmt.Errorf("administrator approval was cancelled or unavailable (%d): %w", r, e)
	}
	return nil
}
func command(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("%s failed: %w\n%s", filepath.Base(exe), e, strings.TrimSpace(string(b)))
	}
	return nil
}
func serviceCommand(args ...string) error { return command(systemExe("sc.exe"), args...) }

type svcStatus struct{ Kind, State, Accept, Exit, Specific, Checkpoint, WaitHint uint32 }
type svcConfig struct {
	Kind, Start, Error uint32
	Binary             *uint16
	Group              *uint16
	Tag                uint32
	Dependencies       *uint16
	Account            *uint16
	Display            *uint16
}

func apiError(operation string, err error) error {
	var code syscall.Errno
	if errors.As(err, &code) {
		return fmt.Errorf("%s (Windows error %d): %w", operation, uint32(code), err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
func queryService() (exists bool, state uint32, binary string, e error) {
	m, _, e := advapi.NewProc("OpenSCManagerW").Call(0, 0, 1)
	if m == 0 {
		return false, 0, "", apiError("OpenSCManagerW", e)
	}
	defer advapi.NewProc("CloseServiceHandle").Call(m)
	name := wide(serviceName)
	h, _, e := advapi.NewProc("OpenServiceW").Call(m, ptr(name), 5)
	runtime.KeepAlive(name)
	if h == 0 {
		if errors.Is(e, syscall.Errno(1060)) {
			return false, 0, "", nil
		}
		return false, 0, "", apiError("OpenServiceW", e)
	}
	defer advapi.NewProc("CloseServiceHandle").Call(h)
	var status svcStatus
	r, _, e := advapi.NewProc("QueryServiceStatus").Call(h, uintptr(unsafe.Pointer(&status)))
	if r == 0 {
		return true, 0, "", apiError("QueryServiceStatus", e)
	}
	query := advapi.NewProc("QueryServiceConfigW")
	buf, e := readServiceConfigBuffer(func(b []byte) (uint32, error) {
		var needed uint32
		var result uintptr
		var err error
		if len(b) == 0 {
			result, _, err = query.Call(h, 0, 0, uintptr(unsafe.Pointer(&needed)))
		} else {
			result, _, err = query.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&needed)))
		}
		runtime.KeepAlive(b)
		if result != 0 {
			return needed, nil
		}
		if errors.Is(err, syscall.ERROR_INSUFFICIENT_BUFFER) {
			return needed, errServiceBufferTooSmall
		}
		return needed, apiError("QueryServiceConfigW", err)
	})
	if e != nil {
		return true, status.State, "", e
	}
	if len(buf) < int(unsafe.Sizeof(svcConfig{})) {
		return true, status.State, "", errors.New("truncated service configuration")
	}
	cfg := (*svcConfig)(unsafe.Pointer(&buf[0]))
	if cfg.Binary == nil {
		return true, status.State, "", errors.New("service executable path is missing")
	}
	base, address := uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(cfg.Binary))
	if address < base || address-base >= uintptr(len(buf)) {
		return true, status.State, "", errors.New("service executable pointer is outside returned buffer")
	}
	binary, e = decodeServiceUTF16(buf, int(address-base))
	runtime.KeepAlive(buf)
	return true, status.State, binary, e
}
func waitState(target uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		exists, s, _, e := queryService()
		if e != nil {
			if target == 0 && errors.Is(e, syscall.Errno(1072)) {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return e
		}
		if !exists && target == 0 {
			return nil
		}
		if exists && s == target {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach state %d before timeout", target)
}
func stopService() error {
	exists, s, _, e := queryService()
	if e != nil || !exists || s == 1 {
		return e
	}
	if s == 2 {
		if e = waitState(4, 30*time.Second); e != nil {
			return e
		}
	}
	if s != 3 {
		if e = serviceCommand("stop", serviceName); e != nil {
			return e
		}
	}
	return waitState(1, 40*time.Second)
}
func startService() error {
	exists, s, _, e := queryService()
	if e != nil {
		return e
	}
	if !exists {
		return errors.New("service is not installed")
	}
	if s == 4 {
		return nil
	}
	if s == 3 {
		if e = waitState(1, 40*time.Second); e != nil {
			return e
		}
	}
	if s != 2 {
		if e = serviceCommand("start", serviceName); e != nil {
			return e
		}
	}
	return waitState(4, 35*time.Second)
}
func denyReparse(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		a, e := syscall.GetFileAttributes(wide(p))
		if e == nil && a&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("installation paths must not contain junctions or symbolic links")
		}
		if e != nil && !errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) && !errors.Is(e, syscall.ERROR_PATH_NOT_FOUND) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func protectData(path string, serviceSID string, writable bool) error {
	sddl := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	if serviceSID != "" {
		rights := "0x1200a9"
		if writable {
			rights = "0x1301bf"
		}
		sddl += "(A;OICI;" + rights + ";;;" + serviceSID + ")"
	}
	return applyPathDACL(path, sddl)
}
func applyPathDACL(path, sddl string) error {
	var sd uintptr
	r, _, e := advapi.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW").Call(ptr(wide(sddl)), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		return e
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	r, _, e = advapi.NewProc("SetFileSecurityW").Call(ptr(wide(path)), 0x80000004, sd)
	if r == 0 {
		return e
	}
	return nil
}
func setupMutex() (func(), error) {
	h, _, e := kernel.NewProc("CreateMutexW").Call(0, 1, ptr(wide("Global\\OnTechChatServer.Setup")))
	if h == 0 {
		return nil, e
	}
	if errors.Is(e, syscall.ERROR_ALREADY_EXISTS) {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, errors.New("another installer or maintenance operation is already running")
	}
	return func() { kernel.NewProc("ReleaseMutex").Call(h); syscall.CloseHandle(syscall.Handle(h)) }, nil
}

func shellOpenURL(target string) error {
	r, _, e := shell.NewProc("ShellExecuteW").Call(0, ptr(wide("open")), ptr(wide(target)), 0, 0, 1)
	if r <= 32 {
		return fmt.Errorf("open settings browser failed (%d): %w", r, e)
	}
	return nil
}
