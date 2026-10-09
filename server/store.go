package server

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// 암호화된 단일 파일 스냅샷 저장소. 소규모 자체 호스팅용이며 SQLite 호환 파일이 아닙니다.
// 모든 변경은 잠금 안에서 복제 -> 검증 -> fsync -> 교체 후 공개됩니다.
type User struct {
	AgentEnabled             bool            `json:"agent_enabled,omitempty"`
	LocationRequestsDisabled bool            `json:"location_requests_disabled,omitempty"`
	DisplayName              string          `json:"display_name,omitempty"`
	StatusMessage            string          `json:"status_message,omitempty"`
	AvatarColor              string          `json:"avatar_color,omitempty"`
	AvatarImage              string          `json:"avatar_image,omitempty"`
	AvatarRevision           string          `json:"avatar_revision,omitempty"`
	Favorites                map[string]bool `json:"favorites,omitempty"`
	PinnedRooms              map[string]bool `json:"pinned_rooms,omitempty"`
	ID                       string          `json:"id"`
	Username                 string          `json:"username"`
	Hash                     string          `json:"password_hash"`
	Role                     string          `json:"role"`
	Disabled                 bool            `json:"disabled"`
	Created                  int64           `json:"created_at"`
}
type Session struct {
	Persistent bool   `json:"persistent,omitempty"`
	UserID     string `json:"user_id"`
	Expires    int64  `json:"expires_at"`
	Created    int64  `json:"created_at"`
}
type Room struct {
	HiddenFor        map[string]bool            `json:"hidden_for,omitempty"`
	AutoHelp         map[string]autoHelpSetting `json:"auto_help,omitempty"`
	ClearedThrough   map[string]int64           `json:"cleared_through,omitempty"`
	AIClearedThrough int64                      `json:"ai_cleared_through,omitempty"`
	DirectKey        string                     `json:"direct_key,omitempty"`
	AIProfileID      string                     `json:"ai_profile_id,omitempty"`
	AIModelID        string                     `json:"ai_model_id,omitempty"`
	AIEffort         string                     `json:"ai_effort,omitempty"`
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	OwnerID          string                     `json:"owner_id"`
	Kind             string                     `json:"kind"`
	Created          int64                      `json:"created_at"`
	Members          map[string]int64           `json:"members"`
}
type Message struct {
	ID              int64  `json:"id"`
	RoomID          string `json:"room_id"`
	UserID          string `json:"user_id"`
	Username        string `json:"username"`
	Kind            string `json:"kind"`
	Text            string `json:"text"`
	FileID          string `json:"file_id,omitempty"`
	ClientID        string `json:"client_id,omitempty"`
	AIRequestID     string `json:"ai_request_id,omitempty"`
	AgentJobID      string `json:"agent_job_id,omitempty"`
	AIModelID       string `json:"ai_model_id,omitempty"`
	AIEffort        string `json:"ai_effort,omitempty"`
	AIObservedModel string `json:"ai_observed_model,omitempty"`
	Created         int64  `json:"created_at"`
	Expires         int64  `json:"expires_at,omitempty"`
}
type Attachment struct {
	ID      string `json:"id"`
	RoomID  string `json:"room_id"`
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Created int64  `json:"created_at"`
}
type Device struct {
	LocationHistory      *LocationHistoryGrant `json:"location_history,omitempty"`
	LocationSharing      *LocationGrant        `json:"location_sharing,omitempty"`
	NotificationDelivery string                `json:"notification_delivery,omitempty"`
	NotificationPreview  bool                  `json:"notification_preview,omitempty"`
	ID                   string                `json:"id"`
	UserID               string                `json:"user_id"`
	Token                string                `json:"token"`
	Name                 string                `json:"name"`
	Updated              int64                 `json:"updated_at"`
}
type Outbox struct {
	LocationRequestID string `json:"location_request_id,omitempty"`
	// 전송 직전에만 계산합니다. 암호화 스냅샷과 로그에 중복 저장하지 않습니다.
	PreviewTitle  string `json:"-"`
	PreviewBody   string `json:"-"`
	ActionID      string `json:"-"`
	GrantRevision string `json:"-"`
	AutoAction    bool   `json:"-"`
	ID            string `json:"id"`
	MessageID     int64  `json:"message_id"`
	RoomID        string `json:"room_id"`
	UserID        string `json:"user_id"`
	DeviceID      string `json:"device_id"`
	Kind          string `json:"kind"`
	Attempts      int    `json:"attempts"`
	Due           int64  `json:"due_at"`
	Expires       int64  `json:"expires_at"`
	State         string `json:"state"`
	LastError     string `json:"last_error,omitempty"`
}
type MailCursor struct {
	Validity uint32 `json:"uidvalidity"`
	UID      uint32 `json:"uid"`
}
type State struct {
	MailDelivered         map[string]int64                  `json:"mail_delivered,omitempty"`
	LocationHistory       map[string][]LocationHistoryPoint `json:"location_history,omitempty"`
	AgentMigrationVersion int                               `json:"agent_migration_version,omitempty"`
	AIProfiles            map[string]AIProfile              `json:"ai_profiles,omitempty"`
	AIDefaultRooms        map[string]string                 `json:"ai_default_rooms,omitempty"`
	AccessGrants          map[string]AccessGrant            `json:"access_grants,omitempty"`
	AgentProjects         map[string]AgentProject           `json:"agent_projects,omitempty"`
	AgentJobs             map[string]AgentJob               `json:"agent_jobs,omitempty"`
	AgentArtifacts        map[string]AgentArtifact          `json:"agent_artifacts,omitempty"`
	ProgramDefinitions    map[string]ProgramDefinition      `json:"program_definitions,omitempty"`
	ProgramInstances      map[string]ProgramInstance        `json:"program_instances,omitempty"`
	AuditEvents           []AuditEvent                      `json:"audit_events,omitempty"`
	LocationRequests      map[string]LocationRequest        `json:"location_requests,omitempty"`
	Schema                int                               `json:"schema"`
	Revision              uint64                            `json:"revision"`
	NextMessage           int64                             `json:"next_message"`
	Users                 map[string]User                   `json:"users"`
	Sessions              map[string]Session                `json:"sessions"`
	Rooms                 map[string]Room                   `json:"rooms"`
	Messages              []Message                         `json:"messages"`
	Files                 map[string]Attachment             `json:"files"`
	Devices               map[string]Device                 `json:"devices"`
	Outbox                map[string]Outbox                 `json:"outbox"`
	MailCursors           map[string]MailCursor             `json:"mail_cursors"`
	AIRequests            map[string]AIRequest              `json:"ai_requests"`
}

