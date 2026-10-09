//go:build windows

package main

import "os/exec"

func openAIServiceAccountSettings(p paths) error {
	if err := checkOwned(p); err != nil {
		return err
	}
	note := "Codex/Claude를 사용하는 Windows 계정으로 서버를 실행하도록 설정할 수 있습니다.\n\n" +
		"서버 전체가 선택한 계정 권한으로 실행됩니다. Windows 계정 암호가 필요하며 PIN은 사용할 수 없습니다.\n\n" +
		"1. 서비스 목록에서 'OnTech Chat Server'를 더블 클릭\n" +
		"2. [로그온] 탭 → [계정 지정]에서 CLI를 사용하는 이 PC의 계정 선택\n" +
		"3. 해당 Windows 암호 입력 → 적용\n" +
		"4. 이 서비스만 다시 시작\n" +
		"5. AI 설정의 '실제 호출' 확인\n\n" +
		"계정/암호는 Windows 서비스 관리 화면에만 입력합니다. 앱은 이를 읽거나 파일에 저장하지 않습니다.\n" +
		"사용자 폴더 권한을 넓히거나 인증 파일을 복사하지 않습니다. 업데이트 후에도 선택한 계정을 유지합니다.\n\n" +
		"Windows 서비스 관리 화면을 열까요?"
	if message(note, 0x124) != 6 {
		return nil
	}
	cmd := exec.Command(systemExe("mmc.exe"), systemExe("services.msc"))
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
