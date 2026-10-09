package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settingsTestApp(t *testing.T) (*App, *httptest.Server, string, string) {
	t.Helper()
	a, s, _, _, _ := testApp(t)
	a.cfg.configPath = filepath.Join(a.cfg.DataDir, "config.json")
	b, e := configBytes(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(a.cfg.configPath, b, 0600); e != nil {
		t.Fatal(e)
	}
	return a, s, loginTest(t, s, "admin"), loginTest(t, s, "bob")
}
func getSettingMap(t *testing.T, s *httptest.Server, token string) (map[string]any, EditableSettings) {
	t.Helper()
	code, b, _ := request(t, s, token, "GET", "/api/settings", nil)
	expectStatus(t, code, 200, b)
	raw := decodeMap(t, b)
	var v struct {
		Settings EditableSettings `json:"settings"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return raw, v.Settings
}
func settingSave(t *testing.T, s *httptest.Server, token, revision string, c EditableSettings, apply bool) (int, []byte) {
	t.Helper()
	code, b, _ := request(t, s, token, "POST", "/api/settings", settingsRequest{Revision: revision, Settings: c, Apply: apply})
	return code, b
}
func TestSettingsAdminAuthorizationAndCSRF(t *testing.T) {
	_, s, admin, bob := settingsTestApp(t)
	for _, path := range []string{"/api/settings", "/api/settings/import-ini", "/api/settings/test-mail"} {
		method := "POST"
		if path == "/api/settings" {
			method = "GET"
		}
		code, b, _ := request(t, s, "", method, path, nil)
		expectStatus(t, code, 401, b)
		code, b, _ = request(t, s, bob, method, path, map[string]string{})
		expectStatus(t, code, 403, b)
	}
	state, c := getSettingMap(t, s, admin)
	code, b, _ := request(t, s, admin, "POST", "/api/settings", settingsRequest{Revision: state["revision"].(string), Settings: c})
	expectStatus(t, code, 200, b)
}
func TestSettingsPasswordEncryptionAndBlankPreserve(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.Mail.Username = "synthetic@naver.com"
	c.Mail.Password = "Synthetic-mail-only-229!"
	c.Mail.Folder = "인증메일"
	c.Mail.Enabled = false
	code, b := settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	if bytes.Contains(b, []byte(c.Mail.Password)) || bytes.Contains(b, []byte("password_encrypted")) {
		t.Fatal("secret in save response")
	}
	raw, e := os.ReadFile(a.cfg.configPath)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(c.Mail.Password)) {
		t.Fatal("plaintext password on disk")
	}
	cfg, e := loadConfig(a.cfg.configPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = unlockConfiguration(&cfg, a.store); e != nil {
		t.Fatal(e)
	}
	if cfg.Mail.password != c.Mail.Password || cfg.Mail.Folder != "인증메일" || cfg.Mail.Username != c.Mail.Username {
		t.Fatal("JSON round trip failed")
	}
	encrypted := cfg.Mail.PasswordEncrypted
	state, c = getSettingMap(t, s, admin)
	if c.Mail.Password != "" || state["has_mail_password"] != true || state["pending_apply"] != true {
		t.Fatal("bad safe view")
	}
	c.Mail.Folder = "INBOX"
	code, b = settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	cfg, _ = loadConfig(a.cfg.configPath)
	if cfg.Mail.PasswordEncrypted != encrypted {
		t.Fatal("blank password replaced existing password")
	}
	if a.cfg.Mail.Folder == cfg.Mail.Folder && a.cfg.Mail.Username == cfg.Mail.Username {
		t.Fatal("save mutated live configuration")
	}
	if _, e = os.Stat(a.cfg.configPath + ".bak"); e != nil {
		t.Fatal("prior config backup missing")
	}
}
func TestSettingsConflictDoesNotOverwrite(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	rev := state["revision"].(string)
	c.Mail.Folder = "Folder A"
	code, b := settingSave(t, s, admin, rev, c, false)
	expectStatus(t, code, 200, b)
	before, _ := os.ReadFile(a.cfg.configPath)
	c.Mail.Folder = "Folder B"
	code, b = settingSave(t, s, admin, rev, c, false)
	expectStatus(t, code, 409, b)
	after, _ := os.ReadFile(a.cfg.configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("stale save overwrote newer configuration")
	}
}
func TestSettingsInvalidEnabledMailAndLimits(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, base := getSettingMap(t, s, admin)
	before, _ := os.ReadFile(a.cfg.configPath)
	cases := []struct {
		name   string
		change func(*EditableSettings)
	}{
		{"mail_without_credentials", func(c *EditableSettings) { c.Mail.Enabled = true }},
		{"invalid_quota", func(c *EditableSettings) { c.StorageQuotaBytes = 1 }},
		{"invalid_poll", func(c *EditableSettings) { c.Mail.PollSeconds = 1 }},
		{"non_https", func(c *EditableSettings) { c.PublicURL = "http://example.invalid" }},
		{"secret_control_character", func(c *EditableSettings) { c.Mail.Password = "x\ny" }},
		{"FCM_without_key", func(c *EditableSettings) { c.FCM.Enabled = true; c.FCM.ProjectID = "synthetic-test" }},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			c := base
			v.change(&c)
			code, b := settingSave(t, s, admin, state["revision"].(string), c, false)
			expectStatus(t, code, 400, b)
		})
	}
	after, _ := os.ReadFile(a.cfg.configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid request changed disk")
	}
}
func TestSettingsINIImportFourFieldsAndNoEcho(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, _ := getSettingMap(t, s, admin)
	ini := "[COMMON]\r\nMailID=sample@naver.com\r\nPassword=test-only=password!\r\nimapServer=imap.naver.com:993\r\nMailfolder=인증수신\r\n"
	code, b, _ := request(t, s, admin, "POST", "/api/settings/import-ini", map[string]string{"revision": state["revision"].(string), "admin_password": testPassword, "ini_text": ini})
	expectStatus(t, code, 200, b)
	if bytes.Contains(b, []byte("test-only=password!")) {
		t.Fatal("INI password echoed")
	}
	c, e := loadConfig(a.cfg.configPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = unlockConfiguration(&c, a.store); e != nil {
		t.Fatal(e)
	}
	if c.Mail.Username != "sample@naver.com" || c.Mail.password != "test-only=password!" || c.Mail.Host != "imap.naver.com:993" || c.Mail.Folder != "인증수신" || c.Mail.Enabled {
		t.Fatal("incomplete INI import")
	}
	raw, _ := os.ReadFile(a.cfg.configPath)
	for _, term := range []string{"legacy_ini", "username_env", "password_env", "test-only=password!"} {
		if bytes.Contains(raw, []byte(term)) {
			t.Fatal("legacy/plain field persisted", term)
		}
	}
}
func TestSettingsINIRejectsDuplicatesAndUnsafeValues(t *testing.T) {
	for _, in := range []string{"[COMMON]\nMailID=x\nPassword=y\nPassword=z", "[OTHER]\nMailID=x\nPassword=y", "[COMMON]\nMailID=x\nPassword=x\x00y", strings.Repeat("a", 65537)} {
		if _, e := parseLegacyINI(in); e == nil {
			t.Fatal("invalid INI accepted")
		}
	}
}
func TestLegacyConfigAutomaticMigrationOnce(t *testing.T) {
	base := t.TempDir()
	c := defaults()
	c.DataDir = filepath.Join(base, "private")
	c.configPath = filepath.Join(base, "config.json")
	c.Mail.LegacyINI = filepath.Join(base, "Setting.ini")
	c.Mail.UsernameEnv = "SYNTHETIC_ABSENT_USER"
	c.Mail.PasswordEnv = "SYNTHETIC_ABSENT_PASSWORD"
	if e := os.WriteFile(c.Mail.LegacyINI, []byte("[COMMON]\nMailID=old@naver.com\nPassword=old-only-test\nimapServer=imap.naver.com:993\nMailfolder=기존폴더"), 0600); e != nil {
		t.Fatal(e)
	}
	b, _ := configBytes(c)
	os.WriteFile(c.configPath, b, 0600)
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	if a.cfg.Mail.Username != "old@naver.com" || a.cfg.Mail.Folder != "기존폴더" || a.cfg.Mail.password != "old-only-test" {
		t.Fatal("migration failed")
	}
	a.Close()
	if e = os.Remove(c.Mail.LegacyINI); e != nil {
		t.Fatal(e)
	}
	loaded, e := loadConfig(c.configPath)
	if e != nil {
		t.Fatal(e)
	}
	if loaded.Mail.LegacyINI != "" || loaded.Mail.UsernameEnv != "" || loaded.Mail.PasswordEnv != "" {
		t.Fatal("runtime still references legacy inputs")
	}
	a, e = newApp(loaded)
	if e != nil {
		t.Fatal("server still requires deleted INI", e)
	}
	defer a.Close()
	if a.cfg.Mail.password != "old-only-test" {
		t.Fatal("password did not survive restart")
	}
}
func syntheticServiceKey(t *testing.T) string {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	b, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	out, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": "synthetic-project", "client_email": "synthetic@example.invalid", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))})
	return string(out)
}
func TestSettingsFCMKeyEncryptedAndPreserved(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	key := syntheticServiceKey(t)
	c.FCM.ProjectID = "synthetic-project"
	c.FCM.ServiceAccountJSON = key
	code, b := settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	if bytes.Contains(b, []byte("PRIVATE KEY")) {
		t.Fatal("key in API response")
	}
	raw, _ := os.ReadFile(a.cfg.configPath)
	if bytes.Contains(raw, []byte("PRIVATE KEY")) || bytes.Contains(raw, []byte("synthetic@example.invalid")) {
		t.Fatal("plaintext key persisted")
	}
	loaded, _ := loadConfig(a.cfg.configPath)
	if e := unlockConfiguration(&loaded, a.store); e != nil {
		t.Fatal(e)
	}
	if string(loaded.FCM.credentialsJSON) != key {
		t.Fatal("FCM key roundtrip")
	}
	state, c = getSettingMap(t, s, admin)
	if state["has_fcm_key"] != true || c.FCM.ServiceAccountJSON != "" {
		t.Fatal("FCM safe read failed")
	}
	c.FCM.ProjectID = "second-synthetic-project"
	code, b = settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	next, _ := loadConfig(a.cfg.configPath)
	if next.FCM.ServiceAccountEncrypted != loaded.FCM.ServiceAccountEncrypted {
		t.Fatal("blank key changed key")
	}
}
func TestSettingsLegacyMailClearFlagIsIgnored(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.Mail.Password = "synthetic-password"
	c.Mail.Username = "synthetic@naver.com"
	code, b := settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	state, c = getSettingMap(t, s, admin)
	c.Mail.ClearPassword = true
	code, b = settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	cfg, _ := loadConfig(a.cfg.configPath)
	if cfg.Mail.PasswordEncrypted == "" {
		t.Fatal("obsolete clear flag removed a saved password")
	}
}
func TestSettingsApplyQueuedOnlyAfterSave(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.AuthRetentionMinutes = 12
	code, b := settingSave(t, s, admin, state["revision"].(string), c, true)
	expectStatus(t, code, 200, b)
	select {
	case next := <-a.reload:
		if next.AuthRetentionMinutes != 12 {
			t.Fatal("wrong config queued")
		}
	default:
		t.Fatal("apply not queued")
	}
	disk, _ := loadConfig(a.cfg.configPath)
	if disk.AuthRetentionMinutes != 12 || !a.applying {
		t.Fatal("apply before disk persisted")
	}
	state, c = getSettingMap(t, s, admin)
	code, b = settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 409, b)
}
func TestSettingsCipherWrongPurposeAndKeyFail(t *testing.T) {
	a, _, _, _ := settingsTestApp(t)
	value, e := sealSetting(a.store, "mail-password", []byte("synthetic-secret"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = openSetting(a.store, "fcm-service-account", value); e == nil {
		t.Fatal("AAD substitution accepted")
	}
	other, e := openStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if _, e = openSetting(other, "mail-password", value); e == nil {
		t.Fatal("wrong key accepted")
	}
}
func TestSettingsAtomicWriteRejectsSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target.json")
	link := filepath.Join(base, "config.json")
	os.WriteFile(target, []byte("original"), 0600)
	if e := os.Symlink(target, link); e != nil {
		t.Skip("symlinks unavailable")
	}
	if e := atomicConfigFile(link, []byte("changed")); e == nil {
		t.Fatal("symlink write accepted")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "original" {
		t.Fatal("target modified")
	}
}
func TestSettingsMailTestRejectsOtherHostWithoutNetwork(t *testing.T) {
	_, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.Mail.Host = "127.0.0.1:443"
	code, b, _ := request(t, s, admin, "POST", "/api/settings/test-mail", settingsRequest{Revision: state["revision"].(string), Settings: c})
	expectStatus(t, code, 400, b)
}

func TestSettingsRejectsMissingCSRFHeader(t *testing.T) {
	_, s, admin, _ := settingsTestApp(t)
	r, e := http.NewRequest("POST", s.URL+"/api/settings", strings.NewReader(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer "+admin)
	r.Header.Set("Content-Type", "application/json")
	resp, e := s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("missing request header accepted")
	}
}
