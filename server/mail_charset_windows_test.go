package server

import (
	"encoding/base64"
	"testing"
)

func TestMailKoreanCharsetsHeaderAndBody(t *testing.T) {
	cfg := defaults().Mail
	cfg.SubjectFilter = "테스트"
	// Python/CP949로 생성한 고정 fixture: 테스트 본문 123456
	content := []byte{0xc5, 0xd7, 0xbd, 0xba, 0xc6, 0xae, 0x20, 0xba, 0xbb, 0xb9, 0xae, 0x20, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36}
	for _, charset := range []string{"euc-kr", "ks_c_5601-1987", "cp949", "windows-949"} {
		b64 := base64.StdEncoding.EncodeToString(content)
		raw := []byte("Subject: =?" + charset + "?B?" + b64 + "?=\r\nContent-Type: text/plain; charset=" + charset + "\r\nContent-Transfer-Encoding: base64\r\n\r\n" + b64)
		p, e := ParseAuthMail(raw, cfg)
		if e != nil || p == nil || p.Subject != "테스트 본문 123456" || p.Body != "테스트 본문 123456" {
			t.Fatal(charset, p, e)
		}
	}
}
