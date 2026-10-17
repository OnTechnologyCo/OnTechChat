//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed firewall_commands.ps1
var firewallCommands string

//go:embed firewall_ui.ps1
var firewallUI string

const firewallProtocolKey = `SOFTWARE\Classes\ontechchat-admin`

func installedFirewallPlan(p paths, removal bool) (firewallPlan, error) {
	if e := checkOwned(p); e != nil {
		return firewallPlan{}, e
	}
	if removal {
		return firewallPlan{Version: installerVersion, Program: p.exe, RuleName: firewallRuleName, RuleGroup: firewallRuleGroup}, nil
	}
	if e := denyReparse(p.exe); e != nil {
		return firewallPlan{}, e
	}
	if e := denyReparse(p.config); e != nil {
		return firewallPlan{}, e
	}
	st, e := os.Stat(p.config)
	if e != nil {
		return firewallPlan{}, e
	}
	if st.Size() > 256<<10 {
		return firewallPlan{}, errors.New("config.json size limit")
	}
	raw, e := os.ReadFile(p.config)
	if e != nil {
		return firewallPlan{}, e
	}
	return makeFirewallPlan(raw, p.exe)
}
func firewallAction(p paths, mode string) error {
	if mode != "ui" && mode != "remove" && mode != "inspect" && mode != "https-ui" && mode != "https-remove" {
		return errors.New("invalid firewall action")
	}
	autoHTTPS := strings.HasPrefix(mode, "https-")
	if mode == "remove" {
		if e := firewallAction(p, "https-remove"); e != nil {
			return e
		}
	}
	removal := mode == "remove" || mode == "https-remove"
	plan, e := installedFirewallPlan(p, removal)
	if e != nil {
		return e
	}
	if autoHTTPS {
		if removal {
			plan.RuleName, plan.RuleGroup = httpsFirewallRuleName, httpsFirewallRuleGroup
			mode = "remove"
		} else {
			raw, readErr := os.ReadFile(p.config)
			if readErr != nil {
				return readErr
			}
			plan, e = makeHTTPSFirewallPlan(raw, p.exe)
			if e != nil {
				return e
			}
			mode = "ui"
		}
	}
	b, e := json.Marshal(plan)
	if e != nil {
		return e
	}
	ps := filepath.Join(filepath.Dir(systemExe("cmd.exe")), "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-STA", "-Command", firewallCommands+"\n"+firewallUI)
	// 정책/설정값을 PowerShell 소스나 명령 문자열에 보간하지 않습니다.
	// 실행 정책 변경/우회 플래그, 웹 입력 명령, 전체 포트 허용은 사용하지 않습니다.
	cmd.Env = []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(v), "ONTECH_FIREWALL_PLAN=") && !strings.HasPrefix(strings.ToUpper(v), "ONTECH_FIREWALL_MODE=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "ONTECH_FIREWALL_PLAN="+string(b), "ONTECH_FIREWALL_MODE="+mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if e != nil {
		if len(out) > 4096 {
			out = out[:4096]
		}
		return fmt.Errorf("Windows 방화벽 도구: %w\n%s", e, strings.TrimSpace(string(out)))
	}
	return nil
}
func registryReadText(key, name string) (string, bool, error) {
	var h syscall.Handle
	e := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, wide(key), 0, syscall.KEY_READ|syscall.KEY_WOW64_64KEY, &h)
	if errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if e != nil {
		return "", false, e
	}
	defer syscall.RegCloseKey(h)
	var typ, n uint32
	e = syscall.RegQueryValueEx(h, wide(name), nil, &typ, nil, &n)
	if errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if e != nil {
		return "", false, e
	}
	if typ != syscall.REG_SZ || n > 8192 || n%2 != 0 {
		return "", false, errors.New("unexpected protocol registry value")
	}
	b := make([]byte, n)
	if n == 0 {
		return "", true, nil
	}
	e = syscall.RegQueryValueEx(h, wide(name), nil, &typ, &b[0], &n)
	if e != nil {
		return "", false, e
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return syscall.UTF16ToString(u), true, nil
}
func registerFirewallProtocol(p paths) error {
	owner, hasOwner, e := registryReadText(firewallProtocolKey, "OnTechOwner")
	if e != nil {
		return e
	}
	if hasOwner && owner != installID {
		return errors.New("ontechchat-admin protocol belongs to another application")
	}
	if !hasOwner {
		_, exists, e := registryReadText(firewallProtocolKey, "")
		if e != nil {
			return e
		}
		if exists {
			return errors.New("existing protocol registration is unrecognized")
		}
	}
	base := `HKLM\` + firewallProtocolKey
	exe := p.exe
	params := syscall.EscapeArg(exe) + ` --protocol "%1"`
	for _, a := range [][]string{
		{"add", base, "/ve", "/t", "REG_SZ", "/d", "URL:OnTech Chat Local Administration", "/f", "/reg:64"},
		{"add", base, "/v", "OnTechOwner", "/t", "REG_SZ", "/d", installID, "/f", "/reg:64"},
		{"add", base, "/v", "URL Protocol", "/t", "REG_SZ", "/d", "", "/f", "/reg:64"},
		{"add", base + `\shell\open\command`, "/ve", "/t", "REG_SZ", "/d", params, "/f", "/reg:64"},
	} {
		if e := command(systemExe("reg.exe"), a...); e != nil {
			return e
		}
	}
	return nil
}
func unregisterFirewallProtocol() error {
	owner, exists, e := registryReadText(firewallProtocolKey, "OnTechOwner")
	if e != nil {
		return e
	}
	if !exists {
		return nil
	}
	if owner != installID {
		return errors.New("protocol ownership mismatch; it was not removed")
	}
	return command(systemExe("reg.exe"), "delete", `HKLM\`+firewallProtocolKey, "/f", "/reg:64")
}
