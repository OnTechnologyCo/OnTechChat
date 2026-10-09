# OnTechChat AI 친구·서버 에이전트 진행상황

작성일: 2026-09-28
대상: D:\OnMakeSrc\OnTechChat
현재 서버: 1.14.0
목표: 1.14.0

## 현재 단계
- 단계 A~F 구현 및 검증 완료.
- AGENTS.md, README.md, WORK_STATUS.md, 기존 AI 구현/진행 문서, 새 구현 프롬프트 확인 완료.
- 변경 전 백업은 .build/ai-friend-agent-before 아래에 보관.
- 기존 AI 대화·위치·인증함·알림·설치/업데이트 동작 보존.
- AI 친구, superadmin 이행, project/grant, Windows AppContainer runner, Agent tool loop, 등록 프로그램 제어 구현 완료.
- 공식 Tests/Core 및 Server/Core 빌드 성공.
- 운영 설치/서비스 재시작은 수행하지 않음.

## 상태 표기
- 완료: 실제 코드 + 필요한 자동/통합 검증 통과
- 미검증: 구현은 있으나 실제 환경 증거 없음
- 차단: 안전 조건/권한/환경 때문에 기능을 닫은 상태

## 2026-09-28 단계 B 결과

### 완료
- 단계 A: 계획/변경 전 백업 완료.
- 단계 B 서버: AIProfile 기본 친구 `온테크 AI`, 사용자별 기본 AI 방, 기존 AI 방 연결 이행.
- 역할: `user / admin / superadmin` 중앙 권한 계층 추가.
- 기존 `admin`은 1회 멱등 마이그레이션에서 `superadmin`으로 승격.
- 설정/credential/APK 배포/서버 상태/메일 관리 권한을 최고관리자 경계로 이동.
- admin은 명시적 `user.manage` grant가 있는 일반 사용자만 관리.
- 마지막 활성 superadmin 사용 중지/강등 방지.
- Project/AccessGrant 저장 구조와 API 추가.
- 친구 목록에 AI 친구 API/UI 연결.
- 신규 상태 map이 snapshot clone에서 nil로 돌아가는 문제를 테스트로 발견해 `Store.update`에서 보정.

### 검증
- `build.ps1 -Target Tests -Validation Core`: 성공.
- Core 필터를 `Test(Location|AllFriends|AI|Agent)`로 확장.
- Agent 신규 테스트: 역할 이행 멱등성, AI 친구 중복 open/사용자 격리, project grant 경계, 위임 admin 권한 상승 거절.
- Android Tests 87 PASS, NotificationPolicy.Tests 22 PASS.
- `node --check server/web/app.js`, `messenger.js`: 통과.

### 현재 단계
- 단계 C 진행 중: Windows AppContainer + ACL + Job Object + 실제 제한 프로세스 검증.
- Agent 설정 기본값은 비활성. 실제 Windows 격리 self-test가 통과하기 전 ready로 표시하지 않음.

## 2026-09-28 단계 C 결과

### 완료
- 사용자별 안정적 AppContainer profile/SID 생성.
- STARTUPINFOEX + SECURITY_CAPABILITIES로 실제 AppContainer 프로세스 생성.
- 생성 직후 child token의 TokenIsAppContainer 및 AppContainer SID를 실제 확인.
- workspace에는 해당 SID Modify ACL, 승인 toolchain은 Read/Execute ACL만 부여.
- AppContainer 전용 LOCALAPPDATA Packages/AC/Temp 경로를 workspace 내부에 구성.
- 환경은 명시적으로 구성하며 AppData/HOME/TEMP/cache를 workspace로 분리.
- CREATE_SUSPENDED → Job Object 할당 → Resume 순서 적용.
- Job Object에 kill-on-close, active process, memory, job CPU time 제한 적용.
- capability를 주지 않아 네트워크 기본 차단.

### 실제 Windows 통합 검증
- TestAgentWindowsAppContainerBuildAndIsolation: PASS.
- 프로젝트 Go toolchain을 AppContainer 내부에서 실제 실행하여 probe.exe 빌드 성공.
- 빌드/실행 child token IsAppContainer=true.
- workspace 파일 읽기 성공.
- workspace 밖 테스트 secret 파일 읽기 실패.
- host loopback TCP 연결 실패.
- host 테스트 프로세스 OpenProcess 실패.
- TestAgentWindowsDistinctSIDAndJobCancellation: PASS.
- 두 owner의 AppContainer SID가 실제로 서로 다름.
- context 취소 → Job Object 종료 후 지연 자식 marker가 생성되지 않아 자식/손자 탈출 방지 확인.

### 다음
- 단계 D: AgentJob worker + Claude 구조화 planner + workspace 도구(write/read/build/run) + artifact + 채팅 결과 연결.

## 2026-09-28 20:39 재개 후 진행상황

### 단계 D — LLM 도구 루프 / 결과 전달
진행 중이며 핵심 서버 코드 연결 완료:
- Claude CLI structured output planner 추가: `agent_planner.go`
- `--json-schema` 실제 Claude 2.1.283 호출 검증 성공.
- 실제 응답의 `structured_output`에서 `workspace.write` tool call 수신 확인.
- 허용 도구를 `workspace.list/read/write`, `build.run`, `program.run`으로 제한.
- AgentJob queue / planning / running / completed / failed / cancelled / interrupted 상태 구현.
- 사용자·프로젝트별 동시성, queue limit, client_id 중복 방지, project 단위 활성 작업 충돌 방지 구현.
- 도구 실행 직전 사용자/프로젝트 권한 재검사.
- workspace 경로 검증, 절대경로/.. / drive/ADS/링크 경로 차단.
- Windows Runner 실행 직전 workspace 전체 reparse point/symlink/hard-link 검사 추가.
- AppContainer 내부 Go build 및 project exe 실행 연결.
- artifact hash/size/MIME 저장 및 권한 재검사 다운로드 API 추가.
- 서버 재시작 시 활성 AgentJob을 interrupted로 정리.
- 현재 서버 패키지 compile-only Go test 통과.

