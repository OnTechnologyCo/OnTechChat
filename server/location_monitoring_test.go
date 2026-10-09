package server

import (
	"net/http/httptest"
	"testing"
)

func enableLocationGrantForTest(t *testing.T, a *App, s *httptest.Server, token, device string, allowed []string) map[string]any {
	t.Helper()
	a.cfg.FCM.Enabled = true
	code, b, _ := request(t, s, token, "POST", "/api/devices", map[string]any{
		"id": device, "name": "test phone",
		"token":                 "synthetic-FCM-token-for-location-sharing",
		"notification_delivery": "app",
	})
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, token, "PATCH", "/api/devices/"+device+"/location-sharing",
		map[string]any{"enabled": true, "allowed_user_ids": allowed})
	expectStatus(t, code, 200, b)
	return decodeMap(t, b)
}

func TestLocationAutoSharingGrantAndResponse(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	et := loginTest(t, s, eve.Username)
	device := "android-location-test-001"
	grant := enableLocationGrantForTest(t, a, s, bt, device, []string{admin.ID})
	revision := grant["revision"].(string)
	if len(revision) != 32 || grant["enabled"] != true {
		t.Fatal("grant not enabled", grant)
	}

	code, body, _ := request(t, s, at, "GET", "/api/devices/"+device+"/location-sharing", nil)
	expectStatus(t, code, 403, body)

	q, code := makeRequest(t, s, at, bob.ID, "auto-request-admin-001")
	if code != 201 || q["auto_response"] != true || q["consent_required"] != false {
		t.Fatal("allowed requester did not get automatic request", code, q)
	}
	id := q["id"].(string)

	manual, code := makeRequest(t, s, et, bob.ID, "manual-request-eve-001")
	if code != 201 || manual["auto_response"] != false || manual["consent_required"] != true {
		t.Fatal("unlisted requester unexpectedly received automatic access", code, manual)
	}

	fix := responseFix()
	fix["confirmed"] = false
	fix["auto_response"] = true
	fix["device_id"] = device
	fix["grant_revision"] = "00000000000000000000000000000000"
	code, body, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", fix)
	expectStatus(t, code, 403, body)

	fix["grant_revision"] = revision
	code, body, _ = request(t, s, bt, "POST", "/api/location-requests/"+id+"/respond", fix)
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["status"] != "responded" {
		t.Fatal("automatic response not stored")
	}
	code, body, _ = request(t, s, bt, "GET", "/api/devices/"+device+"/location-sharing", nil)
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["last_sent_at"].(float64) <= 0 {
		t.Fatal("last sent timestamp missing")
	}
}

func TestLocationSharingDisableCancelsPendingAutomaticRequest(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	device := "android-location-test-002"
	grant := enableLocationGrantForTest(t, a, s, bt, device, []string{admin.ID})
	if grant["enabled"] != true {
		t.Fatal("grant not enabled")
	}

	q, code := makeRequest(t, s, at, bob.ID, "auto-request-cancel-001")
	if code != 201 || q["auto_response"] != true {
		t.Fatal("automatic request missing", code, q)
	}
	code, body, _ := request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-sharing",
		map[string]any{"enabled": false, "allowed_user_ids": []string{}})
	expectStatus(t, code, 200, body)

	code, body, _ = request(t, s, at, "GET", "/api/location-requests/"+q["id"].(string), nil)
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["status"] != "cancelled" {
		t.Fatal("pending automatic request was not cancelled")
	}
}

func enableAllFriendsGrantForTest(t *testing.T, a *App, s *httptest.Server, token, device string) map[string]any {
	t.Helper()
	a.cfg.FCM.Enabled = true
	code, body, _ := request(t, s, token, "POST", "/api/devices", map[string]any{
		"id": device, "name": "test phone", "token": "synthetic-FCM-token-all-friends", "notification_delivery": "app",
	})
	expectStatus(t, code, 200, body)
	code, body, _ = request(t, s, token, "GET", "/api/devices/"+device+"/location-sharing", nil)
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["enabled"] != false {
		t.Fatal("new device sharing must default off")
	}
	code, body, _ = request(t, s, token, "PATCH", "/api/devices/"+device+"/location-sharing",
		map[string]any{"enabled": true, "scope": "all_friends"})
	expectStatus(t, code, 200, body)
	result := decodeMap(t, body)
	if result["scope"] != "all_friends" || result["enabled"] != true || len(result["allowed_user_ids"].([]any)) != 0 {
		t.Fatal("all-friends grant must not store a snapshot allow-list", result)
	}
	return result
}

