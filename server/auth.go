package server

import (
	"net"
	"net/http"
	"strings"
	"time"
)

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.limiter.allow("login-ip:"+ip, 30, 5*time.Minute) {
		fail(w, apiError{429, "로그인 시도가 너무 많습니다. 5분 후 다시 시도해 주세요."})
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Client   string `json:"client"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if !usernamePattern.MatchString(in.Username) || len(in.Password) > 128 {
		fail(w, apiError{401, "아이디 또는 비밀번호가 올바르지 않습니다."})
		return
	}
	if !a.limiter.allow("login-name:"+in.Username, 10, 5*time.Minute) {
		fail(w, apiError{429, "로그인 시도가 너무 많습니다. 5분 후 다시 시도해 주세요."})
		return
	}
	select {
	case a.passwordSlots <- struct{}{}:
		defer func() { <-a.passwordSlots }()
	default:
		fail(w, apiError{429, "로그인 처리 중입니다. 잠시 후 다시 시도해 주세요."})
		return
	}
	var u User
	found := false
	_ = a.store.view(func(st *State) error { u, found = findUsername(st, in.Username); return nil })
	hash := a.dummyHash
	if found {
		hash = u.Hash
	}
	if !passwordMatches(in.Password, hash) || !found || u.Disabled {
		fail(w, apiError{401, "아이디 또는 비밀번호가 올바르지 않습니다."})
		return
	}
	token := randomToken()
	expiry := int64(0) // 직접 로그아웃하거나 계정에서 세션을 해제할 때까지 유지합니다.
	e := a.store.update(func(st *State) error {
		current, ok := st.Users[u.ID]
		if !ok || current.Disabled || current.Hash != u.Hash {
			return apiError{401, "다시 로그인해 주세요."}
		}
		for h, s := range st.Sessions {
			if !s.active() {
				delete(st.Sessions, h)
			}
		}
		count := 0

		for _, s := range st.Sessions {
			if s.UserID == u.ID {
				count++

			}
		}
		if count >= 64 {
			return apiError{429, "등록된 로그인이 많습니다. 사용하지 않는 기기에서 로그아웃한 뒤 다시 시도하세요."}
		}
		st.Sessions[digest(token)] = Session{UserID: u.ID, Created: now(), Expires: expiry, Persistent: true}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.setSessionCookie(w, r, token)
	out := map[string]any{"user": publicUser(u), "expires_at": expiry}
	if in.Client == "native" {
		out["token"] = token
	}
	respond(w, 200, out)
}
func (a *App) logout(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		DeviceID string `json:"device_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	rooms := []string{}
	e := a.store.update(func(st *State) error {
		if in.DeviceID != "" {
			if d, ok := st.Devices[in.DeviceID]; ok {
				if d.UserID != u.ID {
					return forbidden()
				}
				rooms = cancelAutoRequestsForDevice(st, in.DeviceID)
				delete(st.Devices, in.DeviceID)
			}
			for key, p := range st.Outbox {
				if p.DeviceID == in.DeviceID && p.UserID == u.ID {
					delete(st.Outbox, key)
				}
			}
		}
		delete(st.Sessions, digest(tokenFrom(r)))
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ontech_session", Value: "", Path: "/", HttpOnly: true, Secure: a.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: -1})
	for _, room := range rooms {
		a.changed(room)
	}
	a.hub.publish([]string{u.ID}, "")
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request, u User) {
	if !a.limiter.allow("pw:"+u.ID, 5, 5*time.Minute) {
		fail(w, apiError{429, "잠시 후 다시 시도해 주세요."})
		return
	}
	var in struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !passwordMatches(in.Current, u.Hash) {
		fail(w, apiError{401, "현재 비밀번호가 올바르지 않습니다."})
		return
	}
	hash, e := passwordHash(in.Password)
	if e != nil {
		fail(w, bad(passwordPolicyMessage))
		return
	}
	e = a.store.update(func(st *State) error {
		current := st.Users[u.ID]
		if current.Hash != u.Hash {
			return bad("계정이 변경되었습니다. 다시 로그인해 주세요.")
		}
		current.Hash = hash
		st.Users[u.ID] = current
		for h, se := range st.Sessions {
			if se.UserID == u.ID {
				delete(st.Sessions, h)
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.hub.publish([]string{u.ID}, "")
	respond(w, 200, map[string]bool{"ok": true, "login_required": true})
}
func (a *App) users(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	e := a.store.view(func(st *State) error {
		for _, v := range sortedUsers(st) {
			switch {
			case isSuperadmin(u):
				out = append(out, publicUser(v))
			case u.Role == roleAdmin && canManageUser(st, u, v):
				out = append(out, publicUser(v))
			case !v.Disabled:
				out = append(out, publicProfile(v))
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
func (a *App) createUser(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if !usernamePattern.MatchString(in.Username) {
		fail(w, bad(usernamePolicyMessage))
		return
	}
	hash, e := passwordHash(in.Password)
	if e != nil {
		fail(w, bad(passwordPolicyMessage))
		return
	}
	v := User{ID: randomID(), Username: in.Username, Hash: hash, Role: roleUser, Created: now()}
	e = a.store.update(func(st *State) error {
		if len(st.Users) >= 100 {
			return bad("최대 사용자 수는 100명입니다.")
		}
		if _, ok := findUsername(st, v.Username); ok {
			return apiError{409, "이미 사용 중인 아이디입니다."}
		}
		addUser(st, v)
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.directoryChanged()
	respond(w, 201, publicUser(v))
}
func (a *App) editUser(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	if id == u.ID {
		fail(w, bad("자신의 계정은 이 기능으로 변경할 수 없습니다."))
		return
	}
	var in struct {
		Disabled     *bool  `json:"disabled,omitempty"`
		Password     string `json:"password,omitempty"`
		AgentEnabled *bool  `json:"agent_enabled,omitempty"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	hash := ""
	if in.Password != "" {
		var e error
		hash, e = passwordHash(in.Password)
		if e != nil {
			fail(w, bad(passwordPolicyMessage))
			return
		}
	}
	cancelAgent := false
	e := a.store.update(func(st *State) error {
		actor, ok := st.Users[u.ID]
		if !ok || actor.Disabled {
			return forbidden()
		}
		v, ok := st.Users[id]
		if !ok {
			return notfound()
		}
		if !canManageUser(st, actor, v) {
			return forbidden()
		}
		if in.Disabled != nil && *in.Disabled && v.Role == roleSuperadmin && countEnabledSuperadmins(st, v.ID) == 0 {
			return bad("마지막 최고관리자는 사용 중지할 수 없습니다.")
		}
		if in.Disabled != nil {
			v.Disabled = *in.Disabled
		}
		if in.AgentEnabled != nil {
			v.AgentEnabled = *in.AgentEnabled
			if !v.AgentEnabled {
				cancelAgent = true
			}
		}
		if hash != "" {
			v.Hash = hash
		}
		st.Users[id] = v
		for h, se := range st.Sessions {
			if se.UserID == id {
				delete(st.Sessions, h)
			}
		}
		if v.Disabled {
			cancelAgent = true
			for k, d := range st.Devices {
				if d.UserID == id {
					delete(st.Devices, k)
				}
			}
		}
		appendAudit(st, AuditEvent{ActorID: actor.ID, Action: "user.update", ResourceType: "user", ResourceID: id, Result: "allowed"})
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if cancelAgent && a.agentJobs != nil {
		a.agentJobs.cancelUnauthorized()
	}
	a.directoryChanged()
	a.hub.publish([]string{id}, "")
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) changeUserRole(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	id := r.PathValue("id")
	if id == u.ID {
		fail(w, bad("자신의 역할은 이 화면에서 변경할 수 없습니다."))
		return
	}
	var in struct {
		Role            string `json:"role"`
		CurrentPassword string `json:"current_password"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !validRole(in.Role) {
		fail(w, bad("지원하지 않는 역할입니다."))
		return
	}
	if !passwordMatches(in.CurrentPassword, u.Hash) {
		fail(w, apiError{401, "현재 최고관리자 비밀번호를 확인하세요."})
		return
	}
	err := a.store.update(func(st *State) error {
		actor, ok := st.Users[u.ID]
		if !ok || actor.Disabled || !isSuperadmin(actor) || actor.Hash != u.Hash {
			return forbidden()
		}
		target, ok := st.Users[id]
		if !ok {
			return notfound()
		}
		if target.Role == roleSuperadmin && in.Role != roleSuperadmin &&
			!target.Disabled && countEnabledSuperadmins(st, target.ID) == 0 {
			return bad("마지막 최고관리자는 강등할 수 없습니다.")
		}
		target.Role = in.Role
		st.Users[id] = target
		for h, se := range st.Sessions {
			if se.UserID == id {
				delete(st.Sessions, h)
			}
		}
		appendAudit(st, AuditEvent{ActorID: actor.ID, Action: "user.role", ResourceType: "user", ResourceID: id, Result: "allowed"})
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if a.agentJobs != nil {
		a.agentJobs.cancelUnauthorized()
	}
	a.directoryChanged()
	a.hub.publish([]string{id}, "")
	respond(w, 200, map[string]bool{"ok": true, "login_required": true})
}
