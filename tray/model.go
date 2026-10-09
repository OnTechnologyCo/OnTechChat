package tray

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const trayVersion = "1.15.17"
const serviceName = "OnTechChatServer"
const windowClass = "OnTechChat.Tray.Window.v1"
const mutexName = `Local\OnTechChatServer.Tray.v1`
const endpointFilename = "tray-endpoint.json"

type endpoint struct {
	LocalURL string `json:"local_url"`
}
type serviceState struct {
	Exists bool
	State  uint32
	Err    string
}

func (s serviceState) text() string {
	if s.Err != "" {
		return "서비스 상태 확인 실패"
	}
	if !s.Exists {
		return "서비스 미설치"
	}
	switch s.State {
	case 1:
		return "서비스 중지됨"
	case 2:
		return "서비스 시작 중"
	case 3:
		return "서비스 종료 중"
	case 4:
		return "서비스 실행 중"
	case 7:
		return "서비스 일시 중지"
	default:
		return "서비스 상태 전환 중"
	}
}
func (s serviceState) iconKey() string {
	if s.Err != "" {
		return "pending"
	}
	if s.Exists && s.State == 4 {
		return "running"
	}
	if s.Exists && (s.State == 2 || s.State == 3 || s.State == 5 || s.State == 6) {
		return "pending"
	}
	return "stopped"
}

// 트레이는 비밀값이 있는 config.json을 읽지 않습니다. 설치기가 만든 로컬 주소만 사용합니다.
func decodeEndpoint(raw []byte) (string, error) {
	if len(raw) > 4096 {
		return "", errors.New("tray endpoint file exceeds 4 KiB")
	}
	var e endpoint
	if err := json.Unmarshal(raw, &e); err != nil {
		return "", errors.New("invalid tray endpoint JSON")
	}
	return validateLocalURL(e.LocalURL)
}
func validateLocalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return "", errors.New("invalid local URL")
	}
	ip := net.ParseIP(u.Hostname())
	if (u.Scheme != "http" && u.Scheme != "https") || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" || u.RawPath != "" {
		return "", errors.New("tray URL must be an HTTP(S) loopback origin without credentials")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid local port")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("empty local port")
	}
	u.Path = "/"
	return u.String(), nil
}
func settingsURL(origin string) (string, error) {
	base, e := validateLocalURL(origin)
	if e != nil {
		return "", e
	}
	return base + "?settings=1", nil
}
func statusDescription(s serviceState, origin string) string {
	out := fmt.Sprintf("%s\n접속 주소: %s\n\n서버: Windows 부팅 시 자동 실행\n트레이: 사용자 로그인 시 표시\n트레이를 종료해도 서버는 계속 실행됩니다.\n\nOnTech Chat Tray %s", s.text(), origin, trayVersion)
	if s.Err != "" {
		out += "\n\n진단: " + s.Err
	}
	return out
}
