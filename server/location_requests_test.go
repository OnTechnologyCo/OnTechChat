package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func makeRequest(t *testing.T, s *httptest.Server, token, target, client string) (map[string]any, int) {
	t.Helper()
	code, b, _ := request(t, s, token, "POST", "/api/location-requests", map[string]any{"user_id": target, "client_id": client, "confirmed": true})
	return decodeMap(t, b), code
}
func responseFix() map[string]any {
	return map[string]any{"latitude": 37.12345, "longitude": 127.12345, "accuracy_m": 20, "captured_at": now(), "approximate": false, "confirmed": true, "client_id": "location-response-test-001"}
}
func TestLocationRequestConsentAndPrivacy(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	var messages, rooms int
	a.store.view(func(st *State) error { messages, rooms = len(st.Messages), len(st.Rooms); return nil })
	c, b, _ := request(t, s, "", "POST", "/api/location-requests", map[string]any{"user_id": bob.ID, "client_id": "request-test-001", "confirmed": true})
	expectStatus(t, c, 401, b)
	c, b, _ = request(t, s, at, "POST", "/api/location-requests", map[string]any{"user_id": bob.ID, "client_id": "request-test-001", "confirmed": false})
	expectStatus(t, c, 400, b)
	q, c := makeRequest(t, s, at, bob.ID, "request-test-001")
	if c != 201 || q["status"] != "pending" || q["popup"] != true || q["location"] != nil {
		t.Fatalf("%d %v", c, q)
	}
	id := q["id"].(string)
	c, b, _ = request(t, s, et, "GET", "/api/location-requests/"+id, nil)
	expectStatus(t, c, 403, b)
	c, b, _ = request(t, s, at, "POST", "/api/location-requests/"+id+"/respond", responseFix())
	expectStatus(t, c, 403, b)
	fix := responseFix()
	fix["confirmed"] = false
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", fix)
	expectStatus(t, c, 400, b)
	fix = responseFix()
	fix["captured_at"] = now() - 60
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", fix)
	expectStatus(t, c, 400, b)
	again, c := makeRequest(t, s, at, bob.ID, "request-test-002")
	if c != 200 || again["id"] != id {
		t.Fatal("duplicate pending request", again)
	}
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", responseFix())
	expectStatus(t, c, 200, b)
	if decodeMap(t, b)["status"] != "responded" {
		t.Fatal("response missing")
	}
	// Repeated responses must preserve the first coordinates.
	fix = responseFix()
	fix["latitude"] = 38.0
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", fix)
	expectStatus(t, c, 200, b)
	for _, token := range []string{at, bt} {
		c, b, _ = request(t, s, token, "GET", "/api/location-requests/"+id, nil)
		expectStatus(t, c, 200, b)
		p := decodeMap(t, b)["location"].(map[string]any)
		if p["latitude"] != 37.12345 {
			t.Fatal("location overwritten", p)
		}
	}
	a.store.view(func(st *State) error {
		if len(st.Messages) != messages || len(st.Rooms) != rooms {
			t.Fatal("lookup created chat content")
		}
		raw, _ := json.Marshal(st)
		var restored State
		if e := json.Unmarshal(raw, &restored); e != nil {
			t.Fatal(e)
		}
		if restored.LocationRequests[id].Result == nil {
			t.Fatal("result not persisted")
		}
		return nil
	})
}
func TestLocationRequestDeclineCancelExpirePreferences(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	q, c := makeRequest(t, s, at, bob.ID, "request-decline-001")
	if c != 201 {
		t.Fatal(q)
	}
	id := q["id"].(string)
	c, b, _ := request(t, s, at, "POST", "/api/location-requests/"+id+"/decision", map[string]string{"decision": "decline"})
	expectStatus(t, c, 403, b)
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/decision", map[string]string{"decision": "decline"})
	expectStatus(t, c, 200, b)
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", responseFix())
	expectStatus(t, c, 409, b)
	q, c = makeRequest(t, s, at, eve.ID, "request-cancel-001")
	if c != 201 {
		t.Fatal(q)
	}
	id = q["id"].(string)
	c, b, _ = request(t, s, at, "POST", "/api/location-requests/"+id+"/decision", map[string]string{"decision": "cancel"})
	expectStatus(t, c, 200, b)
	c, b, _ = request(t, s, et, "POST", "/api/location-requests/"+id+"/respond", responseFix())
	expectStatus(t, c, 409, b)
	q, c = makeRequest(t, s, bt, eve.ID, "request-expired-001")
	if c != 201 {
		t.Fatal(q)
	}
	id = q["id"].(string)
	a.store.update(func(st *State) error {
		v := st.LocationRequests[id]
		v.Expires = now() - 1
		st.LocationRequests[id] = v
		return nil
	})
	c, b, _ = request(t, s, et, "POST", "/api/location-requests/"+id+"/respond", responseFix())
	expectStatus(t, c, 409, b)
	c, b, _ = request(t, s, bt, "GET", "/api/location-requests/"+id, nil)
	expectStatus(t, c, 200, b)
	if decodeMap(t, b)["status"] != "expired" || decodeMap(t, b)["location"] != nil {
		t.Fatal("expiry not enforced")
	}
	c, b, _ = request(t, s, bt, "PATCH", "/api/location-preferences", map[string]bool{"allow_requests": false})
	expectStatus(t, c, 200, b)
	_, c = makeRequest(t, s, et, bob.ID, "request-optout-001")
	if c != 403 {
		t.Fatal("optout ignored")
	}
}
func TestLocationLookupPendingAndFailure(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	device := "popup-location-device-001"
	grant := enableAllFriendsGrantForTest(t, a, s, bt, device)
	q, c := makeRequest(t, s, at, bob.ID, "popup-pending-001")
	if c != 201 {
		t.Fatal(q)
	}
	id := q["id"].(string)
	c, b, _ := request(t, s, bt, "GET", "/api/location-requests?device_id="+device, nil)
	expectStatus(t, c, 200, b)
	if len(decodeMap(t, b)["requests"].([]any)) != 1 {
		t.Fatal("FCM fallback request missing")
	}
	c, b, _ = request(t, s, et, "GET", "/api/location-requests?device_id="+device, nil)
	expectStatus(t, c, 403, b)
	a.store.view(func(st *State) error {
		found := false
		for _, p := range st.Outbox {
			if p.LocationRequestID == id && p.DeviceID == device && p.MessageID == 0 {
				found = true
			}
		}
		if !found {
			t.Fatal("independent location push missing")
		}
		return nil
	})
	failure := map[string]any{"device_id": device, "grant_revision": grant["revision"], "code": "background_restricted"}
	c, b, _ = request(t, s, at, "POST", "/api/location-requests/"+id+"/fail", failure)
	expectStatus(t, c, 403, b)
	c, b, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/fail", failure)
	expectStatus(t, c, 200, b)
	c, b, _ = request(t, s, at, "GET", "/api/location-requests/"+id, nil)
	expectStatus(t, c, 200, b)
	if decodeMap(t, b)["status"] != "failed" || decodeMap(t, b)["failure"] != "background_restricted" {
		t.Fatal("failure hidden")
	}
}