var errUnchanged = errors.New("unchanged transaction")

type Store struct {
	mu        sync.RWMutex
	data      State
	box       cipher.AEAD
	dir       string
	lock      *os.File
	closed    bool
	recovered bool
}

func emptyState() State {
	return State{Schema: 1, NextMessage: 1, Users: map[string]User{}, Sessions: map[string]Session{}, Rooms: map[string]Room{}, Messages: []Message{}, Files: map[string]Attachment{}, Devices: map[string]Device{}, Outbox: map[string]Outbox{}, MailCursors: map[string]MailCursor{}, AIRequests: map[string]AIRequest{}, AIProfiles: map[string]AIProfile{}, AIDefaultRooms: map[string]string{}, AccessGrants: map[string]AccessGrant{}, AgentProjects: map[string]AgentProject{}, AgentJobs: map[string]AgentJob{}, AgentArtifacts: map[string]AgentArtifact{}, ProgramDefinitions: map[string]ProgramDefinition{}, ProgramInstances: map[string]ProgramInstance{}, AuditEvents: []AuditEvent{}}
}
func openStore(dir string) (s *Store, err error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := secureDirectory(dir); e != nil {
		return nil, fmt.Errorf("secure data directory: %w", e)
	}
	lk, e := lockDirectory(dir)
	if e != nil {
		return nil, fmt.Errorf("data directory is locked: %w", e)
	}
	defer func() {
		if err != nil {
			unlockDirectory(lk)
		}
	}()
	kp := filepath.Join(dir, "encryption.key")
	key, e := os.ReadFile(kp)
	if errors.Is(e, os.ErrNotExist) {
		for _, name := range []string{"store.db", "store.db.bak"} {
			if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
				return nil, errors.New("encryption key missing; restore the matching key instead of creating one")
			}
		}
		key = make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return nil, e
		}
		if e = writeExclusive(kp, key, 0600); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	}
	if len(key) != 32 {
		return nil, errors.New("invalid encryption key length")
	}
	box, e := newBox(key)
	if e != nil {
		return nil, e
	}
	s = &Store{dir: dir, box: box, lock: lk, data: emptyState()}
	load := func(name string) (State, error) {
		f, e := os.Open(filepath.Join(dir, name))
		if e != nil {
			return State{}, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		if e != nil || len(b) > 64<<20 {
			return State{}, errors.New("store exceeds size limit")
		}
		p, e := decrypt(box, b)
		if e != nil {
			return State{}, errors.New("store authentication failed (wrong key or corruption)")
		}
		var st State
		if e = json.Unmarshal(p, &st); e != nil {
			return st, e
		}
		if st.Schema != 1 || st.NextMessage < 1 || st.Users == nil || st.Rooms == nil || st.Sessions == nil || st.Files == nil || st.Devices == nil || st.Outbox == nil || st.MailCursors == nil {
			return st, errors.New("invalid store schema")
		}
		return st, nil
	}
	st, e := load("store.db")
	if errors.Is(e, os.ErrNotExist) {
		st, e = load("store.db.bak")
		if e == nil {
			s.recovered = true
		} else if errors.Is(e, os.ErrNotExist) {
			e = nil
			st = emptyState()
		}
	}
	if e != nil {
		return nil, e
	}
	if st.AIRequests == nil {
		st.AIRequests = map[string]AIRequest{}
	}
	ensureAgentStateMaps(&st)
	s.data = st
	if e = os.MkdirAll(filepath.Join(dir, "files"), 0700); e != nil {
		return nil, e
	}
	return s, nil
}
func writeExclusive(path string, b []byte, mode os.FileMode) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		os.Remove(path)
		return e
	}
	return ce
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return unlockDirectory(s.lock)
}
func (s *Store) view(fn func(*State) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return errors.New("store closed")
	}
	return fn(&s.data)
}
func (s *Store) update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	raw, e := json.Marshal(s.data)
	if e != nil {
		return e
	}
	var next State
	if e = json.Unmarshal(raw, &next); e != nil {
		return e
	}
	ensureAgentStateMaps(&next)
	if e = fn(&next); e != nil {
		if errors.Is(e, errUnchanged) {
			return nil
		}
		return e
	}
	next.Revision++
	raw, e = json.Marshal(next)
	if e != nil {
		return e
	}
	if len(raw) > 60<<20 {
		return errors.New("store capacity reached")
	}
	enc, e := encrypt(s.box, raw)
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(s.dir, ".commit-*")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.Write(enc)
	}
	if e == nil {
		e = tmp.Sync()
	}
	ce := tmp.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	main := filepath.Join(s.dir, "store.db")
	bak := main + ".bak"
	if _, e = os.Stat(main); e == nil {
		if e = os.Remove(bak); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = os.Rename(main, bak); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e = os.Rename(name, main); e != nil {
		_ = os.Rename(bak, main)
		return e
	}
	syncDirectory(s.dir)
	s.data = next
	return nil
}

