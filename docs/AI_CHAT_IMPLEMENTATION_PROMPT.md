# OnTechChat AI 대화 기능 개발 프롬프트

작성 기준: 2026-09-28, HomePC의 실제 소스 열람. 이 문서를 받은 개발 모델이 구현을 수행한다.
경로·함수·제약은 아래 확인 결과를 출발점으로 삼되, 착수 시 최신 파일을 다시 읽어 변경 여부를 확인한다.
이 문서 자체는 구현·배포 완료 보고가 아니다.

## 1. 수행할 작업과 완료 조건

너는 기존 OnTechChat을 확장하는 개발자다. 다음 요구를 실제 작동하는 코드와 검증 결과까지 구현하라.

> D:\OnMakeSrc\OnTechChat의 채팅에 AI 대화 기능을 추가한다. 관리자가 사용할 AI 연결, 모델, 추론 설정을 등록하고, 사용자는 허용된 모델과 추론 설정을 선택하여 대화할 수 있어야 한다.

계획·UI 목업·가짜 응답·설정 저장만 만들고 종료하지 않는다. 관리자 설정 저장 → 적용 → 사용자 선택 → 실제 호출 인자/요청에 반영 → 응답 저장 → 다음 대화에 문맥 반영이 연결되어야 한다.
현재 작업은 이 기능의 개발·검증·배포 파일 준비다. 운영 서비스 교체, 재시작, 사용자 로그인 계정 변경, 방화벽 변경, 실제 휴대전화 설치는 별도 지시가 없는 한 실행하지 않는다.
설명, 주석, 사용자 오류 메시지는 한국어를 기본으로 한다.

별도 지정이 없는 부분에 대한 권장 구현 범위는 다음과 같다. 이를 기존 사용자 요구로 둔갑시키지는 말고 설계 결정으로 기록한다.

- 기존 채팅 목록 안에 사용자별 비공개 AI 대화방을 추가한다. 기존 친구/그룹 채팅 전체를 AI에 자동 전송하지 않는다.
- 연결 어댑터는 `openai_compatible`, `claude_cli`, `codex_cli`를 제공한다. 각 연결은 독립적으로 활성화한다. CLI 미설치·미인증이면 그 연결만 비활성 사유를 보여 준다.
- 첫 버전은 텍스트 대화, 대화 이력, 모델·추론 선택, 응답 생성 상태, 중지, 실패 재시도를 완성한다.
- 기존 SSE로 진행 상태와 최종 답변을 갱신한다. 토큰별 스트리밍은 필수 완료 조건이 아니다.
- 파일/이미지 분석, 음성, 웹 검색, MCP 도구 실행, PC 제어, 다중 에이전트, RAG, 자동 모델 전환은 이번 기능에 추가하지 않는다.
- Anthropic 직접 API와 OpenAI Responses API를 추가하려면 별도 어댑터로 구분한다. Chat Completions 어댑터에 형식을 섞지 않는다. 두 직접 API는 이번 필수 범위가 아니다.

## 2. 공식 작업 경로와 기존 규칙

수정 대상 루트는 `D:\OnMakeSrc\OnTechChat` 하나다.
참고 전용 프로젝트는 `D:\src\goclaw`, `D:\src\LLM_Tools\LLM_Tools`, `D:\OnMakeSrc\ClaudeProxyServer`다. 참고 프로젝트는 수정하지 않는다.

착수 순서:

1. 대상 루트 `AGENTS.md`, `README.md`, `docs\WORK_STATUS.md`, 변경할 하위 경로의 `AGENTS.md`를 읽는다.
2. `.build\task.json`, 실제 PID, 프로젝트 잠금과 다른 작업의 진행 여부를 확인한다. 잠금을 지우거나 우회하지 않는다.
3. 현재 변경 사항을 확인하고 타 작업 내용을 덮어쓰지 않는다. Git 저장소가 없으면 변경 대상만 `.build` 아래에 백업한다.
4. 먼저 기존 코드의 데이터 흐름과 변경 파일 목록을 짧게 기록하고 구현을 진행한다.

보존할 규칙:

- 빌드 진입점은 루트 `build.ps1`만 사용한다. 도구 위치는 `build.config.json`을 따른다.
- 임시 파일과 로그는 `.build`, 배포 파일은 `output`만 사용한다. 복제 소스, 별도 버전별 프로젝트, 고정 이름 배포 복사본을 만들지 않는다.
- `archive`는 수정·빌드·실행·재배포하지 않는다.
- 검토 시 버전은 Android 1.7.0 / versionCode 170, 서버·설치기·트레이 1.11.0이었다. 착수 시 실제 버전을 확인하고 변경 범위에 맞게 올린다.
- 위치 기능은 기존 팝업·전용 요청 방식과 기본 공유 꺼짐을 유지한다. 직접 위치 전송 UI나 대기 중 상주 위치 서비스를 복원하지 않는다.
- Windows 설치·업데이트·제거 및 트레이 동작을 AI 기능 작업에 맞춰 임의 변경하지 않는다.
- 최종 변경 내용, 테스트 결과, 버전, 파일 해시, 미검증 항목을 `docs\WORK_STATUS.md`에 기록한다.

## 3. 실제 확인한 코드 지도

다음은 구현 때 우선 읽을 파일과 확인된 역할이다. 줄 번호 대신 함수/형식 이름으로 찾는다.

