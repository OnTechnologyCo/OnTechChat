package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewEightCharacterPasswordPolicy(t *testing.T) {
	cases := []struct {
		name, p string
		ok      bool
	}{
		{"seven ASCII", "Ab12345", false}, {"eight ASCII", "Ab123456", true}, {"numeric eight", "12345678", true},
		{"seven Korean", "가나다라마바사", false}, {"eight Korean", "가나다라마바사아", true},
		{"seven emoji", strings.Repeat("😀", 7), false}, {"eight emoji", strings.Repeat("😀", 8), true},
		{"ASCII limit", strings.Repeat("x", 128), true}, {"over limit", strings.Repeat("x", 129), false},
		{"UTF8 over bytes", strings.Repeat("가", 43), false}, {"invalid UTF8", string([]byte{255, 255, 255, 255, 255, 255, 255, 255}), false},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			if validNewPassword(v.p) != v.ok {
				t.Fatalf("policy mismatch")
			}
		})
	}
	// 기존에 허용되던 12바이트/4자 한글 암호도 로그인 검증은 유지합니다.
	salt := []byte("1234567890abcdef")
	p := "가나다라"
	old := fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordRounds, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derivePassword([]byte(p), salt, passwordRounds)))
	if !passwordMatches(p, old) {
		t.Fatal("existing password invalidated")
	}
}
func TestEightCharacterUserCreationResetAndLogin(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	admin := loginTest(t, s, "admin")
	bob := loginTest(t, s, "bob")
	status, b, _ := request(t, s, admin, "POST", "/api/users", map[string]string{"username": "member8", "password": "Ab123456"})
	expectStatus(t, status, 201, b)
	id := decodeMap(t, b)["id"].(string)
	status, b, _ = request(t, s, "", "POST", "/api/login", map[string]string{"username": "member8", "password": "Ab123456", "client": "native"})
	expectStatus(t, status, 200, b)
	oldToken := decodeMap(t, b)["token"].(string)
	status, b, _ = request(t, s, admin, "POST", "/api/users", map[string]string{"username": "sevenpw", "password": "Ab12345"})
	expectStatus(t, status, 400, b)
	status, b, _ = request(t, s, admin, "POST", "/api/users", map[string]string{"username": "member8", "password": "Ab123456"})
	expectStatus(t, status, 409, b)
	status, b, _ = request(t, s, bob, "POST", "/api/users", map[string]string{"username": "forbidden8", "password": "Ab123456"})
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, "", "POST", "/api/users", map[string]string{"username": "anonymous8", "password": "Ab123456"})
	expectStatus(t, status, 401, b)
	status, b, _ = request(t, s, admin, "PATCH", "/api/users/"+id, map[string]string{"password": "Reset888"})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, oldToken, "GET", "/api/me", nil)
	expectStatus(t, status, 401, b)
	status, b, _ = request(t, s, "", "POST", "/api/login", map[string]string{"username": "member8", "password": "Reset888", "client": "native"})
	expectStatus(t, status, 200, b)
	token := decodeMap(t, b)["token"].(string)
	status, b, _ = request(t, s, token, "POST", "/api/password", map[string]string{"current_password": "Reset888", "new_password": "Changed8"})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, "", "POST", "/api/login", map[string]string{"username": "member8", "password": "Changed8", "client": "native"})
	expectStatus(t, status, 200, b)
}
func TestDefaultHTTPSPortHostAndOrigin(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	_ = s
	a.cfg.PublicURL = "https://chat.example.com:443"
	cases := []struct {
		host, origin string
		want         int
	}{
		{"chat.example.com", "https://chat.example.com", 200},
		{"CHAT.example.com:443", "https://chat.example.com", 200},
		{"chat.example.com", "https://CHAT.example.com:443", 200},
		{"chat.example.com:444", "", 403},
		{"chat.example.com.evil.invalid", "", 403},
		{"chat.example.com", "http://chat.example.com", 403},
		{"chat.example.com", "https://evil.invalid", 403},
		{"chat.example.com", "https://chat.example.com/", 403},
	}
	for _, v := range cases {
		r := httptest.NewRequest("GET", "http://"+v.host+"/healthz", nil)
		r.Host = v.host
		if v.origin != "" {
			r.Header.Set("Origin", v.origin)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != v.want {
			t.Fatalf("host %q origin %q: %d want %d", v.host, v.origin, w.Code, v.want)
		}
		if v.want == 200 && !strings.Contains(w.Body.String(), `"service":"ontech-chat"`) {
			t.Fatal("server marker missing")
		}
	}
	for _, host := range []string{"chat.example.com/", "chat.example.com@evil.invalid", "chat.example.com:65536", "chat.example.com:0", "chat.example.com#x"} {
		if sameAuthority("https", host, "chat.example.com") {
			t.Fatal("bad authority allowed")
		}
	}
	if !sameAuthority("https", "[2001:db8::1]:443", "[2001:db8:0:0:0:0:0:1]") {
		t.Fatal("IPv6 canonical authority mismatch")
	}
}
func TestUserChangeRequiresRequestHeader(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	token := loginTest(t, s, "admin")
	r := httptest.NewRequest(http.MethodPost, "http://"+strings.TrimPrefix(s.URL, "http://")+"/api/users", strings.NewReader(`{"username":"blocked","password":"Ab123456"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
