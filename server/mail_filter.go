package server

import "strings"

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
func mailSubjectMatches(subject string, c MailConfig) bool {
	filter := normalizedMailSubject(mailSubjectFilter(c))
	return filter != "" && strings.Contains(normalizedMailSubject(subject), filter)
}