func TestAllFriendsLocationScopeIncludesNewFriendsAndRechecksMembership(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	device := "android-all-friends-test-001"
	grant := enableAllFriendsGrantForTest(t, a, s, bt, device)
	for i, token := range []string{at, et} {
		q, code := makeRequest(t, s, token, bob.ID, "all-friends-query-"+intString(int64(i)))
		if code != 201 || q["auto_response"] != true {
			t.Fatal("friend cannot query", code, q)
		}
	}
	newcomer := eve
	newcomer.ID = randomID()
	newcomer.Username = "new-location-friend"
	if err := a.store.update(func(st *State) error { st.Users[newcomer.ID] = newcomer; return nil }); err != nil {
		t.Fatal(err)
	}
	nt := loginTest(t, s, newcomer.Username)
	q, code := makeRequest(t, s, nt, bob.ID, "all-friends-new-member")
	if code != 201 || q["auto_response"] != true {
		t.Fatal("new friend was excluded", code, q)
	}
	code, body, _ := request(t, s, at, "PATCH", "/api/devices/"+device+"/location-sharing",
		map[string]any{"enabled": true, "scope": "all_friends"})
	expectStatus(t, code, 403, body)
	a.store.view(func(st *State) error {
		d := st.Devices[device]
		if grantAllows(st, d, bob.ID) || grantAllows(st, d, randomID()) {
			t.Fatal("self or unknown user received access")
		}
		return nil
	})
	if err := a.store.update(func(st *State) error { u := st.Users[newcomer.ID]; u.Disabled = true; st.Users[u.ID] = u; return nil }); err != nil {
		t.Fatal(err)
	}
	fix := responseFix()
	fix["confirmed"], fix["auto_response"], fix["device_id"], fix["grant_revision"] = false, true, device, grant["revision"]
	code, body, _ = request(t, s, bt, "POST", "/api/location-requests/"+q["id"].(string)+"/respond", fix)
	expectStatus(t, code, 403, body)
}

func TestAllFriendsLocationDisableCancelsAndRejectsInFlightResponse(t *testing.T) {
	a, s, admin, bob, _ := testApp(t)
	at, bt := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username)
	device := "android-all-friends-test-002"
	grant := enableAllFriendsGrantForTest(t, a, s, bt, device)
	q, code := makeRequest(t, s, at, bob.ID, "all-friends-stop-001")
	if code != 201 || q["auto_response"] != true {
		t.Fatal(code, q)
	}
	code, body, _ := request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-sharing", map[string]any{"enabled": false})
	expectStatus(t, code, 200, body)
	code, body, _ = request(t, s, at, "GET", "/api/location-requests/"+q["id"].(string), nil)
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["status"] != "cancelled" {
		t.Fatal("pending request not cancelled")
	}
	fix := responseFix()
	fix["confirmed"], fix["auto_response"], fix["device_id"], fix["grant_revision"] = false, true, device, grant["revision"]
	code, body, _ = request(t, s, bt, "POST", "/api/location-requests/"+q["id"].(string)+"/respond", fix)
	expectStatus(t, code, 403, body)
}

func TestAllFriendsScopeChangeRevokesPreviousRevision(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	at, bt, et := loginTest(t, s, admin.Username), loginTest(t, s, bob.Username), loginTest(t, s, eve.Username)
	device := "android-all-friends-test-003"
	before := enableLocationGrantForTest(t, a, s, bt, device, []string{admin.ID})
	if before["scope"] != "selected_users" {
		t.Fatal("legacy permission was silently broadened")
	}
	q, code := makeRequest(t, s, at, bob.ID, "all-friends-old-revision")
	if code != 201 {
		t.Fatal(code, q)
	}
	code, body, _ := request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-sharing", map[string]any{"enabled": true, "scope": "all_friends"})
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["revision"] == before["revision"] {
		t.Fatal("revision not rotated")
	}
	code, body, _ = request(t, s, at, "GET", "/api/location-requests/"+q["id"].(string), nil)
	expectStatus(t, code, 200, body)
	if decodeMap(t, body)["status"] != "cancelled" {
		t.Fatal("old request survived scope change")
	}
	q, code = makeRequest(t, s, et, bob.ID, "all-friends-new-revision")
	if code != 201 || q["auto_response"] != true {
		t.Fatal("all friends not enabled", code, q)
	}
	code, body, _ = request(t, s, bt, "POST", "/api/logout", map[string]any{})
	expectStatus(t, code, 200, body)
	a.store.view(func(st *State) error {
		if autoRequestAllowed(st, st.LocationRequests[q["id"].(string)]) {
			t.Fatal("logout left active location permission")
		}
		return nil
	})
}

func TestAllFriendsGrantDoesNotRequireCurrentFriendSnapshot(t *testing.T) {
	a, s, admin, bob, eve := testApp(t)
	bt := loginTest(t, s, bob.Username)
	if err := a.store.update(func(st *State) error {
		for _, id := range []string{admin.ID, eve.ID} {
			u := st.Users[id]
			u.Disabled = true
			st.Users[id] = u
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	enableAllFriendsGrantForTest(t, a, s, bt, "android-all-friends-empty")
	if err := a.store.update(func(st *State) error { u := st.Users[eve.ID]; u.Disabled = false; st.Users[u.ID] = u; return nil }); err != nil {
		t.Fatal(err)
	}
	et := loginTest(t, s, eve.Username)
	q, code := makeRequest(t, s, et, bob.ID, "all-friends-after-empty")
	if code != 201 || q["auto_response"] != true {
		t.Fatal("friend added after enabling was excluded", code, q)
	}
}
