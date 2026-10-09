package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"time"
)

func testNaverConnection(ctx context.Context, cfg MailConfig) error {
	if cfg.Host != "imap.naver.com:993" {
		return errors.New("네이버 IMAP 주소만 허용합니다.")
	}
	u, p, e := readMailCredentials(cfg)
	if e != nil {
		return errors.New("메일 ID와 비밀번호를 입력하세요.")
	}
	user, e := imapQuote(u)
	if e != nil {
		return errors.New("메일 ID 형식 오류입니다.")
	}
	password, e := imapQuote(p)
	if e != nil {
		return errors.New("메일 비밀번호 형식 오류입니다.")
	}
	folder, e := imapQuote(modifiedUTF7(cfg.Folder))
	if e != nil || cfg.Folder == "" {
		return errors.New("메일함 이름을 확인하세요.")
	}
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{ServerName: "imap.naver.com", MinVersion: tls.VersionTLS12}}
	conn, e := d.DialContext(ctx, "tcp", cfg.Host)
	if e != nil {
		return errors.New("네이버 TLS 연결 실패: 네트워크와 IMAP 설정을 확인하세요.")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c := &imapClient{conn: conn, r: bufio.NewReaderSize(conn, 32<<10)}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	line, e := c.line()
	if e != nil || !strings.HasPrefix(strings.ToUpper(line), "* OK") {
		return errors.New("네이버 메일 서버 응답을 확인하지 못했습니다.")
	}
	if _, e = c.command("LOGIN " + user + " " + password); e != nil {
		return errors.New("네이버 로그인 실패: ID·앱 비밀번호·IMAP 사용 설정을 확인하세요.")
	}
	if _, e = c.command("EXAMINE " + folder); e != nil {
		return errors.New("메일함 접근 실패: 메일 폴더 이름을 확인하세요.")
	}
	// EXAMINE 이후 BODY/STORE/SEARCH 명령을 전송하지 않습니다.
	return nil
}