| 대상 상대 경로 | 확인된 역할 / AI 작업의 연결 지점 |
|---|---|
| `server\app.go` | `App`, `newApp`, `startWorkers`, `Close`, `Handler`, `changed`, `Hub`. 인증 라우트, SSE 알림, 앱 종료와 재시작 연결 |
| `server\config.go` | `Config`, `decodeConfiguration`, `validate`. 설정 schema 2와 알 수 없는 JSON 필드 거부 |
| `server\settings.go` | `EditableSettings`, `settingsEditable`, `candidateSettings`, `settingsResult`, `requireSettingsAdmin`, `saveSettings`. revision 충돌 검사와 저장/적용 분리 |
| `server\settings_storage.go` | `sealSetting`, `openSetting`, `unlockConfiguration`, `persistConfiguration`. 설정 암호화 및 원자적 저장 |
| `server\store.go` | `State`, `Room`, `Message`, `member`, `addMessage`, `Store.update`. 암호화된 스냅샷 저장소 |
| `server\chat.go` | `sendMessage`, `messages`, `messageView`, `deleteMessage`, `markRead`, `events`. 메시지 저장, 읽음, SSE |
| `server\messenger.go` | `canPost`, `roomSummary`, `openDirect`, 친구/프로필. 현재 방 종류 분기 |
| `server\files.go` | 첨부 업로드/다운로드. AI 전송 허용을 추가해도 파일 권한이 자동 확대되지 않게 확인 |
| `server\notifications.go`, `server\push.go`, `server\maintenance.go` | 알림 미리보기·읽음 확인·발송 큐·보관 정리 |
| `server\web\app.js` | `api`, `message-form`의 실제 전송 이벤트. 현재 chat/direct만 전송 허용 |
| `server\web\messenger.js` | `isChat`, `connectEvents`, `selectRoom`, `updateChatHeader`, `renderMessage`, `reloadMessages`, `showMessageActions`. 실제 메신저 UI |
| `server\web\settings.js` | 기존 관리자 설정 다이얼로그와 저장/적용 UI |
| `server\web\index.html`, `style.css`, `messenger.css` | 공통 웹 화면·스타일 |
| `android\OnTechChat\MainActivity.cs`, `ChatWeb.cs` | 서버 URL을 여는 Android WebView와 네이티브 기능 연결 |
| `android\OnTechChat\Core\ChatApi.cs`, `Core\NotificationPolicy.cs`, `PushService.cs` | 네이티브 API/알림 호환성 확인 지점 |
| `server\main.go` | `serveGeneration`과 설정 reload 후 앱 세대 교체 |
| `build.ps1` | 공식 검증·빌드·패키징. Core 검사의 실행 대상 확장이 필요 |

중요한 현재 구조:

- Android는 C#/.NET Android의 WebView 앱이다. MAUI·React·WPF 앱으로 오인하지 않는다. AI UI는 서버 웹 화면을 확장하면 웹과 Android에서 함께 사용된다.
- `store.db`는 SQLite가 아니다. AES-GCM 암호화 스냅샷이며 `Store.update`가 전체 상태를 복제하고 잠금 안에서 저장한다. SQL migration이나 새 DB를 도입하지 않는다.
- 설정 저장은 `revision` 검증 후 이루어진다. `apply=true`이면 응답 뒤 `a.reload`로 서버/작업자를 재구성한다. 저장만 한 설정은 아직 활성 설정이 아니다.
- SSE `/api/events`의 `sync` 이벤트는 방 변경 알림이며 프런트엔드가 다시 조회한다. 이를 AI 최종 응답 반영에도 활용한다.
- `addMessage`는 `(room_id, user_id, client_id)`로 중복 메시지를 막는다. 이 함수 호출 뒤 무조건 AI를 실행하면 중복 호출은 막히지 않는다.

## 4. 참고 코드에서 활용할 부분과 그대로 복사하면 안 되는 부분

### goclaw

먼저 `internal\providers\types.go`, `capabilities.go`, `adapter_openai.go`, `openai_request.go`, `openai_chat.go`, `sse_reader.go`를 읽는다.
필요할 때 `reasoning_capability.go`, `reasoning_resolution.go`, `anthropic_request.go`, `claude_cli_chat.go`, `claude_cli_session.go`, `claude_cli_parse.go`를 읽는다.

- 활용: provider 추상화, 요청/응답 변환 분리, capability에 따른 추론 옵션 검증, 문맥 구성, 취소 가능한 실행.
- 참조 소스의 모델 목록은 현재 서비스 전체 목록을 보장하지 않는다. 실제 연결과 모델별 지원값을 기준으로 한다.
- 현재 `NormalizeReasoningEffort`에는 `max`, `ultra`가 없다. 이를 그대로 복사하면 LLM_Tools에서 선택 가능한 값을 잃을 수 있다.
- `claude_cli_session.go`의 `buildArgs`는 모델을 넘기지만 현재 추론 effort 인자가 없다. `extractFromMessages`는 마지막 사용자 메시지를 꺼내며 세션 이어하기에 의존한다. 새 프로세스로 호출할 때 이를 그대로 복사하면 앞 대화를 잃는다.
- `codex.go`는 Codex CLI 실행기가 아니라 ChatGPT OAuth를 사용하는 HTTP Responses 경로다. Codex CLI 참고 코드로 오인하지 않는다.
- goclaw의 스케줄러·DB·에이전트·도구 체계를 통째로 가져오지 않는다. 다른 모듈의 `internal` 패키지를 직접 import하지 않는다.

### LLM_Tools

실제 소스 루트는 `D:\src\LLM_Tools\LLM_Tools`다.
`src\LlmTools\CliCatalog.cs`, `CliProfile.cs`, `Settings.cs`를 우선 읽는다.

