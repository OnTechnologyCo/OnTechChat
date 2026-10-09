# OnTechChat AI 대화 기능 개발 진행상황

작성일: 2026-09-28  
대상 프로젝트: `D:\OnMakeSrc\OnTechChat`

## 1. 현재 상태

AI 대화 기능 1차 구현과 Core 검증, 서버 공식 빌드까지 완료했습니다.

완료 범위:
- 관리자 AI 전역/Provider/모델/추론 설정
- API Key 암호화 저장 및 조회 시 비공개
- OpenAI compatible Chat Completions adapter
- Claude Code CLI 비대화형 adapter
- Codex CLI adapter 정의 및 안전 조건 미충족 시 런타임 비활성화
- 사용자별 비공개 AI 대화방
- 모델/추론 선택
- 요청 queue/worker, 취소, 실패 재시도
- 대화/AI 요청 상태 암호화 스냅샷 저장
- 완료된 이전 턴을 다음 질문 문맥에 반영
- SSE 기반 상태/최종 답변 갱신
- 웹/Android WebView 공통 AI UI
- 관리자 연결 로컬 확인/실제 호출 테스트 UI
- AI Core 자동 테스트
- 서버 1.12.0 공식 빌드
## 2. 주요 구현 파일

서버:
- `server\ai_config.go`: 설정, provider/model catalog, reasoning resolver, secret 처리
- `server\ai_provider.go`: OpenAI compatible / Claude CLI / Codex 안전 비활성 adapter
- `server\ai_chat.go`: AI 방, 요청 상태, cancel/retry, 문맥 구성, 관리자 AI 테스트
- `server\ai_jobs.go`: bounded queue, 전역/provider 동시성, worker, 완료/실패 저장
- `server\ai_test.go`: AI 설정·전달·격리·중복·취소·문맥·HTTP 실패 테스트

기존 서버 통합:
- `server\config.go`: AI 설정 및 서버 버전 1.12.0
- `server\settings.go`, `settings_storage.go`: 관리자 설정 저장/적용과 비밀값 복호화
- `server\store.go`: Room/Message/State AI metadata와 AIRequest 영속 저장
- `server\app.go`: AI API 라우트 및 worker 생명주기
- `server\chat.go`, `messenger.go`, `files.go`: AI 방 권한/전송 경계

웹:
- `server\web\settings.js`: 관리자 AI 설정/검증/실제 호출 테스트
- `server\web\messenger.js`: AI 방 생성, 모델/추론 선택, 상태/중지/재시도
- `server\web\index.html`, CSS: AI UI
## 3. Provider 검증 상태

### OpenAI compatible
- Chat Completions 형식 사용
- 실제 wire body에 model/messages 반영
- 확인된 모델 capability에서만 reasoning_effort 전달
- provider_default는 wire에 전달하지 않음
- redirect 차단, TLS 검증 유지, 응답 크기 제한
- 401/429/비JSON/빈 응답/길이 초과/timeout 처리
- finish_reason=length 응답은 저장하지 않고 실패 처리
- mock upstream 자동 테스트 통과

### Claude Code CLI
확인 버전: `2.1.283 (Claude Code)`

실제 help에서 다음 옵션 존재를 재확인:
- `-p / --print`
- `--safe-mode`
- `--restricted`
- `--strict-mcp-config`
- `--disable-slash-commands`
- `--no-session-persistence`
- `--tools ""`
- `--permission-prompts none`
- `--model`
- `--effort low|medium|high|xhigh|max`
- `--output-format json`
Claude 실제 최소 호출도 수행했습니다.
- 모델 alias: sonnet
- effort: high
- 결과 JSON 수신 성공
- observed canonical model: `claude-sonnet-5`
- tool/MCP 격리 옵션 조합이 실제 CLI에서 수락됨
- 호출 비용/한도 사용 가능성이 있는 실제 호출 1회 수행
- PowerShell 5.1 파이프 경유 한글 입력은 콘솔 인코딩이 깨졌으나, 서버 구현은 shell 문자열 합성이 아니라 Go exec + UTF-8 stdin을 사용

### Codex CLI
- 모델/effort argv 구성은 구현됨
- 현재 확인 환경에서는 대화 전용으로 모든 built-in 도구 실행을 확실히 차단하는 조건을 확보하지 못함
- 따라서 런타임 catalog에서 사용 가능으로 노출하지 않고 `AI_CLI_TOOL_ISOLATION_UNAVAILABLE` 상태 유지
- 안전 조건을 만족하기 전 자동 fallback이나 강제 활성화하지 않음

## 4. 저장과 격리

