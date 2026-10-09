package main

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
)

// 이 파일은 설치기가 생성하는 비밀값 없는 로컬 접속 주소 안내입니다. 주 설정은 config.json 한 곳에 유지합니다.
func trayEndpoint(listen, tlsCert string) ([]byte, string, error) {
	host, port, e := net.SplitHostPort(listen)
	if e != nil {
		return nil, "", e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, "", errors.New("tray endpoint requires loopback listen address")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return nil, "", errors.New("invalid listen port")
	}
	scheme := "http"
	if tlsCert != "" {
		scheme = "https"
	}
	u := scheme + "://" + net.JoinHostPort(host, port) + "/"
	b, e := json.MarshalIndent(struct {
		LocalURL string `json:"local_url"`
	}{u}, "", "  ")
	return b, u, e
}
