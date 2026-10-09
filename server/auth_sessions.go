package server

import (
	"net/http"
	"time"
)

// 쿠키는 브라우저의 최대 보관 기간 안에서 갱신하고, 서버 세션은 명시적 해제까지 유지합니다.
const sessionCookieSeconds = 400 * 24 * 60 * 60

func (s Session) active() bool {
	return s.Persistent || s.Expires > now()
}

func invalidSession() error {
	return codedAPIError{status: http.StatusUnauthorized, code: "SESSION_INVALID", message: "로그인이 필요합니다."}
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: "ontech_session", Value: token, Path: "/", HttpOnly: true,
		Secure: a.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: sessionCookieSeconds,
		Expires: time.Now().Add(time.Duration(sessionCookieSeconds) * time.Second)})
}

// 기존에 유효한 로그인만 이전합니다. 만료·로그아웃·회수된 인증을 되살리지 않습니다.
func migratePersistentSessions(store *Store) error {
	return store.update(func(st *State) error {
		changed := false
		for hash, session := range st.Sessions {
			user, exists := st.Users[session.UserID]
			if !session.Persistent && session.Expires > now() && exists && !user.Disabled {
				session.Persistent = true
				session.Expires = 0
				st.Sessions[hash] = session
				changed = true
			}
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
}