- AI 방은 `Room.Kind="ai"`, 실제 소유자 1명만 Member로 저장
- AI 답변은 기존 Message에 서버 전용 AI metadata와 함께 저장
- 요청은 `State.AIRequests`에 queued/running/completed/failed/cancelled/interrupted 상태 저장
- 동일 room/user/client_id 재전송은 기존 요청/메시지를 반환하여 provider 중복 호출 방지
- 방당 활성 요청 1개
- 완료된 user/assistant 턴 쌍만 다음 문맥에 포함
- 실패/취소 턴, 다른 방/다른 사용자 데이터는 문맥에서 제외
- 활성 요청의 최종 답변 저장 공간을 예약하여 max_messages 경쟁 방지
## 5. 관리자/사용자 UI

관리자:
- 서버 설정의 AI 영역에서 전역 설정, provider, 모델, reasoning 허용값 관리
- API Key는 새 입력/삭제만 가능하며 조회 응답에는 평문/암호문이 노출되지 않음
- 저장과 적용 분리 유지
- 로컬 실행환경 확인과 실제 모델 호출 테스트 분리
- 실제 호출 전 비용/한도 사용 가능성 확인 dialog 표시

사용자:
- 채팅 목록의 `AI 대화 시작`
- AI 방 상단 모델/추론 선택
- 대기 중/생성 중/실패 상태 표시
- 생성 중지 및 실패 요청 다시 시도
- AI 방에서는 첨부/위치/멤버 초대 등 1차 범위 밖 기능 차단
- 서버 WebView UI이므로 Android 네이티브 화면을 중복 구현하지 않음

## 6. 자동 검증 결과

실행:
`build.ps1 -Target Tests -Validation Core`

결과:
- Android Tests: 87 PASS
- NotificationPolicy.Tests: 22 PASS
- Go Core: `Test(Location|AllFriends|AI)` 통과
- 최종: `BUILD SUCCESS: Tests`

AI 테스트에서 확인한 항목:
- OpenAI model/reasoning wire 전달과 provider_default 생략
- Claude CLI argv와 structured output/observed model
- API key 암호화/유지/명시 삭제
- 사용자/방 문맥 격리
- AI 답변 용량 예약
- Codex 안전 비활성
- 동일 client_id 중복 방지
- 타 사용자 조회/취소 차단
- cancel/retry
- 두 번째 질문에 완료된 첫 턴 문맥 전달
- 원본 메시지 삭제 시 늦은 AI 결과 폐기
- HTTP 401/429/redirect/비JSON/빈/대형 응답/timeout 처리
- 구버전 settings 요청에서 AI 필드 누락 시 기존 AI 설정 유지
웹 JavaScript 구문 검사:
- `node --check server\web\app.js`
- `node --check server\web\settings.js`
- `node --check server\web\messenger.js`
- 모두 exit code 0

## 7. 공식 서버 빌드

실행:
`build.ps1 -Target Server -Validation Core`

결과:
- Go Core 테스트 통과
- payload 9개 파일 검증 성공
- 서버 실행 버전 확인: `OnTechChat Server 1.12.0`
- Windows x64 GUI 통합 설치기 생성 성공
- 최종: `BUILD SUCCESS: Server`

공식 산출물:
`D:\OnMakeSrc\OnTechChat\output\OnTechChat-Server-Setup-v1.12.0-x64.exe`

SHA256:
`dce9e69122a84aeaf916ad6c662afc7d16eba5c5418bb4a9410386ba63fb8768`

Android는 네이티브 변경이 없어 기존:
`output\OnTechChat-v1.7.0-arm64.apk`
SHA256:
`362d3fa42b2fd2bad19baad251dfc06b7b33e012887281efaba708a5e6ef1f2b`
## 8. 추가 수정

기존 `config.example.json`이 JSON 문법이 깨진 상태임을 검증 중 발견했습니다.

기존:
`{schema_version:2,listen:127.0.0.1:8787,data_dir:private}`

수정:
- 표준 JSON 형식으로 변경
- PowerShell `ConvertFrom-Json` 검증 통과
- 수정 후 서버 설치 payload를 다시 빌드
- 최종 설치기 SHA256을 위 값으로 갱신

## 9. 남은 실제 환경 검증

자동/개발 환경에서 완료된 것과 운영 적용은 구분합니다.

미실행:
- 운영 Windows 서비스에 1.12.0 실제 설치/업데이트
- 서비스 계정(Session 0)에서 Claude 인증이 그대로 사용 가능한지 확인
- 관리자 UI에서 실제 OpenAI API Key 저장 후 OpenAI 실호출
- 실제 휴대전화에서 AI UI 입력/스크롤/키보드 동작 확인

Claude는 현재 개발 사용자 세션에서 실제 모델 호출 성공을 확인했지만, Windows 서비스 계정의 PATH/HOME/인증 상태는 별도입니다.

## 10. 결론

현재 소스는 AI 대화 기능 1차 완료 상태이며 Core 테스트와 서버 공식 빌드까지 통과했습니다.
운영 서버 교체·재시작은 이번 작업에서 수행하지 않았습니다.
Codex CLI는 안전한 도구 격리를 확인하기 전까지 의도적으로 비활성 상태입니다.
