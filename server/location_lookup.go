package server

import (
	"net/http"
	"sort"
)

func removeLocationRequestPushes(st *State, q LocationRequest) {
	for id, p := range st.Outbox {
		if p.LocationRequestID == q.ID || (q.MessageID != 0 && p.MessageID == q.MessageID) {
			delete(st.Outbox, id)
		}
	}
}
func (a *App) pendingLocationRequests(w http.ResponseWriter, r *http.Request, u User) {
	device := r.URL.Query().Get("device_id")
	out := []map[string]any{}
	e := a.store.view(func(st *State) error {
		d, ok := st.Devices[device]
		if !ok || d.UserID != u.ID {
			return forbidden()
		}
		var requests []LocationRequest
		for _, q := range st.LocationRequests {
			if q.TargetID == u.ID && locationRequestStatus(st, q) == "pending" && (q.AutoDevice == "" || q.AutoDevice == device) {
				requests = append(requests, q)
			}
		}
		sort.Slice(requests, func(i, j int) bool { return requests[i].Created < requests[j].Created })
		for _, q := range requests {
			out = append(out, publicLocationRequest(st, q))
			if len(out) == 10 {
				break
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]any{"requests": out})
}
func (a *App) failLocationRequest(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		DeviceID string `json:"device_id"`
		Revision string `json:"grant_revision"`
		Code     string `json:"code"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	switch in.Code {
	case "permission", "location_off", "timeout", "background_restricted", "unavailable":
	default:
		fail(w, bad("위치 오류 형식이 올바르지 않습니다."))
		return
	}
	e := a.store.update(func(st *State) error {
		q, e := authorizedLocationRequest(st, r.PathValue("id"), u.ID)
		if e != nil {
			return e
		}
		if q.TargetID != u.ID {
			return forbidden()
		}
		if q.AutoDevice != "" && (q.AutoDevice != in.DeviceID || q.AutoRevision != in.Revision || !autoRequestAllowed(st, q)) {
			return forbidden()
		}
		if locationRequestStatus(st, q) != "pending" {
			return errUnchanged
		}
		q.Status, q.Failure = "failed", in.Code
		st.LocationRequests[q.ID] = q
		removeLocationRequestPushes(st, q)
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
