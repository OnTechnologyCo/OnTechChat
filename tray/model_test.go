package tray

import (
	"strings"
	"testing"
)

func TestLocalURL(t *testing.T) {
	for _, s := range []string{"http://127.0.0.1:8787", "http://127.0.0.1:8787/", "https://[::1]:9443/", "http://127.0.0.2:80"} {
		t.Run(s, func(t *testing.T) {
			u, e := validateLocalURL(s)
			if e != nil || !strings.HasSuffix(u, "/") {
				t.Fatalf("%q %v", u, e)
			}
		})
	}
}
func TestRejectUnsafeURL(t *testing.T) {
	for _, s := range []string{"file:///C:/secret.txt", "javascript:alert(1)", "https://example.com", "http://localhost:8787", "http://0.0.0.0:8787", "http://127.0.0.1.evil.test", "http://u:p@127.0.0.1:8787", "http://127.0.0.1:8787/?x=1", "http://127.0.0.1:8787/#x", "http://127.0.0.1:99999", "http://127.0.0.1:0", "http://127.0.0.1:/", "http://127.0.0.1/path", "http://127.0.0.1/%2f", "http://127.0.0.1/?", "http://127.0.0.1\x00/"} {
		t.Run(s, func(t *testing.T) {
			if _, e := validateLocalURL(s); e == nil {
				t.Fatalf("accepted %q", s)
			}
		})
	}
}
func TestDecodeEndpoint(t *testing.T) {
	u, e := decodeEndpoint([]byte(`{"local_url":"http://127.0.0.1:8787/"}`))
	if e != nil || u != "http://127.0.0.1:8787/" {
		t.Fatal(u, e)
	}
	for _, s := range []string{`{`, `{"password":"not-used"}`, strings.Repeat(" ", 4097)} {
		if _, e := decodeEndpoint([]byte(s)); e == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}
func TestSettingsURL(t *testing.T) {
	u, e := settingsURL("http://127.0.0.1:8787")
	if e != nil || u != "http://127.0.0.1:8787/?settings=1" {
		t.Fatal(u, e)
	}
}
func TestStates(t *testing.T) {
	for _, c := range []struct {
		s   serviceState
		key string
	}{{serviceState{Exists: true, State: 4}, "running"}, {serviceState{Exists: true, State: 1}, "stopped"}, {serviceState{Exists: true, State: 2}, "pending"}, {serviceState{}, "stopped"}, {serviceState{Err: "access denied"}, "pending"}} {
		if c.s.iconKey() != c.key || c.s.text() == "" {
			t.Fatal(c)
		}
	}
}
