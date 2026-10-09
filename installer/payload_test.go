package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedPayload(t *testing.T) {
	files, e := inspectPayload()
	if e != nil {
		t.Fatal(e)
	}
	if len(files) < 3 {
		t.Fatal("incomplete")
	}
	dir := t.TempDir()
	if e = extractPayload(dir); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "config.example.json"))
	if e != nil || len(b) < 10 {
		t.Fatal("missing config payload")
	}
	manifestBytes, e := os.ReadFile(filepath.Join(dir, "apk-release", "latest.json"))
	if e != nil {
		t.Fatal("missing embedded APK manifest")
	}
	var rel struct {
		Package     string `json:"package_name"`
		VersionCode int64  `json:"version_code"`
		VersionName string `json:"version_name"`
		Filename    string `json:"filename"`
		SHA256      string `json:"sha256"`
		Size        int64  `json:"size"`
		MinSDK      int    `json:"min_sdk"`
		Signer      string `json:"signing_cert_sha256"`
	}
	if e = json.Unmarshal(manifestBytes, &rel); e != nil {
		t.Fatal(e)
	}
	apkBytes, e := os.ReadFile(filepath.Join(dir, "apk-release", rel.Filename))
	if e != nil {
		t.Fatal("missing embedded APK")
	}
	h := sha256.Sum256(apkBytes)
	if rel.Package != "com.ontechdev.chat" || rel.VersionCode < 1 || rel.VersionName == "" || rel.MinSDK < 26 ||
		rel.Filename != rel.SHA256+".apk" || rel.Size != int64(len(apkBytes)) ||
		hex.EncodeToString(h[:]) != rel.SHA256 || len(rel.Signer) != 64 {
		t.Fatalf("invalid embedded APK release: %+v", rel)
	}
	var executables int
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if strings.HasSuffix(strings.ToLower(p), ".exe") {
			executables++
		}
		return e
	})
	if executables != 0 {
		t.Fatal("runtime must be copied from setup itself, not embedded recursively")
	}
	if len(payloadDigest()) != 64 {
		t.Fatal("invalid digest")
	}
}
func TestTraversalAndDuplicateRejected(t *testing.T) {
	orig := payload
	defer func() { payload = orig }()
	for _, names := range [][]string{{"../escape"}, {"/absolute"}, {"a/../../outside"}, {"C:/absolute"}, {`..\outside`}, {"a", "A"}, {"safe/./other"}} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		for _, n := range names {
			f, _ := z.Create(n)
			f.Write([]byte("test"))
		}
		z.Close()
		payload = b.Bytes()
		if _, e := inspectPayload(); e == nil {
			t.Fatalf("accepted %q", names)
		}
	}
}
func TestInstallerRejectsMissingPayload(t *testing.T) {
	orig := payload
	defer func() { payload = orig }()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, _ := z.Create("README.txt")
	f.Write([]byte("not an application"))
	z.Close()
	payload = b.Bytes()
	if _, e := inspectPayload(); e == nil {
		t.Fatal("missing server accepted")
	}
}
