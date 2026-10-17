package server

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// 기존 IP 수신기와 별개로 443만 추가한다. DNS/공유기나 외부 프록시는 변경하지 않는다.
func validateAutoHTTPS(c AutoHTTPSConfig, listen string) error {
	if !c.Enabled {
		return nil
	}
	if c.Domain != strings.ToLower(c.Domain) || !validDNSName(c.Domain) || net.ParseIP(c.Domain) != nil ||
		strings.HasSuffix(c.Domain, ".local") || strings.HasSuffix(c.Domain, ".localhost") {
		return errors.New("자동 HTTPS에는 포트·경로 없는 공인 도메인을 입력하세요")
	}
	if !c.AcceptTerms {
		return errors.New("Let's Encrypt 인증서 발급 약관에 동의해 주세요")
	}
	if c.Email != "" {
		m, e := mail.ParseAddress(c.Email)
		if e != nil || m.Address != c.Email || len(c.Email) > 254 {
			return errors.New("인증서 연락용 이메일을 확인하세요")
		}
	}
	_, port, _ := net.SplitHostPort(listen)
	if port == "443" {
		return errors.New("기존 수신기가 443을 사용 중입니다. 기존 HTTPS 설정에서 도메인을 추가해야 합니다. 기존 설정은 유지합니다")
	}
	return nil
}

type managedHTTPSState struct {
	mu          sync.Mutex
	listening   bool
	checking    bool
	state       string
	message     string
	expires     string
	lastAttempt time.Time
}

func (a *App) httpsResult(state, message string) {
	a.httpsState.mu.Lock()
	defer a.httpsState.mu.Unlock()
	a.httpsState.state = state
	a.httpsState.message = message
}
func (a *App) autoHTTPSStatus() map[string]any {
	h := &a.httpsState
	h.mu.Lock()
	defer h.mu.Unlock()
	state, message := h.state, h.message
	if !a.cfg.AutoHTTPS.Enabled {
		state = "disabled"
		message = "도메인 자동 HTTPS가 꺼져 있습니다."
	}
	if state == "" {
		state = "pending"
		message = "설정 적용 후 HTTPS 수신기를 시작합니다."
	}
	return map[string]any{"enabled": a.cfg.AutoHTTPS.Enabled, "domain": a.cfg.AutoHTTPS.Domain,
		"url": func() string {
			if a.cfg.AutoHTTPS.Domain != "" {
				return "https://" + a.cfg.AutoHTTPS.Domain
			}
			return ""
		}(),
		"state": state, "message": message, "listening": h.listening, "checking": h.checking,
		"not_after": h.expires, "tested_from_internet": false}
}

// 인증서 개인키/ACME 계정키는 기존 서버 키로 암호화하여 원자적으로 저장한다.
type encryptedACMECache struct {
	app *App
	dir string
	mu  sync.Mutex
}

