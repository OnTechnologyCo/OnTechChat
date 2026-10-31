package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type MailReport struct {
	Version          string          `json:"version"`
	Mode             string          `json:"mode"`
	OK               bool            `json:"ok"`
	Stage            string          `json:"stage"`
	Message          string          `json:"message"`
	CheckedAt        int64           `json:"checked_at"`
	AutomaticEnabled bool            `json:"automatic_enabled"`
	Recipients       []string        `json:"recipients"`
	Scanned          int             `json:"scanned"`
	Matched          int             `json:"matched"`
	Decoded          int             `json:"decoded"`
	Delivered        int             `json:"delivered"`
	Duplicates       int             `json:"duplicates"`
	Skipped          int             `json:"skipped"`
	Errors           int             `json:"errors"`
	Steps            []string        `json:"steps"`
	Items            []MailCheckItem `json:"items"`
}
type MailCheckItem struct {
	UID    uint32 `json:"uid"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

func (a *App) mailStatusView() map[string]any {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return map[string]any{"enabled": a.cfg.Mail.Enabled, "state": a.mailState, "checked_at": a.mailChecked, "report": a.mailLastReport}
}
func (a *App) runMail(ctx context.Context, cfg MailConfig, manual bool, authorize func() error) (report MailReport, err error) {
	report = MailReport{Version: version, Mode: "automatic", AutomaticEnabled: a.cfg.Mail.Enabled, Recipients: mailRecipients(cfg), Steps: []string{}, Items: []MailCheckItem{}}
	if manual {
		report.Mode = "manual"
	}
	if !a.mailRunMu.TryLock() {
		report.Stage = "busy"
		report.Message = "다른 메일 수집 작업이 진행 중입니다. 잠시 후 다시 누르세요."
		return report, errors.New(report.Message)
	}
	defer a.mailRunMu.Unlock()
	defer func() {
		report.CheckedAt = now()
		report.OK = err == nil && report.Errors == 0
		if err != nil {
			report.Message = err.Error()
		} else {
			report.Message = fmt.Sprintf("메일 %d건 확인 / 제목 일치 %d건 / 본문 처리 %d건 / 메시지 생성 %d건 / 중복 %d건 / 오류 %d건", report.Scanned, report.Matched, report.Decoded, report.Delivered, report.Duplicates, report.Errors)
		}
		a.stateMu.Lock()
		a.mailLastReport = report
		a.mailState = report.Message
		a.mailChecked = report.CheckedAt
		a.stateMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	report.Stage = "settings"
	if e := validateMailSubjectFilters(cfg, true); e != nil {
		return report, e
	}
	if len(mailRecipients(cfg)) == 0 {
		return report, errors.New("메일 수신 사용자를 한 명 이상 선택하세요.")
	}
	if _, _, err = readMailCredentials(cfg); err != nil {
		return report, errors.New("저장된 네이버 ID/앱 비밀번호가 없습니다.")
	}
	report.Stage = "login"
	connect := a.mailConnector
	if connect == nil {
		connect = connectNaver
	}
	c, closeConn, e := connect(ctx, cfg)
	if e != nil {
		return report, e
	}
	defer closeConn()
	report.Steps = append(report.Steps, "네이버 연결·로그인 성공")
	folder, e := imapQuote(modifiedUTF7(cfg.Folder))
	if e != nil {
		return report, errors.New("메일 폴더 이름을 확인하세요.")
	}
	report.Stage = "folder"
	box, e := c.command("EXAMINE " + folder)
	if e != nil {
		return report, errors.New("선택한 메일 폴더 조회 실패")
	}
	var validity uint32
	for _, line := range box.lines {
		if m := uidValidity.FindStringSubmatch(line); len(m) == 2 {
			v, _ := strconv.ParseUint(m[1], 10, 32)
			validity = uint32(v)
		}
	}
	if validity == 0 {
		return report, errors.New("폴더 응답에 UIDVALIDITY가 없습니다.")
	}
	report.Steps = append(report.Steps, "선택한 메일 폴더 읽기 성공 · 읽음 상태는 변경하지 않음")
	key := cfg.Account + "\x1f" + cfg.Folder
	var cursor MailCursor
	if e = a.store.view(func(st *State) error { cursor = st.MailCursors[key]; return nil }); e != nil {
		return report, errors.New("메일 처리 이력 조회 실패")
	}
	if cursor.Validity != validity {
		cursor = MailCursor{Validity: validity}
	}
	query := "UID SEARCH SINCE " + time.Now().Add(-24*time.Hour).Format("02-Jan-2006")
	if !manual && cursor.UID > 0 && cursor.UID < ^uint32(0) {
		query = fmt.Sprintf("UID SEARCH UID %d:*", cursor.UID+1)
	}
	report.Stage = "search"
	search, e := c.command(query)
	if e != nil {
		return report, errors.New("메일 UID 검색 실패")
	}
	ids := []uint32{}
	seen := map[uint32]bool{}
	for _, line := range search.lines {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "*" || !strings.EqualFold(f[1], "SEARCH") {
			continue
		}
		for _, s := range f[2:] {
			v, x := strconv.ParseUint(s, 10, 32)
			id := uint32(v)
			if x == nil && id > 0 && !seen[id] && (manual || id > cursor.UID) {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if manual && len(ids) > 50 {
		ids = ids[len(ids)-50:]
	}
	if !manual && len(ids) > 100 {
		ids = ids[:100]
	}
	report.Steps = append(report.Steps, fmt.Sprintf("대상 UID %d건 · 수동 검사는 최근 50건을 재확인하며 기존 커서를 변경하지 않음", len(ids)))
	for _, id := range ids {
		if e = ctx.Err(); e != nil {
			return report, errors.New("수집 작업 시간 초과 또는 취소 · 이미 생성된 메시지는 유지됩니다.")
		}
		report.Stage = "fetch"
		result, e := c.command(fmt.Sprintf("UID FETCH %d (UID INTERNALDATE RFC822.SIZE BODY.PEEK[]<0.%d>)", id, maxRawMail+1))
		if e != nil {
			return report, errors.New("메일 원문 조회 실패 · UID를 건너뛰지 않고 다음 수집에서 재시도합니다.")
		}
		report.Scanned++
		var when time.Time
		size := 0
		validUID := false
		for _, line := range result.lines {
			if m := internalDate.FindStringSubmatch(line); len(m) == 2 {
				when, _ = time.Parse("_2-Jan-2006 15:04:05 -0700", m[1])
			}
			if m := rfcSize.FindStringSubmatch(line); len(m) == 2 {
				size, _ = strconv.Atoi(m[1])
			}
			if m := fetchUID.FindStringSubmatch(line); len(m) == 2 {
				v, _ := strconv.ParseUint(m[1], 10, 32)
				if v == uint64(id) {
					validUID = true
				}
			}
		}
		item := MailCheckItem{UID: id}
		var parsed *ParsedAuthMail
		switch {
		case !validUID || len(result.literals) != 1:
			item.Result = "metadata_error"
			item.Detail = "UID 또는 본문 응답 누락"
			report.Errors++
		case size > maxRawMail || len(result.literals[0]) > maxRawMail:
			item.Result = "too_large"
			item.Detail = "메일 원문 2MiB 한도 초과"
			report.Errors++
		case manual && !when.IsZero() && when.Before(time.Now().Add(-time.Duration(cfg.MaxAgeMinutes)*time.Minute)):
			item.Result = "outside_window"
			item.Detail = "설정한 최근 메일 범위를 지남"
			report.Skipped++
		default:
			report.Stage = "parse"
			parsed, e = ParseAuthMail(result.literals[0], cfg)
			if parsed != nil {
				report.Matched++
			}
			if e != nil {
				item.Result = "parse_error"
				item.Detail = e.Error()
				report.Errors++
				parsed = nil
			} else if parsed == nil {
				item.Result = "subject_mismatch"
				item.Detail = "제목 필터 불일치"
				report.Skipped++
			} else {
				report.Decoded++
				item.Result = "matched"
			}
		}
		if when.IsZero() {
			when = time.Now()
		}
		if authorize != nil {
			if e = authorize(); e != nil {
				return report, e
			}
		}
		report.Stage = "delivery"
		added, duplicates, e := a.deliverMail(cfg, key, validity, id, parsed, when, !manual)
		if e != nil {
			return report, errors.New("메시지 저장 실패: 수신 사용자·저장 공간을 확인하세요. 처리 이력은 진행하지 않았습니다.")
		}
		report.Delivered += added
		report.Duplicates += duplicates
		if parsed != nil {
			item.Result = "delivered"
			item.Detail = fmt.Sprintf("메시지 %d건 생성 / 이미 전달 %d건", added, duplicates)
		}
		report.Items = append(report.Items, item)
	}
	report.Stage = "complete"
	report.Steps = append(report.Steps, "제목 비교·본문 디코딩·선택 사용자 수신함 저장 검사 완료")
	return report, nil
}
