package server

import (
	"net"
	"net/url"
	"strconv"
)

type NetworkDiagnostic struct {
	Listen        string   `json:"listen"`
	PublicPort    int      `json:"public_port"`
	ListenPort    int      `json:"listen_port"`
	Loopback      bool     `json:"loopback"`
	TLSConfigured bool     `json:"tls_configured"`
	Warnings      []string `json:"warnings"`
}

// 설정에 대한 진단일 뿐, 포트/방화벽/인터넷 도달성을 확인했다고 표시하지 않습니다.
// 추가 네트워크 요청이나 운영체제 명령 실행은 없습니다.
func describeNetworkConfiguration(c Config) NetworkDiagnostic {
	d := NetworkDiagnostic{Listen: c.Listen, TLSConfigured: c.TLSCert != "" && c.TLSKey != "", Warnings: []string{}}
	h, port, _ := net.SplitHostPort(c.Listen)
	d.ListenPort, _ = strconv.Atoi(port)
	if ip := net.ParseIP(h); ip != nil {
		d.Loopback = ip.IsLoopback()
	}
	if c.PublicURL != "" {
		if u, e := url.Parse(c.PublicURL); e == nil {
			p := u.Port()
			if p == "" {
				p = "443"
			}
			d.PublicPort, _ = strconv.Atoi(p)
		}
	}
	if d.PublicPort == 0 {
		d.Warnings = append(d.Warnings, "외부 HTTPS 주소가 아직 설정되지 않았습니다.")
	}
	if c.AutoHTTPS.Enabled {
		d.Warnings = append(d.Warnings, "도메인 자동 HTTPS가 별도 TCP443을 사용합니다. 위 항목은 기존 수신기 정보이며 실제 인증서 상태는 자동 HTTPS 영역에서 확인하세요.")
		return d
	}
	if d.Loopback {
		d.Warnings = append(d.Warnings, "현재 서버 수신 범위는 이 PC 내부뿐입니다. 방화벽 허용만으로 공인 IP 직접 접속이 되지 않습니다.")
	}
	if !d.TLSConfigured {
		d.Warnings = append(d.Warnings, "서버 직접 HTTPS 인증서/키 설정이 없습니다. 별도 HTTPS 프록시가 없다면 TLS 수신 구성이 필요합니다.")
	}
	if d.PublicPort > 0 && d.PublicPort != d.ListenPort {
		d.Warnings = append(d.Warnings, "외부 주소 포트와 서버 수신 포트가 다릅니다. 직접 연결 구성에서는 실제 TLS 수신 포트와 일치해야 합니다.")
	}
	return d
}
