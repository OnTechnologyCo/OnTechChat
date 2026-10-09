//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	serverapp "ontechdev.com/ontechchat/server"
	trayapp "ontechdev.com/ontechchat/tray"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const uninstallKey = `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\OnTechChatServer`

type paths struct{ program, data, menu, exe, config string }

func installPaths() (paths, error) {
	pf, e := folder(0x26)
	if e != nil {
		return paths{}, e
	}
	pd, e := folder(0x23)
	if e != nil {
		return paths{}, e
	}
	sm, e := folder(0x17)
	if e != nil {
		return paths{}, e
	}
	p := paths{program: filepath.Join(pf, "OnTechChatServer"), data: filepath.Join(pd, "OnTechChatServer"), menu: filepath.Join(sm, "OnTech Chat Server")}
	p.exe = filepath.Join(p.program, "bin", "OnTechChat.Server.exe")
	p.config = filepath.Join(p.data, "config.json")
	return p, nil
}
func copyFile(src, dst string) error {
	r, e := os.Open(src)
	if e != nil {
		return e
	}
	defer r.Close()
	w, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if e != nil {
		return e
	}
	_, e = io.Copy(w, r)
	ce := w.Close()
	if e != nil {
		return e
	}
	return ce
}
func writeRecord(path string, p paths) error {
	b, _ := json.MarshalIndent(installRecord{ID: installID, Version: installerVersion, ProgramDir: p.program, DataDir: p.data, PayloadSHA256: payloadDigest()}, "", "  ")
	return os.WriteFile(path, b, 0600)
}
func ownedDirectory(path string) error {
	entries, e := os.ReadDir(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if len(entries) == 0 {
		return nil
	}
	b, e := os.ReadFile(filepath.Join(path, "install-record.json"))
	if e != nil {
		return fmt.Errorf("refusing to overwrite unrecognized directory: %s", path)
	}
	var r installRecord
	if json.Unmarshal(b, &r) != nil || r.ID != installID {
		return errors.New("installation ownership marker mismatch")
	}
	return nil
}
func ownedService(p paths) error {
	exists, _, binary, e := queryService()
	if e != nil {
		return e
	}
	if !exists {
		return nil
	}
	wanted := syscall.EscapeArg(p.exe) + " --config " + syscall.EscapeArg(p.config) + " --service"
	if !strings.EqualFold(strings.TrimSpace(binary), wanted) {
		return errors.New("existing OnTechChatServer service has a different executable; it was not changed")
	}
	return nil
}
func registryValue(name, typ, value string) error {
	return command(systemExe("reg.exe"), "add", uninstallKey, "/v", name, "/t", typ, "/d", value, "/f", "/reg:64")
}
func install(p paths) (err error) {
	stageName := "설치 사전 확인"
	defer func() {
		if err != nil {
			err = fmt.Errorf("설치 단계: %s\n%w", stageName, err)
		}
	}()
	if _, err = inspectPayload(); err != nil {
		return err
	}
	for _, dir := range []string{p.program, p.data, p.menu} {
		if err = denyReparse(dir); err != nil {
			return err
		}
	}
	for _, dir := range []string{p.program, p.data} {
		if err = ownedDirectory(dir); err != nil {
			return err
		}
	}
	if err = ownedService(p); err != nil {
		return err
	}
	current, _ := os.Executable()
	if strings.HasPrefix(strings.ToLower(current), strings.ToLower(p.program)+`\`) {
		return errors.New("run the downloaded installer from outside the installed program folder")
	}
	stage, err := os.MkdirTemp(filepath.Dir(p.program), "OnTechChat-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = extractPayload(stage); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(stage, "bin"), 0755); err != nil {
		return err
	}
	if err = copyFile(current, filepath.Join(stage, "bin", "OnTechChat.Server.exe")); err != nil {
		return err
	}
	stageName = "공용 Codex CLI 준비"
	if _, err = stageSharedCodex(stage, p); err != nil {
		return err
	}
	existed, _, _, err := queryService()
	if err != nil {
		return err
	}
	oldConfig, configReadErr := os.ReadFile(p.config)
	if configReadErr != nil && !os.IsNotExist(configReadErr) {
		return configReadErr
	}
	stageName = "기존 서버 및 모든 세션의 트레이 종료"
	recoveryReady := true
	defer func() {
		if err != nil && existed && recoveryReady {
			if sid, _, _, e := syscall.LookupSID("", `NT SERVICE\`+serviceName); e == nil {
				if sidText, e := sid.String(); e == nil {
					err = errors.Join(err, protectData(p.data, sidText, true), protectData(p.config, sidText, true))
				}
			}
			recovery := serviceCommand("config", serviceName, "start=", "delayed-auto")
			recovery = errors.Join(recovery, startService())
			if recovery != nil {
				err = errors.Join(err, fmt.Errorf("기존 서버 재시작: %w", recovery))
			}
			_ = launchTrayAsDesktopUser(p)
		}
	}()
	if err = stopInstallationRuntime(p); err != nil {
		return err
	}
	stageName = "프로그램 파일 교체"
	if err = writeRecord(filepath.Join(stage, "install-record.json"), p); err != nil {
		return err
	}
	backup, err := replaceProgram(stage, p.program)
	if err != nil {
		recoveryReady = backup == ""
		return err
	}
	recoveryReady = false
	defer func() {
		if err == nil {
			if backup != "" {
				if e := removeTree(backup); e != nil {
					message("업데이트는 완료했지만 이전 파일 정리가 필요합니다.\n"+e.Error(), 0x30)
				}
			}
			return
		}
		// Restore the previous executable and configuration before restarting it.
		if backup != "" {
			if e := stopInstallationRuntime(p); e != nil {
				err = errors.Join(err, e)
				return
			}
			if e := removeTree(p.program); e != nil {
				err = errors.Join(err, e)
				return
			}
			if e := os.Rename(backup, p.program); e != nil {
				err = errors.Join(err, fmt.Errorf("이전 파일 복원 실패 %s: %w", backup, e))
				return
			}
			recoveryReady = true
			if configReadErr == nil {
				if e := os.WriteFile(p.config, oldConfig, 0600); e != nil {
					err = errors.Join(err, e)
				}
			}
		}
	}()
	stageName = "설정 및 서비스 권한 준비"
	if err = os.MkdirAll(p.data, 0700); err != nil {
		return err
	}
	if err = protectData(p.data, "", false); err != nil {
		return err
	}
	if err = writeRecord(filepath.Join(p.data, "install-record.json"), p); err != nil {
		return err
	}
	_, configErr := os.Stat(p.config)
	if os.IsNotExist(configErr) {
		if entries, e := os.ReadDir(filepath.Join(p.data, "private")); e == nil && len(entries) > 0 {
			return errors.New("existing private data without config: restore config from backup; initialization refused")
		}
		if err = command(p.exe, "--config", p.config, "--init"); err != nil {
			return err
		}
	} else if configErr != nil {
		return configErr
	}
	if err = command(p.exe, "--config", p.config, "--migrate-config"); err != nil {
		return err
	}
	if err = command(p.exe, "--config", p.config, "--check-config"); err != nil {
		return err
	}
	// 설치형은 data_dir=private 경로를 고정해 다른 폴더의 권한을 변경하지 않습니다.
	b, err := os.ReadFile(p.config)
	if err != nil {
		return err
	}
	var cfg struct {
		DataDir string `json:"data_dir"`
		Listen  string `json:"listen"`
		TLSCert string `json:"tls_cert"`
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.DataDir != "private" && !strings.EqualFold(filepath.Clean(cfg.DataDir), filepath.Join(p.data, "private")) {
		return errors.New("installed edition requires data_dir=private; no other directory permissions were changed")
	}
	stageName = "Windows 서비스 등록"
	binary := syscall.EscapeArg(p.exe) + " --config " + syscall.EscapeArg(p.config) + " --service"
	args := serviceRegistrationArgs(existed, binary)
	if err = serviceCommand(args...); err != nil {
		return err
	}
	// 설치 실패 시 새로 등록한 서비스만 제거하고 사용자 데이터는 보존합니다.
	defer func() {
		if err != nil && !existed {
			_ = stopService()
			_ = serviceCommand("delete", serviceName)
		}
	}()
	if err = serviceCommand("sidtype", serviceName, "unrestricted"); err != nil {
		return err
	}
	sid, _, _, err := syscall.LookupSID("", `NT SERVICE\`+serviceName)
	if err != nil {
		return err
	}
	sidText, err := sid.String()
	if err != nil {
		return err
	}
	if err = protectData(p.data, sidText, true); err != nil {
		return err
	}
	if err = protectSharedCodexHome(p); err != nil {
		return err
	}
	if err = protectSharedCodexProgram(p, sidText); err != nil {
		return err
	}
	for _, file := range []string{p.config, filepath.Join(p.data, "install-record.json")} {
		if err = denyReparse(file); err != nil {
			return err
		}
		if err = protectData(file, sidText, file == p.config); err != nil {
			return err
		}
	}
	if _, e := os.Stat(p.config + ".bak"); e == nil {
		if e = denyReparse(p.config + ".bak"); e != nil {
			return e
		}
		if e = protectData(p.config+".bak", sidText, true); e != nil {
			return e
		}
	}
	private := filepath.Join(p.data, "private")
	if err = filepath.WalkDir(private, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = denyReparse(path); e != nil {
			return e
		}
		return protectData(path, sidText, true)
	}); err != nil {
		return err
	}
	for _, a := range [][]string{
		{"description", serviceName, "OnTech mail-code inbox, chat and file server."},
		{"failure", serviceName, "reset=", "86400", "actions=", "restart/5000/restart/15000/restart/60000"},
		{"failureflag", serviceName, "1"},
	} {
		if err = serviceCommand(a...); err != nil {
			return err
		}
	}
	stageName = "제거 항목 및 바로가기 등록"
	maintenance := p.exe
	for _, v := range [][3]string{
		{"DisplayName", "REG_SZ", "OnTech Chat Server"}, {"DisplayVersion", "REG_SZ", installerVersion}, {"Publisher", "REG_SZ", "OnTech"},
		{"InstallLocation", "REG_SZ", p.program}, {"UninstallString", "REG_SZ", syscall.EscapeArg(maintenance) + " --uninstall"},
		{"DisplayIcon", "REG_SZ", filepath.Join(p.program, "app.ico")}, {"NoModify", "REG_DWORD", "1"}, {"NoRepair", "REG_DWORD", "1"},
	} {
		if err = registryValue(v[0], v[1], v[2]); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(p.menu, 0755); err != nil {
		return err
	}
	endpoint, localURL, err := trayEndpoint(cfg.Listen, cfg.TLSCert)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(p.program, "bin", "tray-endpoint.json"), endpoint, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(p.menu, "채팅 화면 열기.url"), []byte("[InternetShortcut]\r\nURL="+localURL+"\r\n"), 0644); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(p.menu, "설정 화면.url"), []byte("[InternetShortcut]\r\nURL="+localURL+"?settings=1\r\n"), 0644); err != nil {
		return err
	}
	// 구 버전의 메모장/관리자 권한 브라우저 메뉴 대신 일반 사용자 브라우저에서 설정을 엽니다.
	_ = os.Remove(filepath.Join(p.menu, "설정 편집.cmd"))
	_ = os.Remove(filepath.Join(p.menu, "설정 화면.cmd"))
	stageName = "로컬 방화벽 설정 연결 등록 (규칙은 자동으로 열지 않음)"
	if err = registerFirewallProtocol(p); err != nil {
		return err
	}
	stageName = "Windows 로그인 트레이 자동 실행 등록"
	if err = registerTray(p); err != nil {
		return err
	}
	// Only remove retired app binaries inside an ownership-verified installation.
	for _, old := range []string{filepath.Join(p.program, "Maintenance.exe"), filepath.Join(p.program, "bin", "OnTechChat.Tray.exe")} {
		if err = denyReparse(old); err != nil {
			return err
		}
		if err = os.Remove(old); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("기존 관리/트레이 파일을 닫은 뒤 재시도하세요: %w", err)
		}
	}
	stageName = "Android APK 자동 배포"
	apkBundle := filepath.Join(p.program, "apk-release")
	if err = denyReparse(apkBundle); err != nil {
		return err
	}
	if err = command(p.exe, "--config", p.config, "--publish-apk", apkBundle); err != nil {
		return fmt.Errorf("Setup 포함 Android APK 배포: %w", err)
	}
	stageName = "서버 서비스 시작"
	return startService()
}
func uninstall(p paths, purgeData bool) error {
	if e := checkOwned(p); e != nil {
		return e
	}
	return runRemoval(purgeData, removalSteps{
		firewall:  func() error { return firewallAction(p, "remove") },
		protocol:  unregisterFirewallProtocol,
		startup:   unregisterTray,
		shortcuts: func() error { return removeShortcuts(p) },
		stop:      func() error { return stopInstallationRuntime(p) },
		service: func() error {
			exists, _, _, e := queryService()
			if e != nil {
				return e
			}
			if !exists {
				return nil
			}
			if e = serviceCommand("delete", serviceName); e != nil {
				return e
			}
			return waitState(0, 30*time.Second)
		},
		data:    func() error { return removeTree(p.data) },
		program: func() error { return removeTree(p.program) },
		registry: func() error {
			_, present, e := registryReadText(`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\OnTechChatServer`, "DisplayName")
			if e != nil || !present {
				return e
			}
			return command(systemExe("reg.exe"), "delete", uninstallKey, "/f", "/reg:64")
		},
	})
}
func checkOwned(p paths) error {
	for _, d := range []string{p.program, p.data} {
		if e := denyReparse(d); e != nil {
			return e
		}
		if e := ownedDirectory(d); e != nil {
			return e
		}
	}
	return ownedService(p)
}
func openNotepad(path string) error {
	if _, e := os.Stat(path); e != nil {
		return e
	}
	c := exec.Command(systemExe("notepad.exe"), path)
	return c.Start()
}
func waitParent(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, _, e := kernel.NewProc("OpenProcess").Call(0x00100000, 0, uintptr(pid))
	if h == 0 {
		if errors.Is(e, syscall.Errno(87)) {
			return nil
		} // Parent already exited.
		return e
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	r, _, e := kernel.NewProc("WaitForSingleObject").Call(h, 30000)
	if r != 0 {
		return fmt.Errorf("parent exit wait: %d %v", r, e)
	}
	return nil
}
func main() {
	// Route runtime modes before any setup flag registrations or admin-elevation logic.
	args := os.Args[1:]
	if runtimeServerMode(args) {
		clean := []string{os.Args[0]}
		for _, x := range args {
			if x != "--server-console" {
				clean = append(clean, x)
			}
		}
		os.Args = clean
		serverapp.Main()
		return
	}
	trayRequested := false
	for _, x := range args {
		if x == "--tray" {
			trayRequested = true
		}
	}
	if len(args) == 0 {
		if p, e := installPaths(); e == nil {
			current, _ := os.Executable()
			trayRequested = strings.EqualFold(filepath.Clean(current), filepath.Clean(p.exe))
		}
	}
	if trayRequested {
		if isAdmin() {
			if e := startTrayWithoutElevation(); e != nil {
				message(e.Error(), 0x10)
			}
			return
		}
		clean := []string{os.Args[0]}
		for _, x := range args {
			if x != "--tray" {
				clean = append(clean, x)
			}
		}
		os.Args = clean
		trayapp.Main()
		return
	}

	installFlag := flag.Bool("install", false, "install")
	firewall := flag.Bool("firewall", false, "open local Windows firewall settings with approval")
	aiAccount := flag.Bool("ai-account", false, "open Windows service logon account settings")
	codexLogin := flag.Bool("codex-login", false, "sign in to server-owned Codex home")
	protocol := flag.String("protocol", "", "validated local firewall settings URI only")
	remove := flag.Bool("uninstall", false, "uninstall")
	purgeData := flag.Bool("purge-data", false, "internal removal option confirmed by user")
	worker := flag.Bool("uninstall-worker", false, "internal removal stage")
	parent := flag.Int("wait-pid", 0, "internal parent PID")
	status := flag.Bool("status", false, "service status")
	restart := flag.Bool("restart", false, "restart")
	start := flag.Bool("start", false, "start")
	stop := flag.Bool("stop", false, "stop")
	settings := flag.Bool("settings", false, "edit config")
	login := flag.Bool("login", false, "show initial login")
	logs := flag.Bool("logs", false, "show service log")
	resetAdmin := flag.Bool("reset-admin", false, "reset local admin password with service stopped")
	verify := flag.Bool("verify-payload", false, "validate embedded package only")
	trayFlag := flag.Bool("tray", false, "start the current user tray without elevation")
	flag.Parse()
	if *protocol != "" {
		if len(os.Args) != 3 || !validFirewallProtocol(*protocol) {
			message("지원하지 않는 로컬 관리 요청입니다.", 0x10)
			return
		}
		*firewall = true
	}
	if *trayFlag {
		if err := startTrayWithoutElevation(); err != nil {
			message(err.Error(), 0x10)
		}
		return
	}
	if *verify {
		files, err := inspectPayload()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, f := range files {
			r, err := f.Open()
			if err != nil {
				os.Exit(1)
			}
			_, err = io.Copy(io.Discard, r)
			r.Close()
			if err != nil {
				os.Exit(1)
			}
		}
		fmt.Printf("Payload verified: %d files; SHA256 %s\n", len(files), payloadDigest())
		return
	}
	if !isAdmin() {
		args := os.Args[1:]
		if *firewall {
			args = []string{"--firewall"}
		}
		if err := elevateSelf(args); err != nil {
			message(err.Error(), 0x10)
		}
		return
	}
	if *worker {
		if _, err := removalWorkerDirectory(); err != nil {
			message(err.Error(), 0x10)
			return
		}
		cleanupErr := scheduleWorkerCleanup()
		if cleanupErr != nil {
			exe, _ := os.Executable()
			kernel.NewProc("MoveFileExW").Call(ptr(wide(exe)), 0, 4)
			kernel.NewProc("MoveFileExW").Call(ptr(wide(filepath.Dir(exe))), 0, 4)
			message("임시 제거 도구는 재부팅 후 정리됩니다.\n"+cleanupErr.Error(), 0x30)
		}
		if err := waitParent(*parent); err != nil {
			message(err.Error(), 0x10)
			return
		}
	}
	if *firewall {
		p, e := installPaths()
		if e == nil {
			e = firewallAction(p, "ui")
		}
		if e != nil {
			message("방화벽 설정: "+e.Error(), 0x10)
		}
		return
	}
	unlock, err := setupMutex()
	if err != nil {
		message(err.Error(), 0x10)
		return
	}
	defer unlock()
	p, err := installPaths()
	if err != nil {
		message(err.Error(), 0x10)
		return
	}
	switch {
	case *codexLogin:
		err = sharedCodexLogin(p)
	case *aiAccount:
		err = openAIServiceAccountSettings(p)
	case *firewall:
		err = firewallAction(p, "ui")
	case *worker:
		err = uninstall(p, *purgeData)
		if err == nil {
			note := "설정·계정·대화·첨부파일도 삭제했습니다."
			if !*purgeData {
				note = "선택에 따라 데이터만 보존했습니다:\n" + p.data
			}
			message("서버·트레이·바로가기·자동 시작·설치 폴더를 제거했습니다.\n\n"+note, 0x40)
		}
	case *remove:
		if err = checkOwned(p); err != nil {
			break
		}
		choice := message("온테크 채팅 서버를 제거합니다.\n설정·계정·대화·첨부파일도 모두 삭제할까요?\n\n예: 프로그램과 데이터를 모두 삭제 (복구할 수 없음)\n아니요: 프로그램만 삭제, 데이터 보존\n취소: 제거하지 않음\n\n데이터: "+p.data, 0x123)
		if choice != 6 && choice != 7 {
			return
		}
		// The worker runs outside the installed directory so it can delete that directory.
		tempDir, ce := os.MkdirTemp(filepath.Dir(p.program), "OnTechChat-remove-")
		if ce != nil {
			err = ce
			break
		}
		tmp := filepath.Join(tempDir, "Uninstall.exe")
		exe, _ := os.Executable()
		if err = copyFile(exe, tmp); err != nil {
			_ = os.RemoveAll(tempDir)
			break
		}
		workerArgs := []string{"--uninstall-worker", "--wait-pid", fmt.Sprint(os.Getpid())}
		if choice == 6 {
			workerArgs = append(workerArgs, "--purge-data")
		}
		cmd := exec.Command(tmp, workerArgs...)
		cmd.Dir = filepath.Dir(p.program) // Never hold the installation directory open during removal.
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if err = cmd.Start(); err != nil {
			_ = os.RemoveAll(tempDir)
		} else {
			_ = cmd.Process.Release()
		}
	case *status:
		var exists bool
		var s uint32
		exists, s, _, err = queryService()
		if err == nil {
			stateName := map[uint32]string{0: "미설치", 1: "중지", 2: "시작 중", 3: "종료 중", 4: "실행 중"}[s]
			message(fmt.Sprintf("설치됨: %t\n서비스 상태: %s\n\n프로그램: %s\n설정·데이터: %s", exists, stateName, p.program, p.data), 0x40)
		}
	case *restart || *start || *stop:
		if err = checkOwned(p); err != nil {
			break
		}
		if *restart || *stop {
			err = stopService()
		}
		if err == nil && (*restart || *start) {
			err = startService()
		}
		if err == nil {
			message("서비스 작업을 완료했습니다.", 0x40)
		}
	case *resetAdmin:
		if err = checkOwned(p); err != nil {
			break
		}
		if message("관리자 비밀번호를 재설정할까요?\n서버를 잠시 중지하고 기존 관리자 로그인 세션을 해제합니다.", 0x124) != 6 {
			return
		}
		if err = stopService(); err != nil {
			break
		}
		err = command(p.exe, "--config", p.config, "--reset-admin")
		sid, _, _, aclErr := syscall.LookupSID("", `NT SERVICE\`+serviceName)
		if aclErr == nil {
			var sidText string
			sidText, aclErr = sid.String()
			if aclErr == nil {
				aclErr = filepath.WalkDir(filepath.Join(p.data, "private"), func(path string, d os.DirEntry, e error) error {
					if e != nil {
						return e
					}
					if e = denyReparse(path); e != nil {
						return e
					}
					return protectData(path, sidText, true)
				})
			}
		}
		if err == nil {
			err = aclErr
		}
		if err == nil {
			err = startService()
		}
		if err == nil {
			message("관리자 비밀번호를 재설정했습니다.\n새 비밀번호 파일:\n"+filepath.Join(p.data, "private", "RESET-LOGIN-날짜.txt")+"\n\n확인 후 안전하게 보관하고 파일을 삭제하세요.", 0x40)
		}
	case *settings:
		err = openSettingsBrowser(p)
	case *login:
		err = openNotepad(filepath.Join(p.data, "private", "INITIAL-LOGIN.txt"))
	case *logs:
		err = openNotepad(filepath.Join(p.data, "private", "service.log"))
	default:
		_ = installFlag
		text := "다른 PC용 온테크 채팅 서버를 설치합니다.\n\n• 실행 파일 하나로 서버·트레이·관리 기능 통합\n• Windows 시작 시 자동(지연된 시작) 실행\n• 서버는 로그인 전 실행 / 트레이는 로그인 후 표시\n• 트레이 더블클릭으로 채팅, 우클릭으로 설정·서버 제어\n• 방화벽·포트 진단과 단일 TCP 허용 (별도 승인, 자동 개방 안 함)\n• 장애 종료 시 5초·15초·60초 재시작 시도\n• 바탕화면·시작 메뉴 바로가기 생성\n• 업데이트 시 실행 중인 서버·트레이 자동 종료 후 재시작\n• 최신 Android APK도 Setup에 포함되어 서버 APK 배포 폴더에 자동 등록\n• Android 앱의 업데이트 확인에서 서버로부터 다운로드·설치 가능\n• 제거 시 데이터까지 삭제 또는 보존 선택\n• 기존 설정이 있으면 덮어쓰지 않음\n• 설치 PC의 Codex 실행 엔진을 공용 폴더에 준비\n• 서버용 AI 로그인 1회 (Windows 계정 변경 불필요)\n\n프로그램: " + p.program + "\n데이터: " + p.data + "\n\n메일·FCM·외부 접속은 별도 설정입니다.\n코드 서명과 실제 Windows 재부팅 검증은 아직 없습니다.\n빌드 도구: " + runtime.Version() + "\n인터넷 공개 전 지원 중인 보안 패치로 빌드했는지 확인하세요.\n\n설치할까요?"
		if message(text, 0x124) != 6 {
			return
		}
		err = install(p)
		if err == nil {
			trayErr := launchTrayAsDesktopUser(p)
			trayNote := "트레이가 실행되었습니다. 오른쪽 아래 아이콘을 더블클릭하면 채팅을 엽니다."
			if trayErr != nil {
				trayNote = "트레이 자동 표시는 완료하지 못했습니다. 시작 메뉴 → OnTech Chat Server → 트레이 실행을 누르거나 다시 로그인하세요."
			}
			commonExe, commonHome := serverapp.SharedCodexLocations(p.program, p.data)
			aiNote := "\n\n공용 Codex가 없습니다. 이 PC에 Codex를 설치한 뒤 이 설치 파일을 다시 실행하세요."
			if _, ce := os.Stat(commonExe); ce == nil {
				aiNote = "\n\n공용 Codex 준비 완료. 서버용 AI 로그인은 트레이에서 진행할 수 있습니다."
			}
			message(trayNote+aiNote+"\n\n설치 및 서버 실행을 완료했습니다.\n\n같은 PC에서 http://127.0.0.1:8787 접속\n초기 계정: 시작 메뉴 → OnTech Chat Server → 초기 관리자 정보\n\n재부팅 후 시작 메뉴의 '서버 상태'로 확인하세요.\n브라우저를 닫아도 서버는 계속 실행됩니다.\n서버 PC를 끄거나 절전 상태로 두면 사용할 수 없습니다.", 0x40)
			if _, ce := os.Stat(commonExe); ce == nil {
				if _, ae := os.Stat(filepath.Join(commonHome, "auth.json")); os.IsNotExist(ae) {
					err = sharedCodexLogin(p)
				}
			}
		}
	}
	if err != nil {
		message("작업이 완료되지 않았습니다.\n\n"+err.Error()+"\n\n오류가 표시된 항목을 확인하고 설치 파일로 다시 시도하세요.\n필요한 경우 이 화면을 공유하세요. 비밀번호 파일은 공유하지 마세요.", 0x10)
		os.Exit(1)
	}
}

func openSettingsBrowser(p paths) error {
	raw, e := os.ReadFile(p.config)
	if e != nil {
		return e
	}
	var cfg struct {
		Listen    string `json:"listen"`
		TLSCert   string `json:"tls_cert"`
		PublicURL string `json:"public_url"`
	}
	if e = json.Unmarshal(raw, &cfg); e != nil {
		return e
	}
	_, port, e := net.SplitHostPort(cfg.Listen)
	if e != nil {
		return e
	}
	target := "http://127.0.0.1:" + port + "/?settings=1"
	if cfg.TLSCert != "" {
		target = "https://127.0.0.1:" + port + "/?settings=1"
	}
	return shellOpenURL(target)
}
