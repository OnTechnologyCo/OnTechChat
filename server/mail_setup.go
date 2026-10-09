package server

import (
	"net"
	"net/url"
	"runtime"
	"sort"
	"strings"
)

// 제목에 지정 문구가 포함된 메일의 제목과 본문만 전달합니다.
// 발신자 주소와 DMARC는 검사하지 않습니다.
func riotMailRules() []MailRule {
	return []MailRule{{Service: "Naver Mail", CodePattern: "로그인 코드"}}
}

// 선택을 명시하지 않은 구버전 설정은 기존 개인 인증함으로만 전달합니다.
func mailRecipients(c MailConfig) []string {
	names := c.RecipientUsernames
	if names == nil {
		names = []string{c.OwnerUsername}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, name := range names {
		if name != "" && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	return out
}
func networkSettingsInfo(c Config) map[string]any {
	u, e := url.Parse(c.PublicURL)
	out := map[string]any{"public_url": c.PublicURL, "configured": c.PublicURL != "", "tested_from_internet": false, "mode": "local", "listener": c.Listen, "caddyfile": ""}
	out["windows_firewall_ui"] = runtime.GOOS == "windows"
	out["firewall_helper_uri"] = "ontechchat-admin://firewall"
	out["diagnostic"] = describeNetworkConfiguration(c)
	if e != nil || c.PublicURL == "" {
		return out
	}
	host, _, _ := net.SplitHostPort(c.Listen)
	if net.ParseIP(host).IsLoopback() && c.TLSCert == "" {
		out["mode"] = "https_proxy"
		// Caddyfile 문자열 주입 방지: 완전 검증된 DNS 이름/숫자 포트만 설정 예제를 생성합니다.
		// IP 인증서는 별도 관리가 필요하므로 이 자동 인증 예제는 DNS 이름에만 제공됩니다.
		if net.ParseIP(u.Hostname()) == nil && validDNSName(u.Hostname()) && validPort(u.Port()) {
			out["caddyfile"] = "# 이 파일은 프록시 설정 예제이며, 설치/실행/방화벽 변경은 하지 않습니다.\n" + u.Host + " {\n    reverse_proxy " + c.Listen + " {\n        flush_interval -1\n    }\n}\n"
		}
	} else if c.TLSCert != "" {
		out["mode"] = "direct_tls"
	}
	return out
}
func validDNSName(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func validPort(s string) bool {
	if s == "" {
		return true
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
		if n > 65535 {
			return false
		}
	}
	return n > 0
}
func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
