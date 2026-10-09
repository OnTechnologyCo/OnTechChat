//go:build windows

package tray

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

//go:embed icons/running.ico
var runningIcon []byte

//go:embed icons/stopped.ico
var stoppedIcon []byte

//go:embed icons/pending.ico
var pendingIcon []byte

const (
	wmTray       = 0x8001
	wmState      = 0x8002
	wmSmoke      = 0x8003
	wmReady      = 0x8004 // Installer queries whether the notification icon was added.
	idOpen       = 101
	idSettings   = 102
	idCopy       = 103
	idStatus     = 104
	idStart      = 105
	idStop       = 106
	idRestart    = 107
	idLogs       = 108
	idExit       = 109
	idFirewall   = 110
	idAIAccount  = 111
	idCodexLogin = 112
)

type point struct{ X, Y int32 }
type windowClassEx struct {
	Size, Style                                               uint32
	Callback                                                  uintptr
	ClassExtra, WindowExtra                                   int32
	Instance, Icon, Cursor, Background, Menu, Name, SmallIcon uintptr
}
type winMessage struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type notifyData struct {
	Size                uint32
	Hwnd                uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	BalloonIcon         uintptr
}
type smokeResults struct{ WindowCreated, IconAdded, MenuBuilt, ExplorerRecovery, CallbackRoutes, IconDeleted bool }
type trayUI struct {
	hwnd, instance, taskbarMessage uintptr
	icons                          map[string]uintptr
	added                          bool
	origin, program                string
	state                          serviceState
	mu                             sync.Mutex
	busy                           bool
	pending                        serviceState
	closed                         bool
	open                           func(string) error
	smoke                          *smokeResults
}

var activeTray *trayUI

