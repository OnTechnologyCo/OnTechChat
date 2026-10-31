package server

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// 화면용 구조에는 암호문·외부 파일 경로를 포함하지 않습니다. 빈 새 비밀번호는 기존 값을 유지합니다.
type MailSettings struct {
	SubjectFilter      string     `json:"subject_filter,omitempty"`
	Preset             string     `json:"preset,omitempty"`
	RecipientUsernames []string   `json:"recipient_usernames"`
	Enabled            bool       `json:"enabled"`
	Username           string     `json:"username"`
	Password           string     `json:"password"`
	ClearPassword      bool       `json:"clear_password"`
	Account            string     `json:"account"`
	Host               string     `json:"host"`
	Folder             string     `json:"folder"`
	OwnerUsername      string     `json:"owner_username"`
	PollSeconds        int        `json:"poll_seconds"`
	MaxAgeMinutes      int        `json:"max_age_minutes"`
	RequireDMARC       bool       `json:"require_dmarc"`
	TrustedAuthservIDs []string   `json:"trusted_authserv_ids"`
	Rules              []MailRule `json:"rules"`
}
type FCMSettings struct {
	Enabled            bool   `json:"enabled"`
	ProjectID          string `json:"project_id"`
	ServiceAccountJSON string `json:"service_account_json"`
	ClearKey           bool   `json:"clear_key"`
}
type EditableSettings struct {
	PublicURL            string                 `json:"public_url"`
	MaxFileBytes         int64                  `json:"max_file_bytes"`
	StorageQuotaBytes    int64                  `json:"storage_quota_bytes"`
	SessionHours         int                    `json:"session_hours"`
	AuthRetentionMinutes int                    `json:"auth_retention_minutes"`
	MaxMessages          int                    `json:"max_messages"`
	Mail                 MailSettings           `json:"mail"`
	FCM                  FCMSettings            `json:"fcm"`
	AI                   *AISettings            `json:"ai,omitempty"`
	Agent                *AgentSettings         `json:"agent,omitempty"`
	RemoteControl        *RemoteControlSettings `json:"remotecontrol,omitempty"`
}
type settingsRequest struct {
	Revision      string           `json:"revision"`
	AdminPassword string           `json:"admin_password"`
	Settings      EditableSettings `json:"settings"`
	Apply         bool             `json:"apply"`
}

func settingsEditable(c Config) EditableSettings {
	return EditableSettings{PublicURL: c.PublicURL, MaxFileBytes: c.MaxFileBytes, StorageQuotaBytes: c.StorageQuotaBytes, SessionHours: c.SessionHours, AuthRetentionMinutes: c.AuthRetentionMinutes, MaxMessages: c.MaxMessages,
		Mail:          MailSettings{SubjectFilter: mailSubjectFilter(c.Mail), RecipientUsernames: mailRecipients(c.Mail), Enabled: c.Mail.Enabled, Username: c.Mail.Username, Account: c.Mail.Account, Host: c.Mail.Host, Folder: c.Mail.Folder, OwnerUsername: c.Mail.OwnerUsername, PollSeconds: c.Mail.PollSeconds, MaxAgeMinutes: c.Mail.MaxAgeMinutes, RequireDMARC: c.Mail.RequireDMARC, TrustedAuthservIDs: c.Mail.TrustedAuthservIDs, Rules: c.Mail.Rules},
		FCM:           FCMSettings{Enabled: c.FCM.Enabled, ProjectID: c.FCM.ProjectID},
		RemoteControl: remoteControlSettings(c.RemoteControl),
		AI:            func() *AISettings { v := aiSettingsView(c.AI); return &v }()}
}

