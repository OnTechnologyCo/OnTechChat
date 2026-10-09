package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const apkPackage = "com.ontechdev.chat"
const maxAPKBytes = int64(150 << 20)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// A locally trusted administrator publishes the APK and latest.json. There is no unauthenticated upload API.
// The Android client additionally verifies size/hash/package/version/signer before invoking the OS installer.
type APKRelease struct {
	Schema      int    `json:"schema"`
	Package     string `json:"package_name"`
	VersionCode int64  `json:"version_code"`
	VersionName string `json:"version_name"`
	Filename    string `json:"filename"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	MinSDK      int    `json:"min_sdk"`
	Signer      string `json:"signing_cert_sha256"`
	Notes       string `json:"notes"`
}

func (r APKRelease) validate() error {
	if r.Schema != 1 || r.Package != apkPackage || r.VersionCode < 1 || r.VersionCode > 2100000000 || r.MinSDK < 26 || r.MinSDK > 100 || r.Size < 1024 || r.Size > maxAPKBytes ||
		!shaPattern.MatchString(r.SHA256) || !shaPattern.MatchString(r.Signer) || r.Filename != r.SHA256+".apk" ||
		!utf8.ValidString(r.VersionName) || len(r.VersionName) > 80 || strings.TrimSpace(r.VersionName) == "" || !utf8.ValidString(r.Notes) || len(r.Notes) > 4000 {
		return errors.New("invalid APK release metadata")
	}
	return nil
}
func updateFolder(c Config) string { return filepath.Join(c.DataDir, "apk-releases") }
func regularUpdateFile(dir, name string) (*os.File, error) {
	// Fixed basenames only; never follow directory symlinks/reparse points or arbitrary request paths.
	if name != "latest.json" && !(len(name) == 68 && strings.HasSuffix(name, ".apk") && shaPattern.MatchString(strings.TrimSuffix(name, ".apk"))) {
		return nil, errors.New("invalid update filename")
	}
	for _, p := range []string{dir, filepath.Join(dir, name)} {
		s, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("linked update files are not allowed")
		}
	}
	f, e := os.Open(filepath.Join(dir, name))
	if e != nil {
		return nil, e
	}
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("not a regular update file")
	}
	return f, nil
}
func readAPKRelease(dir string) (APKRelease, error) {
	var rel APKRelease
	f, e := regularUpdateFile(dir, "latest.json")
	if e != nil {
		return rel, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 16385))
	d.DisallowUnknownFields()
	if e = d.Decode(&rel); e != nil {
		return rel, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return rel, errors.New("invalid manifest tail")
	}
	return rel, rel.validate()
}
func openVerifiedAPK(dir string, rel APKRelease) (*os.File, error) {
	f, e := regularUpdateFile(dir, rel.Filename)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*os.File, error) { f.Close(); return nil, e }
	st, e := f.Stat()
	if e != nil || st.Size() != rel.Size {
		return fail(errors.New("APK size mismatch"))
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, maxAPKBytes+1))
	if e != nil || n != rel.Size || hex.EncodeToString(h.Sum(nil)) != rel.SHA256 {
		return fail(errors.New("APK digest mismatch"))
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return fail(e)
	}
	var magic [4]byte
	if _, e = io.ReadFull(f, magic[:]); e != nil || string(magic[:]) != "PK\x03\x04" {
		return fail(errors.New("APK archive signature missing"))
	}
	_, e = f.Seek(0, io.SeekStart)
	if e != nil {
		return fail(e)
	}
	return f, nil
}
func (a *App) availableAPK(w http.ResponseWriter, r *http.Request, u User) {
	if !a.limiter.allow("update-check:"+u.ID, 20, 60e9) {
		fail(w, apiError{429, "업데이트 확인은 잠시 후 다시 시도해 주세요."})
		return
	}
	select {
	case a.apkSlots <- struct{}{}:
		defer func() { <-a.apkSlots }()
	default:
		fail(w, apiError{429, "APK 확인 중입니다. 잠시 후 다시 시도하세요."})
		return
	}
	rel, e := readAPKRelease(updateFolder(a.cfg))
	if os.IsNotExist(e) {
		respond(w, 200, map[string]any{"available": false, "schema": 1})
		return
	}
	if e != nil {
		fail(w, apiError{503, "APK 배포 설정을 확인해 주세요."})
		return
	}
	f, e := openVerifiedAPK(updateFolder(a.cfg), rel)
	if e != nil {
		fail(w, apiError{503, "배포 APK의 크기 또는 해시가 일치하지 않습니다. 관리자에게 확인해 주세요."})
		return
	}
	f.Close()
	respond(w, 200, map[string]any{"available": true, "release": rel, "download_path": "/api/app-updates/android/" + rel.SHA256 + ".apk"})
}
func (a *App) downloadAPK(w http.ResponseWriter, r *http.Request, u User) {
	if !a.limiter.allow("update-download:"+u.ID, 8, 60e9) {
		fail(w, apiError{429, "다운로드 요청이 많습니다. 잠시 후 다시 시도하세요."})
		return
	}
	select {
	case a.apkSlots <- struct{}{}:
		defer func() { <-a.apkSlots }()
	default:
		fail(w, apiError{429, "다른 업데이트 전송 중입니다. 잠시 후 다시 시도하세요."})
		return
	}
	name := r.PathValue("file")
	rel, e := readAPKRelease(updateFolder(a.cfg))
	if e != nil || name != rel.SHA256+".apk" {
		fail(w, notfound())
		return
	}
	f, e := openVerifiedAPK(updateFolder(a.cfg), rel)
	if e != nil {
		fail(w, apiError{503, "배포 파일이 변경되었습니다. 업데이트 확인을 다시 해 주세요."})
		return
	}
	defer f.Close()
	// Disallow multiple range requests to avoid multipart amplification; one range supports normal resume.
	if strings.Contains(r.Header.Get("Range"), ",") {
		fail(w, apiError{416, "단일 범위 다운로드만 지원합니다."})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="OnTechChat-%d.apk"`, rel.VersionCode))
	w.Header().Set("ETag", `"`+rel.SHA256+`"`)
	st, _ := f.Stat()
	http.ServeContent(w, r, "OnTechChat.apk", st.ModTime(), f)
}
func (a *App) apkDeployment(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	rel, e := readAPKRelease(updateFolder(a.cfg))
	out := map[string]any{"folder": updateFolder(a.cfg), "manifest": "latest.json", "available": e == nil, "package_name": apkPackage, "max_bytes": maxAPKBytes}
	if e == nil {
		out["release"] = rel
	} else if !os.IsNotExist(e) {
		out["error"] = "latest.json의 필드와 APK 이름·크기를 확인하세요."
	}
	respond(w, 200, out)
}