func copyWide(dst []uint16, s string) {
	u, e := syscall.UTF16FromString(s)
	if e != nil {
		return
	}
	if len(u) > len(dst) {
		u = u[:len(dst)-1]
	}
	copy(dst, u)
}
func (a *trayUI) notification() notifyData {
	n := notifyData{Hwnd: a.hwnd, ID: 1, Flags: 0x1 | 0x2 | 0x4 | 0x80, Callback: wmTray, Icon: a.icons[a.state.iconKey()]}
	n.Size = uint32(unsafe.Sizeof(n))
	copyWide(n.Tip[:], "OnTech Chat · "+a.state.text()+"\n더블클릭: 채팅 열기")
	return n
}
func (a *trayUI) addIcon() bool {
	n := a.notification()
	r, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return false
	}
	a.added = true
	n.Version = 4
	shell32.NewProc("Shell_NotifyIconW").Call(4, uintptr(unsafe.Pointer(&n)))
	return true
}
func (a *trayUI) updateIcon() {
	if !a.added {
		a.addIcon()
		return
	}
	n := a.notification()
	r, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		a.added = false
	}
}
func (a *trayUI) deleteIcon() bool {
	if !a.added {
		return true
	}
	n := a.notification()
	r, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&n)))
	a.added = false
	return r != 0
}
func (a *trayUI) refresh() {
	a.mu.Lock()
	if a.busy || a.closed {
		a.mu.Unlock()
		return
	}
	a.busy = true
	a.mu.Unlock()
	go func() {
		s := queryNamedState(serviceName)
		a.mu.Lock()
		a.pending = s
		a.busy = false
		if !a.closed {
			user32.NewProc("PostMessageW").Call(a.hwnd, wmState, 0, 0)
		}
		a.mu.Unlock()
	}()
}
func (a *trayUI) makeMenu() (uintptr, error) {
	m, _, e := user32.NewProc("CreatePopupMenu").Call()
	if m == 0 {
		return 0, callError("CreatePopupMenu", e)
	}
	add := func(id uintptr, text string, flags uintptr) error {
		r, _, e := user32.NewProc("AppendMenuW").Call(m, flags, id, wp(w(text)))
		if r == 0 {
			return callError("AppendMenuW", e)
		}
		return nil
	}
	rows := []struct {
		id    uintptr
		text  string
		flags uintptr
	}{
		{0, a.state.text(), 2}, {idOpen, "채팅 화면 열기 (더블클릭)", 0}, {idSettings, "설정 화면 열기", 0}, {idCopy, "접속 주소 복사", 0},
		{0, "", 0x800}, {idStatus, "서버 상태 확인", 0}, {idStart, "서버 시작…", 0}, {idRestart, "서버 재시작…", 0}, {idStop, "서버 중지…", 0}, {idLogs, "서버 로그 보기…", 0}, {idFirewall, "방화벽·포트 설정…", 0}, {idCodexLogin, "서버용 Codex 로그인…", 0}, {idAIAccount, "고급: 서비스 실행 계정…", 0},
		{0, "", 0x800}, {idExit, "트레이 종료 (서버는 계속 실행)", 0},
	}
	for _, r := range rows {
		if a.state.Err == "" && ((r.id == idStart && a.state.State == 4) || ((r.id == idStop || r.id == idRestart) && (!a.state.Exists || a.state.State == 1))) {
			r.flags |= 2
		}
		if e = add(r.id, r.text, r.flags); e != nil {
			user32.NewProc("DestroyMenu").Call(m)
			return 0, e
		}
	}
	user32.NewProc("SetMenuDefaultItem").Call(m, idOpen, 0)
	return m, nil
}
func (a *trayUI) showMenu() {
	m, e := a.makeMenu()
	if e != nil {
		showMessage(a.hwnd, e.Error(), 0x10)
		return
	}
	defer user32.NewProc("DestroyMenu").Call(m)
	var p point
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
	user32.NewProc("SetForegroundWindow").Call(a.hwnd)
	id, _, _ := user32.NewProc("TrackPopupMenu").Call(m, 0x100|0x2, uintptr(p.X), uintptr(p.Y), 0, a.hwnd, 0)
	user32.NewProc("PostMessageW").Call(a.hwnd, 0, 0, 0)
	n := a.notification()
	shell32.NewProc("Shell_NotifyIconW").Call(3, uintptr(unsafe.Pointer(&n)))
	if id != 0 {
		a.command(id)
	}
}
func (a *trayUI) command(id uintptr) {
	var err error
	switch id {
	case idOpen:
		err = a.open(a.origin)
	case idSettings:
		var u string
		u, err = settingsURL(a.origin)
		if err == nil {
			err = a.open(u)
		}
	case idCopy:
		err = copyClipboard(a.hwnd, a.origin)
	case idStatus:
		showMessage(a.hwnd, statusDescription(a.state, a.origin), 0x40)
		a.refresh()
	case idStart, idStop, idRestart, idLogs, idFirewall, idAIAccount, idCodexLogin:
		arg := map[uintptr]string{idStart: "--start", idStop: "--stop", idRestart: "--restart", idLogs: "--logs", idFirewall: "--firewall", idAIAccount: "--ai-account", idCodexLogin: "--codex-login"}[id]
		if id == idStop || id == idRestart {
			verb := "중지"
			if id == idRestart {
				verb = "재시작"
			}
			if showMessage(a.hwnd, "서버를 "+verb+"할까요?\n연결된 모든 사용자의 채팅·메일 수집 연결이 잠시 중단됩니다.", 0x124) != 6 {
				return
			}
		}
		// 서버 제어는 서명되지 않은 명령 문자열이나 웹 입력을 받지 않으며, 고정된 관리 도구에만 UAC를 요청합니다.
		exe := filepath.Join(a.program, "bin", "OnTechChat.Server.exe")
		if _, e := os.Stat(exe); e != nil {
			err = errors.New("설치된 관리 도구를 찾지 못했습니다. 설치 파일로 복구하세요.")
		} else {
			err = shellExecute(a.hwnd, "runas", exe, arg)
		}
	case idExit:
		user32.NewProc("DestroyWindow").Call(a.hwnd)
	}
	if err != nil {
		showMessage(a.hwnd, err.Error(), 0x10)
	}
}
func windowProc(hwnd uintptr, msg uint32, wpv, lp uintptr) uintptr {
	a := activeTray
	if a != nil && hwnd == a.hwnd {
		if uintptr(msg) == a.taskbarMessage {
			a.added = false
			a.addIcon()
			return 0
		}
		switch msg {
		case wmReady:
			if a.added {
				return 1
			}
			return 0
		case wmTray:
			switch uint32(lp) & 0xffff {
			case 0x203, 0x401:
				a.command(idOpen)
			case 0x7b:
				a.showMenu()
			}
			return 0
		case wmState:
			a.mu.Lock()
			a.state = a.pending
			a.mu.Unlock()
			a.updateIcon()
			return 0
		case 0x113:
			if wpv == 99 {
				user32.NewProc("DestroyWindow").Call(hwnd)
			} else {
				a.updateIcon()
				a.refresh()
			}
			return 0
		case 0x10:
			user32.NewProc("DestroyWindow").Call(hwnd)
			return 0
		case 0x11:
			return 1
		case 0x16:
			if wpv != 0 {
				user32.NewProc("DestroyWindow").Call(hwnd)
			}
			return 0
		case 0x2:
			a.mu.Lock()
			a.closed = true
			a.mu.Unlock()
			user32.NewProc("KillTimer").Call(hwnd, 1)
			user32.NewProc("KillTimer").Call(hwnd, 99)
			ok := a.deleteIcon()
			if a.smoke != nil {
				a.smoke.IconDeleted = ok
			}
			user32.NewProc("PostQuitMessage").Call(0)
			return 0
		}
	}
	r, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wpv, lp)
	return r
}
func runTray(origin, program string, smoke *smokeResults) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a := &trayUI{origin: origin, program: program, icons: map[string]uintptr{}, state: serviceState{Err: "checking"}, smoke: smoke}
	activeTray = a
	defer func() { activeTray = nil }()
	for key, data := range map[string][]byte{"running": runningIcon, "stopped": stoppedIcon, "pending": pendingIcon} {
		h, e := loadIcon(data)
		if e != nil {
			return e
		}
		a.icons[key] = h
		defer user32.NewProc("DestroyIcon").Call(h)
	}
	instance, _, e := kernel32.NewProc("GetModuleHandleW").Call(0)
	if instance == 0 {
		return callError("GetModuleHandleW", e)
	}
	a.instance = instance
	taskbar, _, e := user32.NewProc("RegisterWindowMessageW").Call(wp(w("TaskbarCreated")))
	if taskbar == 0 {
		return callError("RegisterWindowMessageW", e)
	}
	a.taskbarMessage = taskbar
	class := w(windowClass)
	wc := windowClassEx{Callback: syscall.NewCallback(windowProc), Instance: instance, Name: wp(class), Icon: a.icons["running"]}
	wc.Size = uint32(unsafe.Sizeof(wc))
	atom, _, e := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return callError("RegisterClassExW", e)
	}
	defer func() { user32.NewProc("UnregisterClassW").Call(wp(class), instance); runtime.KeepAlive(class) }()
	hwnd, _, e := user32.NewProc("CreateWindowExW").Call(0, wp(class), wp(w("온테크 채팅 트레이")), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	runtime.KeepAlive(class)
	if hwnd == 0 {
		return callError("CreateWindowExW", e)
	}
	a.hwnd = hwnd
	defer func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()
		a.deleteIcon()
		user32.NewProc("DestroyWindow").Call(hwnd)
	}()
	a.open = func(s string) error { return shellExecute(hwnd, "open", s, "") }
	a.addIcon()
	a.refresh()
	timer, _, e := user32.NewProc("SetTimer").Call(hwnd, 1, 5000, 0)
	if timer == 0 {
		return callError("SetTimer", e)
	}
	if smoke != nil {
		smoke.WindowCreated = true
		smoke.IconAdded = a.added
		menu, e := a.makeMenu()
		if e != nil {
			return e
		}
		count, _, _ := user32.NewProc("GetMenuItemCount").Call(menu)
		smoke.MenuBuilt = count == 15
		user32.NewProc("DestroyMenu").Call(menu)
		a.deleteIcon()
		windowProc(hwnd, uint32(taskbar), 0, 0)
		smoke.ExplorerRecovery = a.added
		var opened []string
		a.open = func(s string) error { opened = append(opened, s); return nil }
		windowProc(hwnd, wmTray, 0, 0x203)
		a.command(idSettings)
		smoke.CallbackRoutes = len(opened) == 2 && opened[0] == origin && opened[1] == origin+"?settings=1"
		t, _, e := user32.NewProc("SetTimer").Call(hwnd, 99, 400, 0)
		if t == 0 {
			return callError("SetTimer(test)", e)
		}
	}
	for {
		var m winMessage
		r, _, e := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return callError("GetMessageW", e)
		}
		if r == 0 {
			break
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}
func Main() {
	startup := flag.Bool("startup", false, "start a tray icon after Windows login")
	quit := flag.Bool("quit", false, "close this session's tray only")
	open := flag.Bool("open", false, "open chat in the current user's browser")
	flag.Parse()
	_ = startup
	existing, _, _ := user32.NewProc("FindWindowW").Call(wp(w(windowClass)), 0)
	if *quit {
		if existing != 0 {
			user32.NewProc("PostMessageW").Call(existing, 0x10, 0, 0)
		}
		return
	}
	if existing != 0 {
		if !*startup || *open {
			user32.NewProc("PostMessageW").Call(existing, wmTray, 0, 0x203)
		}
		return
	}
	h, _, e := kernel32.NewProc("CreateMutexW").Call(0, 0, wp(w(mutexName)))
	if h == 0 {
		if !*startup {
			showMessage(0, callError("CreateMutexW", e).Error(), 0x10)
		}
		return
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	if errors.Is(e, syscall.ERROR_ALREADY_EXISTS) {
		return
	}
	_, program, origin, e := executablePaths()
	if e != nil {
		showMessage(0, e.Error(), 0x10)
		return
	}
	if *open {
		if e = shellExecute(0, "open", origin, ""); e != nil {
			showMessage(0, e.Error(), 0x10)
		}
	}
	if e = runTray(origin, program, nil); e != nil {
		showMessage(0, fmt.Sprintf("트레이를 시작하지 못했습니다.\n%s\n\n서버 서비스는 종료하지 않았습니다.", e), 0x10)
	}
}