미완료:
- Agent 작업 UI 최종 연결(프로젝트 선택/작업 모드/상태/취소/산출물 링크).
- AI 일반 답변과 Agent 작업을 동일 AI 방에서 동시에 시작하지 못하게 하는 상호 배제 최종 정리.
- Agent mock/E2E 자동 테스트 보강.
- 실제 Claude → file write → AppContainer build → run → artifact → chat 최종 응답 full E2E 검증.

### 단계 E — 등록 프로그램 제어
진행 중이며 서버 측 기본 계약 구현:
- ProgramDefinition / ProgramInstance 저장 구조.
- superadmin 전용 프로그램 등록.
- 고정 executable SHA256 확인.
- 사용자 작업공간 내부 executable/workdir 등록 차단.
- 고정 action: start/status/stop/force_stop.
- 사용자 입력으로 executable/cwd/shell 문자열을 받지 않음.
- resource lock 적용.
- 서버가 직접 시작하고 추적한 exact process instance만 stop/kill.
- 정상 종료는 등록된 stop-file signal 방식, 강제 종료는 별도 allow_force action.
- 서버 재시작 시 저장상 running instance를 interrupted로 정리.
- agent runtime 진단 API 추가.

미완료:
- 무해한 fixture 프로그램을 사용한 start/status/normal stop/force stop 실제 통합 테스트.
- program grant/API 권한 회귀 테스트.
- 관리 UI에서 ProgramDefinition 등록/Grant 편집 화면.

### 관리자 설정/UI
- Agent 설정 구조는 기존 저장/적용 경로에 포함.
- workspace root 변경은 프로젝트/활성 작업이 존재하면 차단.
- 서버 설정 화면에 Agent 탭 추가 중:
  - enabled
  - workspace root
  - Go root
  - concurrency / per-user concurrency
  - queue / process / memory / timeout / output / project quota
  - network=blocked
  - isolation=appcontainer
  - 실행기 진단 버튼
- 서버 설정 화면 접근은 superadmin 기준으로 변경.
- AI 친구는 친구 목록에서 이미 표시/열기 가능.
- 채팅 상단에 작업 모드/프로젝트 컨트롤 HTML 추가했으나 JS 연결은 아직 진행 중.

### 현재 검증 상태
완료:
- 단계 B Core 테스트 통과 기록 유지.
- 단계 C 실제 Windows AppContainer 통합 테스트 통과 기록 유지.
- 현재 추가 소스 기준 `go test ./... -run '^$'` compile-only 통과.
- Claude structured output 실제 호출 성공.

아직 실행 필요:
- 변경 후 전체 `build.ps1 -Target Tests -Validation Core`
- 단계 D live end-to-end
- 단계 E fixture 통합 테스트
- JavaScript `node --check`
- 공식 `build.ps1 -Target Server -Validation Core`
- 1.13.0 installer / manifest / SHA256 최종 확인.

### 현재 주의
- 현재는 개발 중간 상태입니다. 공식 1.13.0 배포 완료 상태가 아닙니다.
- 운영 서비스 설치/재시작/방화벽/운영 데이터 변경은 수행하지 않았습니다.
## 2026-09-28 최종 단계 F 결과

### 완료
- AI 친구가 친구 목록에 표시되고 사용자별 기본 AI 방을 멱등 생성/열기.
- 기존 AI 방은 기본 AIProfile에 연결하며 사용자별 대화 소유권은 유지.
- 역할을 user / admin / superadmin으로 분리하고 기존 admin을 1회 멱등 이행.
- 마지막 활성 superadmin 보호, admin 위임 범위 제한, AccessGrant 발급/회수 구현.
- 사용자별 Agent Project 생성/조회/수정과 revision/소유권/Grant 검사 구현.
- 같은 AI 방에서 일반 AI 생성과 Agent 작업이 동시에 실행되지 않도록 양방향 상호 배제.
- Agent 상태 queued → planning → running → completed/failed/cancelled/interrupted 구현.
- owner+project+client_id idempotency와 tool_call_id 중복 방지 구현.
### Windows 실행 격리
- 실제 Windows AppContainer profile/SID 생성 및 child token 검증.
- 사용자별 SID 분리.
- workspace Modify ACL, 승인 toolchain Read/Execute ACL만 부여.
- HOME/USERPROFILE/APPDATA/LOCALAPPDATA/TEMP/cache를 workspace 아래로 제한.
- CREATE_SUSPENDED → Job Object assign → Resume.
- Job Object process/memory/time/kill-on-close 제한.
- capability 없는 AppContainer로 host loopback 네트워크 차단 확인.
- workspace 외 secret 파일 읽기 차단, host process OpenProcess 차단 확인.
- symlink/reparse point/hard-link workspace 실행 전 검사.
- context 취소 시 자식/손자 프로세스 탈출 방지 확인.
### Agent LLM/tool loop
- Claude Code CLI 2.1.283의 --json-schema structured_output 사용.
- 허용 도구: workspace.list/read/write, build.run, program.run, host_program.action.
- 알 수 없는 특권 필드와 도구별 불필요 필드는 schema/서버 검증으로 거절.
- 프로젝트 파일/도구 출력에 포함된 지시문은 데이터로 취급하도록 planner 경계 설정.
- 실제 Claude live E2E 성공:
  - Claude가 go.mod/main.go 생성
  - AppContainer에서 bin/app.exe 빌드
  - AppContainer에서 실행
  - 출력 ONTECH_AGENT_OK 확인
  - Windows PE artifact 등록/인증 다운로드
  - Agent 최종 채팅 결과 저장
