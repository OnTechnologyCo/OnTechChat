package server

import (
	"net/http"
	"sort"
)

type LocationGrant struct {
	Scope       string   `json:"scope,omitempty"`
	Enabled     bool     `json:"enabled"`
	Allowed     []string `json:"allowed_user_ids"`
	Revision    string   `json:"revision"`
	SessionHash string   `json:"session_hash"`
	Created     int64    `json:"created_at"`
	Updated     int64    `json:"updated_at"`
	LastSent    int64    `json:"last_sent_at,omitempty"`
}

func activeLocationGrant(st *State, d Device) bool {
	g := d.LocationSharing
	if g == nil || !g.Enabled || g.Revision == "" || g.SessionHash == "" || d.NotificationDelivery != "app" || len(d.Token) < 20 {
		return false
	}
	u, ok := st.Users[d.UserID]
	se, valid := st.Sessions[g.SessionHash]
	return ok && !u.Disabled && !u.LocationRequestsDisabled && valid && se.UserID == d.UserID && se.active()
}

func grantAllows(st *State, d Device, requester string) bool {
	if !activeLocationGrant(st, d) {
		return false
	}
	// 친구 목록과 동일하게 현재 등록된 활성 계정을 매 요청마다 확인합니다.
	if d.LocationSharing.Scope == "all_friends" {
		u, ok := st.Users[requester]
		return ok && !u.Disabled && requester != d.UserID
	}
	if d.LocationSharing.Scope != "" {
		return false
	}
	// 기존 기기의 선택 공유 범위는 본인이 변경하기 전까지 넓히지 않습니다.
	for _, id := range d.LocationSharing.Allowed {
		if id != requester {
			continue
		}
		u, ok := st.Users[id]
		return ok && !u.Disabled && id != d.UserID
	}
	return false
}

func selectAutoLocationDevice(st *State, target, requester string) (Device, bool) {
	var candidates []Device
	for _, d := range st.Devices {
		if d.UserID == target && grantAllows(st, d, requester) {
			candidates = append(candidates, d)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Updated == candidates[j].Updated {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Updated > candidates[j].Updated
	})
	if len(candidates) == 0 {
		return Device{}, false
	}
	return candidates[0], true
}
func autoRequestAllowed(st *State, q LocationRequest) bool {
	if q.AutoDevice == "" || q.AutoRevision == "" {
		return false
	}
	d, ok := st.Devices[q.AutoDevice]
	return ok && d.UserID == q.TargetID && grantAllows(st, d, q.RequesterID) && d.LocationSharing.Revision == q.AutoRevision
}

func cancelAutoRequestsForDevice(st *State, device string) []string {
	rooms := map[string]bool{}
	for id, q := range st.LocationRequests {
		if q.AutoDevice != device || q.Status != "pending" || q.Expires <= now() {
			continue
		}
		q.Status = "cancelled"
		st.LocationRequests[id] = q
		removeLocationRequestPushes(st, q)
		rooms[q.RoomID] = true
	}
	out := make([]string, 0, len(rooms))
	for room := range rooms {
		out = append(out, room)
	}
	sort.Strings(out)
	return out
}

func locationGrantResult(a *App, d Device) map[string]any {
	allowed := []string{}
	scope := "all_friends"
	enabled, revision, created, updated, lastSent := false, "", int64(0), int64(0), int64(0)
	if g := d.LocationSharing; g != nil && g.Enabled {
		enabled = true
		scope = g.Scope
		if scope == "" {
			scope = "selected_users"
		}
		allowed = append(allowed, g.Allowed...)
		revision, created, updated, lastSent = g.Revision, g.Created, g.Updated, g.LastSent
	}
	sort.Strings(allowed)
	return map[string]any{
		"device_id": d.ID, "enabled": enabled, "allowed_user_ids": allowed, "scope": scope, "supported_scope": "all_friends",
		"revision": revision, "created_at": created, "updated_at": updated, "last_sent_at": lastSent,
		"requires_foreground_service": false, "foreground_service_during_request": true, "background_supported": true, "push_enabled": a.cfg.FCM.Enabled,
	}
}

func (a *App) getDeviceLocationSharing(w http.ResponseWriter, r *http.Request, u User) {
	var out map[string]any
	e := a.store.view(func(st *State) error {
		d, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if d.UserID != u.ID {
			return forbidden()
		}
		out = locationGrantResult(a, d)
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}

func (a *App) setDeviceLocationSharing(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Scope   string   `json:"scope"`
		Enabled *bool    `json:"enabled"`
		Allowed []string `json:"allowed_user_ids"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Enabled == nil {
		fail(w, bad("내 위치 공유 사용 여부를 지정하세요."))
		return
	}
	if *in.Enabled && !a.cfg.FCM.Enabled {
		fail(w, apiError{409, "자동 위치 응답을 사용하려면 서버의 FCM 알림을 먼저 설정하세요."})
		return
	}
	if *in.Enabled && in.Scope != "" && in.Scope != "all_friends" {
		fail(w, bad("위치 공유 범위는 모든 친구만 설정할 수 있습니다."))
		return
	}
	sessionHash := digest(tokenFrom(r))
	rooms := []string{}
	var out map[string]any
	e := a.store.update(func(st *State) error {
		d, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if d.UserID != u.ID {
			return forbidden()
		}
		if !*in.Enabled {
			d.LocationSharing = nil
			st.Devices[d.ID] = d
			rooms = cancelAutoRequestsForDevice(st, d.ID)
			out = locationGrantResult(a, d)
			return nil
		}
		if d.NotificationDelivery != "app" || len(d.Token) < 20 {
			return bad("이 휴대전화의 FCM 기기 등록을 먼저 완료하세요.")
		}
		se, ok := st.Sessions[sessionHash]
		if !ok || se.UserID != u.ID || !se.active() {
			return apiError{401, "로그인이 만료되었습니다."}
		}
		seen := map[string]bool{}
		allowed := make([]string, 0, len(in.Allowed))
		if in.Scope == "all_friends" {
			in.Allowed = nil
		}
		for _, id := range in.Allowed {
			if seen[id] || id == u.ID {
				continue
			}
			v, ok := st.Users[id]
			if !ok || v.Disabled {
				return bad("공유 대상에 사용할 수 없는 사용자가 포함되어 있습니다.")
			}
			seen[id] = true
			allowed = append(allowed, id)
		}
		if in.Scope != "all_friends" && len(allowed) == 0 {
			return bad("현재 위치를 확인할 사용자를 한 명 이상 선택하세요.")
		}
		sort.Strings(allowed)
		created, lastSent := now(), int64(0)
		if old := d.LocationSharing; old != nil {
			if old.Created > 0 {
				created = old.Created
			}
			lastSent = old.LastSent
		}
		rooms = cancelAutoRequestsForDevice(st, d.ID)
		me := st.Users[u.ID]
		me.LocationRequestsDisabled = false
		st.Users[u.ID] = me
		d.LocationSharing = &LocationGrant{Enabled: true, Scope: in.Scope, Allowed: allowed, Revision: randomID(),
			SessionHash: sessionHash, Created: created, Updated: now(), LastSent: lastSent}
		st.Devices[d.ID] = d
		out = locationGrantResult(a, d)
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	for _, room := range rooms {
		a.changed(room)
	}
	respond(w, 200, out)
}
