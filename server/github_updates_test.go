package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type githubFixtureTransport func(*http.Request) (*http.Response, error)

func (f githubFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func githubReleaseFixture() []byte {
	name := "OnTechChat-Server-Setup-v1.15.99-x64.exe"
	raw, _ := json.Marshal(map[string]any{
		"tag_name": "v1.15.99", "draft": false, "prerelease": false,
		"assets": []any{map[string]any{"name": name, "state": "uploaded", "size": 4096, "digest": "sha256:" + strings.Repeat("a", 64), "browser_download_url": githubReleaseBase + "download/v1.15.99/" + name}},
	})
	return raw
}
func TestGitHubUpdateSelectionAndValidation(t *testing.T) {
	raw := githubReleaseFixture()
	out, e := parseGitHubUpdateRelease(raw)
	if e != nil || out.Server == nil || out.Server.Version != "1.15.99" || !releaseIsNewer("1.15.100", "1.15.99") || releaseIsNewer("1.15.9", "1.15.10") {
		t.Fatal(out, e)
	}
	for _, bad := range [][]byte{
		bytes.ReplaceAll(raw, []byte(githubReleaseBase), []byte("http://attacker.invalid/")),
		bytes.ReplaceAll(raw, []byte(`"prerelease":false`), []byte(`"prerelease":true`)),
		bytes.ReplaceAll(raw, []byte("sha256:"), []byte("sha1:")),
		bytes.ReplaceAll(raw, []byte("uploaded"), []byte("new")),
	} {
		if _, e = parseGitHubUpdateRelease(bad); e == nil {
			t.Fatal("unsafe release accepted")
		}
	}
}
func TestGitHubUpdateAdminAccessAndCache(t *testing.T) {
	a, s, admin, bob := settingsTestApp(t)
	calls := 0
	a.releaseUpdates.client = &http.Client{Transport: githubFixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != githubReleaseAPI || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("unexpected URL or credential leak")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(githubReleaseFixture())), Header: make(http.Header)}, nil
	})}
	for _, row := range []struct {
		token  string
		status int
	}{{"", 401}, {bob, 403}, {admin, 200}, {admin, 200}} {
		code, b, _ := request(t, s, row.token, "GET", "/api/app-updates/github", nil)
		expectStatus(t, code, row.status, b)
		if code == 200 && !bytes.Contains(b, []byte(`"server_update_available":true`)) {
			t.Fatal(string(b))
		}
	}
	if calls != 1 {
		t.Fatal("latest release was not cached", calls)
	}
}