- 기록: .build/logs/agent-live-e2e-verification.log
### 등록 프로그램 제어
- superadmin 전용 ProgramDefinition 등록/수정.
- 일반 사용자에게는 Grant된 프로그램/동작만 공개.
- program_id + action만 Agent에 전달하며 executable/cwd/shell/실행계정은 사용자 입력으로 받지 않음.
- 허용 경로를 Program Files, Windows, 서버 관리 registered-programs로 제한.
- executable SHA256을 등록/시작 시 재확인.
- 사용자 Agent workspace 실행 파일을 privileged program으로 등록 금지.
- start/status/stop/force_stop 분리.
- force_stop은 AllowForce가 명시된 정의에서만 허용.
- resource lock, ProgramInstance, PID/실행경로/시작시각 추적.
- 서버가 직접 시작하고 추적한 인스턴스만 종료.
- 무해한 fixture 실제 start → status → normal stop 및 force stop 테스트 성공.
- 등록 프로그램을 Agent 채팅 tool loop에서 start → status → stop 하는 통합 테스트 성공.
- 비신뢰 경로 등록 거절 및 등록 후 exe 변조 시 start 409 거절 테스트 성공.
### UI / 설정
- 친구 목록 AI 친구.
- AI 방의 대화 모드 / 작업 모드 전환.
- Agent 프로젝트 생성/선택.
- 작업 단계/실패 이유/취소/산출물 링크 표시.
- superadmin 서버 설정에 Agent 설정, runtime 진단, ProgramDefinition 등록, Grant 발급/회수 UI.
- Agent 설정은 기존 설정 저장/적용 구조에 포함.
- 프로젝트가 존재하는 동안 workspace root 변경 차단.
- 서버 설정/credential/실행기 정책/program 정의/역할 변경은 superadmin 경계.
- WebView 공통 UI를 사용하므로 Android 네이티브 화면 추가 없음.
### 최종 검증
- node --check:
  - server/web/app.js PASS
  - server/web/settings.js PASS
  - server/web/messenger.js PASS
- config.example.json ConvertFrom-Json PASS.
- build.ps1 -Target Tests -Validation Core:
  - Android Tests 87 PASS
  - NotificationPolicy.Tests 22 PASS
  - Go Core Test(Location|AllFriends|AI|Agent) PASS, 최종 공식 Tests 실행 136.130s
  - BUILD SUCCESS: Tests
- build.ps1 -Target Server -Validation Core:
  - Go Core PASS
  - payload 9 files verified
  - payload SHA256 f276c5b6c03ad338cc9b8fc7f0190c7b08a430a596e864445b4474cae695015f
- Windows Runner에 Job Object CPU 제한과 별도로 동일 timeout의 wall-clock watchdog을 추가해 제한 종료 안정성을 보강.
- 취소는 context.Canceled로 유지하고 내부 시간 제한은 별도 time-limit 오류로 구분.
- TestAgentWindowsResourceLimits 3회 반복 PASS, 취소/리소스 제한 통합 PASS.
  - OnTechChat Server 1.13.0
  - BUILD SUCCESS: Server
### 공식 산출물
- output/OnTechChat-Server-Setup-v1.13.0-x64.exe
- size: 10,506,752 bytes
- SHA256: 4bf374f4119f7adb71eaf4ba5632a5b8752b2bd85b13aa0860837087909d4137
- Android는 네이티브 변경 없음:
  - output/OnTechChat-v1.7.0-arm64.apk
  - SHA256 362d3fa42b2fd2bad19baad251dfc06b7b33e012887281efaba708a5e6ef1f2b

### 미검증
- 실제 설치된 Windows 서비스 계정(Session 0)에서 Claude CLI PATH/HOME/인증 상태.
- 실제 휴대전화 WebView에서 작업 모드의 가상 키보드/긴 코드/스크롤/다운로드 UX.
- 실제 운영 업무 프로그램 등록 및 제어. 이번 검증은 전용 fixture만 사용.
- 운영 데이터의 실제 1.13.0 이행. 운영 설치를 하지 않았으므로 소스/테스트 환경 이행만 검증.
### 차단/의도적 비활성
- Codex CLI Agent/대화 사용: built-in 도구 전체 격리 조건을 확실히 보장하지 못해 runtime unavailable 유지.
- 임의 shell/PowerShell/cmd 명령 실행 기능: 제공하지 않음.
- 임의 host executable/cwd/실행계정 입력: 제공하지 않음.
- 운영 서비스 교체·재시작·방화벽·운영 ACL 변경: 별도 운영 적용 지시 없으므로 수행하지 않음.

### 최종 상태
- 단계 A~F 완료.
- 공식 빌드 작업 상태: completed.
- 서버 기능 버전: 1.13.0.
- 운영 적용만 별도 단계로 남음.
## 2026-09-28 22:18 Setup 내 Android APK 자동 배포 작업

