package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectLocationSendingRemoved(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	code, body, _ := request(t, s, at, "POST", "/api/rooms/direct", map[string]string{"user_id": bob.ID})
	expectStatus(t, code, 201, body)
	room := decodeMap(t, body)["id"].(string)
	for _, token := range []string{at, bt, et} {
		code, body, _ = request(t, s, token, "POST", "/api/rooms/"+room+"/location", responseFix())
		expectStatus(t, code, 410, body)
	}
	code, body, _ = request(t, s, "", "POST", "/api/rooms/"+room+"/location", responseFix())
	expectStatus(t, code, 401, body)
	a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.Kind == "location" {
				t.Fatal("retired API stored coordinates")
			}
		}
		return nil
	})
}
func TestLocationValidationBoundaries(t *testing.T) {
	p := SharedLocation{37, 127, 10, now(), false}
	if !validLocation(p, now()) {
		t.Fatal("valid rejected")
	}
	for _, test := range []struct {
		name string
		p    SharedLocation
	}{
		{"lat", SharedLocation{91, 1, 10, now(), false}}, {"lon", SharedLocation{1, -181, 10, now(), false}},
		{"nan", SharedLocation{math.NaN(), 1, 10, now(), false}}, {"inf", SharedLocation{1, 1, math.Inf(1), now(), false}},
		{"zero_accuracy", SharedLocation{1, 1, 0, now(), false}}, {"old", SharedLocation{1, 1, 10, now() - 121, false}},
		{"future", SharedLocation{1, 1, 10, now() + 16, false}}, {"bad_accuracy", SharedLocation{1, 1, 20001, now(), true}}} {
		t.Run(test.name, func(t *testing.T) {
			if validLocation(test.p, now()) {
				t.Fatal("invalid accepted")
			}
		})
	}
}
func intString(n int64) string { b, _ := json.Marshal(n); return string(b) }
func publishTestAPK(t *testing.T, c Config) (APKRelease, []byte) {
	t.Helper()
	dir := updateFolder(c)
	os.MkdirAll(dir, 0700)
	b := append([]byte("PK\x03\x04"), bytes.Repeat([]byte{5}, 4096)...)
	h := sha256.Sum256(b)
	sha := hex.EncodeToString(h[:])
	r := APKRelease{1, apkPackage, 120, "1.2.0", sha + ".apk", sha, int64(len(b)), 26, strings.Repeat("a", 64), "test notes"}
	j, _ := json.Marshal(r)
	os.WriteFile(filepath.Join(dir, r.Filename), b, 0600)
	os.WriteFile(filepath.Join(dir, "latest.json"), j, 0600)
	return r, b
}
func TestAPKUpdatePublicationAndACL(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	status, b, _ := request(t, s, "", "GET", "/api/app-updates/android", nil)
	expectStatus(t, status, 401, b)
	status, b, _ = request(t, s, bt, "GET", "/api/app-updates/android", nil)
	expectStatus(t, status, 200, b)
	if decodeMap(t, b)["available"] != false {
		t.Fatal("phantom update")
	}
	rel, apk := publishTestAPK(t, a.cfg)
	status, b, _ = request(t, s, bt, "GET", "/api/app-updates/android", nil)
	expectStatus(t, status, 200, b)
	v := decodeMap(t, b)
	if v["available"] != true {
		t.Fatal("missing update")
	}
	path := v["download_path"].(string)
	if bytes.Contains(b, []byte(a.cfg.DataDir)) {
		t.Fatal("private folder leaked to clients")
	}
	status, b, _ = request(t, s, "", "GET", path, nil)
	expectStatus(t, status, 401, b)
	status, b, h := request(t, s, bt, "GET", path, nil)
	expectStatus(t, status, 200, b)
	if !bytes.Equal(b, apk) || h.Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatal("bad APK")
	}
	status, b, _ = request(t, s, bt, "GET", "/api/app-updates/deployment", nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, at, "GET", "/api/app-updates/deployment", nil)
	expectStatus(t, status, 200, b)
	os.WriteFile(filepath.Join(updateFolder(a.cfg), rel.Filename), bytes.Repeat([]byte{7}, len(apk)), 0600)
	status, b, _ = request(t, s, bt, "GET", path, nil)
	expectStatus(t, status, 503, b)
	status, b, _ = request(t, s, bt, "GET", "/api/app-updates/android", nil)
	expectStatus(t, status, 503, b)
}
func TestAPKUpdateMetadataRejects(t *testing.T) {
	c := defaults()
	c.DataDir = t.TempDir()
	r, _ := publishTestAPK(t, c)
	cases := []struct {
		name string
		edit func(*APKRelease)
	}{{"traversal", func(x *APKRelease) { x.Filename = "../secret.apk" }}, {"package", func(x *APKRelease) { x.Package = "other.app" }}, {"large", func(x *APKRelease) { x.Size = maxAPKBytes + 1 }}, {"bad_sha", func(x *APKRelease) { x.SHA256 = "x" }}, {"signer", func(x *APKRelease) { x.Signer = "" }}, {"version", func(x *APKRelease) { x.VersionCode = 0 }}, {"filename", func(x *APKRelease) { x.Filename = "latest.apk" }}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bad := r
			test.edit(&bad)
			if bad.validate() == nil {
				t.Fatal("invalid accepted")
			}
		})
	}
	if _, e := regularUpdateFile(updateFolder(c), "../../encryption.key"); e == nil {
		t.Fatal("traversal accepted")
	}
}