- 활용: 연결별 모델/effort 설정 분리, UI 목록과 서버 검증이 같은 catalog를 사용하는 설계.
- `CliProfile`에는 Claude의 `--model`, `--effort`, Codex의 `-m`, `-c model_reasoning_effort=...` 전달 구조가 있다.
- `CliCatalog.CodexModels`는 선택한 홈의 `models_cache.json`에서 `models[].slug`, `visibility`, `supported_reasoning_levels[].effort`를 읽는다.
- 캐시는 해당 실행 계정/홈에 종속되며 오래되거나 불완전할 수 있다. 목록에 없다는 이유만으로 모든 실제 모델이 영구 미지원이라고 단정하지 않는다.
- 이 프로그램은 ConPTY + 실제 대화형 CLI + xterm.js를 쓰는 WPF 도구다. 웹 채팅 서버에 터미널 UI·TUI 화면 파싱을 이식하지 않는다.
- 목록 불명확 시 임의 effort를 넣는 fallback, 전역 로그인/설정 공유, 광범위 실행 권한은 가져오지 않는다.

### ClaudeProxyServer: 현재 구현의 중요한 함정

`internal\openai\types.go`, `internal\server\chat.go`, `internal\server\server.go`, `internal\fakechat\manager.go`를 읽어 아래를 재확인한다.

- `ChatCompletionRequest`에 추론 effort 필드가 없다.
- `chat.go`는 요청 모델을 감사 로그/응답 정보에 사용하지만 실제 실행은 `s.fc.Send(ctx, userText, fc)`이다. 이 호출에는 모델/effort가 전달되지 않는다.
- `manager.go`의 `spawnChild` 명령에도 모델/effort 인자가 없다. `/v1/models`의 목록만 보고 모델 선택이 동작한다고 판정하지 않는다.
- 하나의 fakechat 브리지와 세션을 공유한다. 동시 실행을 1로 제한해도 사용자별 대화 문맥 분리는 보장되지 않는다.
- 응답 도착 후 한 청크를 보내는 구조라 토큰별 스트리밍이 아니다.
- 따라서 현재 프록시를 이번 다중 사용자 AI 채팅의 기본 백엔드로 사용하지 않는다. HTTP 모양과 큐 설계만 참고한다. 참고 프로젝트를 고쳐 해결하는 것도 이번 범위가 아니다.

## 5. 관리자 설정 계약

기존 설정 화면에 `AI 대화` 탭을 추가하고 기존 저장/적용 경로를 확장한다. AI만 따로 평문 설정 파일이나 임의 전역 변수에 저장하지 않는다.

서버가 관리하는 설정을 다음 두 층으로 나눈다. 실제 Go 타입 이름은 맞춰도 되지만 식별자와 책임은 분리한다.

| 설정 | 필수 내용 |
|---|---|
| AI 전역 | enabled, default_model_id, 요청 제한 시간, 동시 실행 수, bounded queue 한도, 사용자별 요청 제한, 문맥 입력 한도 |
| Provider 연결 | 불변 id, 표시 이름, type, enabled, 연결별 timeout/concurrency, HTTP endpoint와 credential 또는 CLI 실행 파일과 관리자 지정 실행 환경 |
| 허용 모델 | 불변 id, provider_id, 표시 이름, 실제 model 식별자, enabled, 기본 추론 설정, 사용자가 고를 수 있는 추론값, capability 출처와 확인 시각 |

동작 규칙:

1. API 키 입력은 최초/변경 시만 한다. 미입력은 기존 값 유지, 명시적 삭제 선택은 삭제다. 조회 응답은 `has_api_key`만 주고 평문·암호문 모두 제외한다.
2. `sealSetting/openSetting`을 재사용하고 purpose에 provider id를 포함하여 연결 간 비밀값이 섞이지 않게 한다. 복호화는 서버 실행 시 필요한 범위에서만 한다.
3. AI 설정 필드가 없는 구버전 설정 요청은 기존 AI 설정을 유지해야 한다. 누락과 명시적 비활성화/초기화를 포인터 또는 presence-aware 입력으로 구분한다.
4. 기존 schema와 상태 파일은 AI 항목이 없어도 읽혀야 한다. 기본은 AI 비활성화다. 기존 메일/FCM 설정, 계정, 대화를 손상하지 않는다.
5. 기본 모델은 활성 provider의 활성 모델이어야 한다. 참조 중인 provider/model은 우선 비활성화하며 과거 메시지의 모델 정보는 남긴다.
6. 일반 사용자에게는 공개 catalog만 전달한다. API 키, endpoint, CLI 경로, 계정 홈, 내부 오류, 관리자 system prompt는 전달하지 않는다.
7. 모델 목록 조회 실패 시 저장된 목록을 지우지 않는다. 확인 상태와 실패 이유를 보여 주며, 관리자가 검증 후 수동 모델을 등록할 수 있다.
8. 일반 사용자가 모델명 문자열을 임의 입력하거나 다른 연결을 직접 지정하는 API는 제공하지 않는다. 등록된 model id만 받는다.
9. 관리자 연결 테스트는 현재 로그인·역할을 재검증한다. 테스트가 저장된 설정을 쓰는지 화면의 미저장 입력을 쓰는지 표시한다. 테스트가 설정 저장/적용을 암묵적으로 수행하지 않게 한다.
10. 테스트 결과는 실행 파일/연결 도달, 인증, 해당 모델의 요청 수락을 구분한다. 모델 목록 조회 성공만으로 대화 가능 판정을 하지 않는다. 실제 호출 테스트 버튼에는 호출 비용/한도 사용 여부를 표시한다.

## 6. 모델·추론값의 의미와 검증

`모델 선택 UI`, `관리자 저장 검증`, `실제 호출 직전 검증`은 동일한 서버 catalog와 resolver를 사용한다.

