package server

import (
	"net/http"
	"sort"
)

const defaultAIProfileID = "ontech-ai"

type AIProfile struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Avatar         string `json:"avatar"`
	Description    string `json:"description"`
	Enabled        bool   `json:"enabled"`
	DefaultModelID string `json:"default_model_id,omitempty"`
}

func aiDefaultRoomKey(userID, profileID string) string {
	return userID + "|" + profileID
}

func roomActivity(st *State, roomID string, created int64) int64 {
	latest := created
	for _, m := range st.Messages {
		if m.RoomID == roomID && m.Created > latest {
			latest = m.Created
		}
	}
	return latest
}

func ensureDefaultAIProfile(st *State) bool {
	changed := false
	profile, ok := st.AIProfiles[defaultAIProfileID]
	if !ok {
		profile = AIProfile{
			ID: defaultAIProfileID, Name: "온테크 AI", Avatar: "AI",
			Description: "개인 AI 대화와 서버 작업을 연결합니다.", Enabled: true,
		}
		st.AIProfiles[defaultAIProfileID] = profile
		changed = true
	}
	for id, room := range st.Rooms {
		if room.Kind == "ai" && room.AIProfileID == "" {
			room.AIProfileID = defaultAIProfileID
			st.Rooms[id] = room
			changed = true
		}
	}
	for userID := range st.Users {
		key := aiDefaultRoomKey(userID, defaultAIProfileID)
		if existing := st.AIDefaultRooms[key]; existing != "" {
			if room, ok := st.Rooms[existing]; ok && room.Kind == "ai" && room.OwnerID == userID && len(room.Members) == 1 {
				continue
			}
			delete(st.AIDefaultRooms, key)
			changed = true
		}
		type candidate struct {
			id       string
			activity int64
		}
		var rooms []candidate
		for id, room := range st.Rooms {
			if room.Kind != "ai" || room.OwnerID != userID || len(room.Members) != 1 {
				continue
			}
			rooms = append(rooms, candidate{id: id, activity: roomActivity(st, id, room.Created)})
		}
		sort.Slice(rooms, func(i, j int) bool {
			if rooms[i].activity == rooms[j].activity {
				return rooms[i].id < rooms[j].id
			}
			return rooms[i].activity > rooms[j].activity
		})
		if len(rooms) > 0 {
			st.AIDefaultRooms[key] = rooms[0].id
			changed = true
		}
	}
	return changed
}

func (a *App) aiFriends(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	err := a.store.view(func(st *State) error {
		for _, profile := range st.AIProfiles {
			available := false
			reason := ""
			defaultModel := profile.DefaultModelID
			if defaultModel == "" {
				defaultModel = a.cfg.AI.DefaultModelID
			}
			if !profile.Enabled {
				reason = "AI 친구가 비활성화되어 있습니다."
			} else if !a.cfg.AI.Enabled {
				reason = "AI 대화 기능이 비활성화되어 있습니다."
			} else {
				p, m, _, e := resolveAISelection(a.cfg.AI, defaultModel, "")
				if e != nil {
					reason = "사용 가능한 AI 모델이 없습니다."
				} else if ready, why := aiProviderRuntimeState(p); !ready {
					reason = "AI 연결을 사용할 수 없습니다: " + why
				} else {
					available = true
					defaultModel = m.ID
				}
			}
			roomID := st.AIDefaultRooms[aiDefaultRoomKey(u.ID, profile.ID)]
			out = append(out, map[string]any{
				"id": profile.ID, "name": profile.Name, "avatar": profile.Avatar,
				"description": profile.Description, "enabled": profile.Enabled,
				"available": available, "unavailable_reason": reason,
				"default_model_id": defaultModel, "default_room_id": roomID,
			})
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	respond(w, 200, out)
}

func (a *App) openAIFriend(w http.ResponseWriter, r *http.Request, u User) {
	profileID := r.PathValue("id")
	var existing string
	err := a.store.view(func(st *State) error {
		profile, ok := st.AIProfiles[profileID]
		if !ok || !profile.Enabled {
			return notfound()
		}
		existing = st.AIDefaultRooms[aiDefaultRoomKey(u.ID, profileID)]
		if existing != "" {
			room, ok := st.Rooms[existing]
			if !ok || room.Kind != "ai" || room.OwnerID != u.ID || len(room.Members) != 1 {
				existing = ""
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if _, err = a.ensureDefaultAgentProject(u); err != nil {
		fail(w, apiError{503, "AI 작업공간을 자동으로 준비하지 못했습니다."})
		return
	}
	if existing != "" {
		if err = a.reopenRoomInList(existing, u.ID); err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, map[string]any{"id": existing, "kind": "ai", "existing": true})
		return
	}
	_, model, effort, err := a.resolveAIRuntime("", "")
	if err != nil {
		fail(w, err)
		return
	}
	room := Room{
		ID: randomID(), Name: "온테크 AI", OwnerID: u.ID, Kind: "ai",
		Created: now(), Members: map[string]int64{u.ID: 0}, AIProfileID: profileID,
		AIModelID: model.ID, AIEffort: effort,
	}
	created := false
	err = a.store.update(func(st *State) error {
		profile, ok := st.AIProfiles[profileID]
		if !ok || !profile.Enabled {
			return notfound()
		}
		key := aiDefaultRoomKey(u.ID, profileID)
		if id := st.AIDefaultRooms[key]; id != "" {
			if current, ok := st.Rooms[id]; ok && current.Kind == "ai" && current.OwnerID == u.ID && len(current.Members) == 1 {
				room = current
				if room.HiddenFor[u.ID] {
					delete(room.HiddenFor, u.ID)
					st.Rooms[room.ID] = room
					return nil
				}
				return errUnchanged
			}
		}
		if current, ok := st.Users[u.ID]; !ok || current.Disabled {
			return forbidden()
		}
		st.Rooms[room.ID] = room
		st.AIDefaultRooms[key] = room.ID
		appendAudit(st, AuditEvent{ActorID: u.ID, Action: "ai_friend.open", ResourceType: "room", ResourceID: room.ID, Result: "allowed"})
		created = true
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if created {
		a.changed(room.ID)
	}
	respond(w, func() int {
		if created {
			return http.StatusCreated
		}
		return http.StatusOK
	}(), map[string]any{"id": room.ID, "kind": room.Kind, "existing": !created})
}

func (a *App) setAIFriendDefaultRoom(w http.ResponseWriter, r *http.Request, u User) {
	profileID := r.PathValue("id")
	var in struct {
		RoomID string `json:"room_id"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	err := a.store.update(func(st *State) error {
		if profile, ok := st.AIProfiles[profileID]; !ok || !profile.Enabled {
			return notfound()
		}
		room, ok := st.Rooms[in.RoomID]
		if !ok || room.Kind != "ai" || room.OwnerID != u.ID || len(room.Members) != 1 {
			return forbidden()
		}
		if room.AIProfileID != "" && room.AIProfileID != profileID {
			return bad("다른 AI 친구의 대화를 기본 대화로 지정할 수 없습니다.")
		}
		room.AIProfileID = profileID
		st.Rooms[room.ID] = room
		st.AIDefaultRooms[aiDefaultRoomKey(u.ID, profileID)] = room.ID
		appendAudit(st, AuditEvent{ActorID: u.ID, Action: "ai_friend.default_room", ResourceType: "room", ResourceID: room.ID, Result: "allowed"})
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(in.RoomID)
	respond(w, 200, map[string]bool{"ok": true})
}
