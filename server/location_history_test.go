package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func historySetup(t *testing.T) (*App, *httptest.Server, string, string, string, string) {
	a, s, admin, bob, _ := testApp(t)
	at := loginTest(t, s, admin.Username)
	bt := loginTest(t, s, bob.Username)
	device := "android-history-test"
	enableLocationGrantForTest(t, a, s, bt, device, []string{admin.ID})
	return a, s, at, bt, bob.ID, device
}
func historyEnable(t *testing.T, s *httptest.Server, token, device string, share bool) string {
	code, b, _ := request(t, s, token, "PATCH", "/api/devices/"+device+"/location-history", map[string]any{"enabled": true, "confirmed": true, "share_with_friends": share, "interval_seconds": 60})
	expectStatus(t, code, 200, b)
	return decodeMap(t, b)["revision"].(string)
}
func historyPoint(id string, at int64) map[string]any {
	return map[string]any{"id": id, "latitude": 37.5, "longitude": 127.0, "accuracy_m": 10, "captured_at": at, "approximate": false}
}
func historyPath(user string, from, to int64) string {
	return "/api/location-history/" + user + "?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
}
func TestLocationHistoryConsentPermissionsRangeAndReplay(t *testing.T) {
	a, s, at, bt, bob, device := historySetup(t)
	code, b, _ := request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-history", map[string]any{"enabled": true})
	expectStatus(t, code, 400, b)
	rev := historyEnable(t, s, bt, device, false)
	code, b, _ = request(t, s, bt, "POST", "/api/devices", map[string]any{"id": device, "name": "refresh", "token": "synthetic-FCM-token-for-location-sharing", "notification_delivery": "app"})
	expectStatus(t, code, 200, b)
	point := historyPoint(randomID(), now())
	payload := map[string]any{"revision": rev, "points": []any{point}}
	code, b, _ = request(t, s, at, "POST", "/api/devices/"+device+"/location-history", payload)
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", payload)
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", payload)
	expectStatus(t, code, 200, b)
	path := historyPath(bob, now()-3600, now()+60)
	code, b, _ = request(t, s, at, "GET", path, nil)
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, bt, "GET", path, nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["count"].(float64) != 1 {
		t.Fatal("duplicate point")
	}
	point["latitude"] = 38.0
	code, b, _ = request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", payload)
	expectStatus(t, code, 409, b)
	rev = historyEnable(t, s, bt, device, true)
	point = historyPoint(randomID(), now())
	payload = map[string]any{"revision": rev, "points": []any{point}}
	code, b, _ = request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", payload)
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, at, "GET", path, nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["count"].(float64) != 1 {
		t.Fatal("previous consent period exposed")
	}
	code, b, _ = request(t, s, at, "GET", historyPath(bob, now()-3600, now()-60), nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["count"].(float64) != 0 {
		t.Fatal("range not enforced")
	}
	code, b, _ = request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-sharing", map[string]any{"enabled": false})
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, at, "GET", path, nil)
	expectStatus(t, code, 403, b)
	_ = a
}
func TestLocationHistoryRetentionInvalidBatchAndDelete(t *testing.T) {
	a, s, at, bt, bob, device := historySetup(t)
	rev := historyEnable(t, s, bt, device, true)
	_ = a.store.update(func(st *State) error {
		d := st.Devices[device]
		g := *d.LocationHistory
		g.Since = now() - historyRetention - 10
		d.LocationHistory = &g
		st.Devices[device] = d
		return nil
	})
	post := func(points []any) (int, []byte) {
		c, b, _ := request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", map[string]any{"revision": rev, "points": points})
		return c, b
	}
	good := historyPoint(randomID(), now()-60)
	old := historyPoint(randomID(), now()-historyRetention)
	code, b := post([]any{good, old})
	expectStatus(t, code, 400, b)
	code, b, _ = request(t, s, bt, "GET", historyPath(bob, now()-3600, now()+60), nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["count"].(float64) != 0 {
		t.Fatal("partial invalid batch committed")
	}
	code, b = post([]any{good})
	expectStatus(t, code, 200, b)
	future := historyPoint(randomID(), now()+600)
	code, b = post([]any{future})
	expectStatus(t, code, 400, b)
	spoof := historyPoint(randomID(), now())
	spoof["device_id"] = "someone-else"
	code, b = post([]any{spoof})
	expectStatus(t, code, 400, b)
	_ = a.store.update(func(st *State) error {
		st.LocationHistory[bob] = append(st.LocationHistory[bob], LocationHistoryPoint{ID: randomID(), DeviceID: device, Captured: now() - historyRetention - 1})
		return nil
	})
	a.cleanup()
	_ = a.store.view(func(st *State) error {
		if len(st.LocationHistory[bob]) != 1 {
			t.Fatal("expired points not removed")
		}
		return nil
	})
	code, b, _ = request(t, s, bt, "DELETE", "/api/location-history", nil)
	expectStatus(t, code, 200, b)
	code, b = post([]any{good})
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, bt, "GET", historyPath(bob, now()-3600, now()+60), nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["count"].(float64) != 0 {
		t.Fatal("deleted history restored")
	}
	code, b, _ = request(t, s, at, "GET", historyPath(bob, now()-3600, now()+60), nil)
	expectStatus(t, code, 403, b)
}

func TestLocationHistoryDeviceRemovalPreservesOwnHistory(t *testing.T) {
	a, s, at, bt, bob, device := historySetup(t)
	rev := historyEnable(t, s, bt, device, true)
	code, b, _ := request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", map[string]any{"revision": rev, "points": []any{historyPoint(randomID(), now())}})
	expectStatus(t, code, 200, b)
	_ = a.store.update(func(st *State) error { delete(st.Devices, device); return nil })
	a.cleanup()
	path := historyPath(bob, now()-3600, now()+60)
	code, b, _ = request(t, s, bt, "GET", path, nil)
	expectStatus(t, code, 200, b)
	if decodeMap(t, b)["count"].(float64) != 1 {
		t.Fatal("device removal erased own history before retention")
	}
	code, b, _ = request(t, s, at, "GET", path, nil)
	expectStatus(t, code, 403, b)
}

func TestLocationHistoryClockSkewAndExpiredBackup(t *testing.T) {
	a, s, _, bt, bob, device := historySetup(t)
	rev := historyEnable(t, s, bt, device, false)
	post := func(at int64) (int, []byte) {
		code, b, _ := request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", map[string]any{"revision": rev, "points": []any{historyPoint(randomID(), at)}})
		return code, b
	}
	code, b := post(now() - 15)
	expectStatus(t, code, 200, b)
	code, b = post(now() - 60)
	expectStatus(t, code, 400, b)
	_ = a.store.update(func(st *State) error {
		st.LocationHistory[bob] = append(st.LocationHistory[bob], LocationHistoryPoint{ID: randomID(), DeviceID: device, Captured: now() - historyRetention - 1})
		return nil
	})
	code, b = post(now())
	expectStatus(t, code, 200, b)
	if _, e := os.Stat(filepath.Join(a.store.dir, "store.db.bak")); !os.IsNotExist(e) {
		t.Fatal("expired data retained in previous backup", e)
	}
	_ = a.store.view(func(st *State) error {
		for _, p := range st.LocationHistory[bob] {
			if p.Captured <= now()-historyRetention {
				t.Fatal("expired point retained")
			}
		}
		return nil
	})
}

func TestLocationHistoryFollowsSharingAndDeletePreventsRestart(t *testing.T) {
	a, s, _, bt, bob, device := historySetup(t)
	code, b, _ := request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-history", map[string]any{"enabled": true, "confirmed": true, "share_with_friends": true, "follow_sharing": true})
	expectStatus(t, code, 200, b)
	rev := decodeMap(t, b)["revision"].(string)
	code, b, _ = request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-sharing", map[string]any{"enabled": false})
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, bt, "POST", "/api/devices/"+device+"/location-history", map[string]any{"revision": rev, "points": []any{historyPoint(randomID(), now())}})
	expectStatus(t, code, 403, b)
	code, b, _ = request(t, s, bt, "DELETE", "/api/location-history", nil)
	expectStatus(t, code, 200, b)
	a.store.view(func(st *State) error {
		for _, d := range st.Devices {
			if d.UserID == bob && (historyGrantActive(st, d) || activeLocationGrant(st, d)) {
				t.Error("deletion left automatic enrollment enabled")
			}
		}
		return nil
	})
	code, b, _ = request(t, s, bt, "PATCH", "/api/devices/"+device+"/location-history", map[string]any{"enabled": true, "confirmed": true, "share_with_friends": true, "follow_sharing": true})
	expectStatus(t, code, 400, b)
}

func TestLocationHistoryQuick48HourRange(t *testing.T) {
	_, s, _, bt, bob, _ := historySetup(t)
	end := now()
	code, b, _ := request(t, s, bt, "GET", historyPath(bob, end-historyRetention, end), nil)
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, bt, "GET", historyPath(bob, end-historyRetention-1, end), nil)
	expectStatus(t, code, 400, b)
}
