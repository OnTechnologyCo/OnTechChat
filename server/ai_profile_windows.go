//go:build windows

package server

import (
	"path/filepath"
	"syscall"
)

func cliProfileForSID(sid string, profiles []windowsProfileEntry) string {
	for _, profile := range profiles {
		if profile.SID == sid && filepath.IsAbs(profile.Path) {
			return profile.Path
		}
	}
	return ""
}

// 서비스 환경 변수가 다른 홈을 가리켜도 현재 실행 토큰의 프로필만 사용한다.
// 실행 파일 소유자나 다른 로그인 사용자의 인증 홈으로 전환하지 않는다.
func currentCLIProfile() string {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return ""
	}
	var token syscall.Token
	if syscall.OpenProcessToken(process, syscall.TOKEN_QUERY, &token) != nil {
		return ""
	}
	defer token.Close()
	owner, err := token.GetTokenUser()
	if err != nil {
		return ""
	}
	sid, err := owner.User.Sid.String()
	if err != nil {
		return ""
	}
	return cliProfileForSID(sid, nativeWindowsCLIProfiles())
}