### 사용자 요구
- 서버 Setup EXE 안에 현재 공식 Android APK를 포함.
- 신규 설치/서버 업데이트 때 APK도 서버 데이터의 apk-releases로 자동 배포.
- Android 앱은 기존 /api/app-updates/android 및 다운로드 API로 APK를 받아 설치.
- 별도의 수동 APK 복사를 기본 절차로 요구하지 않음.

### 구현 방향
- Server 빌드는 output의 공식 ARM64 APK 존재/서명/package/version/minSdk를 검증한 뒤 digest 이름 APK + latest.json을 Setup payload에 포함.
- 설치기는 서버/설정 적용 후 기존 서버의 --publish-apk 기능으로 DataDir/apk-releases에 원자 배포.
- 기존 APK가 더 최신이면 downgrade 금지 규칙 유지.
- 동일 versionCode 다른 APK도 기존 publish 정책대로 거절.
- Android 다운로드 시 기존 size/SHA256/package/version/signer 검증 흐름 유지.
- 기능 변경으로 서버/설치기/트레이/웹 버전을 1.14.0으로 증가 완료. Android는 1.7.0 유지.
### 22:35 이후 구현/검증 완료
- `build.ps1`에 `Prepare-SetupAPKBundle` 추가.
- Server 빌드 시 공식 `output/OnTechChat-v1.7.0-arm64.apk`가 없으면 Setup 빌드를 중단.
- apksigner로 서명/단일 signer 검증.
- aapt2로 package=com.ontechdev.chat, versionCode=170, versionName=1.7.0, minSdk=26 확인.
- zipalign 재검증.
- APK SHA256 `362d3fa42b2fd2bad19baad251dfc06b7b33e012887281efaba708a5e6ef1f2b`.
- signing certificate SHA256 `7fb644ceceefc9cfa7e6dbf775c1c21833bb62f79a792ef65cd2d82ddb2a99f1`.
- payload에 `apk-release/latest.json`과 digest 이름 APK 포함.
- installer payload 검증에서 APK manifest 1개 + digest APK 1개를 필수화.
- 설치/업데이트 과정에서 `OnTechChat.Server.exe --config ... --publish-apk <설치폴더\apk-release>` 실행 후 서비스 시작.
- 기존 publishAPK의 downgrade/동일 versionCode 다른 hash 거절 및 atomic manifest 교체 정책 재사용.

### 실제 검증
- 공식 `build.ps1 -Target Server -Validation Core` 성공.
- `EMBEDDED APK: Android 1.7.0 (170)` 확인.
- `Payload verified: 11 files`.
- payload SHA256 `de088e2e912255001a03b93bd5190d82c4171f0c4615b4dc52a356d48c838e1c`.
- `OnTechChat Server 1.14.0`.
- installer embedded payload 테스트 PASS.
- 서버 `TestAPK*` 업데이트 게시/ACL/metadata 테스트 PASS.
- 생성된 Setup EXE의 실제 `--publish-apk`를 .build 임시 config/data에서 실행: exit 0.
- 임시 `data\apk-releases`에 latest.json + digest APK 생성 확인.
- 원본 APK와 게시 APK SHA256 동일 확인.
- installer 전체 `go test ./...` PASS.
- Android 기존 구현에서 서버 update API 다운로드 → size/SHA256/package/version/signer 재검사 → FileProvider → Android 설치 확인 화면 연결 확인.

### 최종 산출물
- `output/OnTechChat-Server-Setup-v1.14.0-x64.exe`
- 크기: 38,599,168 bytes
- SHA256: `d46e5a6c5959b0febd982fa9b1ae72169e00dee433e1ead8e283e753f53a22a1`
- 별도 공식 APK는 그대로 `output/OnTechChat-v1.7.0-arm64.apk`.

### 미검증
- 실제 Program Files/ProgramData에 대한 UAC 신규 설치 및 운영 서버 업데이트는 수행하지 않음.
- 실제 Android 단말에서 1.14.0 서버가 자동 게시한 APK를 다운로드하여 OS 설치 화면까지 여는 실기기 동작은 이번 변경에서 재실행하지 않음.
## 2026-09-28 22:40 Setup APK 자동 포함/게시 완료

### 완료
- 서버/설치기/트레이/웹 버전을 1.14.0으로 증가. Android는 1.7.0 / versionCode 170 유지.
- `build.ps1 -Target Server` 실행 시 `output/OnTechChat-v1.7.0-arm64.apk` 존재를 필수로 확인.
- APK 서명, signer 1개, package, versionCode/versionName, minSdk, zipalign을 재검증한 뒤 Setup payload 생성.
- APK는 SHA256 digest 파일명으로 `apk-release/` 아래 포함하고 같은 폴더에 `latest.json` 생성.
- installer payload 검사에서 `apk-release/latest.json` 1개와 digest 이름 APK 1개를 필수로 요구.
- Setup 설치/업데이트 마지막 단계에서 서비스 시작 전에 설치된 통합 EXE의 `--publish-apk` 경로로 APK 자동 게시.
- 기존 서버의 downgrade 방지 및 same-version/different-hash 거절 정책 유지.
- Android 다운로드/설치 API는 기존 `/api/app-updates/android` 경로 재사용.

