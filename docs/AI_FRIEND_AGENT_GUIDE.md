# OnTechChat AI 친구·서버 에이전트 사용 가이드

대상 서버: 1.13.0  
Android: 1.7.0 / versionCode 170  
실행 구조: 사용자별 별도 Windows 로그인/VM 없이 동일 서버 Windows의 AppContainer 격리

## 1. AI 친구 사용
1. 친구 목록의 **온테크 AI**를 선택한다.
2. 처음 열 때 사용자 전용 기본 AI 방이 하나 생성된다. 다시 열면 같은 기본 방을 연다.
3. 별도 문맥이 필요하면 기존 **AI 대화 시작**으로 새 AI 방을 만든다.
4. 상단에서 관리자가 허용한 모델과 추론 수준을 선택한다.
5. 일반 질문은 대화 모드에서 전송한다.

AI 방과 메시지는 사용자별로 분리된다. admin/superadmin 역할만으로 다른 사용자의 개인 AI 대화를 자동 조회할 수 없다.

## 2. 작업 모드
Agent 권한이 허용된 사용자는 AI 방 상단의 **작업 모드**를 누를 수 있다.

1. 작업 모드 선택
2. 기존 프로젝트 선택 또는 **새 프로젝트**
3. 자연어로 만들거나 수정할 프로그램 작업 입력
4. 서버가 Claude planner의 구조화 도구 요청을 검증
5. 승인된 파일 쓰기/Go 빌드/실행만 사용자 AppContainer에서 수행
6. 완료 후 채팅 답변과 산출물 링크 표시

현재 검증된 Agent planner: Claude Code CLI 2.1.283  
현재 검증된 필수 toolchain: 프로젝트의 Windows Go toolchain  
OpenAI compatible 연결은 일반 AI 대화에는 지원하지만, 1.13.0 Agent tool loop는 검증된 Claude CLI만 활성화한다. Codex CLI는 대화 전용 도구 격리를 보장하지 못한 상태이므로 Agent에 사용하지 않는다.

## 3. 권한
### user
- 개인 AI 대화
- 본인 프로젝트
- superadmin이 Agent 사용을 허용한 경우 본인 프로젝트의 Agent 작업
- 명시적 program/project grant 범위

### admin
- user 권한 포함
- superadmin이 user.manage로 위임한 일반 사용자만 관리
- 동급 admin/superadmin이나 위임되지 않은 사용자 변경 금지
- 서버 AI credential, 실행기 정책, 역할 부여 권한 없음

### superadmin
- 서버 AI/Agent 설정
- 역할 변경
- AccessGrant 발급/회수
- 등록 프로그램 정의
- 일반 사용자 Agent 허용
- 마지막 활성 superadmin은 사용 중지/강등할 수 없음

앱 역할과 Windows 관리자 권한은 별개다. superadmin이 생성 코드를 Windows 관리자 권한으로 실행하게 만들지는 않는다.

## 4. Agent 활성화
최고관리자 → 서버 설정 → **서버 에이전트**:

- 서버 에이전트 사용
- 작업공간 루트
- Go toolchain 루트
- 전체/사용자별 동시 작업
- Queue limit
- 프로세스 수
- 메모리
- 작업 시간
- 출력량
- 프로젝트 기본 quota
- network policy = blocked
- Windows isolation = appcontainer

기본값은 비활성이다. 실제 Windows 격리 테스트를 통과한 환경에서만 활성화한다.

Workspace Root를 바꾸는 것은 단순 설정 변경이 아니다. 프로젝트가 존재하면 1.13.0은 변경을 차단한다. 기존 workspace의 이행/보관을 먼저 결정해야 한다.

## 5. Windows 격리 범위
실행 작업은:
- 사용자별 서로 다른 AppContainer SID
- AppContainer token 실제 확인
- workspace만 Modify ACL
- toolchain은 Read/Execute ACL
- HOME/TEMP/AppData/Go cache 사용자별 분리
- 명시 환경만 전달
- network capability 없음
- CREATE_SUSPENDED → Job Object → Resume
- Job Object의 process/memory/time 제한
- cancel/timeout/server stop 시 해당 Job 자식 프로세스 종료
를 사용한다.