// 설정 저장은 현재 관리자 세션으로 승인합니다. 관리자 비밀번호를 재전송하지 않습니다.
// 기존 1.2 API의 admin_password 필드는 호환을 위해 디코딩만 하고 사용하지 않습니다.
func (a *App) requireSettingsAdmin(r *http.Request, u User) error {
	current, e := a.identify(r)
	if e != nil {
		return e
	}
	if current.ID != u.ID || !isSuperadmin(current) {
		return forbidden()
	}
	if !a.limiter.allow("settings-write:"+current.ID, 90, time.Minute) {
		return apiError{429, "설정 요청이 너무 많습니다. 잠시 후 다시 시도하세요."}
	}
	return nil
}
func (a *App) readSavedSettings() (Config, string, error) {
	if a.cfg.configPath == "" {
		return Config{}, "", apiError{503, "설정 파일 경로가 없는 테스트 서버입니다."}
	}
	b, e := boundedFile(a.cfg.configPath, 256<<10)
	if e != nil {
		return Config{}, "", e
	}
	c, e := decodeConfiguration(b, a.cfg.configPath)
	if e != nil {
		return c, "", e
	}
	return c, digest(string(b)), nil
}
func (a *App) settingsResult(c Config, revision string) map[string]any {
	active, _ := configBytes(a.cfg)
	saved, _ := configBytes(c)
	return map[string]any{"auto_https": c.AutoHTTPS, "https_status": a.autoHTTPSStatus(), "settings": settingsEditable(c), "revision": revision, "pending_apply": string(active) != string(saved), "applying": a.applying, "config_path": c.configPath, "listen": a.cfg.Listen, "data_dir": a.cfg.DataDir, "has_mail_password": c.Mail.PasswordEncrypted != "", "has_fcm_key": c.FCM.ServiceAccountEncrypted != "", "migration_notice": c.MigrationNotice, "active_mail_enabled": a.cfg.Mail.Enabled, "active_fcm_enabled": a.cfg.FCM.Enabled, "version": version, "mail_status": a.mailStatusView(), "network": networkSettingsInfo(c), "active_network": networkSettingsInfo(a.cfg)}
}
func (a *App) getSettings(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	c, revision, e := a.readSavedSettings()
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, a.settingsResult(c, revision))
}
func (a *App) candidateSettings(c Config, in EditableSettings) (Config, error) {
	c.PublicURL = strings.TrimSuffix(strings.TrimSpace(in.PublicURL), "/")
	c.MaxFileBytes = in.MaxFileBytes
	c.StorageQuotaBytes = in.StorageQuotaBytes
	c.SessionHours = in.SessionHours
	c.AuthRetentionMinutes = in.AuthRetentionMinutes
	c.MaxMessages = in.MaxMessages
	m := in.Mail
	if len(m.Password) > 1024 || strings.ContainsAny(m.Password, "\r\n\x00") {
		return c, bad("메일 비밀번호 형식을 확인하세요.")
	}
	c.Mail.Enabled = m.Enabled
	c.Mail.Preset = "custom"
	c.Mail.SubjectFilter = strings.TrimSpace(m.SubjectFilter)
	if c.Mail.SubjectFilter == "" && len(m.Rules) > 0 {
		c.Mail.SubjectFilter = mailSubjectFilter(MailConfig{Preset: m.Preset, Rules: m.Rules})
	}
	c.Mail.Rules = nil
	if e := validateMailSubjectFilters(c.Mail, c.Mail.Enabled); e != nil {
		return c, bad(e.Error())
	}
	c.Mail.SubjectFilter = strings.Join(mailSubjectFilters(c.Mail), "\n")
	c.Mail.RecipientUsernames = append([]string{}, m.RecipientUsernames...)
	c.Mail.Username = strings.TrimSpace(m.Username)
	if strings.TrimSpace(m.Account) != "" {
		c.Mail.Account = strings.TrimSpace(m.Account)
	}
	c.Mail.Host = strings.TrimSpace(m.Host)
	c.Mail.Folder = strings.TrimSpace(m.Folder)
	c.Mail.OwnerUsername = strings.TrimSpace(m.OwnerUsername)
	c.Mail.PollSeconds = m.PollSeconds
	c.Mail.MaxAgeMinutes = m.MaxAgeMinutes
	c.Mail.RequireDMARC = false
	c.Mail.TrustedAuthservIDs = nil
	// 사용자명/폴더 변경으로 예전 UID 이력이 재사용되지 않도록 안정적인 계정 식별자를 자동 파생합니다.
	if c.Mail.Username != "" && !strings.EqualFold(strings.TrimSpace(c.Mail.Username), strings.TrimSpace(a.cfg.Mail.Username)) {
		c.Mail.Account = "naver-" + digest(strings.ToLower(c.Mail.Username))[:16]
	}
	if m.Password != "" {
		v, e := sealSetting(a.store, "mail-password", []byte(m.Password))
		if e != nil {
			return c, e
		}
		c.Mail.PasswordEncrypted = v
	}
	f := in.FCM
	c.FCM.Enabled = f.Enabled
	c.FCM.ProjectID = strings.TrimSpace(f.ProjectID)
	if f.ClearKey && f.ServiceAccountJSON != "" {
		return c, bad("Firebase 키 삭제와 새 키 입력을 동시에 할 수 없습니다.")
	}
	if f.ClearKey {
		c.FCM.ServiceAccountEncrypted = ""
		c.FCM.credentialsJSON = nil
	}
	if f.ServiceAccountJSON != "" {
		if len(f.ServiceAccountJSON) > 64<<10 {
			return c, bad("Firebase JSON은 64KiB 이하여야 합니다.")
		}
		tmp := c.FCM
		tmp.credentialsJSON = []byte(f.ServiceAccountJSON)
		if _, e := newFCM(tmp); e != nil {
			return c, bad("Firebase 서비스 계정 JSON과 RSA 비밀키 형식을 확인하세요.")
		}
		v, e := sealSetting(a.store, "fcm-service-account", tmp.credentialsJSON)
		if e != nil {
			return c, e
		}
		c.FCM.ServiceAccountEncrypted = v
	}
	if in.RemoteControl != nil {
		var e error
		c.RemoteControl, e = a.candidateRemoteControl(c.RemoteControl, *in.RemoteControl)
		if e != nil {
			return c, e
		}
	}
	if in.AI != nil {
		var e error
		c.AI, e = a.candidateAISettings(c.AI, *in.AI)
		if e != nil {
			return c, e
		}
	}
	if in.Agent != nil {
		previousAgent := c.Agent
		var e error
		c.Agent, e = candidateAgentSettings(c.Agent, *in.Agent)
		if e != nil {
			return c, e
		}
		if strings.TrimSpace(previousAgent.WorkspaceRoot) != strings.TrimSpace(c.Agent.WorkspaceRoot) {
			blocked := false
			_ = a.store.view(func(st *State) error {
				blocked = len(st.AgentProjects) > 0
				if !blocked {
					for _, q := range st.AgentJobs {
						if q.Status == agentQueued || q.Status == agentPlanning || q.Status == agentRunning {
							blocked = true
							break
						}
					}
				}
				return nil
			})
			if blocked {
				return c, bad("프로젝트 또는 작업이 존재하는 동안 작업공간 루트를 변경할 수 없습니다. 기존 프로젝트 이행 정책을 먼저 처리하세요.")
			}
		}
	}
	c.Mail.LegacyINI = ""
	c.Mail.UsernameEnv = ""
	c.Mail.PasswordEnv = ""
	c.FCM.CredentialsFile = ""
	if e := c.validate(); e != nil {
		return c, bad("설정값을 확인하세요: " + e.Error())
	}
	if e := unlockConfiguration(&c, a.store); e != nil {
		return c, bad("저장된 비밀 설정을 복호화하지 못했습니다. 올바른 encryption.key를 복원하세요.")
	}
	if c.Mail.Enabled {
		if _, _, e := readMailCredentials(c.Mail); e != nil {
			return c, bad("메일을 활성화하려면 메일 ID와 비밀번호가 필요합니다.")
		}
		for _, name := range mailRecipients(c.Mail) {
			found := false
			_ = a.store.view(func(st *State) error { owner, ok := findUsername(st, name); found = ok && !owner.Disabled; return nil })
			if !found {
				return c, bad("메일 수신 사용자가 없거나 사용 중지 상태입니다: " + name)
			}
		}
	}
	if c.FCM.Enabled {
		if _, e := newFCM(c.FCM); e != nil {
			return c, bad("Firebase 키 설정이 올바르지 않습니다.")
		}
	}
	var used int64
	var count int
	_ = a.store.view(func(st *State) error {
		for _, f := range st.Files {
			used += f.Size
		}
		count = len(st.Messages)
		return nil
	})
	if c.StorageQuotaBytes < used {
		return c, bad("저장 용량 한도는 현재 사용량보다 작게 설정할 수 없습니다.")
	}
	if c.MaxMessages < count {
		return c, bad("메시지 한도는 현재 저장된 메시지 수보다 작게 설정할 수 없습니다.")
	}
	return c, nil
}
func (a *App) saveSettings(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in settingsRequest
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.applying {
		fail(w, apiError{409, "설정을 적용하는 중입니다. 재연결 후 다시 시도하세요."})
		return
	}
	old, revision, e := a.readSavedSettings()
	if e != nil {
		fail(w, e)
		return
	}
	if in.Revision != revision {
		fail(w, apiError{409, "다른 화면에서 저장한 설정이 있습니다. 다시 읽은 후 수정하세요."})
		return
	}
	c, e := a.candidateSettings(old, in.Settings)
	if e != nil {
		fail(w, e)
		return
	}
	revision, e = persistConfiguration(c, revision)
	if e != nil {
		fail(w, e)
		return
	}
	if in.Apply {
		a.applying = true
	}
	out := a.settingsResult(c, revision)
	out["ok"] = true
	out["apply_requested"] = in.Apply
	respond(w, 200, out)
	if in.Apply {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 저장 응답을 먼저 전달합니다. 서비스 프로세스 안에서 웹 서버/작업자만 안전하게 재시작합니다.
		select {
		case a.reload <- c:
		default:
		}
	}
}
func (a *App) importINISettings(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		Revision      string `json:"revision"`
		AdminPassword string `json:"admin_password"`
		Text          string `json:"ini_text"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	values, e := parseLegacyINI(in.Text)
	if e != nil {
		fail(w, e)
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.applying {
		fail(w, apiError{409, "설정 적용 중입니다."})
		return
	}
	c, revision, e := a.readSavedSettings()
	if e != nil {
		fail(w, e)
		return
	}
	if revision != in.Revision {
		fail(w, apiError{409, "설정을 다시 읽은 후 가져오세요."})
		return
	}
	edit := settingsEditable(c)
	edit.Mail.Enabled = false
	edit.Mail.Username = values.Username
	edit.Mail.Password = values.Password
	if values.Host != "" {
		edit.Mail.Host = values.Host
	}
	if values.Folder != "" {
		edit.Mail.Folder = values.Folder
	}
	if edit.Mail.Host != "imap.naver.com:993" {
		fail(w, bad("이 서버는 네이버 IMAP(imap.naver.com:993) 설정만 가져올 수 있습니다."))
		return
	}
	c, e = a.candidateSettings(c, edit)
	if e != nil {
		fail(w, e)
		return
	}
	c.MigrationNotice = "INI의 MailID·Password·imapServer·Mailfolder를 JSON으로 가져왔습니다. 규칙을 확인하고 메일 수집을 활성화한 뒤 저장 후 적용하세요."
	revision, e = persistConfiguration(c, revision)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, a.settingsResult(c, revision))
}
func (a *App) testMailSettings(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in settingsRequest
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	a.settingsMu.Lock()
	c, revision, e := a.readSavedSettings()
	a.settingsMu.Unlock()
	if e != nil {
		fail(w, e)
		return
	}
	if in.Revision != revision {
		fail(w, apiError{409, "설정을 다시 읽은 후 연결을 확인하세요."})
		return
	}
	// 연결 확인은 수집 활성화/발신자 규칙과 무관하게 ID·암호·메일함만 확인하며 저장하지 않습니다.
	c.Mail.Username = strings.TrimSpace(in.Settings.Mail.Username)
	c.Mail.Host = strings.TrimSpace(in.Settings.Mail.Host)
	c.Mail.Folder = strings.TrimSpace(in.Settings.Mail.Folder)
	if e = unlockConfiguration(&c, a.store); e != nil {
		fail(w, e)
		return
	}
	if in.Settings.Mail.Password != "" {
		c.Mail.password = in.Settings.Mail.Password
	}
	if in.Settings.Mail.ClearPassword {
		c.Mail.password = ""
	}
	if c.Mail.Host != "imap.naver.com:993" || len(c.Mail.password) > 1024 {
		fail(w, bad("네이버 IMAP 주소와 비밀번호를 확인하세요."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if e = testNaverConnection(ctx, c.Mail); e != nil {
		fail(w, bad(e.Error()))
		return
	}
	respond(w, 200, map[string]any{"ok": true, "saved": false, "message": "로그인과 메일함 읽기 권한을 확인했습니다. 본문 수집·읽음 변경·푸시 발송·설정 저장은 하지 않았습니다."})
}