### 실제 검증
- 공식 APK: 1.7.0 (170), 28,470,282 bytes.
- APK SHA256: `362d3fa42b2fd2bad19baad251dfc06b7b33e012887281efaba708a5e6ef1f2b`.
- APK signer SHA256: `7fb644ceceefc9cfa7e6dbf775c1c21833bb62f79a792ef65cd2d82ddb2a99f1`.
- Server build: `EMBEDDED APK: Android 1.7.0 (170)` 확인.
- Setup payload: 11 files, SHA256 `de088e2e912255001a03b93bd5190d82c4171f0c4615b4dc52a356d48c838e1c`.
- 완성된 Setup EXE를 `.build/setup-apk-integration` 임시 config/data에서 `--publish-apk`로 실제 실행.
- 결과: `Published Android 1.7.0 (code 170). Server restart not required.`
- 게시된 APK의 SHA256과 크기가 공식 APK와 동일.
- installer `go test ./...` PASS.
- server `TestAPK` PASS.
- `build.ps1 -Target Tests -Validation Core` PASS: Android 87, NotificationPolicy 22, Go Core PASS.

### 공식 산출물
- `output/OnTechChat-Server-Setup-v1.14.0-x64.exe`
- size: 38,599,168 bytes
- SHA256: `d46e5a6c5959b0febd982fa9b1ae72169e00dee433e1ead8e283e753f53a22a1`
- 통합 게시 검증 로그: `.build/logs/setup-apk-publish-integration.log`

### 운영 미검증
- 실제 Program Files/ProgramData 경로에서 UAC를 통한 1.14.0 신규 설치/업데이트는 수행하지 않음.
- 실제 Android 단말에서 1.14.0 서버가 자동 게시한 APK를 다운로드하여 OS 설치 화면까지 여는 실기기 검증은 수행하지 않음.

## 2026-09-28 22:18 임시 다운로드 재개
- 공식 다운로드 공유는 `scripts/share.py` 단일 경로를 사용.
- 새 Cloudflare 임시 주소 생성 완료.
- Server Setup HEAD 200 / Content-Length 38,599,168 / 파일명 `OnTechChat-Server-Setup-v1.14.0-x64.exe` 확인.
- Android APK HEAD 200 / Content-Length 28,470,282 / 파일명 `OnTechChat-v1.7.0-arm64.apk` 확인.
- 이 링크는 `scripts/share.py`/cloudflared 프로세스가 실행 중일 때만 유효함.
## 2026-09-28 AI 설정/Agent UX 단순화 재설계
- 사용자 요구: 캡처와 같이 모델별 AI 설정만 간단히 선택.
- Agent 제한/프로젝트/Grant/workspace 경로 설정 UI 제거.
- AI 대화 시작 시 사용자별 기본 workspace 자동 생성.
- workspace에 역할별 관리 파일 `AGENT.md`, 호환용 `AGENTS.md`, `CLAUDE.md` 자동 생성.
- MD에는 현재 사용자 username/role/workspace 범위와 사용 가능한 서버 도구 원칙을 기록.
- Claude Agent planner가 이 정책 MD를 매 요청 자동 입력으로 읽음.
- 일반 질문은 tool 없이 최종 답변, 작업 요청은 LLM이 허용된 구조화 tool을 스스로 선택.
- 실제 권한은 MD가 아니라 기존 서버 소유권 검사/AppContainer/등록 프로그램 action 검증이 최종 enforce.
- 기존 AgentProject 엔진은 내부 호환 계층으로 유지하되 사용자별 기본 project 하나를 자동 선택/생성.
- 목표 서버 기능 버전: 1.15.0.

## 2026-09-28 23:43 AI 설정·자동 Workspace 단순화

### 적용 방향
- AI 설정 화면은 provider별 모델과 추론 강도 중심으로 단순화.
- 별도 서버 Agent 설정 화면, 작업모드, 프로젝트 선택/생성 UI, Grant UI 제거.
- Claude 모델은 기본/low/medium/high/xhigh/max 추론 강도를 선택 가능.
- AI 대화를 열거나 첫 메시지를 보낼 때 사용자별 기본 workspace를 자동 생성.
- 사용자별 workspace에 `AGENT.md`, `AGENTS.md`, `CLAUDE.md`를 서버가 자동 생성/갱신.
- 정책 파일에는 현재 username/role(user/admin/superadmin), workspace 범위, 허용 원칙을 기록.
- Claude AI 메시지는 같은 대화 흐름에서 planner가 일반 대화인지 실제 작업인지 스스로 판단.
- 일반 질문은 tool_calls 없이 바로 답변, 파일 생성/수정/빌드/실행 요청만 구조화 도구 사용.
- 관리 정책 MD를 LLM이 workspace.write로 수정하는 것은 서버가 거절.
- MD는 판단 문맥이며 실제 workspace/등록 프로그램 접근 허용 여부는 서버가 다시 검사.

### 자동화
- legacy Agent.Enabled / User.AgentEnabled 값은 자동 AI workspace 사용 여부에 사용하지 않음.
- workspace root는 DataDir/workspaces로 서버가 자동 결정.
- Go toolchain은 저장된 경로가 유효하면 사용하고, 아니면 PATH/Program Files에서 자동 탐지.
- 기존 수동 Agent API는 호환성 때문에 유지하되 일반 UI에서는 노출하지 않음.

