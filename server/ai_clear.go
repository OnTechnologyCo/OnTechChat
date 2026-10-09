package server

import (
	"net/http"
	"os"
	"path/filepath"
)

// Clearing an AI room removes its stored context and job payloads, never user workspace files.
func (a *App) clearAIConversation(w http.ResponseWriter, r *http.Request, u User) {
	a.clearAIConversationWithList(w, r, u, false)
}

func (a *App) clearAIConversationWithList(w http.ResponseWriter, r *http.Request, u User, hide bool) {
	roomID := r.PathValue("room")
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	var aiIDs, agentIDs, fileIDs []string
	err := a.store.update(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || room.Kind != "ai" || room.OwnerID != u.ID {
			return forbidden()
		}
		kept := make([]Message, 0, len(st.Messages))
		for _, m := range st.Messages {
			if m.RoomID != roomID {
				kept = append(kept, m)
			}
		}
		st.Messages = kept
		for id, file := range st.Files {
			if file.RoomID == roomID {
				fileIDs = append(fileIDs, id)
				delete(st.Files, id)
			}
		}
		room.AIClearedThrough = st.NextMessage - 1
		if hide {
			hideRoomInList(st, &room, u.ID)
		}
		st.Rooms[roomID] = room
		for id, q := range st.AIRequests {
			if q.RoomID == roomID {
				aiIDs = append(aiIDs, id)
				delete(st.AIRequests, id)
			}
		}
		for id, q := range st.AgentJobs {
			if q.RoomID == roomID {
				agentIDs = append(agentIDs, id)
				delete(st.AgentJobs, id)
			}
		}
		for id, q := range st.Outbox {
			if q.RoomID == roomID {
				delete(st.Outbox, id)
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	for _, id := range fileIDs {
		if fileIDPattern.MatchString(id) {
			_ = os.Remove(filepath.Join(a.cfg.DataDir, "files", id))
		}
	}
	for _, id := range aiIDs {
		if a.aiJobs != nil {
			a.aiJobs.cancel(id)
		}
	}
	for _, id := range agentIDs {
		if a.agentJobs != nil {
			a.agentJobs.cancel(id)
		}
	}
	a.store.discardOldBackup()
	a.changed(roomID)
	respond(w, 200, map[string]bool{"ok": true})
}
