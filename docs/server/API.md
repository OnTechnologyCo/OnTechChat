# OnTechChat Server API — 1.15.14

모든 /api 요청은 로그인 세션 또는 지원되는 Bearer 세션으로 인증한다. 변경 요청은 기존 X-OnTech-Request 보호를 유지한다. 사용자/프로젝트/작업/산출물 권한은 요청 시점뿐 아니라 실행·다운로드 시점에도 서버 저장 상태로 다시 검사한다.


## Android 앱 업데이트
- GET /api/app-updates/android — 로그인 사용자에게 현재 게시된 APK metadata와 download_path를 반환.
- GET /api/app-updates/android/{sha256}.apk — 게시 manifest와 실제 파일의 size/SHA256을 다시 확인한 뒤 APK 다운로드.
- GET /api/app-updates/deployment — superadmin 전용 배포 상태/폴더 확인.
- Server Setup 1.15.13부터 공식 Android APK와 latest.json을 설치 payload에 포함한다.
- 신규 설치/서버 업데이트 시 설치기가 기존 --publish-apk 경로를 호출하여 DataDir/apk-releases에 원자 게시한다.
- Android 클라이언트는 서버 metadata 외에도 package/version/size/SHA256/signing certificate를 확인한 뒤 OS 설치 화면을 사용한다.
- 기존 게시 버전보다 낮은 versionCode는 자동 downgrade하지 않고, 같은 versionCode의 다른 APK도 덮어쓰지 않는다.

## AI 친구
- GET /api/ai/catalog — 사용 가능한 AI 모델/추론 공개 정보. credential 제외.
- GET /api/ai/friends — AIProfile 목록과 본인 기본 방/사용 가능 상태.
- POST /api/ai/friends/{id}/open — 본인 기본 AI 방 멱등 생성/열기.
- PATCH /api/ai/friends/{id}/default-room — 본인 AI 방을 기본 방으로 지정.
- POST /api/rooms/ai — 별도 문맥의 AI 방 생성.
- PATCH /api/rooms/{room}/ai — 모델/추론 변경.
- 기존 AI request 조회/cancel/retry API 유지.

## 자동 Agent 작업공간
- AI 친구를 열거나 AI 방을 만들거나 메시지를 보내면 사용자별 기본 작업공간을 서버가 자동 생성한다.
- 자동 작업공간은 기존 수동 프로젝트와 분리하며 실제 host root/storage_key는 일반 UI/API에 노출하지 않는다.
- 작업공간 루트에 `AGENT.md`, `AGENTS.md`, `CLAUDE.md`를 서버가 자동 생성한다.
- 정책 MD에는 현재 username/role/workspace 범위가 들어가며 요청마다 다시 확인·복구한다.
- 모델은 정책 MD를 읽지만 최종 권한 판정은 서버의 현재 사용자/방/프로젝트/등록 프로그램 상태가 수행한다.
- 정책 MD는 `workspace.write`로 수정할 수 없다.
- GET/POST/PATCH `/api/agent/projects`는 이전 버전 호환 및 내부 관리용으로 유지하지만 일반 설정/채팅 UI에는 노출하지 않는다.

## 자동 Agent 작업
- Claude CLI 모델의 AI 메시지는 별도 「작업 모드」 없이 Agent planner로 전달한다.
- 일반 질문이면 planner가 `tool_calls=[]`로 바로 답변하고, 파일/빌드/실행 작업이 필요한 경우에만 허용 도구를 선택한다.
- 상태: queued → planning → running → completed/failed/cancelled/interrupted.
- owner+자동 workspace+client_id로 중복 실행을 방지한다.
- GET `/api/agent/jobs/{id}`, cancel/retry API는 AI 채팅 상태 표시와 중지/재시도에 사용한다.
- POST `/api/agent/jobs` 수동 작업 API는 호환용으로 유지하며 일반 UI에서는 사용하지 않는다.

Agent 도구는 중앙 서버가 검증한 다음만 실행한다:
- workspace.list/read/write
- build.run
- program.run
- host_program.action

일반 Markdown/코드블록/임의 command JSON은 실행하지 않는다. Claude agent planner는 --json-schema structured output만 사용하며 unknown field와 도구별 불필요 필드는 서버에서 거절한다.

## Artifact
- GET /api/agent/artifacts/{id}
현재 project.read 권한, 저장된 상대경로, 실제 파일 SHA256/size를 다시 확인한 뒤 attachment로 전달한다. 변조 또는 권한 회수 시 반환하지 않는다.

## Grant / 역할
- 역할: user, admin, superadmin.
- GET /api/grants — superadmin.
- POST /api/grants — superadmin.
- POST /api/grants/{id}/revoke — superadmin.
- PATCH /api/users/{id}/role — superadmin 재인증 및 마지막 활성 superadmin 보호.
admin은 명시적으로 위임된 user.manage 대상 일반 사용자만 관리한다.