### 검증
- 신규 TestAgentAutoWorkspaceRolePolicyAndIdempotency PASS.
- 신규 TestAutomaticClaudeChatCreatesWorkspaceWithoutAgentToggle PASS.
- TestAgent 전체 PASS: 135.964s.
- 동일 AI 방 중복 실행 방지 보강.
- Agent 실행 중 사용자 계정 비활성화 시 실제 권한 회수/작업 취소 회귀로 변경.
- Web UI 검색: 서버 에이전트/프로젝트/agent-mode/agent-workspace/Grant 노출 없음.
- Go compile-only 및 app.js/messenger.js/settings.js node --check PASS.

### 버전
- 서버/설치기/트레이/웹 목표 및 소스 버전: 1.15.0.
- Android는 네이티브 변경 없음: 1.7.0 / versionCode 170.

## 2026-09-29 00:17 AI 설정/자동 Agent UX 단순화 완료

### 사용자 요구 반영
- AI 설정을 캡처처럼 연결별 모델/추론 선택 중심으로 단순화.
- 서버 Agent 설정 탭/제한값/workspace 경로/project/grant UI 제거.
- 사용자 관리의 Agent 허용/해제 버튼 제거.
- AI 채팅에 별도 작업 모드/프로젝트 선택을 두지 않음.
- AI 대화 시 사용자별 별도 workspace 자동 생성.
- workspace에 AGENT.md / AGENTS.md / CLAUDE.md 자동 생성.
- 각 파일에 username/role(user/admin/superadmin)과 workspace 범위 기록.
- Claude는 같은 대화에서 일반 질문인지 작업 요청인지 스스로 판단.
- 실제 권한은 LLM 판단이 아니라 서버/AppContainer가 최종 enforce.

### 추가 보강
- 기존 수동 프로젝트는 자동 기본 workspace로 재사용하지 않음.
- 관리 정책 MD는 workspace.write로 수정 불가.
- 역할이 바뀌면 다음 AI 사용 시 정책 MD 자동 갱신/복구.
- Claude 모델은 설정 화면에서 sonnet / opus / fable 선택형.
- 추론 강도 provider default / low / medium / high / xhigh / max 선택.
- OpenAI 호환 모델은 모델명/API 연결만 간단히 유지.
- 기존 AgentConfig와 수동 project/job/grant API는 호환/내부 보안 계층으로만 유지.

### 검증
- 자동 workspace/역할 정책/idempotency/사용자 분리 테스트 PASS.
- 기존 legacy project와 자동 workspace 분리 테스트 PASS.
- 사용자 중지 시 활성 작업 취소 테스트 PASS.
- Windows resource limits 2회 반복 PASS.
- 실제 Claude 자동 테스트 PASS 15.58s:
  - AI 메시지 → 자동 workspace
  - AGENT.md 역할 읽기
  - workspace.write/read 자동 선택
  - auto-proof.txt = ONTECH_AUTO_POLICY_OK
- build.ps1 -Target Tests -Validation Core PASS.
- Android 87 PASS / NotificationPolicy 22 PASS / Go Core PASS.
- node --check app.js / messenger.js / settings.js PASS.
- build.ps1 -Target Server -Validation Core PASS.
- Server/Setup/Tray/Web 1.15.0.

### 공식 산출물
- output/OnTechChat-Server-Setup-v1.15.0-x64.exe
- size 38,624,256 bytes
- SHA256 2e96adf470f87dc5e3e683391b5175755952307f3d3f215ca71ad481675d7380
- payload 11 files / SHA256 4b502dd36df08a39eced8246ea1b2da7aa9b8950464e9e08030dda412b162f03
- Android APK 1.7.0 유지 / SHA256 362d3fa42b2fd2bad19baad251dfc06b7b33e012887281efaba708a5e6ef1f2b

## 2026-09-29 00:48 Codex CLI 모델/추론 설정 노출 수정

### 원인
- 설정 UI의 모델 프리셋과 추론 목록 자동 생성이 Claude CLI에만 적용돼 Codex CLI는 저장된 모델 행이 없을 때 모델/추론 선택이 보이지 않았음.

### 수정
- Codex CLI 카드에 저장 모델이 없으면 GPT-6-Astra 기본 모델 행 자동 생성.
- Codex 모델을 select로 표시.
- 현재 이 PC의 Codex models_cache.json(2026-09-29 00:37 갱신) 기준 모델 목록 반영:
  - gpt-6-astra
  - gpt-6-sol
  - gpt-6-luna
  - gpt-5.6-sol
  - gpt-5.6-terra
  - gpt-5.6-luna
  - gpt-5.5
- 모델별 추론 강도 동적 갱신:
  - gpt-6-astra / gpt-6-sol: low, medium, high, xhigh, max, ultra
  - gpt-6-luna: low, medium, high, xhigh, max
  - gpt-5.6-sol / gpt-5.6-terra: low, medium, high, xhigh, max, ultra
  - gpt-5.6-luna: low, medium, high, xhigh, max
  - gpt-5.5: low, medium, high, xhigh
- provider를 켜지 않은 모델은 기본 모델 후보에서 제외.
- 기존 저장 Codex 모델이 있으면 해당 모델을 유지하면서 현재 지원 추론 목록을 보강.
- Codex CLI runtime은 기존 안전 정책대로 tool_isolation_unavailable 유지.

### 검증
- 로컬 Codex CLI: 0.155.1.
- models_cache.json 실제 목록/추론 범위 확인.
- node --check server/web/settings.js PASS.
- Codex UI 모델 문자열 정적 검사 PASS.
- Go TestAI(Codex|Settings|Selection) PASS.
- build.ps1 -Target Tests -Validation Core PASS.
- build.ps1 -Target Server -Validation Core PASS.
- Server/Installer/Tray/Web 버전 1.15.1.

