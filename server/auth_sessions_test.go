package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func restartSessionApp(t *testing.T, a *App, s *httptest.Server) (*App, *httptest.Server) {
	t.Helper()
	cfg := a.cfg
	s.Close()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(next.Handler())
	next.cfg.Listen = strings.TrimPrefix(server.URL, "http://")
	t.Cleanup(func() { server.Close(); next.Close() })
	return next, server
}

func TestPersistentSessionRestartAndLogout(t *testing.T) {
	a, server, admin, _, _ := testApp(t)
	a.cfg.SessionHours = 1
	status, body, headers := request(t, server, "", "POST", "/api/login",
		map[string]string{"username": admin.Username, "password": testPassword, "client": "native"})
	expectStatus(t, status, 200, body)
	login := decodeMap(t, body)
	if login["expires_at"].(float64) != 0 {
		t.Fatal("login still expires")
	}
	token := login["token"].(string)
	cookies := (&http.Response{Header: headers}).Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != sessionCookieSeconds ||
		!cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatal("persistent cookie contract")
	}
	if err := a.store.update(func(st *State) error {
		session := st.Sessions[digest(token)]
		if !session.Persistent {
			t.Fatal("session not persistent")
		}
		session.Created = now() - 366*86400
		st.Sessions[digest(token)] = session
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.cleanup()
	a, server = restartSessionApp(t, a, server)
	status, body, _ = request(t, server, token, "GET", "/api/me", nil)
	expectStatus(t, status, 200, body)
	if decodeMap(t, body)["id"] != admin.ID {
		t.Fatal("identity changed on restart")
	}
	// 웹 쿠키도 동일한 저장 세션을 사용하며 접속 시 보관 기간을 갱신합니다.
	req, _ := http.NewRequest("GET", server.URL+"/api/me", nil)
	req.AddCookie(cookies[0])
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || len(response.Cookies()) != 1 {
		t.Fatal("browser session lost on restart")
	}
	status, body, _ = request(t, server, token, "POST", "/api/password",
		map[string]string{"current_password": "wrong", "new_password": "new-valid-password"})
	expectStatus(t, status, 401, body)
	if decodeMap(t, body)["code"] == "SESSION_INVALID" {
		t.Fatal("wrong password revoked session")
	}
	status, body, _ = request(t, server, token, "GET", "/api/me", nil)
	expectStatus(t, status, 200, body)
	status, body, _ = request(t, server, token, "POST", "/api/logout", map[string]string{})
	expectStatus(t, status, 200, body)
	a, server = restartSessionApp(t, a, server)
	status, body, _ = request(t, server, token, "GET", "/api/me", nil)
	expectStatus(t, status, 401, body)
	if decodeMap(t, body)["code"] != "SESSION_INVALID" {
		t.Fatal("missing confirmed invalidation code")
	}
}

func TestPersistentSessionLegacyMigration(t *testing.T) {
	a, server, admin, bob, _ := testApp(t)
	valid, expired, disabled := randomToken(), randomToken(), randomToken()
	if err := a.store.update(func(st *State) error {
		st.Sessions[digest(valid)] = Session{UserID: admin.ID, Created: now() - 10, Expires: now() + 3600}
		st.Sessions[digest(expired)] = Session{UserID: admin.ID, Created: now() - 7200, Expires: now() - 1}
		user := st.Users[bob.ID]
		user.Disabled = true
		st.Users[bob.ID] = user
		st.Sessions[digest(disabled)] = Session{UserID: bob.ID, Expires: now() + 3600}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a, server = restartSessionApp(t, a, server)
	status, body, _ := request(t, server, valid, "GET", "/api/me", nil)
	expectStatus(t, status, 200, body)
	if err := a.store.view(func(st *State) error {
		if session := st.Sessions[digest(valid)]; !session.Persistent || session.Expires != 0 {
			t.Fatal("valid legacy session not migrated")
		}
		if st.Sessions[digest(expired)].Persistent || st.Sessions[digest(disabled)].Persistent {
			t.Fatal("revoked or expired session revived")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{expired, disabled} {
		status, body, _ = request(t, server, token, "GET", "/api/me", nil)
		expectStatus(t, status, 401, body)
	}
	a.cleanup()
	status, body, _ = request(t, server, valid, "GET", "/api/me", nil)
	expectStatus(t, status, 200, body)
}

func TestPersistentSessionOtherDeviceLoginAndAccountRevocation(t *testing.T) {
	a, server, _, bob, _ := testApp(t)
	token := loginTest(t, server, bob.Username)
	// 새 기기 로그인으로 기존 8번째 로그인 세션을 강제로 없애지 않습니다.
	for i := 0; i < 8; i++ {
		loginTest(t, server, bob.Username)
	}
	status, body, _ := request(t, server, token, "GET", "/api/me", nil)
	expectStatus(t, status, 200, body)
	if err := a.store.update(func(st *State) error {
		user := st.Users[bob.ID]
		user.Disabled = true
		st.Users[bob.ID] = user
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, body, _ = request(t, server, token, "GET", "/api/me", nil)
	expectStatus(t, status, 401, body)
	if decodeMap(t, body)["code"] != "SESSION_INVALID" {
		t.Fatal("account disable was not enforced")
	}
}