func (c *encryptedACMECache) filename(key string) string {
	return filepath.Join(c.dir, digest(key)+".enc")
}
func (c *encryptedACMECache) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.app.ctx.Err(); e != nil {
		return nil, e
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	path := c.filename(key)
	if e := validateConfigTarget(path); e != nil {
		return nil, e
	}
	b, e := boundedFile(path, 256<<10)
	if errors.Is(e, os.ErrNotExist) {
		return nil, autocert.ErrCacheMiss
	}
	if e != nil {
		return nil, e
	}
	return openSetting(c.app.store, "auto-https/"+key, string(b))
}
func (c *encryptedACMECache) Put(ctx context.Context, key string, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.app.ctx.Err(); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(b) > 128<<10 {
		return errors.New("certificate cache size limit")
	}
	v, e := sealSetting(c.app.store, "auto-https/"+key, b)
	if e != nil {
		return e
	}
	return atomicConfigFile(c.filename(key), []byte(v))
}
func (c *encryptedACMECache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.app.ctx.Err(); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	path := c.filename(key)
	if e := validateConfigTarget(path); e != nil {
		return e
	}
	e := os.Remove(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	return e
}

// 설정 세대가 종료되면 자동 갱신의 네트워크 요청도 취소한다.
type acmeTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t acmeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	defer stop()
	defer cancel()
	if t.ctx.Err() != nil {
		return nil, t.ctx.Err()
	}
	// 응답 본문은 ACME 클라이언트가 읽을 때까지 살아 있어야 한다.
	response, e := t.base.RoundTrip(r.Clone(ctx))
	if e != nil {
		return nil, e
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	response.Body.Close()
	if e != nil {
		return nil, e
	}
	response.Body = io.NopCloser(strings.NewReader(string(data)))
	return response, nil
}
func (a *App) startAutoHTTPS() func() {
	if !a.cfg.AutoHTTPS.Enabled {
		return func() {}
	}
	if e := validateAutoHTTPS(a.cfg.AutoHTTPS, a.cfg.Listen); e != nil {
		a.httpsResult("configuration_error", e.Error())
		return func() {}
	}
	dir := filepath.Join(a.cfg.DataDir, "https-certificates")
	if e := validateConfigTarget(dir); e != nil {
		a.httpsResult("storage_error", "인증서 저장 경로를 확인하세요.")
		return func() {}
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		a.httpsResult("storage_error", "인증서 저장 폴더 권한을 확인하세요.")
		return func() {}
	}
	ln, e := net.Listen("tcp", ":443")
	if e != nil {
		a.httpsResult("port_conflict", "443 포트를 열지 못했습니다. 다른 HTTPS 프로그램의 443 사용 여부와 수신 권한을 확인하세요. 기존 접속은 유지됩니다.")
		return func() {}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	manager := &autocert.Manager{Prompt: autocert.AcceptTOS, HostPolicy: autocert.HostWhitelist(a.cfg.AutoHTTPS.Domain),
		Email: a.cfg.AutoHTTPS.Email, Cache: &encryptedACMECache{app: a, dir: dir},
		Client: &acme.Client{DirectoryURL: acme.LetsEncryptURL, HTTPClient: &http.Client{Timeout: 45 * time.Second, Transport: acmeTransport{a.ctx, transport}}}}
	tlsConfig := manager.TLSConfig()
	tlsConfig.MinVersion = tls.VersionTLS12
	// GetCertificate를 통해서만 발급하며 ACME의 TLS-ALPN 검증 프로토콜을 유지한다.
	srv := &http.Server{Handler: a.Handler(), TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 90 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 32 << 10,
		ErrorLog: log.New(io.Discard, "", 0)}
	a.httpsState.mu.Lock()
	a.httpsState.listening = true
	a.httpsState.mu.Unlock()
	a.httpsResult("waiting", "443 수신 준비 완료. 인증서를 확인하는 중입니다. Cloudflare DNS 전용과 서버까지의 TCP 443 연결이 필요합니다.")
	done := make(chan struct{})
	go func() {
		defer close(done)
		if e := srv.ServeTLS(ln, "", ""); e != nil && !errors.Is(e, http.ErrServerClosed) {
			a.httpsState.mu.Lock()
			a.httpsState.listening = false
			a.httpsState.mu.Unlock()
			a.httpsResult("listener_error", "HTTPS 수신기가 중지되었습니다. 기존 주소에서 설정을 다시 적용하세요.")
		}
	}()
	a.beginHTTPSCheck()
	return func() { _ = srv.Close(); _ = ln.Close(); <-done; transport.CloseIdleConnections() }
}

// 로컬 TLS 검사도 공인 루트와 도메인 검증을 수행한다. 인증서 검증 우회는 없다.
func (a *App) beginHTTPSCheck() bool {
	h := &a.httpsState
	h.mu.Lock()
	if !h.listening || h.checking || time.Since(h.lastAttempt) < time.Minute {
		h.mu.Unlock()
		return false
	}
	h.checking = true
	h.lastAttempt = time.Now()
	h.state = "checking"
	h.message = "인증서 발급·HTTPS 연결 확인 중… (최대 90초)"
	h.mu.Unlock()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer func() { h.mu.Lock(); h.checking = false; h.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
		defer cancel()
		dialer := tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: a.cfg.AutoHTTPS.Domain}}
		conn, e := dialer.DialContext(ctx, "tcp", "127.0.0.1:443")
		if e != nil {
			if a.ctx.Err() == nil {
				a.httpsResult("certificate_pending", "인증서 확인 실패. DNS가 이 서버를 가리키는지, Cloudflare가 DNS 전용인지, 공유기·방화벽의 외부 TCP 443이 이 서버 443으로 연결되는지 확인한 뒤 다시 확인하세요.")
			}
			return
		}
		defer conn.Close()
		state := conn.(*tls.Conn).ConnectionState()
		if len(state.VerifiedChains) == 0 {
			a.httpsResult("certificate_pending", "공인 인증서 검증에 실패했습니다.")
			return
		}
		h.mu.Lock()
		h.expires = state.PeerCertificates[0].NotAfter.UTC().Format(time.RFC3339)
		h.mu.Unlock()
		a.httpsResult("certificate_ready", "공인 인증서와 서버 내부 HTTPS 연결 확인 완료 · 자동 갱신됩니다. 휴대전화 LTE·5G에서 도메인 접속을 확인하세요.")
	}()
	return true
}
func (a *App) getAutoHTTPS(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	respond(w, 200, a.autoHTTPSStatus())
}
func (a *App) checkAutoHTTPS(w http.ResponseWriter, r *http.Request, u User) {
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	if !a.cfg.AutoHTTPS.Enabled {
		fail(w, bad("도메인 HTTPS를 먼저 설정하세요."))
		return
	}
	a.beginHTTPSCheck()
	respond(w, 202, a.autoHTTPSStatus())
}
func (a *App) saveAutoHTTPS(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		Revision string `json:"revision"`
		AutoHTTPSConfig
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.applying {
		fail(w, apiError{409, "설정 적용 중입니다."})
		return
	}
	c, revision, e := a.readSavedSettings()
	if e != nil {
		fail(w, e)
		return
	}
	if revision != in.Revision {
		fail(w, apiError{409, "다른 설정이 저장되었습니다. 다시 읽어 주세요."})
		return
	}
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	in.Email = strings.TrimSpace(in.Email)
	c.AutoHTTPS = in.AutoHTTPSConfig
	if e = c.validate(); e != nil {
		fail(w, bad(e.Error()))
		return
	}
	// IP 주소/기존 인증서/수신 포트/다른 설정을 바꾸지 않는다.
	revision, e = persistConfiguration(c, revision)
	if e != nil {
		fail(w, e)
		return
	}
	a.applying = true
	respond(w, 200, map[string]any{"ok": true, "revision": revision, "apply_requested": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	select {
	case a.reload <- c:
	default:
	}
}
