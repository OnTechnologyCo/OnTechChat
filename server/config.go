package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const version = "1.15.31"

type Config struct {
	SchemaVersion   int    `json:"schema_version"`
	MigrationNotice string `json:"migration_notice,omitempty"`
	configPath      string

	Listen               string              `json:"listen"`
	PublicURL            string              `json:"public_url"`
	DataDir              string              `json:"data_dir"`
	TLSCert              string              `json:"tls_cert"`
	TLSKey               string              `json:"tls_key"`
	MaxFileBytes         int64               `json:"max_file_bytes"`
	StorageQuotaBytes    int64               `json:"storage_quota_bytes"`
	SessionHours         int                 `json:"session_hours"`
	AuthRetentionMinutes int                 `json:"auth_retention_minutes"`
	MaxMessages          int                 `json:"max_messages"`
	Mail                 MailConfig          `json:"mail"`
	FCM                  FCMConfig           `json:"fcm"`
	AI                   AIConfig            `json:"ai,omitempty"`
	Agent                AgentConfig         `json:"agent,omitempty"`
	RemoteControl        RemoteControlConfig `json:"remotecontrol,omitempty"`
}
type MailConfig struct {
	SubjectFilter      string   `json:"subject_filter,omitempty"`
	Preset             string   `json:"preset,omitempty"`
	RecipientUsernames []string `json:"recipient_usernames"`
	Username           string   `json:"username"`
	PasswordEncrypted  string   `json:"password_encrypted,omitempty"`
	password           string

	Enabled            bool       `json:"enabled"`
	Account            string     `json:"account"`
	Host               string     `json:"host"`
	Folder             string     `json:"folder"`
	UsernameEnv        string     `json:"username_env,omitempty"`
	PasswordEnv        string     `json:"password_env,omitempty"`
	LegacyINI          string     `json:"legacy_ini,omitempty"`
	OwnerUsername      string     `json:"owner_username"`
	PollSeconds        int        `json:"poll_seconds"`
	MaxAgeMinutes      int        `json:"max_age_minutes"`
	RequireDMARC       bool       `json:"require_dmarc"`
	TrustedAuthservIDs []string   `json:"trusted_authserv_ids"`
	Rules              []MailRule `json:"rules"`
}
type MailRule struct {
	Service     string   `json:"service"`
	Senders     []string `json:"senders"`
	CodePattern string   `json:"code_pattern"`
}
type FCMConfig struct {
	ServiceAccountEncrypted string `json:"service_account_encrypted,omitempty"`
	credentialsJSON         []byte

	Enabled         bool   `json:"enabled"`
	ProjectID       string `json:"project_id"`
	CredentialsFile string `json:"credentials_file,omitempty"`
}

// 완성형 한글(가~힣)과 기존 ASCII 아이디를 같은 정책으로 검사합니다.
// 바이트 수가 아니라 글자 수로 3~40자를 제한하고, 공백/제어문자/경로 구분자는 허용하지 않습니다.
const usernamePolicyMessage = "아이디는 한글·영문·숫자·_.- 3~40자로 입력하세요. 공백과 다른 기호는 사용할 수 없습니다."

