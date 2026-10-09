package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// 실제 메일 계정이 아닌 IMAP 프로토콜 fixture. EXAMINE/SEARCH/BODY.PEEK만 허용합니다.
func mockMailConnector(t *testing.T, mails map[uint32][]byte, fetchFailure bool) func(context.Context, MailConfig) (*imapClient, func(), error) {
	t.Helper()
	return func(ctx context.Context, cfg MailConfig) (*imapClient, func(), error) {
		client, server := net.Pipe()
		stop := context.AfterFunc(ctx, func() { client.Close(); server.Close() })
		go func() {
			defer server.Close()
			br := bufio.NewReader(server)
			for {
				line, e := br.ReadString('\n')
				if e != nil {
					return
				}
				fields := strings.SplitN(strings.TrimSpace(line), " ", 2)
				if len(fields) != 2 {
					return
				}
				tag, cmd := fields[0], fields[1]
				switch {
				case strings.HasPrefix(cmd, "EXAMINE "):
					// 네이버처럼 UIDVALIDITY가 tagged OK 완료 줄에만 오는 경우를 재현합니다.
					fmt.Fprintf(server, "%s OK [UIDVALIDITY 55] done\r\n", tag)
				case strings.HasPrefix(cmd, "UID SEARCH "):
					ids := []int{}
					for id := range mails {
						ids = append(ids, int(id))
					}
					sort.Ints(ids)
					var text strings.Builder
					for _, id := range ids {
						fmt.Fprintf(&text, " %d", id)
					}
					fmt.Fprintf(server, "* SEARCH%s\r\n%s OK done\r\n", text.String(), tag)
				case strings.HasPrefix(cmd, "UID FETCH ") && strings.Contains(cmd, "BODY.PEEK[]"):
					if fetchFailure {
						fmt.Fprintf(server, "%s NO synthetic failure\r\n", tag)
						continue
					}
					var id uint32
					fmt.Sscanf(cmd, "UID FETCH %d", &id)
					raw := mails[id]
					fmt.Fprintf(server, "* 1 FETCH (UID %d INTERNALDATE \"%s\" RFC822.SIZE %d BODY[]<0> {%d}\r\n", id, time.Now().Format("02-Jan-2006 15:04:05 -0700"), len(raw), len(raw))
					server.Write(raw)
					fmt.Fprintf(server, ")\r\n%s OK done\r\n", tag)
				default:
					t.Errorf("unexpected IMAP command: %s", cmd)
					return
				}
			}
		}()
		return &imapClient{conn: client, r: bufio.NewReader(client)}, func() { stop(); client.Close(); server.Close() }, nil
	}
}
func forwardFixture(subject, body string) []byte {
	return []byte("From: self@naver.com\r\nAuthentication-Results: arbitrary.test; dmarc=fail\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body)
}
func TestMailCollectionRescansSkippedUIDAndDeliversIsolatedMessages(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	cfg := a.cfg.Mail
	cfg.Username = "self@naver.com"
	cfg.password = "synthetic-mail-password"
	cfg.SubjectFilter = "테스트"
	cfg.RecipientUsernames = []string{"admin", "bob"}
	a.cfg.Mail = cfg
	a.cfg.FCM.Enabled = true
	key := cfg.Account + "\x1f" + cfg.Folder
	a.store.update(func(st *State) error {
		st.MailCursors[key] = MailCursor{Validity: 55, UID: 999}
		st.Devices["device-admin"] = Device{ID: "device-admin", UserID: admin.ID}
		st.Devices["device-bob"] = Device{ID: "device-bob", UserID: bob.ID}
		return nil
	})
	a.mailConnector = mockMailConnector(t, map[uint32][]byte{1: forwardFixture("테스트 제목 123456", "전달할 실제 내용 654321"), 2: forwardFixture("광고", "테스트 본문에만 있는 문구")}, false)
	report, e := a.runMail(context.Background(), cfg, true, nil)
	if e != nil || !report.OK || report.Scanned != 2 || report.Matched != 1 || report.Decoded != 1 || report.Delivered != 2 || report.Skipped != 1 {
		t.Fatalf("bad report: %+v %v", report, e)
	}
	a.store.view(func(st *State) error {
		if st.MailCursors[key].UID != 999 || len(st.Messages) != 2 || len(st.Outbox) != 2 {
			t.Fatal("cursor or delivery mismatch")
		}
		for _, m := range st.Messages {
			if m.RoomID == authRoomID(eve.ID) || m.Expires <= now() {
				t.Fatal("recipient isolation or expiry broken")
			}
		}
		return nil
	})
	raw, _ := json.Marshal(report)
	for _, secret := range []string{"123456", "654321", "전달할 실제 내용", "synthetic-mail-password", "self@naver.com"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("diagnostic leaked mail content", secret)
		}
	}
	token := loginTest(t, s, "bob")
	code, b, _ := request(t, s, token, "GET", "/api/rooms/"+authRoomID(bob.ID)+"/messages", nil)
	expectStatus(t, code, 200, b)
	if !bytes.Contains(b, []byte("123456")) || !bytes.Contains(b, []byte("654321")) {
		t.Fatal("mail subject/body not exposed in intended inbox")
	}
	report, e = a.runMail(context.Background(), cfg, true, nil)
	if e != nil || report.Delivered != 0 || report.Duplicates != 2 {
		t.Fatal("repeat created duplicate", report, e)
	}
	a.store.update(func(st *State) error { st.Messages = nil; return nil })
	report, e = a.runMail(context.Background(), cfg, true, nil)
	if e != nil || report.Delivered != 0 || report.Duplicates != 2 {
		t.Fatal("ledger did not protect expired/deleted mail", report, e)
	}
}
func TestMailCollectionFailuresAndAutomaticPath(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	cfg := a.cfg.Mail
	cfg.Username = "test"
	cfg.password = "test-secret"
	cfg.SubjectFilter = "테스트"
	cfg.RecipientUsernames = []string{"admin"}
	a.cfg.Mail = cfg
	a.mailConnector = mockMailConnector(t, map[uint32][]byte{1: forwardFixture("테스트", "내용")}, false)
	if e := a.pollMail(); e != nil {
		t.Fatal(e)
	}
	report := a.mailStatusView()["report"].(MailReport)
	if report.Delivered != 1 || report.Mode != "automatic" {
		t.Fatal(report)
	}
	if e := a.pollMail(); e != nil {
		t.Fatal(e)
	}
	if a.mailStatusView()["report"].(MailReport).Scanned != 0 {
		t.Fatal("old UID fetched again")
	}
	a.mailConnector = func(context.Context, MailConfig) (*imapClient, func(), error) {
		return nil, nil, errors.New("fixture login failed")
	}
	report, e := a.runMail(context.Background(), cfg, true, nil)
	if e == nil || report.Stage != "login" || report.OK {
		t.Fatal(report, e)
	}
	a.mailConnector = mockMailConnector(t, map[uint32][]byte{2: forwardFixture("테스트", "내용")}, true)
	report, e = a.runMail(context.Background(), cfg, false, nil)
	if e == nil || report.Stage != "fetch" {
		t.Fatal(report, e)
	}
	a.store.view(func(st *State) error {
		if st.MailCursors[cfg.Account+"\x1f"+cfg.Folder].UID != 1 {
			t.Fatal("failed FETCH advanced cursor")
		}
		return nil
	})
	a.mailRunMu.Lock()
	report, e = a.runMail(context.Background(), cfg, true, nil)
	a.mailRunMu.Unlock()
	if e == nil || report.Stage != "busy" {
		t.Fatal("concurrent collectors permitted")
	}
	a.mailConnector = mockMailConnector(t, map[uint32][]byte{3: []byte("Subject: 테스트\r\nContent-Type: text/plain; charset=x-unsupported\r\n\r\nbody")}, false)
	report, e = a.runMail(context.Background(), cfg, true, nil)
	if e != nil || report.Errors != 1 || report.Matched != 1 || report.OK {
		t.Fatal("body failure was silently ignored", report, e)
	}
}
func TestMailCollectionAPIUsesScreenInputAndRequiresAdminConsent(t *testing.T) {
	a, s, admin, bob := settingsTestApp(t)
	state, edit := getSettingMap(t, s, admin)
	edit.Mail.Username = "test@naver.com"
	edit.Mail.Password = "new-test-password"
	edit.Mail.SubjectFilter = "테스트"
	edit.Mail.RecipientUsernames = []string{"bob"}
	a.mailConnector = mockMailConnector(t, map[uint32][]byte{1: forwardFixture("테스트", "API 본문")}, false)
	before, _ := os.ReadFile(a.cfg.configPath)
	payload := map[string]any{"revision": state["revision"], "settings": edit, "deliver": true}
	code, b, _ := request(t, s, bob, "POST", "/api/settings/collect-mail", payload)
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, "", "POST", "/api/settings/collect-mail", payload)
	expectStatus(t, code, 401, b)
	payload["deliver"] = false
	code, b, _ = request(t, s, admin, "POST", "/api/settings/collect-mail", payload)
	expectStatus(t, code, 400, b)
	payload["deliver"] = true
	payload["revision"] = "stale"
	code, b, _ = request(t, s, admin, "POST", "/api/settings/collect-mail", payload)
	expectStatus(t, code, 409, b)
	payload["revision"] = state["revision"]
	code, b, _ = request(t, s, admin, "POST", "/api/settings/collect-mail", payload)
	expectStatus(t, code, 200, b)
	var report MailReport
	if e := json.Unmarshal(b, &report); e != nil || !report.OK || report.Delivered != 1 || report.AutomaticEnabled {
		t.Fatal("manual API result incorrect", string(b), e)
	}
	after, _ := os.ReadFile(a.cfg.configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("manual test unexpectedly saved configuration")
	}
	edit.Mail.RecipientUsernames = []string{}
	payload["settings"] = edit
	code, b, _ = request(t, s, admin, "POST", "/api/settings/collect-mail", payload)
	expectStatus(t, code, 400, b)
}
func TestMailLiteralFilterWhitespaceAndNoTruncation(t *testing.T) {
	cfg := defaults().Mail
	cfg.SubjectFilter = "테스트 문장 [1]"
	body := strings.Repeat("전체 본문 ", 1500)
	p, e := ParseAuthMail(forwardFixture("테스트\u00a0  문장 [1]", "본문"), cfg)
	if e != nil || p == nil {
		t.Fatal("Unicode whitespace mismatch", e)
	}
	if p, e = ParseAuthMail(forwardFixture("테스트 문장 1", "본문"), cfg); e != nil || p != nil {
		t.Fatal("literal filter interpreted as regex")
	}
	cfg.SubjectFilter = "테스트"
	p, e = ParseAuthMail(forwardFixture("테스트", body), cfg)
	if e != nil || p == nil || p.Body != strings.TrimSpace(body) {
		t.Fatal("body truncated", e)
	}
	cfg.SubjectFilter = ""
	cfg.Rules = nil
	p, e = ParseAuthMail(forwardFixture("테스트", body), cfg)
	if e != nil || p != nil {
		t.Fatal("empty filter matched all mail")
	}
}

