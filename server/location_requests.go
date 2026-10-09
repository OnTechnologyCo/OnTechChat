package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// 서버는 GPS를 직접 조회하지 않습니다. 수동 응답은 매 요청 확인이 필요하며,
// 자동 응답은 대상 사용자가 자신의 등록 기기에서 미리 켠 허용 목록과 활성 세션을 매번 재검증합니다.
type LocationRequest struct {
	Popup        bool            `json:"popup,omitempty"`
	Result       *SharedLocation `json:"result,omitempty"`
	Failure      string          `json:"failure,omitempty"`
	ID           string          `json:"id"`
	RequesterID  string          `json:"requester_id"`
	TargetID     string          `json:"target_id"`
	RoomID       string          `json:"room_id"`
	Status       string          `json:"status"`
	AutoDevice   string          `json:"auto_device_id,omitempty"`
	AutoRevision string          `json:"auto_revision,omitempty"`
	Created      int64           `json:"created_at"`
	Expires      int64           `json:"expires_at"`
	MessageID    int64           `json:"message_id"`
	ResponseID   int64           `json:"response_message_id,omitempty"`
	ClientID     string          `json:"client_id,omitempty"`
}

func locationRequestStatus(st *State, q LocationRequest) string {
	if q.Popup {
		if q.Expires <= now() {
			return "expired"
		}
		if st.Users[q.TargetID].Disabled || st.Users[q.RequesterID].Disabled {
			return "unavailable"
		}
		if q.Status == "pending" && q.AutoDevice != "" && !autoRequestAllowed(st, q) {
			return "cancelled"
		}
		return q.Status
	}
	if q.Status != "pending" {
		return q.Status
	}
	if q.Expires <= now() {
		return "expired"
	}
	if m, ok := findNotificationMessage(st, q.MessageID); !ok || m.Kind == "deleted" {
		return "cancelled"
	}
	if st.Users[q.TargetID].Disabled || st.Users[q.RequesterID].Disabled {
		return "unavailable"
	}
	if _, ok := member(st, q.RoomID, q.TargetID); !ok {
		return "unavailable"
	}
	if _, ok := member(st, q.RoomID, q.RequesterID); !ok {
		return "unavailable"
	}
	return "pending"
}
func publicLocationRequest(st *State, q LocationRequest) map[string]any {
	auto := q.AutoDevice != "" && autoRequestAllowed(st, q)
	out := map[string]any{"id": q.ID, "requester_id": q.RequesterID, "target_id": q.TargetID, "room_id": q.RoomID, "status": locationRequestStatus(st, q), "created_at": q.Created, "expires_at": q.Expires, "message_id": q.MessageID, "response_message_id": q.ResponseID, "requester_name": displayName(st.Users[q.RequesterID]), "target_name": displayName(st.Users[q.TargetID]), "consent_required": !auto, "auto_response": auto}
	out["popup"], out["failure"] = q.Popup, q.Failure
	if q.Popup && locationRequestStatus(st, q) == "responded" {
		out["location"] = q.Result
	}
	if q.AutoDevice != "" {
		out["device_id"], out["grant_revision"] = q.AutoDevice, q.AutoRevision
	}
	return out
}
func currentLocationRequestText(st *State, m Message) string {
	var hint struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(m.Text), &hint) != nil {
		return `{}`
	}
	q, ok := st.LocationRequests[hint.ID]
	if !ok {
		return `{}`
	}
	view := publicLocationRequest(st, q)
	delete(view, "location") // Historical chat cards never carry the new popup coordinates.
	b, _ := json.Marshal(view)
	return string(b)
}
func authorizedLocationRequest(st *State, id, viewer string) (LocationRequest, error) {
	q, ok := st.LocationRequests[id]
	if !ok {
		return q, notfound()
	}
	if viewer != q.RequesterID && viewer != q.TargetID {
		return q, forbidden()
	}
	if _, ok := member(st, q.RoomID, viewer); !q.Popup && !ok {
		return q, forbidden()
	}
	return q, nil
}
func ensureLocationDirect(st *State, uid, peer string) (Room, error) {
	key := directKey(uid, peer)
	for _, room := range st.Rooms {
		if room.Kind == "direct" && room.DirectKey == key {
			if len(room.Members) != 2 {
				return Room{}, forbidden()
			}
			if _, ok := room.Members[uid]; !ok {
				return Room{}, forbidden()
			}
			if _, ok := room.Members[peer]; !ok {
				return Room{}, forbidden()
			}
			return room, nil
		}
	}
	if len(st.Rooms) >= 1000 {
		return Room{}, bad("방 수 제한에 도달했습니다.")
	}
	room := Room{ID: randomID(), DirectKey: key, Name: "1:1 대화", Kind: "direct", OwnerID: uid, Created: now(), Members: map[string]int64{uid: 0, peer: 0}}
	st.Rooms[room.ID] = room
	return room, nil
}
func (a *App) requestLocation(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		UserID    string `json:"user_id"`
		Confirmed bool   `json:"confirmed"`
		ClientID  string `json:"client_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !in.Confirmed || !clientIDPattern.MatchString(in.ClientID) || !fileIDPattern.MatchString(in.UserID) || in.UserID == u.ID {
		fail(w, bad("상대에게 위치 확인 요청을 보낼지 확인해 주세요."))
		return
	}
	if !a.limiter.allow("location-request:"+u.ID, 20, time.Hour) {
		fail(w, apiError{429, "위치 요청은 시간당 20회까지 보낼 수 있습니다."})
		return
	}
	var q LocationRequest
	var out map[string]any
	created := false
	e := a.store.update(func(st *State) error {
		me, mok := st.Users[u.ID]
		target, ok := st.Users[in.UserID]
		if !mok || me.Disabled || !ok || target.Disabled {
			return forbidden()
		}
		if target.LocationRequestsDisabled {
			return apiError{403, "상대가 위치 확인 요청을 받지 않도록 설정했습니다."}
		}
		if st.LocationRequests == nil {
			st.LocationRequests = map[string]LocationRequest{}
		}
		recent := 0
		pending := 0
		for _, v := range st.LocationRequests {
			if !v.Popup && v.RequesterID == u.ID && v.TargetID == in.UserID && v.Status == "pending" {
				v.Status = "cancelled"
				st.LocationRequests[v.ID] = v
				removeLocationRequestPushes(st, v)
				continue
			}
			if v.RequesterID == u.ID && v.TargetID == in.UserID && v.ClientID == in.ClientID {
				q = v
				out = publicLocationRequest(st, q)
				return errUnchanged
			}
			if v.RequesterID == u.ID && v.Created > now()-3600 {
				recent++
			}
			if v.TargetID == in.UserID && locationRequestStatus(st, v) == "pending" {
				pending++
			}
			if v.RequesterID == u.ID && v.TargetID == in.UserID {
				if locationRequestStatus(st, v) == "pending" {
					q = v
					out = publicLocationRequest(st, q)
					return errUnchanged
				}
				if v.Created > now()-60 {
					return apiError{429, "같은 친구에게는 잠시 후 다시 요청해 주세요."}
				}
			}
		}
		if recent >= 20 || pending >= 10 || len(st.LocationRequests) >= 2000 {
			return apiError{429, "위치 요청 한도에 도달했습니다. 잠시 후 시도해 주세요."}
		}
		q = LocationRequest{ID: randomID(), RequesterID: u.ID, TargetID: in.UserID, Popup: true, Status: "pending", Created: now(), Expires: now() + 120, ClientID: in.ClientID}
		if d, ok := selectAutoLocationDevice(st, in.UserID, u.ID); ok {
			q.AutoDevice, q.AutoRevision = d.ID, d.LocationSharing.Revision
		}
		if a.cfg.FCM.Enabled {
			for _, d := range st.Devices {
				if d.UserID != q.TargetID || d.NotificationDelivery != "app" || (q.AutoDevice != "" && d.ID != q.AutoDevice) {
					continue
				}
				id := randomID()
				st.Outbox[id] = Outbox{ID: id, LocationRequestID: q.ID, UserID: d.UserID, DeviceID: d.ID, Kind: "location_request", Due: now(), Expires: q.Expires, State: "pending"}
			}
		}
		st.LocationRequests[q.ID] = q
		out = publicLocationRequest(st, q)
		created = true
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if created && q.RoomID != "" {
		a.changed(q.RoomID)
	}
	status := 200
	if created {
		status = 201
	}
	respond(w, status, out)
}
func (a *App) getLocationRequest(w http.ResponseWriter, r *http.Request, u User) {
	var out map[string]any
	e := a.store.view(func(st *State) error {
		q, e := authorizedLocationRequest(st, r.PathValue("id"), u.ID)
		if e != nil {
			return e
		}
		out = publicLocationRequest(st, q)
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, out)
}
func (a *App) decideLocationRequest(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Decision string `json:"decision"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Decision != "decline" && in.Decision != "cancel" {
		fail(w, bad("거절 또는 취소만 가능합니다. 승인은 위치 확인 후 별도 전송합니다."))
		return
	}
	var out map[string]any
	var room string
	e := a.store.update(func(st *State) error {
		q, e := authorizedLocationRequest(st, r.PathValue("id"), u.ID)
		if e != nil {
			return e
		}
		if (in.Decision == "decline" && u.ID != q.TargetID) || (in.Decision == "cancel" && u.ID != q.RequesterID) {
			return forbidden()
		}
		if locationRequestStatus(st, q) != "pending" {
			return apiError{409, "이미 처리되거나 만료된 요청입니다."}
		}
		q.Status = "declined"
		if in.Decision == "cancel" {
			q.Status = "cancelled"
		}
		st.LocationRequests[q.ID] = q
		removeLocationRequestPushes(st, q)
		out = publicLocationRequest(st, q)
		room = q.RoomID
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	a.changed(room)
	respond(w, 200, out)
}
func removeRequestPushes(st *State, id int64) {
	for k, p := range st.Outbox {
		if p.MessageID == id {
			delete(st.Outbox, k)
		}
	}
}
func (a *App) respondLocationRequest(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Latitude      *float64 `json:"latitude"`
		Longitude     *float64 `json:"longitude"`
		Accuracy      float64  `json:"accuracy_m"`
		CapturedAt    int64    `json:"captured_at"`
		Approximate   bool     `json:"approximate"`
		Confirmed     bool     `json:"confirmed"`
		AutoResponse  bool     `json:"auto_response"`
		DeviceID      string   `json:"device_id"`
		GrantRevision string   `json:"grant_revision"`
		ClientID      string   `json:"client_id"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Latitude == nil || in.Longitude == nil || !clientIDPattern.MatchString(in.ClientID) {
		fail(w, bad("현재 위치 응답 형식을 확인해 주세요."))
		return
	}
	if in.AutoResponse {
		if !clientIDPattern.MatchString(in.DeviceID) || !fileIDPattern.MatchString(in.GrantRevision) {
			fail(w, bad("자동 위치 공유 승인이 유효하지 않습니다."))
			return
		}
	} else if !in.Confirmed {
		fail(w, bad("수신자와 내 위치를 확인하고 전송에 동의해 주세요."))
		return
	}
	p := SharedLocation{*in.Latitude, *in.Longitude, in.Accuracy, in.CapturedAt, in.Approximate}
	if !validLocation(p, now()) {
		fail(w, bad("현재 위치를 다시 측정해 주세요."))
		return
	}
	var out map[string]any
	var room string
	e := a.store.update(func(st *State) error {
		q, e := authorizedLocationRequest(st, r.PathValue("id"), u.ID)
		if e != nil {
			return e
		}
		if q.TargetID != u.ID {
			return forbidden()
		}
		if in.AutoResponse {
			if q.AutoDevice != in.DeviceID || q.AutoRevision != in.GrantRevision || !autoRequestAllowed(st, q) {
				return forbidden()
			}
		}
		// Repeated responses cannot overwrite the original coordinates.
		if q.Status == "responded" {
			out = publicLocationRequest(st, q)
			return errUnchanged
		}
		if locationRequestStatus(st, q) != "pending" {
			return apiError{409, "요청이 취소·거절·만료되었습니다. 위치를 보내지 않았습니다."}
		}
		if p.CapturedAt < q.Created-5 || p.CapturedAt < now()-30 {
			return bad("요청을 받은 뒤 새로 측정한 위치만 보낼 수 있습니다.")
		}
		q.Popup = true
		q.Result = &p
		q.Status = "responded"
		st.LocationRequests[q.ID] = q
		if in.AutoResponse {
			d := st.Devices[q.AutoDevice]
			if d.LocationSharing != nil {
				d.LocationSharing.LastSent = now()
				d.LocationSharing.Updated = now()
				st.Devices[d.ID] = d
			}
		}
		removeLocationRequestPushes(st, q)
		out = publicLocationRequest(st, q)
		room = q.RoomID
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if room != "" {
		a.changed(room)
	}
	respond(w, 200, out)
}
func (a *App) getLocationPreferences(w http.ResponseWriter, r *http.Request, u User) {
	respond(w, 200, map[string]any{"allow_requests": !u.LocationRequestsDisabled, "requires_confirmation": true, "background_tracking": false})
}
func (a *App) setLocationPreferences(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Allow *bool `json:"allow_requests"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Allow == nil {
		fail(w, bad("위치 요청 허용 여부를 지정하세요."))
		return
	}
	rooms := map[string]bool{}
	e := a.store.update(func(st *State) error {
		v, ok := st.Users[u.ID]
		if !ok || v.Disabled {
			return forbidden()
		}
		v.LocationRequestsDisabled = !*in.Allow
		st.Users[u.ID] = v
		if !*in.Allow {
			for id, q := range st.LocationRequests {
				if q.TargetID == u.ID && q.Status == "pending" {
					q.Status = "declined"
					st.LocationRequests[id] = q
					removeLocationRequestPushes(st, q)
					rooms[q.RoomID] = true
				}
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	for room := range rooms {
		a.changed(room)
	}
	respond(w, 200, map[string]bool{"allow_requests": *in.Allow, "requires_confirmation": true})
}
