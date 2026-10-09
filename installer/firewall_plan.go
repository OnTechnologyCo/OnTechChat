package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const firewallRuleName = "OnTechChatServer.Managed.Inbound.TCP.v1"
const firewallRuleGroup = "OnTechChatServer.Managed.Firewall.v1"
const firewallProtocol = "ontechchat-admin://firewall"

// 웹에서 명령, 포트, 실행 파일을 받지 않습니다. 로컬 설치 설정만 읽어 고정 UI를 엽니다.
func validFirewallProtocol(raw string) bool {
	return raw == firewallProtocol || raw == firewallProtocol+"/"
}

type firewallPlan struct {
	Version       string `json:"version"`
	Program       string `json:"program"`
	PublicURL     string `json:"public_url"`
	Listen        string `json:"listen"`
	Port          int    `json:"port"`
	ListenPort    int    `json:"listen_port"`
	Loopback      bool   `json:"loopback"`
	TLSConfigured bool   `json:"tls_configured"`
	Warning       string `json:"warning"`
	RuleName      string `json:"rule_name"`
	RuleGroup     string `json:"rule_group"`
}

func makeFirewallPlan(raw []byte, program string) (firewallPlan, error) {
	p := firewallPlan{Version: installerVersion, Program: program, RuleName: firewallRuleName, RuleGroup: firewallRuleGroup}
	if len(raw) > 256<<10 {
		return p, fmt.Errorf("설정 파일이 너무 큽니다")
	}
	var c struct {
		Listen    string `json:"listen"`
		PublicURL string `json:"public_url"`
		TLSCert   string `json:"tls_cert"`
		TLSKey    string `json:"tls_key"`
	}
	// 알 수 없는 필드를 구조체에 넣지 않아 메일 비밀번호/FCM 키를 자식 프로세스로 보내지 않습니다.
	if json.Unmarshal(raw, &c) != nil {
		return p, fmt.Errorf("config.json 형식을 확인하세요 (비밀 값은 출력하지 않음)")
	}
	p.Listen, p.PublicURL = c.Listen, c.PublicURL
	h, port, err := net.SplitHostPort(c.Listen)
	ip := net.ParseIP(h)
	if err != nil || ip == nil {
		return p, fmt.Errorf("서버 수신 주소 형식이 잘못됐습니다")
	}
	p.ListenPort, err = strconv.Atoi(port)
	if err != nil || p.ListenPort < 1 || p.ListenPort > 65535 {
		return p, fmt.Errorf("서버 수신 포트가 잘못됐습니다")
	}
	p.Loopback = ip.IsLoopback()
	p.TLSConfigured = c.TLSCert != "" && c.TLSKey != ""
	if c.PublicURL == "" {
		p.Warning = "외부 HTTPS 주소를 설정 화면에서 먼저 저장하세요. 아직 열 포트를 결정하지 않았습니다."
		return p, nil
	}
	u, e := url.Parse(c.PublicURL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return p, fmt.Errorf("외부 접속 주소는 경로/인증정보 없는 HTTPS 주소여야 합니다")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return p, fmt.Errorf("외부 주소에 localhost를 사용할 수 없습니다")
	}
	if x := net.ParseIP(u.Hostname()); x != nil && (x.IsLoopback() || x.IsUnspecified()) {
		return p, fmt.Errorf("외부 접속 주소에 루프백/미지정 IP를 사용할 수 없습니다")
	}
	targetPort := u.Port()
	if targetPort == "" {
		targetPort = "443"
	}
	p.Port, e = strconv.Atoi(targetPort)
	// 시스템 관리용 저번호 포트를 실수로 열지 않습니다. HTTPS 443 또는 1024~65535만 허용.
	if e != nil || (p.Port != 443 && p.Port < 1024) || p.Port > 65535 {
		return p, fmt.Errorf("허용할 포트는 443 또는 1024~65535 중 하나여야 합니다")
	}
	warnings := []string{}
	if p.Loopback {
		warnings = append(warnings, "현재 채팅 서버는 127.0.0.1/::1에서만 수신하도록 설정되어 있습니다. 방화벽을 열어도 외부 직접 접속은 되지 않습니다.")
	}
	if !p.TLSConfigured {
		warnings = append(warnings, "직접 HTTPS 인증서/키 설정이 없습니다. 외부 주소 입력이나 방화벽 규칙은 HTTPS 수신기를 생성하지 않습니다.")
	}
	if p.ListenPort != p.Port {
		warnings = append(warnings, "외부 포트와 채팅 서버 수신 포트가 다릅니다. 별도 HTTPS 프록시 사용 여부를 확인하세요.")
	}
	warnings = append(warnings, "이 도구는 OnTechChat.Server.exe 전용입니다. Caddy/nginx 등 다른 HTTPS 프록시의 규칙은 변경하지 않습니다.")
	p.Warning = strings.Join(warnings, "\r\n")
	return p, nil
}
