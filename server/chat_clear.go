package server

import "net/http"

func roomClearedThrough(room Room, userID string) int64 {
	cutoff := room.ClearedThrough[userID]
	if room.AIClearedThrough > cutoff {
		cutoff = room.AIClearedThrough
	}
	return cutoff
}

// 대화 비우기는 요청자에게만 적용한다. 다른 참여자의 원본/첨부파일은 보존한다.
func (a *App) clearChatConversation(w http.ResponseWriter, r *http.Request, u User) {
	a.clearChatConversationWithList(w, r, u, false)
}

func (a *App) clearChatConversationWithList(w http.ResponseWriter, r *http.Request, u User, hide bool) {
	roomID := r.PathValue("room")
	var cutoff int64
	err := a.store.update(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || (room.Kind != "direct" && room.Kind != "chat" && room.Kind != "auth") {
			return forbidden()
		}
		cutoff = st.NextMessage - 1
		if room.Kind == "auth" {
			// 개인 수신함은 다른 참여자가 없으므로 본인 메일 메시지를 실제 저장소에서도 제거합니다.
			// MailDelivered 이력은 유지해 수동 재검사 시 삭제한 과거 메일이 되살아나지 않게 합니다.
			kept := st.Messages[:0]
			for _, m := range st.Messages {
				if m.RoomID == roomID && m.ID <= cutoff {
					continue
				}
				kept = append(kept, m)
			}
			st.Messages = kept
		}
		if room.ClearedThrough == nil {
			room.ClearedThrough = map[string]int64{}
		}
		if cutoff > room.ClearedThrough[u.ID] {
			room.ClearedThrough[u.ID] = cutoff
		}
		if cutoff > room.Members[u.ID] {
			room.Members[u.ID] = cutoff
		}
		if hide {
			hideRoomInList(st, &room, u.ID)
		}
		st.Rooms[roomID] = room
		for id, q := range st.Outbox {
			if q.RoomID == roomID && q.UserID == u.ID && q.MessageID > 0 && q.MessageID <= cutoff {
				delete(st.Outbox, id)
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(roomID)
	respond(w, 200, map[string]any{"ok": true, "cleared_through": cutoff, "scope": "self"})
}
