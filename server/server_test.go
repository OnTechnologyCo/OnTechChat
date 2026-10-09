package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testPassword = "Test-only-password-937!"

func testApp(t *testing.T) (*App, *httptest.Server, User, User, User) {
	t.Helper()
	c := defaults()
	c.DataDir = t.TempDir()
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := passwordHash(testPassword)
	if e != nil {
		t.Fatal(e)
	}
	users := []User{{ID: randomID(), Username: "admin", Role: roleSuperadmin, Hash: hash}, {ID: randomID(), Username: "bob", Role: roleUser, Hash: hash}, {ID: randomID(), Username: "eve", Role: roleUser, Hash: hash}}
	if e = a.store.update(func(st *State) error {
		for _, u := range users {
			addUser(st, u)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(a.Handler())
	a.cfg.Listen = strings.TrimPrefix(server.URL, "http://")
	t.Cleanup(func() { a.cancel(); server.Close(); a.Close() })
	return a, server, users[0], users[1], users[2]
}
func request(t *testing.T, server *httptest.Server, token, method, path string, body any) (int, []byte, http.Header) {
	t.Helper()
	var data io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		data = bytes.NewReader(b)
	}
	r, e := http.NewRequest(method, server.URL+path, data)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-OnTech-Request", "1")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	b, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, b, response.Header
}
func decodeMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.Unmarshal(b, &m); e != nil {
		t.Fatal(e, string(b))
	}
	return m
}
func loginTest(t *testing.T, s *httptest.Server, name string) string {
	t.Helper()
	status, b, _ := request(t, s, "", "POST", "/api/login", map[string]string{"username": name, "password": testPassword, "client": "native"})
	if status != 200 {
		t.Fatalf("login: %d %s", status, b)
	}
	return decodeMap(t, b)["token"].(string)
}
func expectStatus(t *testing.T, got, want int, b []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status %d want %d: %s", got, want, b)
	}
}
func TestPBKDF2Vectors(t *testing.T) {
	vectors := []struct {
		rounds int
		want   string
	}{{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"}, {2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"}, {4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"}}
	for _, v := range vectors {
		if got := hex.EncodeToString(derivePassword([]byte("password"), []byte("salt"), v.rounds)); got != v.want {
			t.Fatal(got)
		}
	}
}
func TestPasswordPolicyAndHash(t *testing.T) {
	if _, e := passwordHash("short"); e == nil {
		t.Fatal("weak password allowed")
	}
	h, e := passwordHash(testPassword)
	if e != nil || !passwordMatches(testPassword, h) || passwordMatches("wrong-password", h) {
		t.Fatal("password verification failed")
	}
	if passwordMatches(testPassword, "broken") {
		t.Fatal("malformed hash allowed")
	}
}
func TestEncryptedStoreTransactionsAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = openStore(dir); e == nil {
		t.Fatal("second writer was allowed")
	}
	u := User{ID: "owner", Username: "owner", Hash: "sensitive-marker", Role: "admin"}
	if e = s.update(func(st *State) error { addUser(st, u); return nil }); e != nil {
		t.Fatal(e)
	}
	revision := s.data.Revision
	e = s.update(func(st *State) error { st.Users["bad"] = User{ID: "bad"}; return errors.New("rollback") })
	if e == nil || s.data.Revision != revision {
		t.Fatal("transaction rollback failed")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "store.db"))
	if bytes.Contains(raw, []byte("sensitive-marker")) {
		t.Fatal("plaintext store")
	}
	s.Close()
	s, e = openStore(dir)
	if e != nil || s.data.Users["owner"].Username != "owner" {
		t.Fatal("persistence failed", e)
	}
	s.Close()
	os.Remove(filepath.Join(dir, "store.db.bak"))
	if e = os.Rename(filepath.Join(dir, "store.db"), filepath.Join(dir, "store.db.bak")); e != nil {
		t.Fatal(e)
	}
	s, e = openStore(dir)
	if e != nil || !s.recovered {
		t.Fatal("backup recovery failed", e)
	}
	s.Close()
	os.Rename(filepath.Join(dir, "encryption.key"), filepath.Join(dir, "lost.key"))
	if _, e = openStore(dir); e == nil {
		t.Fatal("missing encryption key was silently replaced")
	}
}
func TestStoreTamperAndConcurrency(t *testing.T) {
	dir := t.TempDir()
	s, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if e := s.update(func(st *State) error { st.MailCursors[fmt.Sprint(n)] = MailCursor{UID: uint32(n)}; return nil }); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if len(s.data.MailCursors) != 25 {
		t.Fatal("lost updates")
	}
	s.Close()
	p := filepath.Join(dir, "store.db")
	raw, _ := os.ReadFile(p)
	raw[len(raw)-1] ^= 1
	os.WriteFile(p, raw, 0600)
	if _, e = openStore(dir); e == nil {
		t.Fatal("tampering not detected")
	}
}
func TestConfigurationFailClosed(t *testing.T) {
	c := defaults()
	if e := c.validate(); e != nil {
		t.Fatal(e)
	}
	c.Listen = "0.0.0.0:8787"
	if c.validate() == nil {
		t.Fatal("plaintext public listener allowed")
	}
	c = defaults()
	c.Mail.Enabled = true
	if c.validate() == nil {
		t.Fatal("unconfigured sender validation allowed")
	}
	c = defaults()
	c.PublicURL = "https://example.test/path"
	if c.validate() == nil {
		t.Fatal("non-origin public URL allowed")
	}
}
func TestHTTPAuthAndCSRF(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	status, b, _ := request(t, s, "", "GET", "/api/me", nil)
	expectStatus(t, status, 401, b)
	status, b, _ = request(t, s, "", "POST", "/api/login", map[string]string{"username": "admin", "password": "wrongpassword"})
	expectStatus(t, status, 401, b)
	r, _ := http.NewRequest("POST", s.URL+"/api/login", strings.NewReader(`{"username":"admin","password":"`+testPassword+`"}`))
	r.Header.Set("Content-Type", "application/json")
	res, e := s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("missing CSRF request header accepted")
	}
	r.Header.Set("X-OnTech-Request", "1")
	r.Header.Set("Origin", "https://evil.example")
	r.Body, _ = r.GetBody()
	res, e = s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("cross-origin accepted")
	}
	r, _ = http.NewRequest("GET", s.URL+"/healthz", nil)
	r.Host = "evil.example"
	res, e = s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
	status, b, h := request(t, s, "", "POST", "/api/login", map[string]string{"username": "admin", "password": testPassword})
	expectStatus(t, status, 200, b)
	if strings.Contains(string(b), "password_hash") || strings.Contains(string(b), `"token"`) {
		t.Fatal("browser login leaked secret")
	}
	cookie := h.Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatal("cookie protections missing")
	}
	token := loginTest(t, s, "admin")
	status, b, _ = request(t, s, token, "POST", "/api/logout", map[string]any{})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, token, "GET", "/api/me", nil)
	expectStatus(t, status, 401, b)
	status, b, _ = request(t, s, "", "GET", "/private/encryption.key", nil)
	expectStatus(t, status, 404, b)
}
func createTestRoom(t *testing.T, s *httptest.Server, token string) string {
	status, b, _ := request(t, s, token, "POST", "/api/rooms", map[string]any{"name": "테스트 채팅", "members": []string{"bob"}})
	expectStatus(t, status, 201, b)
	return decodeMap(t, b)["id"].(string)
}
func TestHTTPChatPermissionsIdempotencyAndReads(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	at, bt, et := loginTest(t, s, "admin"), loginTest(t, s, "bob"), loginTest(t, s, "eve")
	room := createTestRoom(t, s, at)
	status, b, _ := request(t, s, et, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, bt, "POST", "/api/users", map[string]string{"username": "unauthorized", "password": testPassword})
	expectStatus(t, status, 403, b)
	payload := map[string]string{"text": "<script>alert('xss')</script> 한글 대화", "client_id": "message-test-001"}
	status, b, _ = request(t, s, at, "POST", "/api/rooms/"+room+"/messages", payload)
	expectStatus(t, status, 201, b)
	id := int64(decodeMap(t, b)["id"].(float64))
	status, b, _ = request(t, s, at, "POST", "/api/rooms/"+room+"/messages", payload)
	expectStatus(t, status, 201, b)
	if int64(decodeMap(t, b)["id"].(float64)) != id {
		t.Fatal("duplicate message created")
	}
	status, b, _ = request(t, s, bt, "POST", "/api/rooms/"+room+"/read", map[string]int64{"message_id": id})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, at, "GET", "/api/rooms/"+room+"/members", nil)
	expectStatus(t, status, 200, b)
	if !bytes.Contains(b, []byte(`"last_read_id":1`)) {
		t.Fatal("read receipt missing")
	}
	status, b, _ = request(t, s, bt, "GET", "/api/rooms/"+authRoomID(admin.ID)+"/messages", nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, at, "POST", "/api/rooms/"+authRoomID(admin.ID)+"/messages", payload)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, bt, "POST", "/api/rooms/"+room+"/members", map[string]string{"username": "eve"})
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, at, "DELETE", "/api/rooms/"+room+"/members/"+bob.ID, nil)
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, bt, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 403, b)
	_ = a.store.view(func(st *State) error {
		if len(st.Messages) != 1 {
			t.Fatal("unexpected message count")
		}
		return nil
	})
}
func uploadTest(t *testing.T, s *httptest.Server, token, room, cid, name string, contents []byte) (int, []byte) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	part, e := mw.CreateFormFile("file", name)
	if e != nil {
		t.Fatal(e)
	}
	part.Write(contents)
	mw.Close()
	r, _ := http.NewRequest("POST", s.URL+"/api/rooms/"+room+"/files", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-OnTech-Request", "1")
	r.Header.Set("X-Client-ID", cid)
	r.Header.Set("Authorization", "Bearer "+token)
	res, e := s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}