func TestMultipartAlternativePrefersVisibleHTMLText(t *testing.T) {
	cfg := defaults().Mail
	cfg.SubjectFilter = "로그인 코드"
	raw := strings.Join([]string{
		"Subject: 로그인 코드: 561546",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=alt",
		"",
		"--alt",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"로그인 승인 코드입니다. 561546 계정 관리 [https://tracking.example/secret]",
		"--alt",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><head><style>.x{display:none}</style><script>hidden()</script></head><body><h2>로그인 코드</h2><p>로그인 승인 코드입니다.</p><strong>561546</strong><p>이 코드는 곧 만료됩니다.</p><a href=\"https://tracking.example/secret\">계정 관리</a></body></html>",
		"--alt--", "",
	}, "\r\n")
	p, e := ParseAuthMail([]byte(raw), cfg)
	if e != nil || p == nil {
		t.Fatal("multipart alternative parse failed", e)
	}
	if strings.Contains(p.Body, "https://") || strings.Contains(p.Body, "hidden()") || strings.Contains(p.Body, ".x{") {
		t.Fatal("HTML code or tracking URL leaked", p.Body)
	}
	if strings.Count(p.Body, "로그인 승인 코드입니다.") != 1 || !strings.Contains(p.Body, "561546") || !strings.Contains(p.Body, "계정 관리") {
		t.Fatal("visible HTML text missing or duplicated", p.Body)
	}
}