- `provider_default`는 추론 제어 필드를 보내지 않는다는 뜻이다. `off`나 `none`과 같다고 취급하지 않는다.
- `low/medium/high/xhigh/max/ultra` 등을 전 모델 공통 선택지로 고정하지 않는다. 모델·연결에서 확인된 값만 표시한다.
- 관리자가 허용한 값과 어댑터가 실제 지원하는 값의 교집합만 사용한다. 관리자 입력만으로 어댑터 capability가 생기는 것은 아니다.
- capability가 불명확하면 기본값 생략만 허용하고 검증이 필요함을 표시한다. 사용자가 명시한 미지원 값은 400 오류로 거절한다.
- `max→high`, `ultra→xhigh`처럼 조용히 낮추거나, 선택한 모델이 실패했다고 다른 모델/유료 provider로 자동 변경하지 않는다.
- UI 표시용 번역명과 wire 값은 분리한다. 실제 요청값은 catalog의 원문 식별자를 사용한다.
- 모델 변경 후 기존 effort가 미지원이면 UI가 이를 알리고 유효한 값을 다시 선택하게 한다. 서버도 동일하게 검사한다.
- 일반 사용자의 선택값 우선순위는 해당 방의 명시적 설정 → 관리자 기본값이다. 명시된 값이 무효일 때 기본값으로 몰래 대체하지 않는다.
- 생성 요청 수락 시 provider/model/effort/설정 revision을 스냅샷으로 확정한다. 실행 중 선택을 바꿔도 진행 중 요청의 모델이 바뀌면 안 된다.
- `requested`, `resolved/sent`, provider가 반환한 `observed model`을 구분한다. provider가 실제 모델/effort를 돌려주지 않으면 확인되지 않은 값을 확인 완료로 표시하지 않는다.
- 모델이 답변 문장으로 자기 이름을 말하는 것은 검증 증거가 아니다. HTTP payload 또는 CLI argv와 구조화 응답을 검증한다.

## 7. Provider 구현 기준

공통 실행 입력은 서버가 구성한 메시지 목록, 확정된 모델/추론 설정, timeout/cancellation이다. 결과는 최종 텍스트, 종료 상태, 선택적 usage/model metadata이다.
외부 HTTP와 CLI 실행은 interface로 분리하여 가짜 HTTP 서버/가짜 실행기로 테스트할 수 있게 한다. AI UI 코드가 provider별 요청 JSON을 직접 만들지 않는다.

### OpenAI 호환 HTTP

- 관리자가 저장한 API base URL에서만 호출한다. 일반 요청 body로 endpoint/header/API key를 덮어쓸 수 없게 한다.
- 이번 어댑터는 Chat Completions 형식이다. base URL에 `/v1`이 있는지 고려하여 경로 중복을 막고 테스트한다.
- `model`과 `messages`를 실제 JSON에 넣는다. `reasoning_effort`와 토큰/temperature 필드는 해당 연결·모델 지원이 확인될 때만 보낸다.
- `reasoning_effort`를 받지 않는 호환 서버에 억지로 넣거나 무시되어도 성공이라고 표시하지 않는다.
- HTTP(S)만 허용하고 URL userinfo, query에 든 키, 임의 redirect로 인증 헤더가 다른 호스트로 넘어가는 경우를 차단한다. localhost/LAN 프록시는 관리자 명시 설정으로 허용할 수 있다. 일반 사용자에게 내부 URL 탐색 기능은 주지 않는다.
- TLS 검증을 끄지 않는다. timeout, 응답 크기 한도, non-2xx, 비JSON, 빈 답변, 잘린 응답을 처리한다.
- 네트워크 결과가 불명확한 생성 요청을 자동 반복하지 않는다. 최종 답변이 없는 경우를 빈 정상 답변으로 저장하지 않는다.

### Claude Code / Codex CLI

- 설치된 CLI의 `--version`, `--help`와 해당 비대화형 하위 명령 도움말을 먼저 확인한다. 참조 코드의 대화형 명령을 서버 명령으로 그대로 쓰지 않는다.
- UI 자동화, ConPTY, 터미널 화면 scraping, fakechat 공유 세션 대신 지원되는 비대화형 구조화 출력 경로를 사용한다.
- Claude는 참조 코드의 `--model/--effort`, Codex는 `-m`과 `model_reasoning_effort` 전달 관계를 출발점으로 삼되, 설치 버전의 비대화형 문법으로 검증한다.
- 실행 파일/런타임은 관리자 설정이다. 사용자 텍스트를 `cmd /c`, PowerShell 또는 문자열 명령에 합성하지 않는다. 인자 배열과 stdin을 사용하며, 긴 한글·개행·따옴표 입력을 테스트한다.
- 기본은 요청별 격리 실행과 서버 저장 이력 전달이다. 공유 `--continue`, `resume --last`, 공용 fakechat 세션을 사용하지 않는다. 명시적 세션을 도입한다면 사용자+방+provider 단위로 격리하고 서버가 생성한 ID만 사용한다.
- 기본 홈의 인증 파일·설정·기록을 복사하거나 전역 설정을 수정하지 않는다. 로그인 비밀을 웹에 표시하지 않는다. 관리자 지정 실행 계정/인증 구성이 없다면 설정 필요 상태로 표시한다.
- Windows 서비스 계정/세션 0의 PATH·홈·로그인은 데스크톱 사용자와 다를 수 있다. 개발 터미널 실행 성공을 서비스 실행 성공으로 보고하지 않는다.
- 대화 실행 CWD는 운영 소스/사용자 문서 폴더가 아닌 격리된 AI 작업 폴더다. CWD 변경이나 read-only 플래그만으로 파일 읽기 차단이 보장된다고 가정하지 않는다.
- 2026-09-30 사용자 지시: Codex와 Claude CLI 모두 실제 대화/자동 계획 실행을 지원한다. 과거의 도구 격리 미확보 상수로 Codex 전체를 차단하지 않는다. Codex는 일회성 임시 CWD, 사용자 설정/규칙 제외, 읽기 전용 및 승인 금지, 내장 도구 제한으로 실행한다. 파일 변경·빌드·프로그램 제어는 서버가 검증하는 구조화 도구로 수행한다. 실행 옵션 실패·미인증·모델 거부는 실제 원인으로 표시하고 위험한 권한 우회 옵션으로 재시도하지 않는다. CLI 플래그를 사용자 간 완전한 OS 읽기 격리로 설명하지 않는다.
- `--dangerously-skip-permissions`, `danger-full-access`를 기본 해결책으로 넣지 않는다. 로그인·샌드박스 설치를 자동 우회하지 않는다.
- stdout은 구조화 결과에서 최종 답변만 파싱한다. stderr/진행 로그/추론 원문을 답변으로 붙이지 않는다. provider가 반환한 요약과 최종 답변 외의 내부 추론 원문 표시를 필수 기능으로 만들지 않는다.
- timeout/취소 시 자신이 시작한 프로세스와 자식만 종료하고 파이프를 닫고 Wait한다. 이미지 이름으로 다른 사용자의 CLI를 종료하지 않는다.
- 인증 오류, 사용량 한도, 미지원 모델, 출력 형식 변경, 프로세스 실패는 각각 사용자에게 이해 가능한 실패로 처리한다. 검증하지 못한 연결을 사용 가능으로 표시하지 않는다.