func TestHTTPFileUploadDownloadLimitsAndDeletion(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	at, bt, et := loginTest(t, s, "admin"), loginTest(t, s, "bob"), loginTest(t, s, "eve")
	room := createTestRoom(t, s, at)
	a.cfg.MaxFileBytes = 1024
	a.cfg.StorageQuotaBytes = 1200
	content := []byte("첨부파일 내용\n<script>not executable</script>")
	status, b := uploadTest(t, s, at, room, "file-test-001", "../../테스트.log", content)
	expectStatus(t, status, 201, b)
	out := decodeMap(t, b)
	file := out["file"].(map[string]any)
	id := file["id"].(string)
	messageID := int64(out["message"].(map[string]any)["id"].(float64))
	if file["name"] != "테스트.log" {
		t.Fatal("unsafe filename")
	}
	status, b, h := request(t, s, bt, "GET", "/api/files/"+id, nil)
	expectStatus(t, status, 200, b)
	if !bytes.Equal(b, content) || !strings.Contains(h.Get("Content-Disposition"), "attachment") || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("download mismatch")
	}
	status, b, _ = request(t, s, et, "GET", "/api/files/"+id, nil)
	expectStatus(t, status, 403, b)
	status, b = uploadTest(t, s, at, room, "file-test-001", "different.log", []byte("duplicate"))
	expectStatus(t, status, 200, b)
	if decodeMap(t, b)["id"].(float64) != float64(messageID) {
		t.Fatal("file duplicate mismatch")
	}
	status, b = uploadTest(t, s, at, room, "file-too-big-001", "big.bin", bytes.Repeat([]byte("a"), 1025))
	expectStatus(t, status, 413, b)
	status, b = uploadTest(t, s, at, room, "file-space-001", "one.bin", bytes.Repeat([]byte("x"), 800))
	expectStatus(t, status, 201, b)
	status, b = uploadTest(t, s, at, room, "file-space-002", "two.bin", bytes.Repeat([]byte("x"), 800))
	expectStatus(t, status, 507, b)
	status, b, _ = request(t, s, bt, "DELETE", fmt.Sprintf("/api/messages/%d", messageID), nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, at, "DELETE", fmt.Sprintf("/api/messages/%d", messageID), nil)
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, at, "GET", "/api/files/"+id, nil)
	expectStatus(t, status, 404, b)
	if _, e := os.Stat(filepath.Join(a.cfg.DataDir, "files", id)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("deleted file remains")
	}
}
func TestHTTPUserAdministrationAndSessionRevocation(t *testing.T) {
	_, s, _, bob, _ := testApp(t)
	at, bt := loginTest(t, s, "admin"), loginTest(t, s, "bob")
	status, b, _ := request(t, s, at, "POST", "/api/users", map[string]string{"username": "newperson", "password": testPassword})
	expectStatus(t, status, 201, b)
	if bytes.Contains(b, []byte("password")) {
		t.Fatal("password field leaked")
	}
	status, b, _ = request(t, s, at, "PATCH", "/api/users/"+bob.ID, map[string]bool{"disabled": true})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, bt, "GET", "/api/me", nil)
	expectStatus(t, status, 401, b)
	status, b, _ = request(t, s, at, "POST", "/api/password", map[string]string{"current_password": testPassword, "new_password": "Different-strong-password-872!"})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, at, "GET", "/api/me", nil)
	expectStatus(t, status, 401, b)
}
func TestSSERealtimeAndLogout(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	at, bt := loginTest(t, s, "admin"), loginTest(t, s, "bob")
	room := createTestRoom(t, s, at)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/api/events", nil)
	r.Header.Set("Authorization", "Bearer "+bt)
	res, e := s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(res.Body)
	nextEvent := func() string {
		for scan.Scan() {
			line := scan.Text()
			if strings.HasPrefix(line, "event: ") {
				return strings.TrimPrefix(line, "event: ")
			}
		}
		return ""
	}
	if nextEvent() != "ready" {
		t.Fatal("SSE not ready")
	}
	status, b, _ := request(t, s, at, "POST", "/api/rooms/"+room+"/messages", map[string]string{"text": "realtime", "client_id": "sse-message-001"})
	expectStatus(t, status, 201, b)
	if nextEvent() != "sync" {
		t.Fatal("SSE sync missing")
	}
	status, b, _ = request(t, s, bt, "POST", "/api/logout", map[string]any{})
	expectStatus(t, status, 200, b)
	if nextEvent() != "logout" {
		t.Fatal("SSE logout missing")
	}
	a.cancel()
}

