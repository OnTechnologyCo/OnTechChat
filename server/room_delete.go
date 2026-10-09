package server

import "net/http"

// 대화방 삭제는 본인의 목록/기록만 비웁니다. 공유방 탈퇴/상대 원본 삭제와 구분합니다.
func (a *App) deleteRoomFromList(w http.ResponseWriter, r *http.Request, u User) {
	var kind string
	err := a.store.view(func(st *State) error {
		room, ok := member(st, r.PathValue("room"), u.ID)
		if !ok {
			return forbidden()
		}
		kind = room.Kind
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if kind == "ai" {
		a.clearAIConversationWithList(w, r, u, true)
	} else {
		a.clearChatConversationWithList(w, r, u, true)
	}
}

func hideRoomInList(st *State, room *Room, userID string) {
	if room.HiddenFor == nil {
		room.HiddenFor = map[string]bool{}
	}
	room.HiddenFor[userID] = true
	delete(room.AutoHelp, userID)
	user := st.Users[userID]
	delete(user.PinnedRooms, room.ID)
	st.Users[userID] = user
}

// 친구 목록에서 직접 대화를 다시 열 때 목록만 복구하며 이전 삭제 경계는 유지합니다.
func (a *App) reopenRoomInList(roomID, userID string) error {
	err := a.store.update(func(st *State) error {
		room, ok := member(st, roomID, userID)
		if !ok {
			return forbidden()
		}
		if !room.HiddenFor[userID] {
			return errUnchanged
		}
		delete(room.HiddenFor, userID)
		st.Rooms[roomID] = room
		return nil
	})
	if err == nil {
		a.hub.publish([]string{userID}, roomID)
	}
	return err
}
