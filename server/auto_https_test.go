package server

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/crypto/acme/autocert"
	"os"
	"path/filepath"
	"testing"
)

func TestAutoHTTPSRejectsUnsafeConfiguration(t *testing.T) {
	base := AutoHTTPSConfig{Enabled: true, Domain: "chat.example.com", AcceptTerms: true}
	if e := validateAutoHTTPS(base, "127.0.0.1:8787"); e != nil {
		t.Fatal(e)
	}
	for _, domain := range []string{"175.119.187.91", "https://chat.example.com", "chat.example.com:443", "chat.example.com/path", "*.example.com", "x.local", "x\n.example.com", "x..com", "-x.com"} {
		c := base
		c.Domain = domain
		if validateAutoHTTPS(c, "127.0.0.1:8787") == nil {
			t.Fatal(domain)
		}
	}
	c := base
	c.AcceptTerms = false
	if validateAutoHTTPS(c, "127.0.0.1:8787") == nil {
		t.Fatal("terms required")
	}
	c = base
	c.Email = "Name <a@example.com>"
	if validateAutoHTTPS(c, "127.0.0.1:8787") == nil {
		t.Fatal("email injection")
	}
	if validateAutoHTTPS(base, "0.0.0.0:443") == nil {
		t.Fatal("must preserve existing 443 listener")
	}
}
func TestAutoHTTPSSettingsPreserveExistingAccessAndAuthorization(t *testing.T) {
	a, s, admin, bob := settingsTestApp(t)
	old, rev, e := a.readSavedSettings()
	if e != nil {
		t.Fatal(e)
	}
	data := map[string]any{"revision": rev, "enabled": true, "domain": "chat.example.com", "email": "", "accept_terms": true}
	for _, token := range []string{"", bob} {
		code, b, _ := request(t, s, token, "POST", "/api/settings/https", data)
		want := 403
		if token == "" {
			want = 401
		}
		expectStatus(t, code, want, b)
		code, b, _ = request(t, s, token, "GET", "/api/settings/https", nil)
		expectStatus(t, code, want, b)
		code, b, _ = request(t, s, token, "POST", "/api/settings/https/check", map[string]any{})
		expectStatus(t, code, want, b)
	}
	code, b, _ := request(t, s, admin, "POST", "/api/settings/https", data)
	expectStatus(t, code, 200, b)
	saved, _, e := a.readSavedSettings()
	if e != nil {
		t.Fatal(e)
	}
	if saved.Listen != old.Listen || saved.PublicURL != old.PublicURL || saved.TLSCert != old.TLSCert || saved.TLSKey != old.TLSKey {
		t.Fatal("existing HTTPS overwritten")
	}
	if !saved.AutoHTTPS.Enabled || saved.AutoHTTPS.Domain != "chat.example.com" {
		t.Fatal("configuration not persisted")
	}
	select {
	case next := <-a.reload:
		if next.AutoHTTPS != saved.AutoHTTPS {
			t.Fatal("reload mismatch")
		}
	default:
		t.Fatal("apply not queued")
	}
	// 기존 API가 새 필드를 보내지 않아도 설정은 보존한다.
	_, edit := getSettingMap(t, s, admin)
	next, e := a.candidateSettings(saved, edit)
	if e != nil || next.AutoHTTPS != saved.AutoHTTPS {
		t.Fatal("ordinary settings lost HTTPS", e)
	}
	a.applying = false
	code, b, _ = request(t, s, admin, "POST", "/api/settings/https", data)
	expectStatus(t, code, 409, b)
	a.cfg.AutoHTTPS = saved.AutoHTTPS
	if !a.validHost("chat.example.com") || !a.validHost("chat.example.com:443") || a.validHost("evil.example.com") {
		t.Fatal("domain host scope")
	}
}
func TestAutoHTTPSCacheEncryptedRestartAndDelete(t *testing.T) {
	a, _, _, _ := settingsTestApp(t)
	dir := filepath.Join(a.cfg.DataDir, "https-certificates")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	c := &encryptedACMECache{app: a, dir: dir}
	ctx := context.Background()
	key := "../../synthetic-key"
	plain := []byte("SYNTHETIC-PRIVATE-CERTIFICATE")
	if _, e := c.Get(ctx, key); e != autocert.ErrCacheMiss {
		t.Fatal(e)
	}
	if e := c.Put(ctx, key, plain); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(c.filename(key))
	if e != nil || bytes.Contains(raw, plain) {
		t.Fatal("plaintext cache", e)
	}
	fresh := &encryptedACMECache{app: a, dir: dir}
	got, e := fresh.Get(ctx, key)
	if e != nil || !bytes.Equal(got, plain) {
		t.Fatal("cache round trip", e)
	}
	if e := fresh.Delete(ctx, key); e != nil {
		t.Fatal(e)
	}
	if _, e := fresh.Get(ctx, key); e != autocert.ErrCacheMiss {
		t.Fatal(e)
	}
	if e := fresh.Delete(ctx, key); e != nil {
		t.Fatal(e)
	}
}
func TestAutoHTTPSStatusDoesNotClaimInternetSuccess(t *testing.T) {
	a, _, _, _ := settingsTestApp(t)
	a.cfg.AutoHTTPS = AutoHTTPSConfig{Enabled: true, Domain: "chat.example.com", AcceptTerms: true}
	a.httpsResult("port_conflict", "port occupied")
	status := a.autoHTTPSStatus()
	if status["state"] != "port_conflict" || status["tested_from_internet"] != false || status["listening"] != false {
		t.Fatal(status)
	}
	b, _ := json.Marshal(status)
	if bytes.Contains(b, []byte("private")) || bytes.Contains(b, []byte("key")) {
		t.Fatal("secret status")
	}
}
