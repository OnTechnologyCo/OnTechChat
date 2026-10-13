package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type App struct {
	cfg              Config
	settingsMu       sync.Mutex
	applying         bool
	reload           chan Config
	store            *Store
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	limiter          Limiter
	hub              *Hub
	uploadMu         sync.Mutex
	passwordSlots    chan struct{}
	apkSlots         chan struct{}
	stateMu          sync.Mutex
	mailState        string
	mailChecked      int64
	mailRunMu        sync.Mutex
	mailLastReport   MailReport
	mailConnector    func(context.Context, MailConfig) (*imapClient, func(), error)
	pushState        string
	push             PushSender
	dummyHash        string
	mailInspector    func(context.Context, MailConfig, bool) (MailDiscovery, error)
	conversationHelp *conversationHelpState
	aiExecutor       AIExecutor
	aiJobs           *aiJobManager
	agentJobs        *agentJobManager
	agentPlanner     AgentPlanner
	agentRunner      AgentRunner
	hostPrograms     *hostProgramController
}
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan string]bool
}

func (h *Hub) subscribe(user string) (chan string, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs = map[string]map[chan string]bool{}
	}
	if len(h.subs[user]) >= 8 {
		return nil, func() {}, false
	}
	ch := make(chan string, 16)
	if h.subs[user] == nil {
		h.subs[user] = map[chan string]bool{}
	}
	h.subs[user][ch] = true
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[user], ch)
		if len(h.subs[user]) == 0 {
			delete(h.subs, user)
		}
	}, true
}
func (h *Hub) publish(users []string, room string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, u := range users {
		for ch := range h.subs[u] {
			select {
			case ch <- room:
			default:
			}
		}
	}
}
func newApp(c Config) (*App, error) {
	s, e := openStore(c.DataDir)
	if e != nil {
		return nil, e
	}
	c, e = migrateConfiguration(c, s)
	if e != nil {
		s.Close()
		return nil, e
	}
	if e = unlockConfiguration(&c, s); e != nil {
		s.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	hash, e := passwordHash(randomToken())
	if e != nil {
		s.Close()
		cancel()
		return nil, e
	}
	if e = migrateAgentState(s); e != nil {
		s.Close()
		cancel()
		return nil, e
	}
	if e = migratePersistentSessions(s); e != nil {
		s.Close()
		cancel()
		return nil, e
	}
	if e = interruptAIRequests(s); e != nil {
		s.Close()
		cancel()
		return nil, e
	}
	if e = interruptAgentJobs(s); e != nil {
		s.Close()
		cancel()
		return nil, e
	}
	if e = interruptProgramInstances(s); e != nil {
		s.Close()
		cancel()
		return nil, e
	}
	a := &App{cfg: c, reload: make(chan Config, 1), store: s, ctx: ctx, cancel: cancel, hub: &Hub{}, passwordSlots: make(chan struct{}, 2), apkSlots: make(chan struct{}, 4), mailState: "disabled", pushState: "disabled", dummyHash: hash, aiExecutor: newDefaultAIExecutor(), agentPlanner: newDefaultAgentPlanner(), agentRunner: newAgentRunner(c.Agent), hostPrograms: newHostProgramController()}
	a.conversationHelp = &conversationHelpState{entries: map[string]conversationHelpScope{}, running: map[string]bool{}}
	a.aiJobs = newAIJobManager(a)
	a.agentJobs = newAgentJobManager(a)
	if c.FCM.Enabled {
		p, e := newFCM(c.FCM)
		if e != nil {
			s.Close()
			cancel()
			return nil, e
		}
		a.push = p
		a.pushState = "configured; not yet sent"
	}
	return a, nil
}
func (a *App) startWorkers() {
	a.wg.Add(1)
	go func() { defer a.wg.Done(); a.conversationHelpMaintenance(a.ctx.Done()) }()
	for _, work := range []func(){a.maintenanceLoop, a.mailLoop, a.pushLoop} {
		a.wg.Add(1)
		go func(f func()) { defer a.wg.Done(); f() }(work)
	}
	if a.aiJobs != nil {
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.aiJobs.run(a.ctx) }()
	}
	if a.agentJobs != nil {
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.agentJobs.run(a.ctx) }()
	}
}
func (a *App) Close() error {
	a.cancel()
	if a.hostPrograms != nil {
		a.hostPrograms.closeAll()
	}
	a.wg.Wait()
	if a.aiJobs != nil {
		a.aiJobs.wait()
	}
	if a.agentJobs != nil {
		a.agentJobs.wait()
	}
	return a.store.Close()
}
func (a *App) changed(room string, extra ...string) {
	a.invalidateConversationHelp()
	users := append([]string{}, extra...)
	_ = a.store.view(func(st *State) error {
		r := st.Rooms[room]
		for id := range r.Members {
			users = append(users, id)
		}
		return nil
	})
	a.hub.publish(users, room)
}

