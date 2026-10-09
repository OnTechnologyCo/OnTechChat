package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type PushSender interface {
	Send(context.Context, Device, Outbox) error
}
type pushError struct {
	code         string
	permanent    bool
	unregistered bool
}

func (e pushError) Error() string { return e.code }

type FCM struct {
	mu                sync.Mutex
	email, project    string
	key               *rsa.PrivateKey
	client            *http.Client
	token             string
	until             time.Time
	tokenURL, sendURL string
}

func newFCM(c FCMConfig) (*FCM, error) {
	b := c.credentialsJSON
	if len(b) == 0 {
		return nil, errors.New("FCM credentials missing from JSON settings")
	}
	var e error
	if len(b) > 64<<10 {
		return nil, errors.New("FCM service account file too large")
	}
	var v struct {
		Type  string `json:"type"`
		Email string `json:"client_email"`
		Key   string `json:"private_key"`
	}
	if e = json.Unmarshal(b, &v); e != nil || v.Type != "service_account" || v.Email == "" {
		return nil, errors.New("invalid FCM service account JSON")
	}
	block, _ := pem.Decode([]byte(v.Key))
	if block == nil {
		return nil, errors.New("invalid FCM private key")
	}
	parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		return nil, errors.New("FCM key must be RSA PKCS8")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 {
		return nil, errors.New("FCM key must be RSA-2048 or stronger")
	}
	if strings.ContainsAny(c.ProjectID, "/\\?#\r\n") {
		return nil, errors.New("invalid FCM project id")
	}
	return &FCM{email: v.Email, project: c.ProjectID, key: key, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, tokenURL: "https://oauth2.googleapis.com/token", sendURL: "https://fcm.googleapis.com/v1/projects/" + url.PathEscape(c.ProjectID) + "/messages:send"}, nil
}
func (f *FCM) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Now().Add(time.Minute).Before(f.until) {
		return f.token, nil
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iss": f.email, "scope": "https://www.googleapis.com/auth/firebase.messaging", "aud": f.tokenURL, "iat": now(), "exp": now() + 3600})
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	signature, e := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if e != nil {
		return "", pushError{code: "signing_error", permanent: true}
	}
	assertion := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, e := http.NewRequestWithContext(ctx, "POST", f.tokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := f.client.Do(req)
	if e != nil {
		return "", pushError{code: "oauth_network"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", pushError{code: "oauth_http_" + strconv.Itoa(resp.StatusCode)}
	}
	var out struct {
		Token   string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); e != nil || out.Token == "" || out.Expires <= 0 {
		return "", pushError{code: "oauth_response"}
	}
	f.token = out.Token
	f.until = time.Now().Add(time.Duration(out.Expires) * time.Second)
	return f.token, nil
}
func (f *FCM) Send(ctx context.Context, d Device, p Outbox) error {
	token, e := f.accessToken(ctx)
	if e != nil {
		return e
	}
	title, body := "온테크 채팅", "새 채팅 메시지가 도착했습니다."
	if d.NotificationPreview && (p.Kind == "text" || p.Kind == "file") && p.PreviewBody != "" {
		title, body = p.PreviewTitle, p.PreviewBody
	}
	if p.Kind == "auth" {
		title = "온테크 인증함"
		body = "로그인 인증번호가 도착했습니다. 앱에서 확인해 주세요."
	}
	if p.Kind == "location_request" {
		if p.AutoAction {
			title, body = "위치 공유 요청", "허용한 사용자의 현재 위치 요청을 처리합니다."
		} else {
			title, body = "위치 확인 요청", "친구가 현재 위치를 요청했습니다. 앱에서 승인하거나 거절하세요."
		}
	}
	if p.Kind == "location" {
		title, body = "위치 응답", "친구가 위치를 보냈습니다. 앱에서 확인해 주세요."
	}
	ttl := p.Expires - now()
	if ttl < 1 {
		return pushError{code: "expired", permanent: true}
	}
	if ttl > 86400 {
		ttl = 86400
	}
	data := map[string]string{"message_id": strconv.FormatInt(p.MessageID, 10), "room_id": p.RoomID, "kind": p.Kind, "recipient_id": d.UserID}
	if p.ActionID != "" {
		data["location_request_id"] = p.ActionID
	}
	if p.AutoAction {
		data["location_request_id"] = p.ActionID
		data["location_auto"] = "1"
		data["location_grant_revision"] = p.GrantRevision
	}
	payload := map[string]any{"message": map[string]any{"token": d.Token, "notification": map[string]string{"title": title, "body": body}, "data": data, "android": map[string]any{"priority": "high", "ttl": fmt.Sprintf("%ds", ttl), "notification": map[string]string{"channel_id": "ontech_messages", "visibility": "PRIVATE", "tag": "message-" + strconv.FormatInt(p.MessageID, 10)}}}}
	if d.NotificationDelivery == "app" {
		// 새 앱이 계정/만료 상태를 먼저 확인하고 Android 알림을 직접 게시합니다.
		// 이전 앱은 기존 notification payload를 유지합니다.
		msg := payload["message"].(map[string]any)
		delete(msg, "notification")
		data := msg["data"].(map[string]string)
		data["delivery"] = "app"
		data["title"] = title
		data["body"] = body
		delete(msg["android"].(map[string]any), "notification")
	}
	b, _ := json.Marshal(payload)
	req, e := http.NewRequestWithContext(ctx, "POST", f.sendURL, strings.NewReader(string(b)))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := f.client.Do(req)
	if e != nil {
		return pushError{code: "fcm_network"}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var result struct {
		Error struct {
			Details []struct {
				Code string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &result)
	for _, d := range result.Error.Details {
		if d.Code == "UNREGISTERED" {
			return pushError{code: "unregistered", permanent: true, unregistered: true}
		}
	}
	if resp.StatusCode == 401 {
		f.mu.Lock()
		f.token = ""
		f.mu.Unlock()
	}
	return pushError{code: "fcm_http_" + strconv.Itoa(resp.StatusCode), permanent: resp.StatusCode == 400 || resp.StatusCode == 403 || resp.StatusCode == 404}
}
func (a *App) devices(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	e := a.store.view(func(st *State) error {
		for _, d := range st.Devices {
			if d.UserID == u.ID {
				sharing := d.LocationSharing != nil && d.LocationSharing.Enabled
				out = append(out, map[string]any{"id": d.ID, "name": d.Name, "updated_at": d.Updated, "preview_enabled": d.NotificationPreview, "location_sharing": sharing})
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
func (a *App) registerDevice(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		ID       string `json:"id"`
		Token    string `json:"token"`
		Name     string `json:"name"`
		Delivery string `json:"notification_delivery"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if (in.Delivery != "" && in.Delivery != "app" && in.Delivery != "system") || !clientIDPattern.MatchString(in.ID) || len(in.Token) < 20 || len(in.Token) > 4096 || len(in.Name) > 100 || strings.ContainsAny(in.Token, "\r\n\x00") {
		fail(w, bad("기기 ID·FCM 토큰·이름을 확인해 주세요."))
		return
	}
	e := a.store.update(func(st *State) error {
		count := 0
		for id, d := range st.Devices {
			if d.UserID == u.ID {
				count++
			}
			if d.Token == in.Token && id != in.ID {
				delete(st.Devices, id)
			}
		}
		if old, ok := st.Devices[in.ID]; ok && old.UserID != u.ID {
			return forbidden()
		}
		if _, ok := st.Devices[in.ID]; !ok && count >= 8 {
			return bad("기기는 사용자당 최대 8대입니다.")
		}
		old := st.Devices[in.ID]
		st.Devices[in.ID] = Device{ID: in.ID, UserID: u.ID, Token: in.Token, Name: in.Name, Updated: now(), NotificationPreview: old.NotificationPreview, NotificationDelivery: in.Delivery, LocationSharing: old.LocationSharing, LocationHistory: old.LocationHistory}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]bool{"ok": true, "push_enabled": a.cfg.FCM.Enabled})
}
func (a *App) removeDevice(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	rooms := []string{}
	e := a.store.update(func(st *State) error {
		d, ok := st.Devices[id]
		if !ok {
			return notfound()
		}
		if d.UserID != u.ID {
			return forbidden()
		}
		rooms = cancelAutoRequestsForDevice(st, id)
		delete(st.Devices, id)
		for k, p := range st.Outbox {
			if p.DeviceID == id {
				delete(st.Outbox, k)
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	for _, room := range rooms {
		a.changed(room)
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) pushLoop() {
	if a.push == nil {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.deliverPush()
		}
	}
}
func (a *App) deliverPush() {
	items := []Outbox{}
	_ = a.store.view(func(st *State) error {
		for _, p := range st.Outbox {
			if p.State == "pending" && p.Due <= now() {
				items = append(items, p)
				if len(items) >= 25 {
					break
				}
			}
		}
		return nil
	})
	for _, p := range items {
		if a.ctx.Err() != nil {
			return
		}
		var d Device
		allowed := false
		_ = a.store.view(func(st *State) error {
			current, ok := st.Outbox[p.ID]
			if !ok || current.State != "pending" {
				return nil
			}
			d, ok = st.Devices[p.DeviceID]
			u, uok := st.Users[p.UserID]
			if p.LocationRequestID != "" {
				q, found := st.LocationRequests[p.LocationRequestID]
				allowed = ok && uok && !u.Disabled && found && d.UserID == q.TargetID && p.UserID == q.TargetID && p.Expires > now() && locationRequestStatus(st, q) == "pending"
				if allowed && q.AutoDevice != "" {
					allowed = q.AutoDevice == d.ID && autoRequestAllowed(st, q)
				}
				if allowed {
					p.ActionID, p.GrantRevision, p.AutoAction = q.ID, q.AutoRevision, q.AutoDevice != ""
				}
				return nil
			}
			_, m := member(st, p.RoomID, p.UserID)
			allowed = ok && uok && !u.Disabled && m && d.UserID == p.UserID && p.Expires > now()
			if !allowed {
				return nil
			}
			message, found := findNotificationMessage(st, p.MessageID)
			allowed = found && message.RoomID == p.RoomID && !notificationDismissed(st, message, p.UserID, now())
			if allowed && p.Kind == "location_request" {
				var hint struct {
					ID string `json:"id"`
				}
				if json.Unmarshal([]byte(message.Text), &hint) == nil {
					if q, ok := st.LocationRequests[hint.ID]; ok && q.AutoDevice != "" {
						allowed = q.AutoDevice == p.DeviceID && locationRequestStatus(st, q) == "pending" && autoRequestAllowed(st, q)
						if allowed {
							p.ActionID, p.GrantRevision, p.AutoAction = q.ID, q.AutoRevision, true
						}
					}
				}
			}
			if allowed && d.NotificationPreview {
				p.PreviewTitle, p.PreviewBody = previewForMessage(st, message)
			}
			return nil
		})
		var e error
		if allowed {
			ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
			e = a.push.Send(ctx, d, p)
			cancel()
		} else {
			e = pushError{code: "revoked_or_expired", permanent: true}
		}
		var pe pushError
		_ = errors.As(e, &pe)
		_ = a.store.update(func(st *State) error {
			item, ok := st.Outbox[p.ID]
			if !ok || item.State != "pending" {
				return nil
			}
			item.Attempts++
			if e == nil {
				item.State = "sent"
				item.LastError = ""
			} else {
				item.LastError = pe.code
				if item.LastError == "" {
					item.LastError = "send_error"
				}
				if pe.permanent || item.Attempts >= 8 {
					item.State = "failed"
				} else {
					item.Due = now() + int64(5*(1<<min(item.Attempts, 8)))
				}
			}
			st.Outbox[p.ID] = item
			if pe.unregistered {
				if current, ok := st.Devices[p.DeviceID]; ok && current.Token == d.Token {
					delete(st.Devices, p.DeviceID)
				}
			}
			return nil
		})
		a.stateMu.Lock()
		if e == nil {
			a.pushState = "last request accepted by FCM (device delivery not confirmed)"
		} else {
			a.pushState = "last request failed: " + pe.code
		}
		a.stateMu.Unlock()
	}
}