func parserConfig() MailConfig {
	c := defaults().Mail
	c.TrustedAuthservIDs = []string{"mx.naver.test"}
	c.Rules[0].Senders = []string{"account@riotgames.example"}
	return c
}
func rawMail(subject, body, extra string) []byte {
	return []byte("From: Riot <account@riotgames.example>\r\nTo: owner@example.test\r\nAuthentication-Results: mx.naver.test; dmarc=pass header.from=riotgames.example\r\nSubject: " + subject + "\r\n" + extra + "\r\n" + body)
}
func TestMailParserUTF8HTMLBase64AndQuotedPrintable(t *testing.T) {
	c := parserConfig()
	subject := mime.BEncoding.Encode("utf-8", "로그인 코드: 012345")
	cases := []struct {
		name string
		raw  []byte
	}{{"encoded_subject", rawMail(subject, "Welcome", "Content-Type: text/plain; charset=utf-8\r\n")}, {"HTML", rawMail("로그인 코드", "<html><head><script>로그인 코드: 999999</script></head><body>로그인 코드: <strong>012345</strong></body></html>", "Content-Type: text/html; charset=utf-8\r\n")}, {"Base64", rawMail("로그인 코드", base64.StdEncoding.EncodeToString([]byte("로그인 코드: 012345")), "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n")}}
	var q bytes.Buffer
	qw := quotedprintable.NewWriter(&q)
	qw.Write([]byte("로그인 코드: 012345"))
	qw.Close()
	cases = append(cases, struct {
		name string
		raw  []byte
	}{"QuotedPrintable", rawMail("로그인 코드", q.String(), "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n")})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p, e := ParseAuthMail(test.raw, c)
			if e != nil || p == nil || p.Subject == "" || p.Service != "Naver Mail" {
				t.Fatalf("parse failed: %#v %v", p, e)
			}
		})
	}
}
func TestMailParserTitleFilterAndDecodingFailures(t *testing.T) {
	c := parserConfig()
	for _, subject := range []string{"Receipt: 012345", "본문에만 키워드", "로그인 번호"} {
		p, e := ParseAuthMail(rawMail(subject, "로그인 코드 654321", "Content-Type: text/plain; charset=utf-8\r\n"), c)
		if e != nil || p != nil {
			t.Fatal("nonmatching title forwarded", p, e)
		}
	}
	p, e := ParseAuthMail(rawMail("로그인 코드: 012345", "다른 숫자 654321", "Content-Type: text/plain; charset=utf-8\r\n"), c)
	if e != nil || p == nil || p.Body != "다른 숫자 654321" {
		t.Fatal("body was treated as authentication extraction", p, e)
	}
	if _, e = ParseAuthMail(bytes.Repeat([]byte("x"), maxRawMail+1), c); !errors.Is(e, errMailTooLarge) {
		t.Fatal("size limit missing")
	}
	if _, e = ParseAuthMail(rawMail("로그인 코드", "abc", "Content-Type: text/plain; charset=x-unsupported\r\n"), c); e == nil {
		t.Fatal("unsupported charset hidden")
	}
	if p, e = ParseAuthMail(rawMail("unrelated", "abc", "Content-Type: text/plain; charset=x-unsupported\r\n"), c); e != nil || p != nil {
		t.Fatal("nonmatching body unnecessarily decoded")
	}
}
func TestMailParserMultipartIgnoresAttachments(t *testing.T) {
	c := parserConfig()
	body := "--boundary\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n로그인 코드: 012345\r\n--boundary\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=ignored.txt\r\n\r\n로그인 코드: 777777\r\n--boundary--\r\n"
	p, e := ParseAuthMail(rawMail("로그인 코드", body, "Content-Type: multipart/mixed; boundary=boundary\r\n"), c)
	if e != nil || p == nil || p.Subject == "" {
		t.Fatal("multipart parse", p, e)
	}
}
func TestIMAPLiteralProtocolAndSafety(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	raw := rawMail("로그인 코드: 012345", "", "Content-Type: text/plain\r\n")
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		line, _ := r.ReadString('\n')
		if !strings.Contains(line, "BODY.PEEK[]") {
			return
		}
		fmt.Fprintf(server, "* 1 FETCH (UID 9 INTERNALDATE \"26-Sep-2026 12:00:00 +0900\" RFC822.SIZE %d BODY[]<0> {%d}\r\n", len(raw), len(raw))
		server.Write(raw)
		fmt.Fprint(server, ")\r\nA0001 OK fetch done\r\n")
	}()
	c := &imapClient{conn: client, r: bufio.NewReader(client)}
	result, e := c.command("UID FETCH 9 (UID INTERNALDATE RFC822.SIZE BODY.PEEK[]<0.2097153>)")
	if e != nil || len(result.literals) != 1 || !bytes.Equal(result.literals[0], raw) {
		t.Fatal("literal decode failed", e)
	}
	if _, e = imapQuote("bad\r\nLOGOUT"); e == nil {
		t.Fatal("IMAP command injection allowed")
	}
	if got := modifiedUTF7("INBOX & 테스트"); !strings.HasPrefix(got, "INBOX &- &") || !strings.HasSuffix(got, "-") {
		t.Fatal(got)
	}
}
func TestMailIngestionDuplicateUIDAndExpiry(t *testing.T) {
	a, s, admin, _, _ := testApp(t)
	token := loginTest(t, s, "admin")
	key := "test-account\x1fINBOX"
	p := &ParsedAuthMail{Service: "Riot Games", Subject: "로그인 코드: 012345", Body: "테스트 본문"}
	if e := a.consumeMail(key, 7, 9, p, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := a.consumeMail(key, 7, 9, p, time.Now()); e != nil {
		t.Fatal(e)
	}
	_ = a.store.view(func(st *State) error {
		if len(st.Messages) != 1 || st.MailCursors[key].UID != 9 {
			t.Fatal("mail deduplication failed")
		}
		return nil
	})
	status, b, _ := request(t, s, token, "GET", "/api/rooms/"+authRoomID(admin.ID)+"/messages", nil)
	expectStatus(t, status, 200, b)
	if !bytes.Contains(b, []byte("012345")) {
		t.Fatal("authentication code missing")
	}
	_ = a.store.update(func(st *State) error { st.Messages[0].Expires = now() - 1; return nil })
	status, b, _ = request(t, s, token, "GET", "/api/rooms/"+authRoomID(admin.ID)+"/messages", nil)
	expectStatus(t, status, 200, b)
	if bytes.Contains(b, []byte("012345")) || !bytes.Contains(b, []byte("expired")) {
		t.Fatal("expired code exposed")
	}
	a.cleanup()
	_ = a.store.view(func(st *State) error {
		if len(st.Messages) != 0 {
			t.Fatal("retention cleanup failed")
		}
		return nil
	})
	if _, e := os.Stat(filepath.Join(a.cfg.DataDir, "store.db.bak")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("expired backup remains")
	}
	if e := a.consumeMail(key, 8, 1, p, time.Now()); e != nil {
		t.Fatal(e)
	}
	_ = a.store.view(func(st *State) error {
		if len(st.Messages) != 1 {
			t.Fatal("UIDVALIDITY reset ignored")
		}
		return nil
	})
}

type fakePush struct {
	calls    int
	failOnce bool
}

func (f *fakePush) Send(_ context.Context, _ Device, _ Outbox) error {
	f.calls++
	if f.failOnce && f.calls == 1 {
		return pushError{code: "fcm_http_503"}
	}
	return nil
}
func TestDurablePushQueueRetryAndMembershipRevocation(t *testing.T) {
	a, _, admin, bob, _ := testApp(t)
	a.cfg.FCM.Enabled = true
	fake := &fakePush{failOnce: true}
	a.push = fake
	room := "queue-room"
	device := Device{ID: "test-device-001", UserID: bob.ID, Token: "synthetic-token-for-tests-only", Name: "test"}
	_ = a.store.update(func(st *State) error {
		st.Rooms[room] = Room{ID: room, Kind: "chat", OwnerID: admin.ID, Members: map[string]int64{admin.ID: 0, bob.ID: 0}}
		st.Devices[device.ID] = device
		_, e := addMessage(st, Message{RoomID: room, UserID: admin.ID, Text: "sensitive code 012345", Kind: "text", ClientID: "queue-message-001"}, a.cfg.MaxMessages, true)
		return e
	})
	a.deliverPush()
	_ = a.store.update(func(st *State) error {
		if len(st.Outbox) != 1 {
			t.Fatal("outbox not created")
		}
		for id, p := range st.Outbox {
			if p.Attempts != 1 || p.State != "pending" || p.LastError != "fcm_http_503" {
				t.Fatal("retry state wrong", p)
			}
			p.Due = now()
			st.Outbox[id] = p
		}
		return nil
	})
	a.deliverPush()
	_ = a.store.view(func(st *State) error {
		for _, p := range st.Outbox {
			if p.State != "sent" || p.Attempts != 2 {
				t.Fatal("retry not sent")
			}
		}
		return nil
	})
	_ = a.store.update(func(st *State) error {
		_, e := addMessage(st, Message{RoomID: room, UserID: admin.ID, Text: "second", Kind: "text", ClientID: "queue-message-002"}, a.cfg.MaxMessages, true)
		r := st.Rooms[room]
		delete(r.Members, bob.ID)
		st.Rooms[room] = r
		return e
	})
	a.deliverPush()
	if fake.calls != 2 {
		t.Fatal("push sent to removed room member")
	}
}
func TestFCMOAuthJWTAndPrivatePayload(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	oauthCalls, sendCalls := 0, 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			oauthCalls++
			r.ParseForm()
			assertion := r.Form.Get("assertion")
			parts := strings.Split(assertion, ".")
			if len(parts) != 3 {
				t.Error("invalid JWT")
				http.Error(w, "bad", 400)
				return
			}
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig) != nil {
				t.Error("JWT signature invalid")
			}
			claimsBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
			claims := decodeMap(t, claimsBytes)
			if claims["aud"] != server.URL+"/token" || claims["scope"] != "https://www.googleapis.com/auth/firebase.messaging" {
				t.Error("OAuth claims invalid")
			}
			respond(w, 200, map[string]any{"access_token": "synthetic-oauth-token", "expires_in": 3600})
		case "/send":
			sendCalls++
			if r.Header.Get("Authorization") != "Bearer synthetic-oauth-token" {
				t.Error("OAuth bearer not used")
			}
			raw, _ := io.ReadAll(r.Body)
			if bytes.Contains(raw, []byte("012345")) || bytes.Contains(raw, []byte("password")) {
				t.Error("sensitive content in FCM payload")
			}
			m := decodeMap(t, raw)["message"].(map[string]any)
			data := m["data"].(map[string]any)
			if data["message_id"] != "123" || data["room_id"] != "private-room" {
				t.Error("message reference missing")
			}
			if sendCalls == 3 {
				respond(w, 404, map[string]any{"error": map[string]any{"details": []map[string]string{{"errorCode": "UNREGISTERED"}}}})
			} else {
				respond(w, 200, map[string]string{"name": "projects/test/messages/accepted"})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	f := &FCM{email: "test@example.test", project: "test", key: key, client: server.Client(), tokenURL: server.URL + "/token", sendURL: server.URL + "/send"}
	device := Device{Token: "synthetic-device-token"}
	job := Outbox{MessageID: 123, RoomID: "private-room", Kind: "auth", Expires: now() + 120}
	for i := 0; i < 2; i++ {
		if e = f.Send(context.Background(), device, job); e != nil {
			t.Fatal(e)
		}
	}
	if oauthCalls != 1 {
		t.Fatal("OAuth token cache failed")
	}
	e = f.Send(context.Background(), device, job)
	var pe pushError
	if !errors.As(e, &pe) || !pe.unregistered || !pe.permanent {
		t.Fatal("unregistered token handling failed", e)
	}
	if _, e = url.Parse(f.sendURL); e != nil {
		t.Fatal(e)
	}
}
func TestLoginRateLimiting(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	for i := 0; i < 10; i++ {
		status, b, _ := request(t, s, "", "POST", "/api/login", map[string]string{"username": "missing", "password": "Wrong-password-123"})
		expectStatus(t, status, 401, b)
	}
	status, b, _ := request(t, s, "", "POST", "/api/login", map[string]string{"username": "missing", "password": "Wrong-password-123"})
	expectStatus(t, status, 429, b)
}

func TestHubConcurrentSubscribers(t *testing.T) {
	h := &Hub{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			user := fmt.Sprint(n)
			for k := 0; k < 100; k++ {
				ch, unsub, ok := h.subscribe(user)
				if !ok {
					t.Error("subscribe refused")
					return
				}
				h.publish([]string{user}, "room")
				select {
				case <-ch:
				default:
					t.Error("event missing")
				}
				unsub()
			}
		}(i)
	}
	wg.Wait()
	h.mu.Lock()
	if len(h.subs) != 0 {
		t.Error("subscription leak")
	}
	h.mu.Unlock()
	var stops []func()
	for i := 0; i < 8; i++ {
		_, stop, ok := h.subscribe("capped")
		if !ok {
			t.Fatal("early cap")
		}
		stops = append(stops, stop)
	}
	if _, _, ok := h.subscribe("capped"); ok {
		t.Fatal("connection cap missing")
	}
	for _, stop := range stops {
		stop()
	}
}

