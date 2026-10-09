package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"
)

type MailDiscovery struct {
	Folders            []string `json:"folders"`
	TrustedAuthservIDs []string `json:"trusted_authserv_ids"`
	MatchedHeaders     int      `json:"matched_headers"`
	CheckedHeaders     int      `json:"checked_headers"`
	Message            string   `json:"message"`
	Saved              bool     `json:"saved"`
}

// 메일 원문/제목/인증번호/비밀번호는 이 응답에 절대 포함하지 않습니다.
func (a *App) discoverMailSettings(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		Revision  string           `json:"revision"`
		Settings  EditableSettings `json:"settings"`
		ScanRules bool             `json:"scan_rules"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	if !a.limiter.allow("mail-setup:"+u.ID, 6, time.Minute) {
		fail(w, apiError{429, "메일 자동 확인은 1분에 6번까지 가능합니다."})
		return
	}
	a.settingsMu.Lock()
	c, revision, e := a.readSavedSettings()
	a.settingsMu.Unlock()
	if e != nil {
		fail(w, e)
		return
	}
	if revision != in.Revision {
		fail(w, apiError{409, "설정을 다시 읽은 뒤 자동 확인하세요."})
		return
	}
	if e = unlockConfiguration(&c, a.store); e != nil {
		fail(w, e)
		return
	}
	m := in.Settings.Mail
	c.Mail.Username = strings.TrimSpace(m.Username)
	c.Mail.Folder = strings.TrimSpace(m.Folder)
	c.Mail.Host = m.Host
	if m.Password != "" {
		c.Mail.password = m.Password
	}
	if c.Mail.Host != "imap.naver.com:993" || c.Mail.Folder == "" || len(c.Mail.Folder) > 500 || len(c.Mail.Username) > 320 || len(c.Mail.password) > 1024 || strings.ContainsAny(c.Mail.Username+c.Mail.password+c.Mail.Folder, "\r\n\x00") {
		fail(w, bad("네이버 ID·앱 비밀번호·메일 폴더를 확인하세요."))
		return
	}
	if _, _, e = readMailCredentials(c.Mail); e != nil {
		fail(w, bad("최초 한 번은 네이버 ID와 앱 비밀번호가 필요합니다."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	inspect := a.mailInspector
	if inspect == nil {
		inspect = inspectNaverMailbox
	}
	report, e := inspect(ctx, c.Mail, in.ScanRules)
	if e != nil {
		fail(w, bad(e.Error()))
		return
	}
	report.Saved = false
	respond(w, 200, report)
}
func connectNaver(ctx context.Context, cfg MailConfig) (*imapClient, func(), error) {
	if cfg.Host != "imap.naver.com:993" {
		return nil, nil, errors.New("네이버 IMAP만 허용합니다.")
	}
	u, p, e := readMailCredentials(cfg)
	if e != nil {
		return nil, nil, errors.New("메일 ID와 앱 비밀번호를 입력하세요.")
	}
	user, e := imapQuote(u)
	if e != nil {
		return nil, nil, e
	}
	password, e := imapQuote(p)
	if e != nil {
		return nil, nil, e
	}
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{ServerName: "imap.naver.com", MinVersion: tls.VersionTLS12}}
	conn, e := d.DialContext(ctx, "tcp", cfg.Host)
	if e != nil {
		return nil, nil, errors.New("네이버 TLS 연결 실패: 인터넷 연결과 IMAP 사용 설정을 확인하세요.")
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	closeConn := func() { stop(); conn.Close() }
	c := &imapClient{conn: conn, r: bufio.NewReaderSize(conn, 32<<10)}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	line, e := c.line()
	if e != nil || !strings.HasPrefix(strings.ToUpper(line), "* OK") {
		closeConn()
		return nil, nil, errors.New("네이버 메일 서버 응답 오류입니다.")
	}
	if _, e = c.command("LOGIN " + user + " " + password); e != nil {
		closeConn()
		return nil, nil, errors.New("네이버 로그인 실패: 2단계 인증·앱 비밀번호·IMAP 사용 설정을 확인하세요.")
	}
	return c, closeConn, nil
}
func inspectNaverMailbox(ctx context.Context, cfg MailConfig, scan bool) (MailDiscovery, error) {
	c, closeConn, e := connectNaver(ctx, cfg)
	if e != nil {
		return MailDiscovery{}, e
	}
	defer closeConn()
	return inspectMailClient(c, cfg, scan)
}

// 별도 테스트에서 가상 IMAP 서버로 명령을 검증합니다. STORE/DELETE/EXPUNGE는 사용하지 않습니다.
func inspectMailClient(c *imapClient, cfg MailConfig, scan bool) (MailDiscovery, error) {
	out := MailDiscovery{Folders: []string{}, TrustedAuthservIDs: []string{}}
	listed, e := c.command(`LIST "" "*"`)
	if e != nil {
		return out, errors.New("메일함 목록 조회 실패입니다.")
	}
	out.Folders = listMailboxNames(listed)
	folder, e := imapQuote(modifiedUTF7(cfg.Folder))
	if e != nil {
		return out, e
	}
	_, e = c.command("EXAMINE " + folder)
	if e != nil {
		return out, errors.New("선택한 메일 폴더에 접근할 수 없습니다. INBOX 또는 정확한 폴더를 선택하세요.")
	}
	out.Message = "로그인과 메일함 목록을 확인했습니다. 본문 수집·읽음 변경·설정 저장은 하지 않았습니다."
	if !scan {
		return out, nil
	}
	return out, nil
}

// LIST의 인용부호 및 literal 이름을 지원합니다. 디코딩한 이름을 파일 경로로 사용하지 않습니다.
func listMailboxNames(r imapResult) []string {
	out := []string{}
	literal := 0
	for _, line := range r.lines {
		if !strings.HasPrefix(strings.ToUpper(line), "* LIST ") {
			continue
		}
		rest := strings.TrimSpace(line[7:])
		end := strings.Index(rest, ")")
		if len(rest) == 0 || rest[0] != '(' || end < 0 {
			continue
		}
		noselect := strings.Contains(strings.ToLower(rest[:end]), "\\noselect")
		rest = strings.TrimSpace(rest[end+1:])
		_, rest, ok := imapValue(rest)
		if !ok {
			continue
		}
		name := ""
		if literalSize.MatchString(rest) {
			if literal < len(r.literals) {
				name = string(r.literals[literal])
				literal++
			}
		} else {
			name, _, ok = imapValue(rest)
			if !ok {
				continue
			}
		}
		if noselect {
			continue
		}
		name, e := decodeModifiedUTF7(name)
		if e == nil && name != "" && len(name) <= 500 && !strings.ContainsAny(name, "\r\n\x00") {
			out = append(out, name)
		}
		if len(out) >= 100 {
			break
		}
	}
	return sortedUnique(out)
}
func imapValue(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	if s[0] != '"' {
		i := strings.IndexByte(s, ' ')
		if i < 0 {
			return s, "", true
		}
		return s[:i], strings.TrimSpace(s[i+1:]), true
	}
	var out strings.Builder
	for i := 1; i < len(s); i++ {
		if s[i] == '"' {
			return out.String(), strings.TrimSpace(s[i+1:]), true
		}
		if s[i] == '\\' {
			i++
			if i >= len(s) {
				break
			}
		}
		out.WriteByte(s[i])
	}
	return "", "", false
}
func decodeModifiedUTF7(s string) (string, error) {
	var out strings.Builder
	for len(s) > 0 {
		at := strings.IndexByte(s, '&')
		if at < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:at])
		s = s[at+1:]
		end := strings.IndexByte(s, '-')
		if end < 0 {
			return "", errors.New("invalid UTF7")
		}
		if end == 0 {
			out.WriteByte('&')
		} else {
			b, e := base64.RawStdEncoding.DecodeString(strings.ReplaceAll(s[:end], ",", "/"))
			if e != nil || len(b)%2 != 0 {
				return "", errors.New("invalid UTF7")
			}
			units := make([]uint16, len(b)/2)
			for i := range units {
				units[i] = binary.BigEndian.Uint16(b[i*2:])
			}
			out.WriteString(string(utf16.Decode(units)))
		}
		s = s[end+1:]
	}
	return out.String(), nil
}
