package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFirewallProtocolAllowlist(t *testing.T) {
	for _, v := range []string{firewallProtocol, firewallProtocol + "/"} {
		if !validFirewallProtocol(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", "ontechchat-admin://firewall?port=22", "ontechchat-admin://firewall;calc", "ontechchat-admin://uninstall", "https://example.test", "ontechchat-admin://firewall#x", "ontechchat-admin://firewall\" --uninstall", "ONTECHCHAT-ADMIN://firewall"} {
		if validFirewallProtocol(v) {
			t.Fatal(v)
		}
	}
}
func TestFirewallPlanDirectTLS(t *testing.T) {
	raw := []byte(`{"listen":"0.0.0.0:49333","public_url":"https://203.0.113.18:49333","tls_cert":"private/cert.pem","tls_key":"private/key.pem","mail":{"password_encrypted":"NO_LEAK"}}`)
	p, e := makeFirewallPlan(raw, `C:\Program Files\OnTechChatServer\bin\OnTechChat.Server.exe`)
	if e != nil {
		t.Fatal(e)
	}
	if p.Port != 49333 || p.ListenPort != 49333 || p.Loopback || !p.TLSConfigured {
		t.Fatalf("bad plan: %+v", p)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "NO_LEAK") || strings.Contains(string(b), "cert.pem") || strings.Contains(string(b), "key.pem") {
		t.Fatal("private config leaked")
	}
	if !strings.Contains(p.Warning, "프록시") {
		t.Fatal("proxy scope warning missing")
	}
}
func TestFirewallPlanWarnsLoopbackAndMissingTLS(t *testing.T) {
	p, e := makeFirewallPlan([]byte(`{"listen":"127.0.0.1:8787","public_url":"https://203.0.113.18:49333"}`), "OnTechChat.Server.exe")
	if e != nil || !p.Loopback || p.TLSConfigured || !strings.Contains(p.Warning, "수신 포트가 다릅니다") || !strings.Contains(p.Warning, "외부 직접 접속은 되지 않습니다") {
		t.Fatalf("%+v %v", p, e)
	}
	p, e = makeFirewallPlan([]byte(`{"listen":"127.0.0.1:8787"}`), "OnTechChat.Server.exe")
	if e != nil || p.Port != 0 || !strings.Contains(p.Warning, "먼저 저장") {
		t.Fatal(p, e)
	}
}
func TestFirewallPlanHTTPS443(t *testing.T) {
	p, e := makeFirewallPlan([]byte(`{"listen":"[::]:443","public_url":"https://chat.example.test","tls_cert":"cert","tls_key":"key"}`), "server.exe")
	if e != nil || p.Port != 443 || p.Loopback {
		t.Fatal(p, e)
	}
}
func TestFirewallPlanRejectsMalformedSettings(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"listen":"hostname:49333"}`, `{"listen":"0.0.0.0:0"}`, `{"listen":"0.0.0.0:65536"}`,
		`{"listen":"0.0.0.0:49333","public_url":"http://203.0.113.10:49333"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://user:pass@example.test"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://example.test/?port=22"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://example.test:22"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://example.test:65536"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://127.0.0.1:49333"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://0.0.0.0:49333"}`,
		`{"listen":"0.0.0.0:49333","public_url":"https://localhost:49333"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, e := makeFirewallPlan([]byte(raw), "server.exe"); e == nil {
				t.Fatal("accepted invalid plan")
			}
		})
	}
	if _, e := makeFirewallPlan(make([]byte, (256<<10)+1), "server.exe"); e == nil {
		t.Fatal("oversize accepted")
	}
}
func TestFirewallCommandsAreScoped(t *testing.T) {
	b, e := os.ReadFile("firewall_commands.ps1")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, text := range []string{"Direction='Inbound'", "Protocol='TCP'", "LocalPort=[string]$port", "Program=[string]$script:Plan.program", "$rule.Group -cne $script:Plan.rule_group", "[StringComparison]::OrdinalIgnoreCase", "-PolicyStore ActiveStore"} {
		if !strings.Contains(s, text) {
			t.Fatalf("missing safeguard %s", text)
		}
	}
	for _, text := range []string{"Set-NetFirewallProfile", "Disable-NetFirewallRule", "Invoke-Expression", "-CimSession", "-ExecutionPolicy", "OverrideBlockRules"} {
		if strings.Contains(s, text) {
			t.Fatalf("unsafe command %s", text)
		}
	}
	b, e = os.ReadFile("firewall_ui.ps1")
	if e != nil {
		t.Fatal(e)
	}
	s = string(b)
	if !strings.Contains(s, "'YesNo','Warning','Button2'") || !strings.Contains(s, "규칙 저장 실패") || !strings.Contains(s, "인터넷 접속 성공을 의미하지 않습니다") {
		t.Fatal("confirmation/result scope missing")
	}
}