## 8. AI 방과 메시지·작업 데이터

권장 구조:

- `Room.Kind = "ai"`를 추가한다. `OwnerID`는 현재 사용자이며 `Members`에는 실제 소유자 한 명만 둔다. 로그인 가능한 가짜 AI 계정은 만들지 않는다.
- 방에 선택된 `model_id`와 effort를 저장한다. 생성 중에는 해당 방의 선택 변경을 409로 거절하여 상태를 단순하게 유지한다.
- AI 답변도 기존 `Message`에 저장한다. 기존 텍스트 표시/알림 호환성을 위해 `Kind="text"`를 유지하고 서버만 설정할 수 있는 AI 작성자/모델/request metadata를 추가하는 방식을 우선한다.
- AI 작성자는 서버 전용 표현을 사용한다. 빈 UserID를 쓰면 `messageView`, 작성자 표시, 복사/삭제, 미리보기, 읽음/푸시가 이를 정확히 처리해야 한다. 사용자 계정으로 사칭하여 저장하지 않는다.
- `State`에 AI 요청 map을 추가한다. 요청에는 id, owner/room, client_id, 사용자 메시지 id, 답변 메시지 id, 상태, 모델/추론 snapshot, 시각, 공개 오류 코드, 재시도 원본 id를 보관한다. API 키는 저장하지 않는다.
- 구 데이터의 map nil, 없는 optional 필드, 스냅샷 재열기와 backup 복구를 처리한다. AI 상태 때문에 기존 schema를 무조건 거절하지 않는다.
- 작업 이력에는 보관 상한/정리 정책을 둔다. 중복 방지 기록의 보관 기간을 정하고 그 기간 안에 client_id 중복 방지가 유지되게 한다.

기존 경로의 의미를 함께 점검한다:

- `canPost`에 AI 텍스트 전송을 허용한 결과 파일 업로드/위치 전송까지 열리지 않게 한다. 첫 버전 AI 방의 첨부 UI와 API는 명시적으로 미지원 처리한다.
- AI 방 생성은 전용 핸들러가 담당하며 기존 일반 방 생성 body로 종류/소유자를 조작할 수 없게 한다.
- AI 방 초대/멤버 변경/친구 프로필 전환은 막는다. 방 목록, 고정, 이력, 읽음은 기존 기능을 재사용한다.
- 관리자 역할 자체가 타인의 AI 문맥을 조회하는 예외가 되지 않게 한다. 설정 관리자 권한과 대화 소유권을 구분한다.

## 9. 비동기 생성, 중복 방지, 취소 계약

상태는 `queued → running → completed | failed | cancelled | interrupted`로 정의한다. terminal 상태에서 늦게 도착한 결과가 completed로 덮어쓰지 못하게 한다.

수락과 실행 순서:

1. 세션, 사용자 활성 상태, 방 소유권, AI 활성 설정, model/effort, 입력 크기, rate limit, 큐 한도를 검사한다.
2. `Store.update`의 한 트랜잭션 안에서 중복 요청을 조회하고 사용자 메시지와 AI 요청을 함께 만든다. 동일 `(room, user, client_id)`는 기존 메시지/요청을 반환하고 provider를 다시 호출하지 않는다. 같은 id의 다른 본문은 충돌로 거절한다.
3. 사용자 메시지와 완료 답변을 담을 공간을 수락 시 확인하고, 실행 중 요청의 답변 저장 공간이 다른 쓰기에 소모되지 않게 예약 또는 동등한 보장을 구현한다. 매 요청 placeholder를 읽음 처리했다가 실제 답변 알림이 사라지는 방식은 피한다.
4. 저장 성공 후 작업자를 깨우고 202와 request/message 정보를 반환한다. 요청 저장 전 provider를 호출하지 않는다.
5. 네트워크/CLI 호출은 `Store.view/update`와 설정 잠금 밖에서 실행한다. 잠금 안에서 기다리거나 sleep하지 않는다.
6. 작업자는 `a.ctx`를 부모로 하되 요청별 timeout/cancel을 갖는다. 202 응답 완료로 취소되는 HTTP `r.Context()`를 그대로 background job에 연결하지 않는다.
7. 실행 직전과 결과 반영 직전에 소유권, 사용자 활성 상태, 원본 메시지 삭제 여부, 요청 상태를 재검증한다.
8. 완료 답변, 답변 id, completed 상태, 필요 알림 큐를 한 트랜잭션으로 저장한다. 저장 실패를 completed로 표시하지 않는다. 상태 변경 뒤 `a.changed(roomID)`를 호출한다.

