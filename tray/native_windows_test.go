//go:build windows

package tray

import (
	"os"
	"testing"
	"unsafe"
)

func TestWindowsStructSizes(t *testing.T) {
	if unsafe.Sizeof(notifyData{}) != 976 || unsafe.Sizeof(windowClassEx{}) != 80 || unsafe.Sizeof(winMessage{}) != 48 {
		t.Fatalf("layout mismatch: notify=%d class=%d msg=%d", unsafe.Sizeof(notifyData{}), unsafe.Sizeof(windowClassEx{}), unsafe.Sizeof(winMessage{}))
	}
}
func TestServiceReadOnly(t *testing.T) {
	s := queryNamedState("EventLog")
	if !s.Exists || s.Err != "" {
		t.Fatalf("%+v", s)
	}
	s = queryNamedState("OnTechTray_MissingReadOnly_38172")
	if s.Exists || s.Err != "" {
		t.Fatalf("%+v", s)
	}
}
func TestNativeTraySmoke(t *testing.T) {
	if os.Getenv("ONTECH_TRAY_UI_TEST") != "1" {
		t.Skip("set ONTECH_TRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	var r smokeResults
	if e := runTray("http://127.0.0.1:8787/", t.TempDir(), &r); e != nil {
		t.Fatal(e)
	}
	if !r.WindowCreated || !r.IconAdded || !r.MenuBuilt || !r.ExplorerRecovery || !r.CallbackRoutes || !r.IconDeleted {
		t.Fatalf("%+v", r)
	}
	t.Logf("%+v (no browser, clipboard, service control, startup registration, or installation)", r)
}

func TestTrayReadyMeansIconRegistered(t *testing.T) {
	previous := activeTray
	defer func() { activeTray = previous }()
	activeTray = &trayUI{hwnd: 1}
	if windowProc(1, wmReady, 0, 0) != 0 {
		t.Fatal("window without icon reported ready")
	}
	activeTray.added = true
	if windowProc(1, wmReady, 0, 0) != 1 {
		t.Fatal("registered icon not reported ready")
	}
}
