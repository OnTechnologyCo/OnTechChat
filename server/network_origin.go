package server

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// 기본 HTTPS 포트의 표기 차이만 정규화합니다. 다른 Host/Origin을 신뢰하지 않습니다.
func canonicalAuthority(scheme, authority string) (string, bool) {
	if scheme != "https" && scheme != "http" {
		return "", false
	}
	if authority == "" || strings.ContainsAny(authority, "/\\?#@ \t\r\n") {
		return "", false
	}
	u, e := url.Parse(scheme + "://" + authority)
	if e != nil || u.User != nil || u.Hostname() == "" {
		return "", false
	}
	h := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(h); ip != nil {
		h = ip.String()
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return "", false
	}
	return net.JoinHostPort(h, strconv.Itoa(n)), true
}
func sameAuthority(scheme, first, second string) bool {
	a, ok := canonicalAuthority(scheme, first)
	if !ok {
		return false
	}
	b, ok := canonicalAuthority(scheme, second)
	return ok && a == b
}
func sameWebOrigin(raw, scheme, host string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != scheme || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return sameAuthority(scheme, u.Host, host)
}
