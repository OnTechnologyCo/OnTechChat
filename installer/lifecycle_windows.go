//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Match full executable paths; never terminate by image name alone.
func ownedRuntimePath(p paths, image string) bool {
	for _, candidate := range []string{p.exe, filepath.Join(p.program, "bin", "OnTechChat.Tray.exe"), filepath.Join(p.program, "Maintenance.exe")} {
		if strings.EqualFold(filepath.Clean(image), filepath.Clean(candidate)) {
			return true
		}
	}
	return false
}
func processImage(h syscall.Handle) (string, error) {
	b := make([]uint16, 32768)
	n := uint32(len(b))
	ok, _, e := kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(h), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
	if ok == 0 {
		return "", apiError("QueryFullProcessImageNameW", e)
	}
	return syscall.UTF16ToString(b[:n]), nil
}
func stopOwnedProcesses(p paths) error {
	snapshot, e := syscall.CreateToolhelp32Snapshot(2, 0)
	if e != nil {
		return e
	}
	defer syscall.CloseHandle(snapshot)
	entry := syscall.ProcessEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	var failures []error
	for e = syscall.Process32First(snapshot, &entry); e == nil; e = syscall.Process32Next(snapshot, &entry) {
		if int(entry.ProcessID) == os.Getpid() {
			continue
		}
		name := syscall.UTF16ToString(entry.ExeFile[:])
		if !strings.EqualFold(name, filepath.Base(p.exe)) && !strings.EqualFold(name, "OnTechChat.Tray.exe") && !strings.EqualFold(name, "Maintenance.exe") {
			continue
		}
		h, err := syscall.OpenProcess(0x101001, false, entry.ProcessID)
		if errors.Is(err, syscall.Errno(87)) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("프로세스 %d 확인: %w", entry.ProcessID, err))
			continue
		}
		image, err := processImage(h)
		if err == nil && ownedRuntimePath(p, image) {
			user := syscall.NewLazyDLL("user32.dll")
			callback := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
				var pid uint32
				user.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
				if pid == entry.ProcessID {
					user.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
				}
				return 1
			})
			user.NewProc("EnumWindows").Call(callback, 0)
			result, _, _ := kernel.NewProc("WaitForSingleObject").Call(uintptr(h), 1500)
			if result != 0 {
				if err = syscall.TerminateProcess(h, 0); err == nil {
					result, _, err = kernel.NewProc("WaitForSingleObject").Call(uintptr(h), 10000)
					if result == 0 {
						err = nil
					} else {
						err = fmt.Errorf("종료 대기 실패 %s: %d %v", image, result, err)
					}
				}
			}
		}
		syscall.CloseHandle(h)
		if err != nil {
			failures = append(failures, err)
		}
	}
	if !errors.Is(e, syscall.ERROR_NO_MORE_FILES) {
		failures = append(failures, e)
	}
	return errors.Join(failures...)
}

// Disable restarts before stopping, including service failure recovery.
func stopInstallationRuntime(p paths) error {
	if e := ownedService(p); e != nil {
		return e
	}
	exists, _, _, e := queryService()
	if e != nil {
		return e
	}
	if exists {
		if e = serviceCommand("config", serviceName, "start=", "disabled"); e != nil {
			return e
		}
		_ = stopService() // An unresponsive owned process is terminated below.
	}
	if e = stopOwnedProcesses(p); e != nil {
		return e
	}
	if exists {
		return waitState(1, 10*time.Second)
	}
	return nil
}
func removeTree(path string) (err error) {
	// Preserve the ownership marker if a locked child prevents complete removal.
	marker := filepath.Join(path, "install-record.json")
	record, recordErr := os.ReadFile(marker)
	defer func() {
		if err != nil && recordErr == nil {
			if _, e := os.Stat(path); e == nil {
				err = errors.Join(err, os.WriteFile(marker, record, 0600))
			}
		}
	}()
	if e := denyReparse(path); e != nil {
		return e
	}
	var e error
	for i := 0; i < 20; i++ {
		e = os.RemoveAll(path)
		if e == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("남은 폴더 %s: %w", path, e)
}

// The staged tree and previous tree stay on the same volume.
func replaceProgram(stage, destination string) (backup string, err error) {
	if _, e := os.Stat(destination); e == nil {
		backup, err = os.MkdirTemp(filepath.Dir(destination), "OnTechChat-rollback-")
		if err != nil {
			return "", err
		}
		if err = os.Remove(backup); err != nil {
			return "", err
		}
		if err = os.Rename(destination, backup); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if err = os.Rename(stage, destination); err != nil {
		if backup != "" {
			if restore := os.Rename(backup, destination); restore != nil {
				return backup, errors.Join(err, fmt.Errorf("복원 실패; 이전 파일 %s: %w", backup, restore))
			}
		}
		return "", err
	}
	return backup, nil
}
