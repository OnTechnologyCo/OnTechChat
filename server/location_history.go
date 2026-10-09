package server

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const historyRetention = int64(2 * 86400)

type LocationHistoryGrant struct {
	FollowSharing bool   `json:"follow_sharing,omitempty"`
	Enabled       bool   `json:"enabled"`
	Share         bool   `json:"share_with_friends"`
	Interval      int    `json:"interval_seconds"`
	Revision      string `json:"revision"`
	SessionHash   string `json:"session_hash"`
	Since         int64  `json:"since"`
}
type LocationHistoryPoint struct {
	ID          string  `json:"id"`
	DeviceID    string  `json:"device_id,omitempty"`
	Revision    string  `json:"revision,omitempty"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Accuracy    float64 `json:"accuracy_m"`
	Captured    int64   `json:"captured_at"`
	Approximate bool    `json:"approximate"`
}

func historyGrantActive(st *State, d Device) bool {
	g := d.LocationHistory
	if g == nil || !g.Enabled || (g.FollowSharing && !activeLocationGrant(st, d)) {
		return false
	}
	u, ok := st.Users[d.UserID]
	se, valid := st.Sessions[g.SessionHash]
	return ok && !u.Disabled && valid && se.UserID == d.UserID && se.active()
}
func historyView(d Device) map[string]any {
	g := d.LocationHistory
	if g == nil {
		g = &LocationHistoryGrant{Interval: 60}
	}
	return map[string]any{"enabled": g.Enabled, "share_with_friends": g.Share, "interval_seconds": g.Interval, "revision": g.Revision, "retention_days": 2}
}
func pruneLocationHistory(st *State) bool {
	changed := false
	for owner, points := range st.LocationHistory {
		kept := points[:0]
		for _, p := range points {
			_, ok := st.Users[owner]
			if p.Captured <= now()-historyRetention || !ok {
				changed = true
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) == 0 {
			delete(st.LocationHistory, owner)
		} else {
			st.LocationHistory[owner] = kept
		}
	}
	return changed
}
func (a *App) getLocationHistorySettings(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var out map[string]any
	e := a.store.view(func(st *State) error {
		d, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if d.UserID != u.ID {
			return forbidden()
		}
		out = historyView(d)
		if d.LocationHistory != nil && d.LocationHistory.FollowSharing && !historyGrantActive(st, d) {
			out["enabled"] = false
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
func (a *App) setLocationHistorySettings(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var in struct {
		Enabled       bool `json:"enabled"`
		Share         bool `json:"share_with_friends"`
		FollowSharing bool `json:"follow_sharing"`
		Interval      int  `json:"interval_seconds"`
		Confirmed     bool `json:"confirmed"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Interval == 0 {
		in.Interval = 60
	}
	if in.Interval != 60 {
		fail(w, bad("기록 간격은 1분입니다."))
		return
	}
	if in.Enabled && !in.Confirmed {
		fail(w, bad("이 기기에서 이동 기록 수집에 동의해야 합니다."))
		return
	}
	var out map[string]any
	pruned := false
	e := a.store.update(func(st *State) error {
		d, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if d.UserID != u.ID {
			return forbidden()
		}
		if in.Enabled && (in.Share || in.FollowSharing) && !activeLocationGrant(st, d) {
			return bad("친구에게 경로를 공유하려면 먼저 내 위치 조회를 허용하세요.")
		}
		hash := digest(tokenFrom(r))
		se, ok := st.Sessions[hash]
		if !ok || se.UserID != u.ID || !se.active() {
			return forbidden()
		}
		d.LocationHistory = &LocationHistoryGrant{Enabled: in.Enabled, Share: in.Enabled && in.Share, FollowSharing: in.FollowSharing, Interval: in.Interval, Revision: randomID(), SessionHash: hash, Since: now()}
		st.Devices[d.ID] = d
		out = historyView(d)
		pruned = pruneLocationHistory(st)
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if pruned {
		a.store.discardOldBackup()
	}
	respond(w, 200, out)
}
func validHistoryPoint(p LocationHistoryPoint, since int64) bool {
	return fileIDPattern.MatchString(p.ID) && len(p.ID) <= 64 &&
		!math.IsNaN(p.Latitude) && !math.IsInf(p.Latitude, 0) && p.Latitude >= -90 && p.Latitude <= 90 &&
		!math.IsNaN(p.Longitude) && !math.IsInf(p.Longitude, 0) && p.Longitude >= -180 && p.Longitude <= 180 &&
		!math.IsNaN(p.Accuracy) && !math.IsInf(p.Accuracy, 0) && p.Accuracy > 0 && p.Accuracy <= 20000 &&
		p.Captured > now()-historyRetention && p.Captured >= since-30 && p.Captured <= now()+30
}
func (a *App) uploadLocationHistory(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	var in struct {
		Revision string                 `json:"revision"`
		Points   []LocationHistoryPoint `json:"points"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if len(in.Points) < 1 || len(in.Points) > 100 {
		fail(w, bad("한 번에 1~100개 위치를 전송하세요."))
		return
	}
	if !a.limiter.allow("history-upload:"+u.ID, 60, time.Minute) {
		fail(w, apiError{429, "잠시 후 다시 전송하세요."})
		return
	}
	pruned := false
	e := a.store.update(func(st *State) error {
		d, ok := st.Devices[r.PathValue("id")]
		if !ok {
			return notfound()
		}
		if d.UserID != u.ID || !historyGrantActive(st, d) || d.LocationHistory.SessionHash != digest(tokenFrom(r)) {
			return forbidden()
		}
		g := d.LocationHistory
		if g.Revision != in.Revision {
			return apiError{409, "기록 설정이 변경되었습니다. 기기에서 다시 시작하세요."}
		}
		pruned = pruneLocationHistory(st)
		if st.LocationHistory == nil {
			st.LocationHistory = map[string][]LocationHistoryPoint{}
		}
		points := st.LocationHistory[u.ID]
		seen := map[string]LocationHistoryPoint{}
		for _, p := range points {
			seen[p.DeviceID+"/"+p.ID] = p
		}
		for _, p := range in.Points {
			if !validHistoryPoint(p, g.Since) || p.DeviceID != "" || p.Revision != "" {
				return bad("위치 좌표·측정 시각·식별자를 확인하세요.")
			}
			p.DeviceID = d.ID
			p.Revision = g.Revision
			if old, exists := seen[d.ID+"/"+p.ID]; exists {
				if old != p {
					return apiError{409, "동일 위치 ID의 내용이 다릅니다."}
				}
				continue
			}
			points = append(points, p)
			seen[d.ID+"/"+p.ID] = p
		}
		if len(points) > 6000 {
			return apiError{429, "최근 2일 위치 기록 한도에 도달했습니다."}
		}
		count := len(points)
		for owner, rows := range st.LocationHistory {
			if owner != u.ID {
				count += len(rows)
			}
		}
		if count > 50000 {
			return apiError{429, "서버 위치 보관 한도에 도달했습니다. 기록 삭제 후 다시 시도하세요."}
		}
		sort.Slice(points, func(i, j int) bool {
			if points[i].Captured == points[j].Captured {
				return points[i].ID < points[j].ID
			}
			return points[i].Captured < points[j].Captured
		})
		st.LocationHistory[u.ID] = points
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if pruned {
		a.store.discardOldBackup()
	}
	respond(w, 200, map[string]any{"accepted": len(in.Points)})
}
func (a *App) getLocationHistory(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	target := r.PathValue("user")
	from, e1 := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	to, e2 := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if e1 != nil || e2 != nil || from < 0 || to < from || to-from > historyRetention {
		fail(w, bad("최근 48시간 이내의 조회 범위를 지정하세요."))
		return
	}
	points := []LocationHistoryPoint{}
	e := a.store.view(func(st *State) error {
		person, ok := st.Users[target]
		if !ok || person.Disabled {
			return notfound()
		}
		devices := map[string]bool{}
		for id, d := range st.Devices {
			if d.UserID == target && historyGrantActive(st, d) && d.LocationHistory.Share && grantAllows(st, d, u.ID) {
				devices[id] = true
			}
		}
		if target != u.ID && len(devices) == 0 {
			return forbidden()
		}
		for _, p := range st.LocationHistory[target] {
			if p.Captured < from || p.Captured > to || p.Captured <= now()-historyRetention {
				continue
			}
			d, exists := st.Devices[p.DeviceID]
			if target != u.ID && (!exists || d.UserID != target) {
				continue
			}
			if target != u.ID && (!devices[p.DeviceID] || d.LocationHistory.Revision != p.Revision) {
				continue
			}
			p.Revision = ""
			points = append(points, p)
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	truncated := len(points) > 3000
	if truncated {
		points = points[:3000]
	}
	respond(w, 200, map[string]any{"points": points, "count": len(points), "truncated": truncated, "retention_days": 2, "from": from, "to": to})
}
func (a *App) deleteLocationHistory(w http.ResponseWriter, r *http.Request, u User) {
	noHelpCache(w)
	e := a.store.update(func(st *State) error {
		delete(st.LocationHistory, u.ID)
		for id, d := range st.Devices {
			if d.UserID == u.ID {
				d.LocationSharing = nil
				st.Devices[id] = d
			}
			if d.UserID == u.ID && d.LocationHistory != nil {
				g := *d.LocationHistory
				g.Enabled = false
				g.Share = false
				g.Revision = randomID()
				g.Since = now()
				d.LocationHistory = &g
				st.Devices[id] = d
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.store.discardOldBackup()
	respond(w, 200, map[string]any{"ok": true, "recording_stopped": true})
}