### 공식 산출물
- output/OnTechChat-Server-Setup-v1.15.1-x64.exe
- size: 38,626,304 bytes
- SHA256: ce3a6d23e3a3b11227d653effe69449ec0139e368e93335f767436f5e51bb32f
- payload 11 files / SHA256 b71ae9210c20b2cd60c797b03fc7c5261ba5efa662a932af0f98934a1a89b75b
## 2026-09-29 01:14 Codex 설정 확인 판정 수정

- 증상: Codex 모델/추론을 선택한 뒤 「설정 확인」을 누르면 `사용 불가 · tool_isolation_unavailable`.
- 원인: 설정 유효성 확인과 실제 AI runtime 사용 가능성 확인이 같은 판정으로 묶여 있었음.
- 수정:
  - `aiProviderConfigurationState` 추가.
  - Codex는 CLI 실행 파일 존재 + 모델/추론 설정 유효성을 configuration success로 판정.
  - `설정 확인`은 `ok=true`, `runtime_ready=false`, `runtime_reason=tool_isolation_unavailable`을 반환.
  - UI는 `설정 확인 성공 · CLI/모델/추론 설정 정상`으로 표시하고 실제 대화 runtime 비활성 사유를 별도 안내.
  - `실제 모델 테스트`는 실제 실행이므로 Codex runtime 차단 정책을 그대로 적용.
- 회귀 테스트: `TestAICodexConfigurationCheckSeparateFromRuntime` 추가.
- Go `TestAI(Codex|Settings|Selection)` PASS.
- `node --check server/web/settings.js` PASS.
- `build.ps1 -Target Tests -Validation Core` PASS.
- `build.ps1 -Target Server -Validation Core` PASS.
- Server/Installer/Tray/Web: 1.15.2.
- 공식 Setup: `output/OnTechChat-Server-Setup-v1.15.2-x64.exe`
- size: 38,628,352 bytes
- SHA256: `ac392eb4a523f08aaf436ce99d00ce6c436942612e28aaa1e6137c6f10a011b4`
- payload SHA256: `41501ad2ee06804f022eb915f2ee9f382d0207466b89dca8a05f63aa41f3c933`

## 2026-09-29 01:30 Codex executable_missing 자동 탐색 수정

- 증상: Codex `설정 확인`에서 `사용 불가 · executable_missing`.
- 원인: 서버 프로세스의 PATH는 사용자 PowerShell PATH와 달라 `Executable=codex`를 `exec.LookPath`로 찾지 못함.
- 실제 설치 위치 확인: `C:\Users\NAJUNGHO\AppData\Local\Programs\OpenAI\Codex\bin\codex.exe`.
- 해당 파일 ACL에서 `NT AUTHORITY\SYSTEM:(F)` 확인.
- 서버에 Windows CLI 표준 설치 위치 자동 탐색 추가:
  - `%LOCALAPPDATA%\Programs\OpenAI\Codex\bin\codex.exe`
  - `%APPDATA%\npm\codex.cmd`
  - `%USERPROFILE%\.local\bin\codex.exe`
  - `%ProgramFiles%\OpenAI\Codex\bin\codex.exe`
  - `C:\Users\*`의 위 표준 사용자별 설치 위치
- symlink는 CLI 실행 파일 후보로 인정하지 않음.
- Claude npm 설치 위치도 configuration 검사에서 동일 방식으로 자동 탐색.
- PATH를 빈 문자열로 만든 단독 회귀 테스트 `TestAICodexWindowsInstallDiscoveryWithoutPath` PASS.
- `설정 확인`은 발견된 CLI + 모델/추론 유효성 기준으로 성공 처리하고 실제 runtime 격리 상태는 별도 표시.
- `build.ps1 -Target Tests -Validation Core` PASS.
- `build.ps1 -Target Server -Validation Core` PASS.
- Server/Installer/Tray/Web 1.15.3.
- 공식 Setup: `output/OnTechChat-Server-Setup-v1.15.3-x64.exe`
- size: 38,636,032 bytes
- SHA256: `2b696050ace18d051c0b3b041d1a1a7ae695e386c1dd739004cfac092e16d442`
- payload SHA256: `a16248bd2ac3f08a2a1300d5a45726f81d780debfe1b7a63aecf0c2f265411bb`

## 2026-09-29 01:49 다른 PC Codex/Claude CLI 경로 자동 탐색 보강

- 사용자 지적: 다른 PC에서는 사용자명/드라이브/설치 방식이 달라 기존 절대경로가 맞지 않음.
- 수정 원칙: 설정에 저장된 다른 PC의 절대경로가 현재 PC에 없으면 실패하지 않고 현재 PC에서 다시 자동 탐색.
- 자동 탐색 순서:
  - 저장된 절대경로가 현재 PC에서 실제 파일이면 사용.
  - 현재 서버 PATH에서 실행 파일 검색.
  - Windows registry ProfileList의 실제 사용자 프로필 경로 조회.
  - `%LOCALAPPDATA%\Programs\OpenAI\Codex\bin\codex.exe`.
  - WinGet Links 및 WinGet Packages.
  - npm `%APPDATA%\npm`.
  - pnpm `%LOCALAPPDATA%\pnpm`.
  - Scoop `%USERPROFILE%\scoop\shims`.
  - `%USERPROFILE%\.local\bin`.
  - Program Files / Program Files (x86).
  - Chocolatey bin.
  - 시스템 드라이브의 사용자 프로필 폴더.