프로젝트 실행 직전 workspace 안의 symlink/reparse point/hard link를 거절한다. .., 절대경로, UNC, drive-relative, ADS 등의 경로도 API/tool 단계에서 거절한다.

### 디스크 한계
프로젝트 파일 쓰기와 빌드 후 총 크기는 project quota로 검사한다. 그러나 별도의 Windows 볼륨/파일시스템 quota를 설치하지 않으므로 생성 프로세스가 실행 도중 순간적으로 디스크를 사용하는 양까지 OS 수준에서 완전히 제한하는 것은 아니다.

## 6. 등록 프로그램
최고관리자 → 서버 설정 → Agent → **등록 프로그램**.

등록 시 서버가 고정하는 값:
- program ID/name
- executable
- work directory
- executable SHA256
- start 고정 인자
- 정상 종료 signal 파일
- resource lock key
- 허용 actions

허용 경로:
- Program Files
- Program Files (x86)
- Windows
- 서버 데이터의 registered-programs

사용자 Agent workspace에 있는 실행 파일은 등록할 수 없다.

일반 사용자/AI 작업은 executable/cwd/shell command를 지정하지 못하고 program_id + action만 요청한다. 허용 actions에 대한 AccessGrant가 있어야 한다.

지원 action:
- start
- status
- stop
- force_stop — 등록 시 별도 allow_force 필요

서버가 직접 시작해 추적하는 정확한 인스턴스만 종료한다. 이미지 이름으로 다른 프로세스를 일괄 종료하지 않는다.

## 7. AccessGrant 예
관리 설정의 **Access Grant**에서 대상 사용자, 자원 종류/ID, action을 지정한다.

예:
- admin에게 특정 사용자 관리: user / 사용자 ID / user.manage
- 협업 project 읽기: project / project ID / project.read
- project 실행: project.run
- project 관리: project.manage
- 프로그램 시작: program.start
- 상태 확인: program.status
- 정상 종료: program.stop
- 강제 종료: program.force_stop

Grant를 회수하면 이후 요청/도구 호출에서 즉시 새 권한을 검사하고, 더 이상 허용되지 않는 활성 Agent 작업은 취소 대상으로 연결된다.

## 8. 중복·취소·재시작
- Agent job: (owner, project, client_id) 중복 방지
- 같은 client_id로 다른 요청: 409
- tool_call_id: 같은 도구 결과는 재사용, 같은 ID를 다른 도구명으로 재사용하면 거절
- 같은 project의 활성 쓰기 작업은 중복 시작하지 않음
- 같은 AI 방에서 일반 AI 생성과 Agent 작업을 동시에 시작하지 않음
- 재시작 시 queued/planning/running AgentJob → interrupted
- 실행 여부가 불명확한 등록 프로그램 동작은 재시작 후 자동 반복하지 않음

## 9. 오류·복구
- AGENT_PERMISSION_REVOKED: 사용자/프로젝트 권한 다시 확인
- AGENT_PROVIDER_UNAVAILABLE: Claude CLI 설치/로그인/실행 경로 확인
- AGENT_TOOLCHAIN_UNAVAILABLE: Go root와 bin\go.exe 확인
- AGENT_TIMEOUT: 작업 범위 또는 timeout 확인
- AGENT_TOOL_LIMIT / AGENT_TURN_LIMIT: 반복 루프가 발생하지 않는지 요청/프로젝트 상태 확인
- Artifact 409: 등록 후 파일이 변경되어 hash/size가 달라진 상태
- Program executable 변경 409: 재등록 전까지 실행하지 않음

## 10. 운영 적용 전 확인
자동 테스트 통과와 운영 적용은 별개다.

별도 확인 대상:
- Windows 서비스 계정(Session 0)에서 Claude CLI PATH/HOME/인증
- 운영 서버의 Go toolchain RX ACL 호환성
- 실제 휴대전화 WebView의 작업 모드 UI/키보드/긴 로그
- 실제 등록 업무 프로그램의 정상 종료 계약
- 운영 backup/restore 절차
- 장기 디스크 사용량

운영 서버 설치·재시작, 방화벽 변경, 사용자 데이터 삭제는 공식 설치 단계를 명시적으로 수행하기 전 자동 검증 완료로 간주하지 않는다.