// 코드 만료 이후 이전 백업에도 코드가 남지 않도록 마지막 백업을 제거합니다.
func (s *Store) discardOldBackup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(filepath.Join(s.dir, "store.db.bak"))
	syncDirectory(s.dir)
}
func findUsername(st *State, name string) (User, bool) {
	for _, u := range st.Users {
		if u.Username == name {
			return u, true
		}
	}
	return User{}, false
}
func member(st *State, room, user string) (Room, bool) {
	r, ok := st.Rooms[room]
	if !ok {
		return r, false
	}
	u, exists := st.Users[user]
	if !exists || u.Disabled {
		return r, false
	}
	_, ok = r.Members[user]
	return r, ok
}
func authRoomID(user string) string { return "auth-" + user }
func addUser(st *State, u User) {
	st.Users[u.ID] = u
	id := authRoomID(u.ID)
	st.Rooms[id] = Room{ID: id, Name: "개인 인증함", OwnerID: u.ID, Kind: "auth", Created: now(), Members: map[string]int64{u.ID: 0}}
}
func activeAIAnswerReservations(st *State) int {
	n := 0
	for _, q := range st.AIRequests {
		if q.Status == aiQueued || q.Status == aiRunning {
			n++
		}
	}
	return n
}
func liveMessageCount(st *State) int {
	n := 0
	for _, m := range st.Messages {
		if m.Kind != "deleted" && (m.Expires == 0 || m.Expires > now()) {
			n++
		}
	}
	return n
}
func aiMessageCapacityAvailable(st *State, max, additional int) bool {
	return liveMessageCount(st)+activeAIAnswerReservations(st)+additional <= max
}
func addMessage(st *State, m Message, max int, enablePush bool) (Message, error) {
	for _, old := range st.Messages {
		if m.ClientID != "" && old.ClientID == m.ClientID && old.UserID == m.UserID && old.RoomID == m.RoomID {
			return old, nil
		}
	}
	kept := st.Messages[:0]
	for _, v := range st.Messages {
		if v.Kind != "deleted" && (v.Expires == 0 || v.Expires > now()) {
			kept = append(kept, v)
		}
	}
	st.Messages = kept
	reserved := activeAIAnswerReservations(st)
	if m.UserID == "" && m.AIRequestID != "" && reserved > 0 {
		reserved--
	}
	if len(st.Messages)+reserved >= max {
		return m, errors.New("message capacity reached; delete older messages or increase max_messages")
	}
	m.ID = st.NextMessage
	st.NextMessage++
	m.Created = now()
	st.Messages = append(st.Messages, m)
	// 새로 저장한 메시지만 삭제된 목록을 다시 표시합니다. 중복 재전송은 위에서 반환됩니다.
	if room, ok := st.Rooms[m.RoomID]; ok && len(room.HiddenFor) > 0 {
		for id := range room.Members {
			delete(room.HiddenFor, id)
		}
		st.Rooms[room.ID] = room
	}
	if enablePush {
		r := st.Rooms[m.RoomID]
		for _, d := range st.Devices {
			if d.UserID == m.UserID {
				continue
			}
			if _, ok := r.Members[d.UserID]; !ok {
				continue
			}
			u, ok := st.Users[d.UserID]
			if !ok || u.Disabled {
				continue
			}
			id := randomID()
			expires := now() + 86400
			if m.Expires > 0 {
				expires = m.Expires
			}
			st.Outbox[id] = Outbox{ID: id, MessageID: m.ID, RoomID: m.RoomID, UserID: d.UserID, DeviceID: d.ID, Kind: m.Kind, Due: now(), Expires: expires, State: "pending"}
		}
	}
	return m, nil
}
