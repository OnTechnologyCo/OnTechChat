package server

import (
	"errors"
	"net/http"
	"time"
)

// 외부 설치 PC에서 실행됩니다. 모델/개발 PC로 메일·비밀번호를 전송하지 않습니다.
func (a *App) collectMailSettings(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		Revision string           `json:"revision"`
		Settings EditableSettings `json:"settings"`
		Deliver  bool             `json:"deliver"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := a.requireSettingsAdmin(r, u); e != nil {
		fail(w, e)
		return
	}
	if !in.Deliver {
		fail(w, bad("수신함으로 실제 전달하는 테스트 실행 확인이 필요합니다."))
		return
	}
	if !a.limiter.allow("mail-collect:"+u.ID, 6, time.Minute) {
		fail(w, apiError{429, "메일 수집 테스트는 1분에 6번까지 가능합니다."})
		return
	}
	a.settingsMu.Lock()
	c, revision, e := a.readSavedSettings()
	applying := a.applying
	a.settingsMu.Unlock()
	if e != nil {
		fail(w, e)
		return
	}
	if applying || revision != in.Revision {
		fail(w, apiError{409, "설정이 바뀌었거나 적용 중입니다. 설정을 다시 읽고 실행하세요."})
		return
	}
	// 화면의 메일 입력값만 사용합니다. 다른 설정의 미저장 변경은 테스트에 섞지 않습니다.
	edit := settingsEditable(c)
	edit.Mail = in.Settings.Mail
	edit.Mail.Enabled = true
	c, e = a.candidateSettings(c, edit)
	if e != nil {
		fail(w, e)
		return
	}
	authorize := func() error {
		if r.Context().Err() != nil || a.ctx.Err() != nil {
			return errors.New("수집 요청이 취소되었습니다.")
		}
		current, e := a.identify(r)
		if e != nil || current.ID != u.ID || !isSuperadmin(current) {
			return errors.New("관리자 권한 또는 세션이 만료되어 전달을 중단했습니다.")
		}
		return nil
	}
	report, _ := a.runMail(r.Context(), c.Mail, true, authorize)
	// report에는 메일 원문·제목·인증번호·비밀번호가 아닌 단계/UID/결과만 들어갑니다.
	respond(w, 200, report)
}
