# OnTechChat — 단일 작업 경로

**한국어** | [English](README.en.md)

프로젝트 루트: D:\OnMakeSrc\OnTechChat
다른 에이전트는 **AGENTS.md → docs/WORK_STATUS.md** 순서로 읽고 작업한다.

| 용도 | 공식 경로 |
|---|---|
| Android 1.8.9 소스 | android/OnTechChat/ |
| Windows 서버 1.15.32 소스 | server/ |
| 통합 설치기 / 트레이 | installer/ / tray/ |
| 유일한 빌드 진입점 | build.ps1 |
| 배포 APK | output/OnTechChat-v1.8.9-arm64.apk |
| 서버 설치 파일 | output/OnTechChat-Server-Setup-v1.15.32-x64.exe |
| 배포 파일 해시 | output/manifest.json / SHA256SUMS.txt |
| 빌드 중간 파일·로그 | .build/ |
| 이전 버전 보관 | archive/ (수정·빌드·실행 금지) |

Server Setup 1.15.32은 공식 Android 1.8.9 APK를 내부 payload에 포함하며, 신규 설치/업데이트 시 서버 `DataDir\apk-releases`에 자동 게시한다. Android 앱은 기존 업데이트 API에서 해당 APK를 다운로드해 검증 후 OS 설치 화면을 사용한다.

PowerShell에서 실행:
- 전체 검증·빌드: .\build.ps1 -Target All
- Android 검증·빌드: .\build.ps1 -Target Android
- 서버 검증·빌드: .\build.ps1 -Target Server
- 테스트: .\build.ps1 -Target Tests
- 에뮬레이터용: .\build.ps1 -Target Emulator (배포 폴더에 출력하지 않음)

솔루션은 공식 앱 소스를 여는 용도다. 빌드는 위 스크립트로 실행한다.
출력 파일명은 버전을 포함하며 종류별 최신 파일 하나만 output에 둔다. 버전별 소스/배포 폴더는 새로 만들지 않는다.
기본 -Validation Core는 핵심 검사만 수행한다. 전체 회귀 검사는 필요한 경우에만 -Validation Full을 사용한다.
다운로드는 scripts/share.py 하나만 사용하며 .build/download.json에서 현재 주소를 확인한다.
임시 다운로드 주소는 프로세스가 종료되면 사용할 수 없다. 재부팅 자동 실행은 설정하지 않았다.
설치된 운영 서버·Caddy·데이터·방화벽은 이 정리 작업에서 변경하지 않았다.

1.15.12: AI 상단 모델/추론 선택 제거, 사용자 간 본인 대화 비우기, Codex/Claude 범용 웹검색, 관리자용 RemoteControl MCP 연결을 추가했다.
다른 PC의 CLI 설치·서비스 계정 인증 및 삭제 범위는 docs/server/AI-CLI-AND-CHAT.md를 확인한다.

1.15.11: 설치 PC의 Codex 실행 엔진을 공용 경로에 준비합니다. 설치 완료 후 서버용 Codex 로그인으로 AI 인증을 한 번 설정하세요. Windows 서비스 계정 변경이나 Windows 암호 입력은 필요 없습니다. 개인 Codex 인증은 복사하지 않으며 업데이트 후 서버 인증을 유지합니다. 상세 절차: docs/server/AI-CLI-AND-CHAT.md.

1.15.13: 친구/참여 대화 선택 → 범위 미리보기 → AI 요약·할 일·답장·질문 → 근거 이동과 초안 활용을 추가했습니다. 모바일 입력창 하단 여백을 축소했습니다. 사용법·보관 범위·제한: docs/server/CONVERSATION-HELP.md.

1.15.14 / Android1.8.0(180): GPS1분 간격 기록,48시간 보관, 암호화 로컬 큐·서버 재전송, 날짜/시간별 경로도, 별도 친구공유·중지·삭제를 추가했습니다. 사용법: docs/server/LOCATION-HISTORY.md. 서버와 앱 모두 업데이트하세요.

1.15.15 / Android1.8.1(181): AI 파일/이미지 첨부·Codex 이미지 수정, 채팅방별 자동 AI 도움 체크, 지도 이동경로(기본최근1시간), 기존 위치 허용에 따른 자동기록, 자동 업데이트 다운로드를 추가했습니다. 상세: docs/server/RELEASE-1.15.15.md.
