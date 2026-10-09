package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAPKPublisherOrderingAndRetention(t *testing.T) {
	c := defaults()
	c.DataDir = filepath.Join(t.TempDir(), "private")
	source := t.TempDir()
	b := append([]byte("PK\x03\x04"), bytes.Repeat([]byte("x"), 2044)...)
	h := sha256.Sum256(b)
	sha := hex.EncodeToString(h[:])
	rel := APKRelease{Schema: 1, Package: apkPackage, VersionCode: 120, VersionName: "1.2.0", Filename: sha + ".apk", SHA256: sha, Size: int64(len(b)), MinSDK: 26, Signer: sha, Notes: "test"}
	write := func() {
		raw, _ := json.Marshal(rel)
		if e := os.WriteFile(filepath.Join(source, "latest.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	os.WriteFile(filepath.Join(source, rel.Filename), b, 0600)
	if e := publishAPK(c, source); e != nil {
		t.Fatal(e)
	}
	out, e := readAPKRelease(updateFolder(c))
	if e != nil || out.VersionCode != 120 {
		t.Fatal(out, e)
	}
	if e := publishAPK(c, source); e != nil {
		t.Fatal("idempotent", e)
	}
	rel.VersionCode = 119
	write()
	if e := publishAPK(c, source); e == nil {
		t.Fatal("downgrade accepted")
	}
	after, _ := readAPKRelease(updateFolder(c))
	if after.VersionCode != 120 {
		t.Fatal("old manifest damaged")
	}
	rel.VersionCode = 121
	write()
	os.WriteFile(filepath.Join(source, rel.Filename), bytes.Repeat([]byte("y"), len(b)), 0600)
	if e := publishAPK(c, source); e == nil {
		t.Fatal("corrupt accepted")
	}
	after, _ = readAPKRelease(updateFolder(c))
	if after.VersionCode != 120 {
		t.Fatal("publication order damaged")
	}
}
