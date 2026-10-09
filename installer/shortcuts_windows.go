//go:build windows

package main

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Encode scripts as UTF-16 to preserve Korean shortcut names on every locale.
func powershell(script string) *exec.Cmd {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	ps := filepath.Join(filepath.Dir(systemExe("cmd.exe")), "WindowsPowerShell", "v1.0", "powershell.exe")
	c := exec.Command(ps, "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func runPowerShell(script string) error {
	out, e := powershell("$ErrorActionPreference='Stop';" + script).CombinedOutput()
	if e != nil {
		return fmt.Errorf("Windows 설정: %w\n%s", e, strings.TrimSpace(string(out)))
	}
	return nil
}
func createShortcut(link, target, args, icon string) error {
	if e := denyReparse(link); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(link), 0755); e != nil {
		return e
	}
	script := "$w=New-Object -ComObject WScript.Shell;$l=$w.CreateShortcut(" + psQuote(link) + ");$l.TargetPath=" + psQuote(target) + ";$l.Arguments=" + psQuote(args) + ";$l.WorkingDirectory=" + psQuote(filepath.Dir(target)) + ";$l.IconLocation=" + psQuote(icon+",0") + ";$l.Description='OnTech Chat Server';$l.Save();$v=$w.CreateShortcut(" + psQuote(link) + ");if($v.TargetPath -ne " + psQuote(target) + " -or $v.Arguments -ne " + psQuote(args) + "){throw 'Shortcut verification failed'}"
	return runPowerShell(script)
}
func registerShortcuts(p paths) error {
	desktop, e := folder(0x19)
	if e != nil {
		return e
	} // Common desktop, all users.
	icon := filepath.Join(p.program, "app.ico")
	for _, item := range [][3]string{
		{desktop, "OnTech Chat Server", "--tray"},
		{p.menu, "트레이 실행", "--tray"},
		{p.menu, "서버 상태", "--status"}, {p.menu, "서버 재시작", "--restart"},
		{p.menu, "서버 중지", "--stop"}, {p.menu, "서버 시작", "--start"},
		{p.menu, "초기 관리자 정보", "--login"}, {p.menu, "서버 로그", "--logs"},
		{p.menu, "관리자 비밀번호 재설정", "--reset-admin"}, {p.menu, "방화벽·포트 설정", "--firewall"},
		{p.menu, "프로그램 제거", "--uninstall"},
	} {
		if e = createShortcut(filepath.Join(item[0], item[1]+".lnk"), p.exe, item[2], icon); e != nil {
			return e
		}
		if item[0] == p.menu {
			if e = os.Remove(filepath.Join(p.menu, item[1]+".cmd")); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	}
	return nil
}
func removeShortcuts(p paths) error {
	desktop, e := folder(0x19)
	if e != nil {
		return e
	}
	profiles := "$profiles=Get-ItemProperty 'HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\ProfileList\\*';foreach($profile in $profiles){$h=[Environment]::ExpandEnvironmentVariables($profile.ProfileImagePath);if($h){$roots+=Join-Path $h 'Desktop';$roots+=Join-Path $h 'AppData\\Roaming\\Microsoft\\Windows\\Start Menu';$roots+=Join-Path $h 'OneDrive\\Desktop'}};"
	return errors.Join(removeShortcutRoots(p, []string{desktop}, profiles), removeTree(p.menu))
}

func removeShortcutRoots(p paths, roots []string, extra string) error {
	var quoted []string
	for _, root := range roots {
		quoted = append(quoted, psQuote(root))
	}
	script := "$targets=@(" + psQuote(p.exe) + "," + psQuote(filepath.Join(p.program, "bin", "OnTechChat.Tray.exe")) + "," + psQuote(filepath.Join(p.program, "Maintenance.exe")) + ");$roots=@(" + strings.Join(quoted, ",") + ");" + extra
	script += "$w=New-Object -ComObject WScript.Shell;$fail=@();foreach($root in ($roots|Select-Object -Unique)){if(Test-Path -LiteralPath $root){$scanErrors=@();Get-ChildItem -LiteralPath $root -Filter '*.lnk' -Recurse -File -ErrorAction SilentlyContinue -ErrorVariable scanErrors|ForEach-Object {try{$l=$w.CreateShortcut($_.FullName);if($targets -contains $l.TargetPath){Remove-Item -LiteralPath $_.FullName -Force}}catch{$fail+=$_.Exception.Message}};foreach($scanError in $scanErrors){$fail+=$scanError.Exception.Message}}};if($fail.Count){throw ($fail -join [Environment]::NewLine)}"
	return runPowerShell(script)
}

// The helper waits for its parent to exit before removing itself.
// Its only removal target is its own validated temporary directory.
func removalWorkerDirectory() (string, error) {
	exe, e := os.Executable()
	if e != nil {
		return "", e
	}
	dir := filepath.Dir(exe)
	p, e := installPaths()
	if e != nil {
		return "", e
	}
	if !strings.EqualFold(filepath.Dir(dir), filepath.Dir(p.program)) || !strings.HasPrefix(filepath.Base(dir), "OnTechChat-remove-") || filepath.Base(exe) != "Uninstall.exe" {
		return "", errors.New("invalid removal helper path")
	}
	return dir, nil
}
func scheduleWorkerCleanup() error {
	dir, e := removalWorkerDirectory()
	if e != nil {
		return e
	}
	script := "$ErrorActionPreference='Stop';$pidToWait=" + fmt.Sprint(os.Getpid()) + ";Wait-Process -Id $pidToWait -ErrorAction SilentlyContinue;for($i=0;$i -lt 40;$i++){try{Remove-Item -LiteralPath " + psQuote(dir) + " -Recurse -Force;if(!(Test-Path -LiteralPath " + psQuote(dir) + ")){exit 0}}catch{};Start-Sleep -Milliseconds 250};exit 1"
	c := powershell(script)
	c.Dir = filepath.Dir(systemExe("cmd.exe"))
	if e = c.Start(); e != nil {
		return e
	}
	return c.Process.Release()
}
