//go:build windows

package server

import (
	"errors"
	"syscall"
	"unsafe"
)

func replaceConfigFile(from, to string) error {
	a, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	r, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if r == 0 {
		return e
	}
	return nil
}
func rejectConfigReparse(path string) error {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	attrs, e := syscall.GetFileAttributes(p)
	if e != nil {
		return e
	}
	if attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("configuration path must not contain a reparse point")
	}
	return nil
}
