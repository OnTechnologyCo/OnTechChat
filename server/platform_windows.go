//go:build windows

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// 새 데이터 폴더만 현재 사용자, SYSTEM, 로컬 관리자에게 허용합니다.
func secureDirectory(path string) error {
	// 서비스는 관리자 설치기가 부여한 전용 SID ACL을 변경하지 않습니다.
	// LocalService에는 WRITE_DAC를 부여하지 않으며 다른 서비스에 접근권한을 넓히지 않습니다.
	if windowsServiceMode {
		p, e := syscall.UTF16PtrFromString(path)
		if e != nil {
			return e
		}
		attrs, e := syscall.GetFileAttributes(p)
		if e != nil {
			return e
		}
		if attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("service data directory must not be a junction or symbolic link")
		}
		return nil
	}

	handle, e := syscall.GetCurrentProcess()
	if e != nil {
		return e
	}
	var token syscall.Token
	if e := syscall.OpenProcessToken(handle, syscall.TOKEN_QUERY, &token); e != nil {
		return e
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		return e
	}
	sid, e := user.User.Sid.String()
	if e != nil {
		return e
	}
	sddl, e := syscall.UTF16PtrFromString("D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if e != nil {
		return e
	}
	adv := syscall.NewLazyDLL("advapi32.dll")
	convert := adv.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	set := adv.NewProc("SetFileSecurityW")
	var sd uintptr
	r, _, e := convert.Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		return fmt.Errorf("security descriptor: %w", e)
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	r, _, e = set.Call(uintptr(unsafe.Pointer(p)), uintptr(0x80000004), sd)
	if r == 0 {
		return fmt.Errorf("directory ACL: %w", e)
	}
	return nil
}
func lockDirectory(dir string) (*os.File, error) {
	p, e := syscall.UTF16PtrFromString(filepath.Join(dir, "server.lock"))
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), "server.lock"), nil
}
func unlockDirectory(f *os.File) error { return f.Close() }
func syncDirectory(string)             {}