동시성 및 재시도:

- 방마다 활성 요청은 1개다. 같은 client_id의 재전송은 기존 결과를 반환하고, 다른 메시지가 생성 중 추가되면 409와 현재 request_id를 반환한다.
- 전역/provider별 실행 상한과 bounded queue, 사용자별 AI 호출 제한을 둔다. 무제한 goroutine/대기열을 만들지 않는다.
- 일반 API의 분당 240회 제한만으로 AI 사용량을 통제했다고 보지 않는다.
- 실패 요청의 재시도는 명시적 사용자 동작이다. 새 client_id를 사용하고 원래 사용자 메시지는 재사용한다. 이 재시도도 중복 방지한다. completed 요청 재시도는 거절하고 새 질문을 사용하게 한다.
- 자동 provider fallback, 무한 재시도, 응답 중단 뒤 몰래 재호출을 하지 않는다. upstream 수락 이후 timeout은 이미 비용/한도가 사용되었을 수 있음을 상태에 반영한다.
- 취소는 상태와 취소 함수를 연결한다. 취소/완료 경합에서 트랜잭션으로 먼저 확정된 terminal 상태를 유지한다. 서버 취소가 upstream 과금 취소를 보장한다고 표시하지 않는다.
- 로그아웃은 화면/조회 연결을 정리한다. 명시적으로 중지하지 않은 수락 완료 작업은 계속될 수 있고 다음 로그인에서 조회할 수 있다. 사용자 비활성화/원본 메시지 삭제는 실행 및 결과 반영을 차단한다.
- 설정 적용·서버 종료 시 작업을 취소하고 작업자 종료를 기다린다. 재시작 시 남은 queued/running 요청은 interrupted로 확정하고 자동 재호출하지 않는다.
- 기존 `Close`, `startWorkers`, `serveGeneration`의 생명주기 안에 넣는다. 저장소가 닫힌 뒤 작업자가 쓰지 않게 한다.

## 10. 문맥 구성과 개인정보 경계

- 일반 클라이언트는 전체 `messages[]`, role, system prompt를 임의 제출하지 않는다. 서버가 승인된 현재 AI 방 이력에서 생성한다.
- 포함: 현재 AI 방의 유효한 사용자 텍스트와 정상 완료된 AI 답변, 이번 입력, 관리자가 정한 대화 지침.
- 제외: 다른 방/다른 사용자, 개인 인증함, 인증번호, 위치 요청/응답, 메일, 첨부 경로, 삭제·만료 메시지, 실패/취소된 미완성 답변.
- 새 질문의 문맥에는 실패했던 이전 질문만 고아 메시지로 쌓이지 않게 완료된 턴 쌍과 이번 입력을 기준으로 구성한다. 명시적 재시도는 원본 입력 직전의 유효 문맥을 사용한다.
- user/assistant 역할은 서버 저장 정보로만 결정한다. 사용자가 본문에 `system:` 또는 JSON을 적어도 권한이나 실제 role이 변하면 안 된다.
- provider/model 변경 시 같은 방의 텍스트 이력은 이어갈 수 있으나 각 답변의 모델 metadata는 당시 값으로 유지한다.
- 턴 수와 입력량 상한을 함께 둔다. 한글의 글자 수를 토큰 수라고 가정하지 않는다. 정확한 tokenizer가 없으면 보수적인 byte/문자 예산을 사용하고 추정임을 명시한다.
- 오래된 완료 턴부터 쌍으로 제외하며 이번 입력과 필요한 지침을 유지한다. 길이 제한 초과를 무한 재시도로 해결하지 않는다.
- CLI가 stateless라면 제한된 전체 문맥을 매번 전달한다. CLI session을 resume한다면 같은 과거 이력을 또 전달하여 중복 문맥을 만들지 않는다. 첫 버전은 stateless 원칙을 우선한다.
- 대화 내용, API 키, 쿠키, CLI stdout 원문, 인증 파일을 일반 로그에 남기지 않는다. request_id와 상태·소요시간·비밀이 제거된 오류 코드만 기록한다.

## 11. API와 화면 구현 기준

아래는 권장 API 계약이다. 기존 프로젝트 관례와 충돌하면 이름은 조정할 수 있으나 계약을 문서화하고 서버/클라이언트/테스트를 함께 바꾼다.

| API | 권한과 동작 |
|---|---|
| 기존 `GET/POST /api/settings` 확장 | 관리자 AI 설정 조회·저장·적용. revision 유지 |
| `POST /api/settings/test-ai` | 관리자만 연결/모델 테스트. 비밀값 비공개 |
| `GET /api/ai/catalog` | 로그인 사용자용 활성 모델·추론 목록과 기본값, 공개 상태 |
| `POST /api/rooms/ai` | 현재 사용자 소유의 AI 방 생성. model_id/effort/name 허용, owner/members 임의 지정 금지 |
| `PATCH /api/rooms/{room}/ai` | 소유자만 모델/추론 변경. 생성 중 409 |
| 기존 `POST /api/rooms/{room}/messages` | 일반 방은 기존 처리, AI 방은 중복 방지와 비동기 작업 생성 후 202 |
| `GET /api/rooms/{room}/ai/requests/{id}` | 소유자의 요청 상태·결과 id 조회. 요청과 방의 일치 검사 |
| `POST /api/rooms/{room}/ai/requests/{id}/cancel` | 소유자의 활성 작업 중지. 반복 호출 안전 |
| `POST /api/rooms/{room}/ai/requests/{id}/retry` | 실패/취소/중단 요청만 새 client_id로 재시도 |

