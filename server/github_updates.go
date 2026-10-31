package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const githubReleaseAPI = "https://api.github.com/repos/OnTechnologyCo/OnTechChat/releases/latest"
const githubReleaseBase = "https://github.com/OnTechnologyCo/OnTechChat/releases/"

var releaseVersionPattern = regexp.MustCompile(`^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}$`)
var setupAssetPattern = regexp.MustCompile(`^OnTechChat-Server-Setup-v([0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6})-x64\.exe$`)
var apkAssetPattern = regexp.MustCompile(`^OnTechChat-v([0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6})-arm64\.apk$`)

type githubUpdateAsset struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}
type githubUpdateRelease struct {
	Tag     string             `json:"tag"`
	URL     string             `json:"url"`
	Server  *githubUpdateAsset `json:"server"`
	Android *githubUpdateAsset `json:"android"`
}
type githubUpdateCache struct {
	mu      sync.Mutex
	next    time.Time
	release githubUpdateRelease
	err     error
	client  *http.Client // Test fixtures replace transport; production uses only the fixed public endpoint.
}

func parseGitHubUpdateRelease(raw []byte) (out githubUpdateRelease, err error) {
	var data struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			State  string `json:"state"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &data) != nil || data.Draft || data.Prerelease || !strings.HasPrefix(data.Tag, "v") || !releaseVersionPattern.MatchString(strings.TrimPrefix(data.Tag, "v")) {
		return out, errors.New("GitHub 정식 릴리스 정보를 확인하지 못했습니다.")
	}
	out.Tag, out.URL = data.Tag, githubReleaseBase+"tag/"+data.Tag
	for _, asset := range data.Assets {
		setup, apk := setupAssetPattern.FindStringSubmatch(asset.Name), apkAssetPattern.FindStringSubmatch(asset.Name)
		if setup == nil && apk == nil {
			continue
		}
		limit := int64(250 << 20)
		if apk != nil {
			limit = maxAPKBytes
		}
		hash := strings.TrimPrefix(asset.Digest, "sha256:")
		if asset.State != "uploaded" || asset.Size < 1024 || asset.Size > limit || asset.Digest != "sha256:"+hash || !shaPattern.MatchString(hash) || asset.URL != githubReleaseBase+"download/"+data.Tag+"/"+asset.Name {
			return out, errors.New("GitHub 배포 파일의 주소·크기·SHA256 정보가 올바르지 않습니다.")
		}
		info := &githubUpdateAsset{Name: asset.Name, URL: asset.URL, Size: asset.Size, SHA256: hash}
		if setup != nil {
			if out.Server != nil || "v"+setup[1] != data.Tag {
				return out, errors.New("서버 설치 파일 버전이 모호합니다.")
			}
			info.Version, out.Server = setup[1], info
		} else {
			if out.Android != nil {
				return out, errors.New("Android APK가 둘 이상 등록되어 있습니다.")
			}
			info.Version, out.Android = apk[1], info
		}
	}
	if out.Server == nil && out.Android == nil {
		return out, errors.New("릴리스에 설치 파일이나 APK가 없습니다.")
	}
	return out, nil
}
func releaseIsNewer(candidate, installed string) bool {
	if !releaseVersionPattern.MatchString(candidate) || !releaseVersionPattern.MatchString(installed) {
		return false
	}
	a, b := strings.Split(candidate, "."), strings.Split(installed, ".")
	for i := range a {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func (c *githubUpdateCache) latest(ctx context.Context) (githubUpdateRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.next) {
		return c.release, c.err
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleaseAPI, nil)
	if err != nil {
		return githubUpdateRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "OnTechChat-Server/"+version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := client.Do(req)
	var out githubUpdateRelease
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			err = errors.New("GitHub 릴리스를 확인하지 못했습니다. 네트워크 또는 요청 제한을 확인하고 잠시 후 다시 시도하세요.")
		} else {
			var raw []byte
			raw, err = io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
			if err == nil {
				out, err = parseGitHubUpdateRelease(raw)
			}
		}
	}
	c.next = time.Now().Add(10 * time.Minute)
	if err != nil {
		c.next = time.Now().Add(30 * time.Second)
		err = errors.New("GitHub 업데이트 조회 실패: 정식 릴리스와 인터넷 연결을 확인하세요.")
	}
	c.release, c.err = out, err
	return out, err
}
func (a *App) githubUpdates(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	if !a.limiter.allow("github-update:"+u.ID, 20, time.Minute) {
		fail(w, apiError{429, "업데이트 확인은 잠시 후 다시 시도하세요."})
		return
	}
	rel, err := a.releaseUpdates.latest(r.Context())
	if err != nil {
		fail(w, apiError{503, err.Error()})
		return
	}
	newer := rel.Server != nil && releaseIsNewer(rel.Server.Version, version)
	respond(w, 200, map[string]any{"source": "github", "current_server": version, "server_update_available": newer, "release": rel})
}