## 등록 프로그램
- GET /api/agent/programs — 사용자에게 허용된 프로그램/동작만.
- POST /api/agent/programs — superadmin 등록. executable/cwd/fixed args는 등록 시 고정하며 SHA256을 저장한다.
- PATCH /api/agent/programs/{id} — superadmin 전용. revision 충돌을 검사하며 실행 중인 정의는 변경할 수 없다.
- POST /api/agent/programs/{id}/actions — 요청은 action만 받는다.
지원 동작: start, status, stop, force_stop(등록 시 명시 허용).
프로그램 경로는 Program Files, Windows 또는 서버 관리 registered-programs 경로로 제한하며 Agent workspace 실행 파일은 등록할 수 없다.

## 실행기 진단
- GET /api/agent/diagnostics — superadmin.
설정·toolchain·planner 구성 상태를 반환한다. 이 응답의 ready는 실제 AppContainer self-test 수행 증거와 동일한 의미가 아니다.

## 보안 경계
Agent 생성 코드/빌드/실행은 Windows AppContainer + Job Object에서 수행한다. 사용자별 AppContainer SID를 사용하며 workspace에만 쓰기 ACL, 등록 toolchain에는 RX ACL만 부여한다. 환경은 allow-list로 새로 구성하며 서버 credential을 상속하지 않는다. network capability는 부여하지 않는다. workspace의 symlink/reparse point/hard link는 실행 전에 거절한다.

## AI 대화 삭제 (1.15.8)
- DELETE /api/rooms/{room}/ai/messages: AI 방 소유자만 전체 대화와 AI/Agent 요청 이력을 삭제한다. 다른 사용자/일반 방은 403.
- 기존 DELETE /api/messages/{id}: 본인의 AI 방에서는 AI 답변도 개별 삭제할 수 있다.
- 전체 삭제는 진행 중인 요청을 취소하며 완료 응답의 사후 저장을 차단한다. 작업공간 파일은 유지한다.
- GET /api/rooms/{room}/messages 응답의 cleared_through 이하 메시지는 클라이언트 로컬 목록에서도 제거한다.
- 삭제된 작업 조회의 404는 상태 폴링 종료로 처리한다.

## 1.15.12 본인 대화 비우기
- DELETE /api/rooms/{room}/messages: 참여 중인 direct/chat 방의 과거 대화를 요청자에게만 삭제 처리합니다.
- 응답: ok, cleared_through, scope=self. 범위는 저장 트랜잭션 시점까지의 메시지 ID입니다.
- 메시지 목록의 cleared_through는 요청자의 삭제 범위입니다. 클라이언트는 기존 캐시도 이 값 이하를 제거해야 합니다.
- 미참여자 및 auth/ai 방은 403. AI 전체 삭제는 기존 DELETE /api/rooms/{room}/ai/messages를 사용합니다.
- 다른 참여자의 원본/첨부파일은 보존합니다. 요청자의 과거 첨부파일 조회는 차단합니다.

### 1.15.12 RemoteControl MCP
- EditableSettings.remotecontrol: enabled, plugins_dir(선택), host(LAN IPv4), port, username, password(새 값), clear_password. 조회에는 has_password만 포함.
- POST /api/settings/test-remotecontrol: 최고관리자 세션 + revision + remotecontrol. 미저장 입력으로 MCP 초기화/도구 조회/pc_get_status를 검사하며 설정은 저장하지 않음.
- Agent remotecontrol.list/call: 관리자 이상만 실행. call program_id=목록의 pc_* 도구 이름, content=JSON 인자 객체 문자열. 나머지 도구 필드는 비움. 매 호출 현재 역할/프로젝트/작업 상태 확인.

## 1.15.13 선택 대화 분석
- GET /api/ai/context/friends?limit=50&after=ID: 본인 친구 최소 정보와 next_cursor. 최대100개.
- GET /api/ai/context/conversations?limit=50&after=ID&friend_id=ID: 본인이 참여한 direct/chat만 반환. friend_id는 선택.
- POST /api/ai/context/preview: {room_id,limit,from?,to?,timezone?}. limit1~200, from/to Unix초, timezone IANA. preview_token/messages/count/total_matching/truncated/expires_at 반환.
- POST /api/ai/context/analyze: {preview_token,action,question?,ai_room_id?}. action=summary|tasks|reply|question. 직접 질문 최대6000 UTF-8바이트. 본인의 AI방 선택 모델 또는 서버 기본 모델 사용.
- 분석 응답: result_id/text/sources/room_id/action/count/truncated/timezone/expires_at. sources는 id/sender/created_at/text만 포함.
- POST /api/ai/context/check: {preview_token} 또는 {result_id}. 유효하면 {valid:true}. 소유자가 아니거나 사라진 토큰404, 범위변경/만료409, 참여권한없음403.
- GET /api/ai/context/results/{id}: 현재 원문/권한 재검증 후 결과. GET /api/ai/context/messages/{room}/{id}: 접근 가능한 원문 텍스트.
- 인증 세션 필수. 관리자 예외 없음. 모든 응답 no-store. 메시지/결과 본문을 로그에 남기지 않음.
- 사용자당6회/분·1개 실행, 서버 최대2개 분석 동시 실행(기존 AI 동시성 설정 이내). AI 실패502, 제한429, 요청취소/시간초과408.
- 미리보기와 결과 최대15분 메모리 보관, 사용자당12개/서버1000개. 프로세스 재시작 시 소멸. 전체 정책은 CONVERSATION-HELP.md 참고.