모든 변경 API에 기존 로그인 검사, `X-OnTech-Request: 1`, Origin 검증, JSON/크기 검증을 유지한다.
오류는 기존 `error` 필드를 유지하면서 AI 전용 `code`를 추가할 수 있다. 400 잘못된 선택, 401 로그인, 403 권한, 409 충돌, 429 제한, 503 사용 불가를 구분한다. background 실패는 요청 상태에도 남긴다.
request id만 아는 타인은 내용·상태·취소·재시도에 접근할 수 없어야 한다.

UI:

- 채팅 화면에 `AI 대화 시작` 버튼을 추가하고 기존 방 목록 안에서 AI 배지로 구분한다.
- 방 상단에 모델과 추론 선택을 둔다. 허용 목록이 없으면 설정 필요/사용 중지 상태를 표시하고 전송을 막는다.
- `대기 중`, `답변 생성 중`, `중지`, `실패 사유`, `다시 시도`를 표시한다. 토큰 출력을 흉내 내기 위해 타이머로 완성된 문장을 쪼개지 않는다.
- 최초 202 응답 이후 `pendingSend`를 정리하되 request 상태는 별도로 추적한다. HTTP 결과가 불명확할 때는 같은 client_id를 유지한다.
- SSE `sync`와 방/작업 조회를 연결한다. 활성 방에 대해서만 제한된 polling fallback을 둘 수 있다. 새로고침·재접속 시 서버 상태에서 복원한다.
- `loadingSerial`, `messengerEpoch` 등 기존 오래된 응답 방지 패턴을 유지한다. 방 이동/로그아웃 뒤 이전 방의 늦은 응답이 현재 화면을 덮어쓰지 않게 한다.
- AI 답변은 우선 안전한 텍스트와 코드 줄바꿈으로 표시한다. 외부 CDN/임의 HTML/`innerHTML`로 답변을 렌더링하거나 CSP를 완화하지 않는다. Markdown이 필요하면 sanitizer까지 포함해 별도 검증한다.
- 기존 `app.js`의 전송 이벤트와 `messenger.js`의 화면 함수를 둘 다 점검한다. AI 화면만 추가하고 실제 전송의 chat/direct 제한을 남겨 두지 않는다.
- 새 JS/CSS 파일을 추가하면 `index.html` 로딩뿐 아니라 `app.go` 정적 파일 허용 목록과 embed 포함 여부도 확인한다.
- AI 방의 위치·첨부·초대 등 미지원 버튼을 숨기고 서버도 거절한다. 기존 친구 채팅에서는 그대로 동작해야 한다.
- 모바일 너비, 가상 키보드, 긴 코드, 스크롤, 뒤로가기, 대화 복사를 확인한다. AI UI를 Android 네이티브 화면에 중복 구현하지 않는다.

## 12. 알림과 저장 완료 기준

- AI 사용자 입력, 대기/생성 상태, 부분 출력은 푸시하지 않는다. 정상 완료 답변만 기존 Outbox/알림 경로에 한 번 넣는다.
- 실패/취소 상태는 시스템 오류를 AI가 말한 정상 답변처럼 저장하거나 알림하지 않는다.
- AI 답변 작성자가 실제 User가 아닌 경우 표시 이름, 마지막 메시지 미리보기, 읽지 않음, 읽음 후 알림 제거가 모두 정상인지 확인한다.
- 일반 알림 미리보기 설정과 Android 알림 정책을 유지한다. 새 metadata만으로 네이티브 알림을 깨뜨리지 않는다.
- 프런트엔드에서 생성 중 placeholder를 최종 message id처럼 읽음 처리하지 않는다.
- user input 삭제나 계정 비활성화 후 지연 결과가 되살아나거나 발송되지 않게 한다.

## 13. 권장 구현 순서와 변경 파일 분리

1. 최신 코드 재확인 → 변경 파일/계약/범위 기록 → 원본 보존.
2. `ai_config`/catalog/resolver와 암호화 설정 입력·출력·기존 설정 호환성 구현.
3. Provider interface와 HTTP/CLI 실행기를 구현하고 가짜 upstream으로 실제 model/effort 전달을 먼저 검증.
4. AI 방/요청 상태, 중복 방지, 문맥 구성, 작업자, 종료·취소·실패 처리 구현.
5. 인증된 API와 기존 메시지/방 목록/알림 경로 연결.
6. 관리자 AI 설정과 공통 웹 AI 대화 UI 구현.
7. 아래 핵심 테스트 및 필요한 기존 회귀 실행 → 공식 빌드 → 문서/버전/배포 산출물 정리.

파일은 기존 package server 안에서 `ai_config.go`, `ai_catalog.go`, `ai_provider.go`, `ai_http.go`, `ai_cli.go`, `ai_chat.go`, `ai_jobs.go`, `ai_test.go` 등으로 책임을 나눌 수 있다. 이름은 제안이며 기능마다 파일을 불필요하게 늘리지는 않는다.
`chat.go`, `settings.go`, `app.go`에 모든 provider 로직을 몰아넣지 않는다. Go 표준 라이브러리와 기존 의존성을 우선한다.
상황이 바뀌면 기능/계약 단위로 수정하고 중간 완성 상태와 남은 항목을 기록한다. UI만 만들어 놓고 백엔드를 TODO로 남기지 않는다.

## 14. 반드시 실행할 검증

자동 검증은 실제 외부 모델 과금 없이 `httptest`와 주입 가능한 가짜 CLI runner로 먼저 수행한다. 단순 문자열 존재 검사는 기능 검증의 대체가 아니다.

