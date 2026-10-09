//go:build windows

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

const windowsServiceName = "OnTechChatServer"

var windowsServiceMode bool

type serviceStatus struct{ Kind, State, Accept, Exit, Specific, Checkpoint, WaitHint uint32 }
type serviceTableEntry struct {
	Name *uint16
	Proc uintptr
}

var serviceAPI = syscall.NewLazyDLL("advapi32.dll")
var setServiceStatus = serviceAPI.NewProc("SetServiceStatus")

// 종료 요청은 context 취소로 연결합니다. 강제 프로세스 종료가 기본 동작이 아닙니다.
func runWindowsService(c Config) error {
	windowsServiceMode = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var handle uintptr
	var state serviceStatus
	var finalErr error
	var logFile *os.File
	report := func(next uint32, code uint32) {
		mu.Lock()
		defer mu.Unlock()
		if state.State == 1 || (state.State == 3 && next == 4) {
			return
		}
		state = serviceStatus{Kind: 0x10, State: next, Exit: code}
		if next == 4 {
			state.Accept = 1 | 4
		}
		if next == 2 || next == 3 {
			state.Checkpoint = 1
			state.WaitHint = 30000
		}
		if code == 1066 {
			state.Specific = 1
		}
		setServiceStatus.Call(handle, uintptr(unsafe.Pointer(&state)))
	}
	control := syscall.NewCallback(func(ctrl uint32, eventType uint32, data uintptr, user uintptr) uintptr {
		switch ctrl {
		case 1, 5:
			report(3, 0)
			cancel()
		case 4:
			mu.Lock()
			if handle != 0 && state.State != 1 {
				setServiceStatus.Call(handle, uintptr(unsafe.Pointer(&state)))
			}
			mu.Unlock()
		}
		return 0
	})
	name, e := syscall.UTF16PtrFromString(windowsServiceName)
	if e != nil {
		return e
	}
	callback := syscall.NewCallback(func(argc uint32, argv uintptr) uintptr {
		h, _, err := serviceAPI.NewProc("RegisterServiceCtrlHandlerExW").Call(uintptr(unsafe.Pointer(name)), control, 0)
		if h == 0 {
			finalErr = fmt.Errorf("register service: %w", err)
			return 0
		}
		handle = h
		report(2, 0)
		// 관리자가 설치 때 서비스 전용 SID에 접근 권한을 부여한 데이터 폴더입니다.
		logPath := filepath.Join(c.DataDir, "service.log")
		if stat, err := os.Stat(logPath); err == nil && stat.Size() > 5<<20 {
			_ = os.Remove(logPath + ".2")
			_ = os.Rename(logPath+".1", logPath+".2")
			_ = os.Rename(logPath, logPath+".1")
		}
		logFile, e = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			finalErr = e
			report(1, 1066)
			return 0
		}
		log.SetOutput(&boundedServiceLog{path: logPath, f: logFile})
		log.Printf("Windows service starting: %s", version)
		finalErr = serve(ctx, c, func() {
			if ctx.Err() == nil {
				report(4, 0)
			}
		})
		code := uint32(0)
		if finalErr != nil && !errors.Is(finalErr, context.Canceled) {
			code = 1066
			log.Printf("Service stopped with error: %v", finalErr)
		} else {
			log.Print("Service stopped normally")
		}
		// SetServiceStatus(STOPPED) 이후에는 SCM이 프로세스를 종료할 수 있습니다.
		if closer, ok := log.Writer().(*boundedServiceLog); ok {
			_ = closer.Close()
		}
		report(1, code)
		return 0
	})
	table := []serviceTableEntry{{Name: name, Proc: callback}, {}}
	r, _, err := serviceAPI.NewProc("StartServiceCtrlDispatcherW").Call(uintptr(unsafe.Pointer(&table[0])))
	if r == 0 {
		return fmt.Errorf("service dispatcher (start via Windows Services, not double-click): %w", err)
	}
	return finalErr
}

// 파일 로그가 무제한으로 커지지 않도록 5 MiB x 3 파일로 제한합니다.
type boundedServiceLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func (w *boundedServiceLog) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if stat, e := w.f.Stat(); e == nil && stat.Size()+int64(len(b)) > 5<<20 {
		_ = w.f.Close()
		_ = os.Remove(w.path + ".2")
		_ = os.Rename(w.path+".1", w.path+".2")
		_ = os.Rename(w.path, w.path+".1")
		f, e := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			w.f = nil
			return 0, e
		}
		w.f = f
	}
	return w.f.Write(b)
}
func (w *boundedServiceLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	e := w.f.Close()
	w.f = nil
	return e
}