var usernamePattern = regexp.MustCompile(`^[a-z0-9가-힣_.-]{3,40}$`)
var clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,100}$`)
var fileIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func defaults() Config {
	return Config{SchemaVersion: 2, Listen: "127.0.0.1:8787", DataDir: "private", MaxFileBytes: 25 << 20, StorageQuotaBytes: 5 << 30, SessionHours: 168, AuthRetentionMinutes: 15, MaxMessages: 20000, AI: defaultAIConfig(), Agent: defaultAgentConfig(),
		Mail: MailConfig{Preset: "custom", Account: "naver-main", Host: "imap.naver.com:993", Folder: "INBOX", OwnerUsername: "admin", PollSeconds: 15, MaxAgeMinutes: 15, RequireDMARC: false, TrustedAuthservIDs: []string{}, Rules: riotMailRules()}}
}
func loadConfig(path string) (Config, error) {
	raw, e := boundedFile(path, 256<<10)
	if e != nil {
		return Config{}, e
	}
	return decodeConfiguration(raw, path)
}
func decodeConfiguration(raw []byte, path string) (Config, error) {
	c := defaults()
	c.Mail.Preset = "" // Existing 1.2 JSON has no preset: preserve its custom rules.
	if len(raw) > 256<<10 {
		return c, errors.New("config size limit")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("config must contain exactly one JSON object")
	}
	base, e := filepath.Abs(filepath.Dir(path))
	if e != nil {
		return c, e
	}
	for _, p := range []*string{&c.DataDir, &c.TLSCert, &c.TLSKey, &c.Mail.LegacyINI, &c.FCM.CredentialsFile} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
	c.configPath, e = filepath.Abs(path)
	if e != nil {
		return c, e
	}
	return c, c.validate()
}
func (c Config) validate() error {
	if e := validateRemoteControl(c.RemoteControl); e != nil {
		return e
	}
	if c.Mail.Preset != "" && c.Mail.Preset != "custom" && c.Mail.Preset != "riot" {
		return errors.New("unknown mail preset")
	}
	if len(c.Mail.RecipientUsernames) > 50 {
		return errors.New("at most 50 authentication recipients")
	}
	seenRecipients := map[string]bool{}
	for _, name := range c.Mail.RecipientUsernames {
		if !usernamePattern.MatchString(name) || seenRecipients[name] {
			return errors.New("invalid or duplicate authentication recipient")
		}
		seenRecipients[name] = true
	}

	if c.SchemaVersion != 2 {
		return errors.New("unsupported config schema")
	}
	if len(c.Mail.Username) > 320 || strings.ContainsAny(c.Mail.Username, "\r\n\x00") || len(c.Mail.Folder) > 500 || strings.ContainsAny(c.Mail.Folder, "\r\n\x00") || len(c.Mail.Account) > 80 {
		return errors.New("invalid mail account fields")
	}
	if len(c.Mail.PasswordEncrypted) > 8192 || len(c.FCM.ServiceAccountEncrypted) > 128<<10 {
		return errors.New("encrypted settings too large")
	}

	host, port, e := net.SplitHostPort(c.Listen)
	if e != nil || port == "" {
		return errors.New("listen must contain IP:port")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("listen must be an IP literal")
	}
	if !ip.IsLoopback() && (c.TLSCert == "" || c.TLSKey == "" || c.PublicURL == "") {
		return errors.New("non-loopback binding requires TLS cert/key and public_url")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("TLS cert/key must be configured together")
	}
	if c.PublicURL != "" {
		u, e := url.Parse(c.PublicURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("public_url must be an HTTPS origin without a path")
		}
	}
	if c.DataDir == "" || c.MaxFileBytes < 1024 || c.MaxFileBytes > 100<<20 || c.StorageQuotaBytes < c.MaxFileBytes || c.SessionHours < 1 || c.SessionHours > 720 || c.AuthRetentionMinutes < 1 || c.AuthRetentionMinutes > 60 || c.MaxMessages < 100 || c.MaxMessages > 100000 {
		return errors.New("invalid server limits")
	}
	if c.Mail.PollSeconds < 5 || c.Mail.PollSeconds > 3600 || c.Mail.MaxAgeMinutes < 1 || c.Mail.MaxAgeMinutes > 60 {
		return errors.New("invalid mail timing")
	}
	if c.Mail.Enabled {
		if c.Mail.Host != "imap.naver.com:993" || c.Mail.Account == "" || c.Mail.Folder == "" {
			return errors.New("invalid Naver mail configuration")
		}
		if mailSubjectFilter(c.Mail) == "" || len([]rune(mailSubjectFilter(c.Mail))) > 200 || strings.ContainsAny(mailSubjectFilter(c.Mail), "\r\n\x00") {
			return errors.New("제목 필터 문장을 1~200자로 입력하세요.")
		}
		if len(mailRecipients(c.Mail)) == 0 {
			return errors.New("메일을 받을 사용자를 한 명 이상 선택하세요.")
		}
	}
	if c.FCM.Enabled && (c.FCM.ProjectID == "" || (c.FCM.CredentialsFile == "" && c.FCM.ServiceAccountEncrypted == "")) {
		return errors.New("FCM project and service account required")
	}
	if e := validateAIConfig(c.AI); e != nil {
		return e
	}
	if e := validateAgentConfig(c.Agent); e != nil {
		return e
	}
	return nil
}
