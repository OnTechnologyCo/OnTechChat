package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSettingsSaveRequiresSessionNotPassword(t *testing.T) {
	a, s, admin, bob := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.AuthRetentionMinutes = 12
	code, b, _ := request(t, s, admin, "POST", "/api/settings", map[string]any{"revision": state["revision"], "settings": c})
	expectStatus(t, code, 200, b)
	state, c = getSettingMap(t, s, admin)
	for _, path := range []string{"/api/settings", "/api/settings/discover-mail", "/api/settings/import-ini", "/api/settings/test-mail"} {
		code, b, _ = request(t, s, bob, "POST", path, map[string]any{})
		expectStatus(t, code, 403, b)
		code, b, _ = request(t, s, "", "POST", path, map[string]any{})
		expectStatus(t, code, 401, b)
	}
	for _, cross := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"revision": state["revision"], "settings": c})
		r, _ := http.NewRequest("POST", s.URL+"/api/settings", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+admin)
		r.Header.Set("Content-Type", "application/json")
		if cross {
			r.Header.Set("X-OnTech-Request", "1")
			r.Header.Set("Origin", "https://other.example")
		}
		res, e := s.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		expectStatus(t, res.StatusCode, 403, data)
	}
	_ = a.store.update(func(st *State) error { delete(st.Sessions, digest(admin)); return nil })
	code, b, _ = request(t, s, admin, "POST", "/api/settings", map[string]any{"revision": state["revision"], "settings": c})
	expectStatus(t, code, 401, b)
}
func TestSettingOperationRechecksCurrentRole(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	var stale User
	_ = a.store.view(func(st *State) error { stale, _ = findUsername(st, "admin"); return nil })
	_ = a.store.update(func(st *State) error { u := st.Users[stale.ID]; u.Role = "user"; st.Users[u.ID] = u; return nil })
	r := httptest.NewRequest("POST", s.URL+"/api/settings", nil)
	r.Header.Set("Authorization", "Bearer "+admin)
	if e := a.requireSettingsAdmin(r, stale); e == nil {
		t.Fatal("stale administrator accepted")
	}
}
func TestDiscoverySessionPasswordReuseNoMutation(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.Mail.Username = "fake@naver.com"
	c.Mail.Password = "not-a-real-secret-731!"
	c.Mail.Enabled = false
	code, b := settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	state, c = getSettingMap(t, s, admin)
	before, _ := os.ReadFile(a.cfg.configPath)
	calls := 0
	a.mailInspector = func(ctx context.Context, cfg MailConfig, scan bool) (MailDiscovery, error) {
		calls++
		if cfg.password != "not-a-real-secret-731!" || cfg.Username != "fake@naver.com" || !scan {
			t.Fatal("saved credentials not reused")
		}
		return MailDiscovery{Folders: []string{"INBOX", "인증함"}, TrustedAuthservIDs: []string{"mx.naver.com"}, MatchedHeaders: 1, CheckedHeaders: 2, Message: "synthetic inspection"}, nil
	}
	code, b, _ = request(t, s, admin, "POST", "/api/settings/discover-mail", map[string]any{"revision": state["revision"], "settings": c, "scan_rules": true})
	expectStatus(t, code, 200, b)
	after, _ := os.ReadFile(a.cfg.configPath)
	if calls != 1 || !bytes.Equal(before, after) {
		t.Fatal("discovery wrote configuration")
	}
	if bytes.Contains(b, []byte("not-a-real-secret")) || bytes.Contains(b, []byte("password_encrypted")) {
		t.Fatal("discovery secret leakage")
	}
	if decodeMap(t, b)["saved"] != false {
		t.Fatal("incorrect saved state")
	}
	c.Mail.Host = "attacker.invalid:993"
	code, b, _ = request(t, s, admin, "POST", "/api/settings/discover-mail", map[string]any{"revision": state["revision"], "settings": c, "scan_rules": true})
	expectStatus(t, code, 400, b)
	if calls != 1 {
		t.Fatal("arbitrary host inspected")
	}
}
func TestMailLegacyFilterAndNoSenderEnforcement(t *testing.T) {
	c := defaults()
	if c.Mail.RequireDMARC || len(c.Mail.Rules[0].Senders) != 0 {
		t.Fatal("removed restrictions remain")
	}
	if mailSubjectFilter(MailConfig{Rules: []MailRule{{CodePattern: "테스트"}}}) != "테스트" {
		t.Fatal("legacy filter lost")
	}
	a, s, admin, _ := settingsTestApp(t)
	state, edit := getSettingMap(t, s, admin)
	edit.Mail.SubjectFilter = "테스트"
	edit.Mail.Preset = "riot"
	edit.Mail.RequireDMARC = true
	edit.Mail.Rules = []MailRule{{Service: "ignored", Senders: []string{"other@example.test"}, CodePattern: "(bad)"}}
	code, b := settingSave(t, s, admin, state["revision"].(string), edit, false)
	expectStatus(t, code, 200, b)
	loaded, e := loadConfig(a.cfg.configPath)
	if e != nil || loaded.Mail.SubjectFilter != "테스트" || loaded.Mail.RequireDMARC || len(loaded.Mail.Rules) != 0 {
		t.Fatal("legacy restrictions restored", e)
	}
}
func TestMailTitleOnlyWithUntrustedHeaders(t *testing.T) {
	c := defaults().Mail
	for _, header := range []string{"", "From: nobody@example.test\r\n", "From: self@naver.com\r\nAuthentication-Results: naver.com; dmarc=fail\r\n"} {
		p, e := ParseAuthMail([]byte(header+"Subject: 로그인 코드 TEST\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n내용 ABC 123456"), c)
		if e != nil || p == nil || p.Subject != "로그인 코드 TEST" || p.Body != "내용 ABC 123456" {
			t.Fatal("title-only forwarding rejected", p, e)
		}
	}
}
func TestMailboxDiscoveryUTF7AndLiteral(t *testing.T) {
	for _, name := range []string{"INBOX", "인증 메일", "Riot & Netflix", "📩 코드"} {
		decoded, e := decodeModifiedUTF7(modifiedUTF7(name))
		if e != nil || decoded != name {
			t.Fatal("UTF7 roundtrip", name, e)
		}
	}
	r := imapResult{lines: []string{`* LIST (\HasNoChildren) "/" "INBOX"`, `* LIST () "/" "` + modifiedUTF7("인증 메일") + `"`, `* LIST (\Noselect) "/" "Hidden"`, `* LIST () NIL {4}`}, literals: [][]byte{[]byte("test")}}
	folders := listMailboxNames(r)
	if len(folders) != 3 || strings.Contains(strings.Join(folders, ","), "Hidden") {
		t.Fatal(folders)
	}
}
func TestIMAPDiscoveryOnlyConnectsAndListsFolders(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		br := bufio.NewReader(server)
		for _, want := range []string{`LIST "" "*"`, `EXAMINE "INBOX"`} {
			line, e := br.ReadString('\n')
			if e != nil {
				done <- e
				return
			}
			f := strings.SplitN(strings.TrimSpace(line), " ", 2)
			if len(f) != 2 || f[1] != want {
				done <- fmt.Errorf("unexpected command %q", line)
				return
			}
			fmt.Fprintf(server, "* LIST () \"/\" \"INBOX\"\r\n%s OK done\r\n", f[0])
		}
		done <- nil
	}()
	report, e := inspectMailClient(&imapClient{conn: client, r: bufio.NewReader(client)}, defaults().Mail, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if len(report.TrustedAuthservIDs) != 0 || report.CheckedHeaders != 0 || report.MatchedHeaders != 0 {
		t.Fatal("obsolete sender scan ran", report)
	}
}
func TestSelectedAuthenticationRecipientsIsolationAndDedup(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	a.cfg.Mail.RecipientUsernames = []string{"admin", "bob"}
	key := "synthetic-mail"
	parsed := &ParsedAuthMail{Service: "Riot Games", Subject: "로그인 코드: 012345", Body: "테스트 본문"}
	if e := a.consumeMail(key, 1, 7, parsed, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := a.consumeMail(key, 1, 7, parsed, time.Now()); e != nil {
		t.Fatal(e)
	}
	_ = a.store.view(func(st *State) error {
		counts := map[string]int{}
		for _, m := range st.Messages {
			counts[m.RoomID]++
		}
		if counts[authRoomID(admin.ID)] != 1 || counts[authRoomID(bob.ID)] != 1 || counts[authRoomID(eve.ID)] != 0 {
			t.Fatal(counts)
		}
		return nil
	})
	et := loginTest(t, s, "eve")
	code, b, _ := request(t, s, et, "GET", "/api/rooms/"+authRoomID(bob.ID)+"/messages", nil)
	if code != 403 && code != 404 {
		t.Fatal(code, string(b))
	}
	_ = a.store.update(func(st *State) error { u := st.Users[bob.ID]; u.Disabled = true; st.Users[u.ID] = u; return nil })
	if e := a.consumeMail(key, 1, 8, parsed, time.Now()); e != nil {
		t.Fatal(e)
	}
	_ = a.store.view(func(st *State) error {
		n := 0
		for _, m := range st.Messages {
			if m.RoomID == authRoomID(bob.ID) {
				n++
			}
		}
		if n != 1 {
			t.Fatal("disabled user received another code")
		}
		return nil
	})
}
func TestInvalidRecipientRejectedAndOldOwnerFallback(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, c := getSettingMap(t, s, admin)
	c.Mail.Username = "fake"
	c.Mail.Password = "synthetic-password"
	c.Mail.TrustedAuthservIDs = []string{"naver.com"}
	c.Mail.Enabled = true
	c.Mail.RecipientUsernames = []string{"bob", "missing"}
	code, b := settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 400, b)
	for _, names := range [][]string{{"bob", "bob"}, {"bad user"}} {
		c.Mail.RecipientUsernames = names
		code, b = settingSave(t, s, admin, state["revision"].(string), c, false)
		expectStatus(t, code, 400, b)
	}
	c.Mail.RecipientUsernames = nil
	c.Mail.Enabled = false
	code, b = settingSave(t, s, admin, state["revision"].(string), c, false)
	expectStatus(t, code, 200, b)
	cfg, e := loadConfig(a.cfg.configPath)
	if e != nil || len(mailRecipients(cfg.Mail)) != 0 {
		t.Fatal("deselected recipients were silently restored", e)
	}
}
func TestNetworkGuideLiteralIPAndSafeTemplate(t *testing.T) {
	c := defaults()
	c.PublicURL = "https://chat.example.com"
	info := networkSettingsInfo(c)
	if info["tested_from_internet"] != false || !strings.Contains(info["caddyfile"].(string), "flush_interval -1") {
		t.Fatal(info)
	}
	c.PublicURL = "https://203.0.113.12:8443"
	if networkSettingsInfo(c)["caddyfile"] != "" {
		t.Fatal("IP automatic certificate example generated")
	}
	for _, host := range []string{"evil{.example.com", "bad..example.com", "-invalid.example.com", "a.example.com\n{"} {
		if validDNSName(host) {
			t.Fatal("unsafe hostname")
		}
	}
	c.PublicURL = ""
	if networkSettingsInfo(c)["configured"] != false {
		t.Fatal("local only marked publicly configured")
	}
}
