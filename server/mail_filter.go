package server

import (
	"errors"
	"strings"
)

// 구형 필드는 읽기 호환만 유지합니다. 정규식·발신자·수신 인증 결과는 실행하지 않습니다.
func mailSubjectFilter(c MailConfig) string {
	if c.SubjectFilter != "" {
		return strings.TrimSpace(c.SubjectFilter)
	}
	if len(c.Rules) == 1 {
		v := strings.TrimSpace(c.Rules[0].CodePattern)
		if strings.HasPrefix(v, "(?i)") && strings.Contains(v, "로그인") {
			return "로그인 코드"
		}
		return v
	}
	return ""
}
func normalizedMailSubject(s string) string {
	s = strings.NewReplacer("\u200b", "", "\ufeff", "").Replace(s)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

const maxMailSubjectFilters = 20
const maxMailSubjectFilterRunes = 200

// 한 줄에 한 조건. 쉼표/정규식 문자는 제목의 일부로 취급합니다.
func mailSubjectFilters(c MailConfig) []string {
	filters := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(mailSubjectFilter(c), "\r\n", "\n"), "\n") {
		value := strings.TrimSpace(line)
		key := normalizedMailSubject(value)
		if key != "" && !seen[key] {
			filters = append(filters, value)
			seen[key] = true
		}
	}
	return filters
}
func validateMailSubjectFilters(c MailConfig, required bool) error {
	raw := strings.ReplaceAll(mailSubjectFilter(c), "\r\n", "\n")
	if strings.ContainsAny(raw, "\r\x00") {
		return errors.New("제목 조건에는 줄바꿈 외의 제어 문자를 사용할 수 없습니다.")
	}
	for _, line := range strings.Split(raw, "\n") {
		if len([]rune(strings.TrimSpace(line))) > maxMailSubjectFilterRunes {
			return errors.New("제목 조건은 한 줄에 1~200자로 입력하세요.")
		}
	}
	filters := mailSubjectFilters(c)
	if len(filters) > maxMailSubjectFilters {
		return errors.New("제목 조건은 최대 20개까지 입력할 수 있습니다.")
	}
	if required && len(filters) == 0 {
		return errors.New("제목 조건을 한 줄에 하나씩 입력하세요.")
	}
	return nil
}
func mailSubjectMatches(subject string, c MailConfig) bool {
	subject = normalizedMailSubject(subject)
	for _, filter := range mailSubjectFilters(c) {
		if strings.Contains(subject, normalizedMailSubject(filter)) {
			return true
		}
	}
	return false
}