| 영역 | 통과해야 하는 내용 |
|---|---|
| 구 데이터 | AI 없는 기존 설정/암호화 상태 재열기 성공, 기존 메시지·계정·메일·FCM 값 보존 |
| 관리자 | 비관리자 저장/테스트 차단, revision 409, 키 미입력 유지/명시 삭제, 조회·로그·응답에 키 원문/암호문 없음 |
| 적용 | 저장만 한 설정은 실행에 반영되지 않음, 적용 후 새 요청부터 사용, 적용 중 기존 작업의 정상 중단 |
| 모델/추론 | 모델별 목록, provider_default 생략, 미지원 값 거절, 다른 provider의 모델/effort 혼입 방지 |
| 실제 전달 | 선택한 model과 effort가 HTTP JSON/CLI argv에 실제 존재, 미지원 필드는 없음, UI 표시와 sent 값 일치 |
| HTTP | `/v1` 경로 중복, redirect/인증 헤더, 401/429/5xx, timeout, 비JSON, 빈/큰/잘린 응답 |
| CLI | 설치/인증 불가, 긴 한글 stdin, 따옴표·개행, 구조화 결과, stderr 분리, timeout/취소/자식 정리 |
| 격리 | 두 사용자·여러 AI 방 문맥 분리, 타인 상태 조회/취소/재시도 금지, 일반/인증/위치 데이터 미전송 |
| 문맥 | 두 번째 질문에 첫 질문/완료 답변 포함, 삭제·만료·실패 턴 제외, 길이 한도, 재시도 원본 문맥 |
| 중복 | 동시 동일 client_id 여러 번 제출해 사용자 메시지 1개·요청 1개·provider 호출 1회·최종 답변 1개 |
| 경합 | 방당 생성 1개, 큐 포화, 취소와 완료 경합, 삭제/비활성화 후 지연 결과 폐기 |
| 지속성 | 수락/완료 저장 실패 처리, 답변 용량 보장, 서버 재시작의 interrupted 처리, 자동 재호출 없음 |
| UI | 실제 버튼 전송·선택·중지·재시도, 새로고침 복원, 방 이동 중 늦은 결과 차단, 모바일 레이아웃 |
| 호환 | 기존 친구/그룹 채팅, 첨부, 읽음, 개인 인증함, 위치 팝업, 설정 저장이 기존대로 동작 |
| 알림 | 최종 답변만 한 번 큐 등록, 읽음/미리보기 정책 유지, 부분/실패/취소 결과 미발송 |

빌드 주의:

- 검토 당시 `build.ps1 -Validation Core`의 서버 필터는 `Test(Location|AllFriends)`였다. Core 성공만으로 새 AI 검사가 실행되었다고 보고하면 안 된다.
- 새 테스트는 `TestAI...` 등 식별 가능한 이름으로 작성하고 공식 스크립트의 Core 검사에 AI 테스트를 포함한다. 수정한 공통 설정/채팅/알림 함수의 관련 회귀도 선택하여 포함한다.
- 기본 검증 명령은 `build.ps1 -Target Tests -Validation Core`, 서버 산출물은 `build.ps1 -Target Server -Validation Core`다. 구현 시 실제 파라미터/스크립트를 재확인한다.
- Android 네이티브/버전이 바뀌면 공식 Android 또는 All 빌드를 수행한다. 서버 웹 자산만 바뀌었다면 APK를 의무적으로 수정하지 않는다.
- Windows 설치/제거, 전체 Full 회귀, 운영 서버 실험을 관련 근거 없이 추가하지 않는다. 변경된 공통 코드의 구체적인 위험을 해결할 때만 검증을 확장한다.
- 실제 AI 계정 연결 시험은 이미 준비된 승인된 설정이 있을 때 최소 호출로 확인한다. 인증/사용량 때문에 실행하지 못한 provider는 mock 통과와 실연동 미검증을 구분한다.

## 15. 최종 보고 형식

다음 순서로 한국어로 보고한다.

1. 사용자가 어디에서 AI를 설정하고 모델·추론을 선택하는지.
2. 구현한 연결 종류와 각 연결의 실제 검증 상태. 설정만 저장되는 선택지는 완료로 표시하지 않는다.
3. 변경한 핵심 파일과 기존 데이터 호환 방식.
4. 실행한 테스트/빌드 명령, 실제 실행된 AI 테스트, 결과와 로그 경로.
5. 새 서버/필요시 APK 버전, 공식 output 경로, SHA256.
6. 미검증 사항과 실행에 필요한 관리자 설정만 구체적으로 기록.

빌드 성공, mock 테스트 성공, 실제 모델 응답, Windows 서비스 실행, 실제 Android 기기 동작을 서로 구분한다.
완료할 수 없는 연결이 있으면 어떤 환경/문법/권한/인증 조건 때문에 막혔는지 설명하고 해당 기능을 안전한 비활성 상태로 남긴다. 이를 전체 실연동 완료라고 보고하지 않는다.

## 착수 직전 핵심 확인 10개

- 실제 목표는 OnTechChat이며 참고 프로젝트 3개는 수정하지 않는다.
- Android UI는 서버 WebView다. 새 프런트엔드 프레임워크를 도입하지 않는다.
- store.db는 SQLite가 아니다.
- 현재 ClaudeProxyServer는 모델/추론 선택과 다중 사용자 세션 분리를 보장하지 않는다.
- goclaw의 Codex HTTP 구현과 LLM_Tools의 Codex CLI 구현을 혼동하지 않는다.
- 추론값은 모델별로 검증하고 실제 wire/argv 전달을 확인한다.
- 메시지 중복 방지와 AI 호출 중복 방지는 한 트랜잭션으로 연결한다.
- 네트워크/CLI 실행 중 저장소 잠금을 잡지 않는다.
- 설정 적용/종료/재시작에서 작업과 응답이 중복되거나 섞이지 않게 한다.
- Core 테스트 필터에 AI 검사가 실제 포함되었는지 확인한다.