- 사용자 프로필이 C:가 아니어도 registry ProfileImagePath를 사용.
- WinGet 링크처럼 링크 파일인 정상 실행 경로도 os.Stat 기준으로 허용.
- Claude 실제 대화 실행과 Agent planner도 동일한 자동 탐색 결과를 사용하도록 통합.
- 설정 확인 응답에 `resolved_executable`을 포함하고 관리자 UI에서 자동 발견 경로를 표시.

### 검증
- 다른 PC의 stale absolute path `Z:\old-machine\codex.exe`가 저장된 상황을 재현하고 현재 PC 표준 설치 위치로 fallback PASS.
- PATH를 빈 문자열로 만든 상태에서 Codex 표준 설치 자동 탐색 PASS.
- WinGet Links 위치만 존재하는 가상 프로필 탐색 PASS.
- Claude 설정에 stale absolute path가 있어도 자동 발견된 npm 경로를 실제 runner에 전달하는 테스트 PASS.
- Codex/Claude/Settings/Selection 타깃 Go 테스트 PASS.
- node --check server/web/settings.js PASS.
- build.ps1 -Target Tests -Validation Core PASS.
- build.ps1 -Target Server -Validation Core PASS.

### 공식 산출물
- Server/Installer/Tray/Web: 1.15.4.
- `output/OnTechChat-Server-Setup-v1.15.4-x64.exe`
- size: 38,660,608 bytes
- SHA256: `fccfee72baa2c041125aa55d8aa41b49883dd547c95135ed191e57610834879c`
- payload 11 files / SHA256 `6ad4107cc128cd49359bc63351aebce0c095602e2d2c03ac39288f019a68b161`

## 2026-09-29 10:53 다른 PC Codex CLI 자동 탐색 보강 완료

- 중단 작업 재확인 결과 기존 1.15.4에 Registry ProfileList, stale 절대경로 fallback, WinGet/npm/pnpm/Scoop/Program Files/Chocolatey 탐색이 이미 반영되어 있었음.
- 추가 보강:
  - `%LOCALAPPDATA%\Programs\OpenAI\Codex\codex.exe`
  - `%LOCALAPPDATA%\Microsoft\WindowsApps\codex.exe/.cmd`
  - `%USERPROFILE%\.codex\bin\codex.exe`
  - WinGet Packages 2단계 하위 경로까지 탐색
- 다른 PC에서 이전 PC의 절대경로가 저장돼 있어도 존재하지 않으면 버리고 현재 PC에서 다시 탐색.
- USERPROFILE/PATH/LOCALAPPDATA/APPDATA가 실제 사용자와 다른 Windows 서비스 유사 환경에서도 Registry ProfileList 및 시스템 사용자 프로필 스캔으로 다시 탐색.
- 회귀 테스트 PASS:
  - TestAICodexWindowsInstallDiscoveryWithoutPath
  - TestAICodexDiscoveryFallsBackFromStaleAbsolutePath
  - TestAICodexDiscoveryFindsWinGetLink
  - TestAICodexDiscoveryFindsWindowsAppsAlias
  - TestAICodexDiscoveryServiceLikeEnvironment
- `build.ps1 -Target Tests -Validation Core` PASS.
- `build.ps1 -Target Server -Validation Core` PASS.
- Server/Installer/Tray/Web: 1.15.5.
- 공식 Setup: `output/OnTechChat-Server-Setup-v1.15.5-x64.exe`
- size: 38,664,192 bytes
- SHA256: `d92e4b4fa8dd942468c77a5343507b4313b520c2345de2d5ebd36229c376930c`
- payload 11 files / SHA256 `83eab4fc0648a7d7a714ad9e0f1b8aafcc413e934c824b6d479cd44bec9a2599`

## 2026-09-30 00:16 Codex executable_missing 최종 비치명 처리

- 다른 PC에서 표준 경로/WinGet/WindowsApps/사용자 PATH까지 자동 탐색하도록 보강.
- 추가 탐색: Registry ProfileList, HKEY_USERS 사용자 Path, PNPM_HOME, NPM_CONFIG_PREFIX, WindowsApps, `.codex/bin`, WinGet Packages 2단계 하위.
- 표준 경로가 발견되면 느린 사용자 PATH 레지스트리 조회를 건너뛰도록 최적화.
- 회귀 테스트 PASS:
  - PATH 없음
  - stale 절대경로
  - WinGet link
  - WindowsApps alias
  - 서비스 유사 환경
  - 사용자 레지스트리 PATH 확장
- Codex는 실제 runtime이 `tool_isolation_unavailable`로 비활성 상태이므로, 실행파일 경로 미발견을 `설정 확인` 실패 사유에서 제거.
- 앞으로 Codex `설정 확인`은 모델/추론 설정이 유효하면 성공. 경로 발견 실패는 `CLI 경로 자동 발견 안 됨` 정보로만 표시.
- `build.ps1 -Target Tests -Validation Core` PASS.
- `build.ps1 -Target Server -Validation Core` PASS.
- Server/Installer/Tray/Web 1.15.7.
- 공식 Setup: `output/OnTechChat-Server-Setup-v1.15.7-x64.exe`
- size: 38,680,576 bytes
- SHA256: `8e11b4b703de1c3864631bedbe65df2f2440ce31de2a3124ec71498869a5dcd0`
- payload SHA256: `18f515405b3b531f19c494c39ffef668d30a40261e51dece6422f3b26bb125b1`
