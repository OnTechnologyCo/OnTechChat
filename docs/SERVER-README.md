OnTechChat Server 1.15.34 / Android 1.8.10 (190).
서버 Setup EXE에는 빌드 시 검증한 공식 Android APK가 포함됩니다.
신규 설치와 서버 업데이트 시 포함 APK를 ProgramData의 private\apk-releases에 자동 게시합니다.
Android 앱은 로그인한 서버의 /api/app-updates/android에서 APK를 다운로드하고 package/version/size/SHA256/signing certificate를 확인한 후 OS 설치 화면을 사용합니다.
이미 게시된 더 높은 versionCode를 낮추거나 같은 versionCode의 다른 APK를 덮어쓰지 않습니다.
내 위치 조회 허용이 켜져 있으면 앱을 열 때1분 간격 이동 기록을 자동 시작합니다. 별도 기록 스위치는 없고48시간 보관합니다.
연결이 끊기면 기기에 암호화 보관하고 재연결 시 전송합니다. 기록 중에는 Android 알림과 중지 버튼을 표시합니다.
기존 내 위치 조회 허용 범위로 경로를 공유합니다. 새 허용은 모든 친구 대상이고 기존 선택 공유는 자동 확대하지 않습니다. 관리자 우회는 없습니다.
내 프로필/친구 프로필 → 이동 경로에서 지도와 시간별 지점을 확인합니다. 기본 최근1시간,1/2/6/12/24/48시간 즉시 조회를 지원합니다.
현재 위치 확인 버튼은 이동 경로로 통합했습니다. GPS 알림 권한 없이 기록할 수 있으며 해당 알림 채널 표시는 Android 설정에서 끕니다.
공식 배포: output/OnTechChat-v1.8.10-arm64.apk, output/OnTechChat-Server-Setup-v1.15.34-x64.exe.
Windows 설치·업데이트·제거의 기존 설정·계정·대화·첨부 보존 정책을 유지합니다.
공용 Codex는 설치 PC의 실행 엔진을 준비하고 서버용 AI 로그인을 사용합니다. 개인 인증은 복사하지 않습니다.
GPS 사용법·API·제한: docs/LOCATION-HISTORY.md. AI 설정: docs/AI-CLI-AND-CHAT.md.


도메인 자동 HTTPS: 서버 설정 → 외부 접속. 상세 순서는 docs/RELEASE-1.15.34.md를 확인하세요. 기존 IP 접속을 보존하며 DNS 전용과 외부 TCP443 연결이 필요합니다.

Windows 설치 파일은 Inno Setup 설치 마법사입니다. 기존 설치기의 설정·데이터 보존과 서비스 업데이트·제거 처리를 유지합니다. 설치 후 설정 화면에서 도메인 HTTPS를 구성하세요.
