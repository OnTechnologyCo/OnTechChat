//go:build windows

package server

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

func sharedCodexRoots() (string, string) {
	folder := func(id uintptr) string {
		var b [260]uint16
		r, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("SHGetFolderPathW").Call(0, id, 0, 0, uintptr(unsafe.Pointer(&b[0])))
		if int32(r) < 0 {
			return ""
		}
		return syscall.UTF16ToString(b[:])
	}
	pf, pd := folder(0x26), folder(0x23)
	if pf == "" || pd == "" {
		return "", ""
	}
	return filepath.Join(pf, "OnTechChatServer"), filepath.Join(pd, "OnTechChatServer")
}
func sharedCodexExecutable() string {
	program, data := sharedCodexRoots()
	if program == "" || data == "" {
		return ""
	}
	exe, _ := SharedCodexLocations(program, data)
	return exe
}
func sharedCodexHome(executable string) string {
	program, data := sharedCodexRoots()
	return sharedCodexHomeFor(executable, program, data)
}