func TestLogoutRevokesOwnedDeviceOnly(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	adminToken := loginTest(t, s, "admin")
	bobToken := loginTest(t, s, "bob")
	deviceID := "test-device-logout"
	status, b, _ := request(t, s, bobToken, "POST", "/api/devices", map[string]string{"id": deviceID, "name": "synthetic device", "token": "synthetic-FCM-token-not-a-real-device"})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, adminToken, "POST", "/api/logout", map[string]string{"device_id": deviceID})
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, adminToken, "GET", "/api/me", nil)
	expectStatus(t, status, 200, b)
	if err := a.store.update(func(st *State) error {
		st.Outbox["synthetic-pending"] = Outbox{ID: "synthetic-pending", UserID: bob.ID, DeviceID: deviceID, State: "pending"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, b, _ = request(t, s, bobToken, "POST", "/api/logout", map[string]string{"device_id": deviceID})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, bobToken, "GET", "/api/me", nil)
	expectStatus(t, status, 401, b)
	_ = a.store.view(func(st *State) error {
		if _, ok := st.Devices[deviceID]; ok {
			t.Error("device survived logout")
		}
		if _, ok := st.Outbox["synthetic-pending"]; ok {
			t.Error("pending notification survived logout")
		}
		if st.Users[admin.ID].Disabled {
			t.Error("unrelated user modified")
		}
		return nil
	})
}

func TestRoomCreationMemberLimitIncludesOwner(t *testing.T) {
	a, s, _, _, _ := testApp(t)
	token := loginTest(t, s, "admin")
	names := []string{}
	if e := a.store.update(func(st *State) error {
		for i := 0; i < 50; i++ {
			name := fmt.Sprintf("member%03d", i)
			names = append(names, name)
			addUser(st, User{ID: randomID(), Username: name, Role: "user"})
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	status, b, _ := request(t, s, token, "POST", "/api/rooms", map[string]any{"name": "too many", "members": names})
	expectStatus(t, status, 400, b)
	status, b, _ = request(t, s, token, "POST", "/api/rooms", map[string]any{"name": "maximum allowed", "members": names[:49]})
	expectStatus(t, status, 201, b)
	if got := len(decodeMap(t, b)["members"].(map[string]any)); got != 50 {
		t.Fatalf("member count %d", got)
	}
}
