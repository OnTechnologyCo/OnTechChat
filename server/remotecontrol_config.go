package server

import (
	"net"
	"path/filepath"
	"strings"
)

type RemoteControlConfig struct {
	Enabled           bool   `json:"enabled"`
	PluginsDir        string `json:"plugins_dir,omitempty"`
	Host              string `json:"host,omitempty"`
	Port              int    `json:"port,omitempty"`
	Username          string `json:"username,omitempty"`
	PasswordEncrypted string `json:"password_encrypted,omitempty"`
	password          string
}
type RemoteControlSettings struct {
	Enabled       bool   `json:"enabled"`
	PluginsDir    string `json:"plugins_dir,omitempty"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	ClearPassword bool   `json:"clear_password,omitempty"`
	HasPassword   bool   `json:"has_password"`
}

func remoteControlSettings(c RemoteControlConfig) *RemoteControlSettings {
	return &RemoteControlSettings{Enabled: c.Enabled, PluginsDir: c.PluginsDir, Host: c.Host, Port: c.Port, Username: c.Username, HasPassword: c.PasswordEncrypted != ""}
}
func validateRemoteControl(c RemoteControlConfig) error {
	if len(c.PasswordEncrypted) > 8192 || len(c.PluginsDir) > 1024 || len(c.Username) > 128 || strings.ContainsAny(c.Username, "\r\n\x00") {
		return bad("RemoteControl 설정 형식을 확인하세요.")
	}
	if c.PluginsDir != "" && (!filepath.IsAbs(c.PluginsDir) || strings.HasPrefix(c.PluginsDir, `\\`)) {
		return bad("MCP plugins 경로는 서버 PC의 로컬 절대경로여야 합니다.")
	}
	if !c.Enabled {
		return nil
	}
	ip := net.ParseIP(c.Host)
	if ip == nil || ip.To4() == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return bad("RemoteControl 대상은 신뢰하는 LAN의 IPv4 또는 127.0.0.1로 지정하세요.")
	}
	if c.Port < 1 || c.Port > 65535 || c.Username == "" || (c.PasswordEncrypted == "" && c.password == "") {
		return bad("RemoteControl MCP 포트와 Remote ID/PW를 입력하세요.")
	}
	return nil
}
func (a *App) candidateRemoteControl(c RemoteControlConfig, in RemoteControlSettings) (RemoteControlConfig, error) {
	c.Enabled = in.Enabled
	c.PluginsDir = strings.TrimSpace(in.PluginsDir)
	c.Host = strings.TrimSpace(in.Host)
	c.Port = in.Port
	c.Username = strings.TrimSpace(in.Username)
	if c.Port == 0 {
		c.Port = 5901
	}
	if len(in.Password) > 1024 || strings.ContainsAny(in.Password, "\r\n\x00") || (in.ClearPassword && in.Password != "") {
		return c, bad("RemoteControl 비밀번호 입력을 확인하세요.")
	}
	if in.ClearPassword {
		c.PasswordEncrypted = ""
		c.password = ""
	}
	if in.Password != "" {
		v, e := sealSetting(a.store, "remotecontrol-password", []byte(in.Password))
		if e != nil {
			return c, e
		}
		c.PasswordEncrypted = v
		c.password = in.Password
	}
	return c, validateRemoteControl(c)
}
func remoteControlAllowed(u User) bool {
	return !u.Disabled && (u.Role == roleAdmin || u.Role == roleSuperadmin)
}
