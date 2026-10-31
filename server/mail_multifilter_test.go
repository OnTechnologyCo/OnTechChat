package server

import (
	"context"
	"fmt"
	"mime"
	"strings"
	"testing"
)

func TestMailMultipleSubjectFiltersMatching(t *testing.T) {
	cfg := defaults().Mail
	cfg.SubjectFilter = " 로그인 코드 \r\n\r\n이메일 인증\r\n Login   CODE \r\nlogin code\n문구, [1]\n\u200b"
	if got := mailSubjectFilters(cfg); len(got) != 4 {
		t.Fatalf("expected 4 distinct nonempty phrases, got %q", got)
	}
	for _, tc := range []struct {
		name, subject, body string
		match               bool
	}{
		{"first", "서비스 로그인 코드: 123456", "본문", true},
		{"second", "이메일 인증 안내", "본문", true},
		{"encoded", mime.QEncoding.Encode("utf-8", "이메일 인증 안내"), "본문", true},
		{"case whitespace", "Your LOGIN\u00a0  CODE", "본문", true},
		{"overlap", "로그인 코드 이메일 인증", "본문", true},
		{"literal comma", "문구, [1] 안내", "본문", true},
		{"not regex", "문구, 1 안내", "본문", false},
		{"comma not delimiter", "문구만 있음", "본문", false},
		{"body only", "광고 안내", "로그인 코드 이메일 인증", false},
		{"no match", "다른 제목", "본문", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseAuthMail(forwardFixture(tc.subject, tc.body), cfg)
			if err != nil || (parsed != nil) != tc.match {
				t.Fatalf("match=%v parsed=%+v err=%v", tc.match, parsed, err)
			}
			if parsed != nil && parsed.Body != tc.body {
				t.Fatal("body was changed")
			}
		})
	}
	cfg.SubjectFilter = "\n \n\u200b\ufeff\n"
	if mailSubjectMatches("어떤 제목", cfg) {
		t.Fatal("blank lines matched all subjects")
	}
}

func TestMailMultipleSubjectFiltersSettingsRoundTrip(t *testing.T) {
	a, s, admin, _ := settingsTestApp(t)
	state, edit := getSettingMap(t, s, admin)
	edit.Mail.SubjectFilter = " 로그인 코드 \r\n\n이메일 인증\r\n로그인   코드\n"
	code, body := settingSave(t, s, admin, state["revision"].(string), edit, false)
	expectStatus(t, code, 200, body)
	loaded, err := loadConfig(a.cfg.configPath)
	if err != nil || loaded.Mail.SubjectFilter != "로그인 코드\n이메일 인증" {
		t.Fatalf("saved filters lost: %q %v", loaded.Mail.SubjectFilter, err)
	}
	state, edit = getSettingMap(t, s, admin)
	if edit.Mail.SubjectFilter != loaded.Mail.SubjectFilter || !mailSubjectMatches("이메일 인증 메일", loaded.Mail) {
		t.Fatal("settings read/reload lost the second phrase")
	}
	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, fmt.Sprintf("제목 %d", i))
	}
	for _, invalid := range []string{strings.Repeat("가", 201), "정상\n" + strings.Repeat("가", 201), strings.Join(many, "\n"), "정상\x00제목"} {
		edit.Mail.SubjectFilter = invalid
		code, body = settingSave(t, s, admin, state["revision"].(string), edit, false)
		expectStatus(t, code, 400, body)
	}
	// Disabled collection may clear the field. Old rules must not silently restore it.
	edit.Mail.Enabled = false
	edit.Mail.SubjectFilter = ""
	edit.Mail.Rules = nil
	code, body = settingSave(t, s, admin, state["revision"].(string), edit, false)
	expectStatus(t, code, 200, body)
	loaded, err = loadConfig(a.cfg.configPath)
	if err != nil || loaded.Mail.SubjectFilter != "" || len(mailSubjectFilters(loaded.Mail)) != 0 {
		t.Fatal("clearing filters restored an old rule", err)
	}
	valid := defaults()
	valid.Mail.SubjectFilter = strings.Join(many[:20], "\n")
	if err = valid.validate(); err != nil {
		t.Fatal("20 filters rejected", err)
	}
	valid.Mail.SubjectFilter = strings.Repeat("가", 200)
	if err = valid.validate(); err != nil {
		t.Fatal("200-character filter rejected", err)
	}
	valid.Mail.Enabled = true
	valid.Mail.SubjectFilter = "\u200b\n "
	if err = valid.validate(); err == nil {
		t.Fatal("enabled blank filter accepted")
	}
	// Existing single-subject config and legacy rule still match after upgrade.
	for _, cfg := range []MailConfig{{SubjectFilter: "이전 문구"}, {Rules: []MailRule{{CodePattern: "이전 문구"}}}} {
		if !mailSubjectMatches("이전 문구 안내", cfg) {
			t.Fatal("legacy single phrase lost")
		}
	}
}

func TestMailMultipleSubjectFiltersDeliveryOnce(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	cfg := a.cfg.Mail
	cfg.Username = "fixture@naver.com"
	cfg.password = "synthetic-password"
	cfg.SubjectFilter = "로그인 코드\n이메일 인증\n로그인"
	cfg.RecipientUsernames = []string{"admin", "bob"}
	a.cfg.Mail = cfg
	a.mailConnector = mockMailConnector(t, map[uint32][]byte{
		1: forwardFixture("로그인 코드 안내", "첫 번째 본문"),
		2: forwardFixture("이메일 인증 안내", "두 번째 본문"),
		3: forwardFixture("로그인 코드 이메일 인증", "중복 조건 본문"),
		4: forwardFixture("다른 제목", "로그인 코드 이메일 인증"),
	}, false)
	report, err := a.runMail(context.Background(), cfg, false, nil)
	if err != nil || !report.OK || report.Scanned != 4 || report.Matched != 3 || report.Delivered != 6 || report.Skipped != 1 {
		t.Fatalf("automatic OR delivery failed: %+v %v", report, err)
	}
	report, err = a.runMail(context.Background(), cfg, true, nil)
	if err != nil || !report.OK || report.Matched != 3 || report.Delivered != 0 || report.Duplicates != 6 {
		t.Fatalf("manual rescan duplicated matching mail: %+v %v", report, err)
	}
	a.store.view(func(st *State) error {
		if len(st.Messages) != 6 {
			t.Fatalf("expected one message per mail per recipient, got %d", len(st.Messages))
		}
		return nil
	})
}