type apiError struct {
	status  int
	message string
}

func (e apiError) Error() string { return e.message }
func bad(s string) error         { return apiError{400, s} }
func forbidden() error           { return apiError{403, "접근 권한이 없습니다."} }
func notfound() error            { return apiError{404, "항목을 찾을 수 없습니다."} }
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	var ce codedAPIError
	if errors.As(e, &ce) {
		body := map[string]any{"error": ce.message, "code": ce.code}
		for k, v := range ce.data {
			body[k] = v
		}
		respond(w, ce.status, body)
		return
	}
	var ae apiError
	if errors.As(e, &ae) {
		respond(w, ae.status, map[string]string{"error": ae.message})
		return
	}
	log.Print("request failed: internal storage or I/O error")
	respond(w, 500, map[string]string{"error": "서버 처리에 실패했습니다. 잠시 후 다시 시도해 주세요."})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return apiError{415, "JSON 요청이 필요합니다."}
	}
	limit := int64(64 << 10)
	if strings.HasPrefix(r.URL.Path, "/api/settings") {
		limit = 256 << 10
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("잘못된 JSON 요청입니다.")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return bad("요청은 JSON 객체 한 개여야 합니다.")
	}
	return nil
}
func tokenFrom(r *http.Request) string {
	if s := r.Header.Get("Authorization"); strings.HasPrefix(s, "Bearer ") {
		return strings.TrimPrefix(s, "Bearer ")
	}
	if c, e := r.Cookie("ontech_session"); e == nil {
		return c.Value
	}
	return ""
}
func (a *App) identify(r *http.Request) (User, error) {
	token := tokenFrom(r)
	if len(token) < 32 || len(token) > 100 {
		return User{}, invalidSession()
	}
	var u User
	ok := false
	e := a.store.view(func(st *State) error {
		se, exists := st.Sessions[digest(token)]
		if exists && se.active() {
			u, ok = st.Users[se.UserID]
			ok = ok && !u.Disabled
		}
		return nil
	})
	if e != nil {
		return u, e
	}
	if !ok {
		return u, invalidSession()
	}
	return u, nil
}
func publicUser(u User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "disabled": u.Disabled, "agent_enabled": u.AgentEnabled, "created_at": u.Created, "display_name": displayName(u), "status_message": u.StatusMessage, "avatar_color": avatarColor(u), "avatar_revision": u.AvatarRevision}
}
func (a *App) secureCookie(r *http.Request) bool {
	return r.TLS != nil || (a.cfg.PublicURL != "" && sameAuthority("https", r.Host, originHost(a.cfg.PublicURL)))
}
func originHost(s string) string {
	u, e := url.Parse(s)
	if e != nil {
		return ""
	}
	return u.Host
}
func (a *App) validHost(host string) bool {
	_, port, _ := net.SplitHostPort(a.cfg.Listen)
	switch strings.ToLower(host) {
	case "127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port:
		return true
	}
	return a.cfg.PublicURL != "" && sameAuthority("https", host, originHost(a.cfg.PublicURL))
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"ok": true, "version": version, "service": "ontech-chat", "password_min_chars": passwordMinChars, "username_policy": "hangul-ascii-3-40", "supports_korean_usernames": true, "messenger_ui": true, "direct_chats": true, "profiles": true, "notification_settings": true, "notification_reconcile_version": 1, "location_sharing": true, "location_requests": 3, "location_popup": 1, "location_auto_response": 1, "app_notification_delivery": 1, "apk_updates": 1, "ai_chat": true, "ai_friend": true, "agent_api": true})
	})
	mux.HandleFunc("POST /api/login", a.login)
	type handler func(http.ResponseWriter, *http.Request, User)
	auth := func(h handler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u, e := a.identify(r)
			if e != nil {
				fail(w, e)
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" && !a.limiter.allow("user:"+u.ID, 240, time.Minute) {
				fail(w, apiError{429, "요청이 너무 많습니다."})
				return
			}
			h(w, r, u)
		}
	}
	mux.HandleFunc("GET /api/me", auth(func(w http.ResponseWriter, r *http.Request, u User) {
		a.setSessionCookie(w, r, tokenFrom(r))
		respond(w, 200, publicUser(u))
	}))
	mux.HandleFunc("POST /api/logout", auth(a.logout))
	mux.HandleFunc("POST /api/password", auth(a.changePassword))
	mux.HandleFunc("GET /api/friends", auth(a.friends))
	mux.HandleFunc("GET /api/ai/context/friends", auth(a.helpFriends))
	mux.HandleFunc("GET /api/ai/context/conversations", auth(a.helpConversations))
	mux.HandleFunc("GET /api/rooms/{room}/ai-help", auth(a.autoHelpStatus))
	mux.HandleFunc("PUT /api/rooms/{room}/ai-help", auth(a.autoHelpSet))
	mux.HandleFunc("POST /api/ai/context/preview", auth(a.helpPreview))
	mux.HandleFunc("POST /api/ai/context/analyze", auth(a.helpAnalyze))
	mux.HandleFunc("POST /api/ai/context/check", auth(a.helpCheck))
	mux.HandleFunc("GET /api/ai/context/results/{id}", auth(a.helpResult))
	mux.HandleFunc("GET /api/ai/context/messages/{room}/{id}", auth(a.helpSource))
	mux.HandleFunc("GET /api/profile", auth(a.getProfile))
	mux.HandleFunc("PATCH /api/profile", auth(a.updateProfile))
	mux.HandleFunc("PATCH /api/friends/{id}", auth(a.favoriteFriend))
	mux.HandleFunc("GET /api/users/{id}/avatar", auth(a.avatar))
	mux.HandleFunc("POST /api/rooms/direct", auth(a.openDirect))
	mux.HandleFunc("GET /api/ai/catalog", auth(a.aiCatalog))
	mux.HandleFunc("GET /api/ai/friends", auth(a.aiFriends))
	mux.HandleFunc("POST /api/ai/friends/{id}/open", auth(a.openAIFriend))
	mux.HandleFunc("PATCH /api/ai/friends/{id}/default-room", auth(a.setAIFriendDefaultRoom))
	mux.HandleFunc("POST /api/rooms/ai", auth(a.createAIRoom))
	mux.HandleFunc("GET /api/agent/projects", auth(a.listAgentProjects))
	mux.HandleFunc("POST /api/agent/projects", auth(a.createAgentProject))
	mux.HandleFunc("GET /api/agent/projects/{id}", auth(a.getAgentProject))
	mux.HandleFunc("PATCH /api/agent/projects/{id}", auth(a.updateAgentProject))
	mux.HandleFunc("POST /api/agent/jobs", auth(a.createAgentJob))
	mux.HandleFunc("GET /api/agent/jobs/{id}", auth(a.getAgentJob))
	mux.HandleFunc("POST /api/agent/jobs/{id}/cancel", auth(a.cancelAgentJob))
	mux.HandleFunc("POST /api/agent/jobs/{id}/retry", auth(a.retryAgentJob))
	mux.HandleFunc("GET /api/agent/artifacts/{id}", auth(a.downloadAgentArtifact))
	mux.HandleFunc("GET /api/agent/programs", auth(a.listHostPrograms))
	mux.HandleFunc("POST /api/agent/programs", auth(a.registerHostProgram))
	mux.HandleFunc("PATCH /api/agent/programs/{id}", auth(a.updateHostProgram))
	mux.HandleFunc("POST /api/agent/programs/{id}/actions", auth(a.hostProgramAction))
	mux.HandleFunc("GET /api/agent/diagnostics", auth(a.diagnoseAgentRuntime))
	mux.HandleFunc("GET /api/grants", auth(a.listGrants))
	mux.HandleFunc("POST /api/grants", auth(a.createGrant))
	mux.HandleFunc("POST /api/grants/{id}/revoke", auth(a.revokeGrant))
	mux.HandleFunc("PATCH /api/rooms/{room}/ai", auth(a.updateAIRoom))
	mux.HandleFunc("GET /api/rooms/{room}/ai/requests/{request}", auth(a.getAIRequest))
	mux.HandleFunc("POST /api/rooms/{room}/ai/requests/{request}/cancel", auth(a.cancelAIRequest))
	mux.HandleFunc("POST /api/rooms/{room}/ai/requests/{request}/retry", auth(a.retryAIRequest))
	mux.HandleFunc("PATCH /api/rooms/{room}/preference", auth(a.roomPreference))
	mux.HandleFunc("GET /api/users", auth(a.users))
	mux.HandleFunc("POST /api/users", auth(a.createUser))
	mux.HandleFunc("PATCH /api/users/{id}", auth(a.editUser))
	mux.HandleFunc("PATCH /api/users/{id}/role", auth(a.changeUserRole))
	mux.HandleFunc("GET /api/rooms", auth(a.rooms))
	mux.HandleFunc("DELETE /api/rooms/{room}", auth(a.deleteRoomFromList))
	mux.HandleFunc("POST /api/rooms", auth(a.createRoom))
	mux.HandleFunc("GET /api/rooms/{room}/members", auth(a.members))
	mux.HandleFunc("POST /api/rooms/{room}/members", auth(a.addMember))
	mux.HandleFunc("DELETE /api/rooms/{room}/members/{user}", auth(a.removeMember))
	mux.HandleFunc("POST /api/rooms/{room}/leave", auth(a.leaveRoom))
	mux.HandleFunc("GET /api/rooms/{room}/messages", auth(a.messages))
	mux.HandleFunc("POST /api/rooms/{room}/messages", auth(a.sendMessage))
	mux.HandleFunc("POST /api/rooms/{room}/read", auth(a.markRead))
	mux.HandleFunc("POST /api/rooms/{room}/location", auth(a.sendLocation))
	mux.HandleFunc("POST /api/location-requests", auth(a.requestLocation))
	mux.HandleFunc("GET /api/location-requests/{id}", auth(a.getLocationRequest))
	mux.HandleFunc("GET /api/location-requests", auth(a.pendingLocationRequests))
	mux.HandleFunc("POST /api/location-requests/{id}/fail", auth(a.failLocationRequest))
	mux.HandleFunc("POST /api/location-requests/{id}/decision", auth(a.decideLocationRequest))
	mux.HandleFunc("POST /api/location-requests/{id}/respond", auth(a.respondLocationRequest))
	mux.HandleFunc("GET /api/devices/{id}/location-history", auth(a.getLocationHistorySettings))
	mux.HandleFunc("PATCH /api/devices/{id}/location-history", auth(a.setLocationHistorySettings))
	mux.HandleFunc("POST /api/devices/{id}/location-history", auth(a.uploadLocationHistory))
	mux.HandleFunc("GET /api/location-history/{user}", auth(a.getLocationHistory))
	mux.HandleFunc("DELETE /api/location-history", auth(a.deleteLocationHistory))
	mux.HandleFunc("GET /api/location-preferences", auth(a.getLocationPreferences))
	mux.HandleFunc("PATCH /api/location-preferences", auth(a.setLocationPreferences))
	mux.HandleFunc("GET /api/app-updates/android", auth(a.availableAPK))
	mux.HandleFunc("GET /api/app-updates/android/{file}", auth(a.downloadAPK))
	mux.HandleFunc("GET /api/app-updates/deployment", auth(a.apkDeployment))
	mux.HandleFunc("DELETE /api/messages/{id}", auth(a.deleteMessage))
	mux.HandleFunc("DELETE /api/rooms/{room}/ai/messages", auth(a.clearAIConversation))
	mux.HandleFunc("DELETE /api/rooms/{room}/messages", auth(a.clearChatConversation))
	mux.HandleFunc("POST /api/rooms/{room}/files", auth(a.upload))
	mux.HandleFunc("GET /api/files/{id}", auth(a.download))
	mux.HandleFunc("GET /api/events", auth(a.events))
	mux.HandleFunc("GET /api/status", auth(a.status))
	mux.HandleFunc("GET /api/settings", auth(a.getSettings))
	mux.HandleFunc("POST /api/settings", auth(a.saveSettings))
	mux.HandleFunc("POST /api/settings/import-ini", auth(a.importINISettings))
	mux.HandleFunc("POST /api/settings/test-mail", auth(a.testMailSettings))
	mux.HandleFunc("POST /api/settings/test-ai", auth(a.testAISettings))
	mux.HandleFunc("POST /api/settings/test-remotecontrol", auth(a.testRemoteControlSettings))
	mux.HandleFunc("POST /api/settings/discover-mail", auth(a.discoverMailSettings))
	mux.HandleFunc("POST /api/settings/collect-mail", auth(a.collectMailSettings))
	mux.HandleFunc("GET /api/devices", auth(a.devices))
	mux.HandleFunc("GET /api/devices/{id}/location-sharing", auth(a.getDeviceLocationSharing))
	mux.HandleFunc("PATCH /api/devices/{id}/location-sharing", auth(a.setDeviceLocationSharing))
	mux.HandleFunc("GET /api/devices/{id}/notifications", auth(a.getDeviceNotifications))
	mux.HandleFunc("PATCH /api/devices/{id}/notifications", auth(a.setDeviceNotifications))
	mux.HandleFunc("POST /api/notifications/state", auth(a.notificationState))
	mux.HandleFunc("POST /api/devices", auth(a.registerDevice))
	mux.HandleFunc("DELETE /api/devices/{id}", auth(a.removeDevice))
	web, _ := fs.Sub(webFiles, "web")
	fileServer := http.FileServer(http.FS(web))
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" && r.URL.Path != "/location-history.js" && r.URL.Path != "/location-map.js" && r.URL.Path != "/conversation-auto.js" && r.URL.Path != "/navigation.js" && r.URL.Path != "/composer.js" && r.URL.Path != "/composer.css" && r.URL.Path != "/vendor/leaflet.js" && r.URL.Path != "/vendor/leaflet.css" && r.URL.Path != "/vendor/maplibre-gl-csp.js" && r.URL.Path != "/vendor/maplibre-gl-csp-worker.js" && r.URL.Path != "/vendor/maplibre-gl.css" && r.URL.Path != "/vendor/openfreemap-liberty-ko.json" && r.URL.Path != "/vendor/openfreemap-sprite.json" && r.URL.Path != "/vendor/openfreemap-sprite.png" && r.URL.Path != "/vendor/openfreemap-sprite@2x.json" && r.URL.Path != "/vendor/openfreemap-sprite@2x.png" && r.URL.Path != "/conversation-help.js" && r.URL.Path != "/settings.js" && r.URL.Path != "/messenger.js" && r.URL.Path != "/messenger.css" {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				log.Print("request panic recovered; details suppressed")
				respond(w, 500, map[string]string{"error": "서버 오류"})
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; worker-src 'self'; connect-src 'self' https://tiles.openfreemap.org/planet https://tiles.openfreemap.org/planet/ https://tiles.openfreemap.org/fonts/; img-src 'self' blob: data: https://tile.openstreetmap.org; frame-src blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if !a.validHost(r.Host) {
			respond(w, 403, map[string]string{"code": "HOST_NOT_ALLOWED", "error": "허용되지 않은 서버 주소입니다. 서버 설정의 외부 HTTPS 주소와 앱 주소를 맞추고, 프록시가 원래 Host 헤더를 전달하는지 확인하세요."})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			scheme := "http"
			if a.secureCookie(r) {
				scheme = "https"
			}
			if !sameWebOrigin(origin, scheme, r.Host) {
				respond(w, 403, map[string]string{"code": "ORIGIN_NOT_ALLOWED", "error": "다른 사이트의 요청은 허용하지 않습니다. HTTPS 프록시의 Host 전달과 서버 외부 주소 설정을 확인하세요."})
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-OnTech-Request") != "1" {
			fail(w, apiError{403, "요청 확인 헤더가 필요합니다."})
			return
		}
		if a.secureCookie(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		mux.ServeHTTP(w, r)
	})
}
func sortedUsers(st *State) []User {
	out := []User{}
	for _, u := range st.Users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}
