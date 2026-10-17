package main

import (
	"encoding/json"
	"errors"
	"net"
	"regexp"
)

const httpsFirewallProtocol = "ontechchat-admin://https-firewall"
const httpsFirewallRuleName = "OnTechChatServer.Managed.AutoHTTPS.TCP.v1"
const httpsFirewallRuleGroup = "OnTechChatServer.Managed.AutoHTTPS.v1"

func validHTTPSFirewallProtocol(raw string) bool {
	return raw == httpsFirewallProtocol || raw == httpsFirewallProtocol+"/"
}
func makeHTTPSFirewallPlan(raw []byte, program string) (firewallPlan, error) {
	var c struct {
		AutoHTTPS struct {
			Enabled bool   `json:"enabled"`
			Domain  string `json:"domain"`
		} `json:"auto_https"`
	}
	if len(raw) > 256<<10 || json.Unmarshal(raw, &c) != nil {
		return firewallPlan{}, errors.New("HTTPS 설정 파일 형식을 확인하세요")
	}
	domain := c.AutoHTTPS.Domain
	valid := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	if !c.AutoHTTPS.Enabled || len(domain) > 253 || net.ParseIP(domain) != nil || !valid.MatchString(domain) {
		return firewallPlan{}, errors.New("서버 설정에서 도메인 자동 HTTPS를 먼저 저장하세요")
	}
	return firewallPlan{Version: installerVersion, Program: program, PublicURL: "https://" + domain, Listen: ":443", Port: 443, ListenPort: 443, TLSConfigured: true,
		RuleName: httpsFirewallRuleName, RuleGroup: httpsFirewallRuleGroup,
		Warning: "자동 HTTPS용 TCP 443만 별도 규칙으로 허용합니다. 기존 IP 접속 규칙은 유지합니다. 공유기를 사용한다면 외부 TCP 443을 이 서버의 TCP 443으로 전달하세요. Cloudflare는 DNS 전용으로 유지하세요."}, nil
}
