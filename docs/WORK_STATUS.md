# 완료 — GitHub v1.15.35 정식 릴리스 게시 (2026-10-31 15:19 KST)

- 소스 커밋: 92e99310bb21ee96de038b8665c4363eb390b400.
- PR #4 main 병합: https://github.com/OnTechnologyCo/OnTechChat/pull/4
- 병합/릴리스 태그 커밋: 1095a0c3d4b39904fc27e1b7ccb6aa249c69e2c0. 병합 트리가 검증한 소스와 동일함을 확인.
- 정식 최신 릴리스: https://github.com/OnTechnologyCo/OnTechChat/releases/tag/v1.15.35
- PC Inno Setup1.15.35, Android1.8.11(191), android-update.json, manifest.json, SHA256SUMS.txt 총5개 업로드 및 공개 완료.
- GitHub latest API가 v1.15.35를 반환. 태그의 소스 커밋 확인. 로그인/토큰 없이 모든 자산을 실제 GET하여 HTTP200/크기/전체SHA256이 로컬 원본과 일치함을 확인.
- 서버 다운로드: https://github.com/OnTechnologyCo/OnTechChat/releases/download/v1.15.35/OnTechChat-Server-Setup-v1.15.35-x64.exe
- APK 다운로드: https://github.com/OnTechnologyCo/OnTechChat/releases/download/v1.15.35/OnTechChat-v1.8.11-arm64.apk
- GitHub 다운로드는 임시 터널/개발 PC 실행 상태에 의존하지 않음. 아래 임시 다운로드 기록은 이전 배포 방식.
- 검증 기록: .build/logs/release-1.15.35-github-verification.json. 운영 서버/실제 휴대전화 설치는 수행하지 않음.

# 완료 — 2026-10-31 메일 다중 제목 / GitHub 업데이트

- 공식 루트: D:\OnMakeSrc\OnTechChat. 서버1.15.35 / Android1.8.11(191).
- 메일 제목 조건: 한 줄당 하나, 최대20개/각200자, OR 포함 비교. 빈 줄·중복 조건 정리, 대소문자·연속 공백 무시. 기존 단일 조건 유지, 본문만 일치하는 메일 제외, 사용자별 중복 전달 방지 유지.
- GitHub: 공개 OnTechnologyCo/OnTechChat 최신 정식 릴리스 사용. 서버 설정 → 업데이트에서 PC Inno Setup/ARM64 APK 다운로드 링크 제공.
- 새 Android는 별도 공개 HTTP 클라이언트로 GitHub APK 직접 다운로드. 채팅 토큰·쿠키 미전달, 고정 저장소/HTTPS/다운로드 호스트 제한, manifest·APK SHA256/크기/패키지/버전/기존 서명 검증 및 Android 설치 승인 유지.
- 공식 빌드가 output/android-update.json을 생성합니다. 앞으로 같은 GitHub 릴리스에 Setup/APK/android-update.json/manifest.json/SHA256SUMS.txt를 함께 게시해야 합니다. 새 APK가 있고 메타데이터가 없으면 오류로 안내합니다.
- 기존 서버 APK 배포 API와 Setup 포함 APK는 이전 앱 호환용으로 유지합니다. 설정 화면의 APK 배포 폴더 메뉴는 GitHub 업데이트 메뉴로 변경했습니다.

## 검증 및 배포 파일
- build.ps1 -Target All -Validation Core 성공: 2026-10-31 15:07:48 KST. task.json completed, 빌드 잠금 정상 해제.
- Android 핵심211/알림22 통과. 서버 Core132 통과/실계정 opt-in5 skip/실패0 (159.137초). 설치기 HTTPS 핵심 통과, Inno 컴파일/x64 GUI/내부 payload30개/실행버전1.15.35 검증.
- 실제 headless Chrome UI15개 통과: 다중 줄 입력/이전 단일 조건/전송 payload/저장값 복원/PC Setup 및 APK 링크/신규 서버 버전 표시.
- APK730항목, ARM64/package191/minSdk26/정렬 확인. 기존 서명 SHA256 7fb644ceceefc9cfa7e6dbf775c1c21833bb62f79a792ef65cd2d82ddb2a99f1 유지.
- APK: output/OnTechChat-v1.8.11-arm64.apk / 28494858 bytes / SHA256 565ca02a065a6ce91f38dfe1b557ee7b5107154650ab185fcd3738d5df804bd6.
- Setup: output/OnTechChat-Server-Setup-v1.15.35-x64.exe / 34226445 bytes / SHA256 5548bb71755efff748e16d38bea98034a2d74df6605065ddda8e2c07f22610fa.
- android-update.json:526 bytes / SHA256 af9dcbe74628bcd5a047257a3b3c281aba60a9cef78c3e0a7e0446b1d74c23a4. APK191/서명/크기/SHA256과 일치.
- 공식 scripts/share.py 공유를 통해 공개 GET200/파일명/크기/전체 SHA256 일치 확인(15:08 KST).
- APK: https://conventions-documentary-memorabilia-federal.trycloudflare.com/apk
- Setup: https://conventions-documentary-memorabilia-federal.trycloudflare.com/server
- 공유는 개발 PC/프로세스 실행 중에만 유효. MCP session5cfe8c64bdad42708ea7ae2579f768c8, Python PID13284/cloudflared PID14184. 최신 주소 .build/download.json.

## 환경 및 남은 범위
- 최초 Core 경로 검사 실패는 SYSTEM의 C:\Windows\Temp가 테스트상 신뢰된 Windows 루트에 포함된 데 따른 fixture 환경 차이. 기존 .build/system-test-temp/SystemTemp/GOTMPDIR 사용 후 해당 검사와 전체Core 모두 통과. 제품 권한 코드는 변경하지 않음.
- 최초 APK 빌드의 NuGet System32/SysWOW64 경로 불일치를 기존 개발 캐시의 프로세스 환경 참조로 해결. 기존 Android debug.keystore도 프로세스 환경으로 참조하여 서명을 유지함. 키 복사/새 키 생성/전역 Windows 환경 변경 없음.
- 첫 실패 후 재빌드 기록을 성공으로 숨기지 않음. 최종 All/Core만 위 완료 결과로 사용.
- GitHub 공개 최신 릴리스 v1.15.34의 실제 API/Setup/APK 자산 메타데이터를 확인했음. 이후 사용자 요청으로 새1.15.35 소스 커밋/푸시/main 병합/릴리스 게시 완료(상단 최종 기록 참조). GitHub CLI OnTechnologyCo 인증과 공개 저장소 접근은 정상.
- 운영 서버 설치·서비스 재시작·실제 메일 전송/휴대전화 설치는 수행하지 않음.
- 주요 신규: server/github_updates.go 및 검사, server/mail_multifilter_test.go, Android Core/GitHubUpdates.cs 및 검사, docs/server/RELEASE-1.15.35.md.
- 로그: .build/logs/server-core-test.log, Tests-test.log, NotificationPolicy.Tests-test.log, android-arm64-*.log, inno-setup-build.log, release-1.15.35-download-verification.json. UI: .build/mail35-dom.html.
- 변경 원본 백업: .build/mail-multi-20261031-backup, .build/github-updates-20261031-backup. 제품 소스/설치 payload에는 포함하지 않음.

# 완료 — GitHub v1.15.34 게시 (2026-10-18 KST)

- 소스 변경 커밋: `6c3577edfa43eb42099835f7efa95f6598605124`.
- PR #3 main 병합 완료: https://github.com/OnTechnologyCo/OnTechChat/pull/3
- 병합 커밋: `c296a1e3a80817cb02db3940a59da5e56bec9f1e`.
- 정식 릴리스: https://github.com/OnTechnologyCo/OnTechChat/releases/tag/v1.15.34
- Inno Setup 서버 1.15.34, Android 1.8.10 ARM64 APK, SHA256SUMS.txt, manifest.json 업로드 완료. GitHub의 파일 크기 및 SHA-256이 로컬 배포 파일과 일치함.
- 기존 GitHub CLI 인증으로 업로드 성공. 아래 과거 기록의 GitHub 업로드 제한은 해결됨.
- 네이버 지도 인증은 https://chat.ontechdev.com 기준 성공. 실제 휴대폰 지도 화면 확인은 별도이며 IP 주소 인증 성공을 의미하지 않음.

# 2026-10-18 01:44 KST — 도메인 NAVER 인증 성공
- 사용자의 콘솔 확인 완료 후 재검사: https://chat.ontechdev.com/ NAVER 인증 HTTP200 result 확인.
- 기존 https://175.119.187.91:49333/는 NAVER 인증401 유지. 앱의 서버 접속 주소를 도메인 HTTPS로 변경해야 하며 휴대전화 실제 지도 표시까지 확인한 것은 아님.
- HomePC GitHub CLI는 OnTechnologyCo 계정으로 유효하게 로그인돼 있음. 이전 연결 앱403과 별개로 현재 CLI 인증을 사용해 소스/릴리스 업로드 진행.

# 2026-10-18 01:45 KST — NAVER 인증 후속 확인
- 실제 운영 https://chat.ontechdev.com/healthz가 공인 TLS 검증 포함 HTTP200 / 서버1.15.34로 응답. 기존 https://175.119.187.91:49333/healthz도200. 이전 운영 미설치/도메인 미연결 상태는 이제 변경됨.
- 공개 Client ID r6i3znksyy로 NAVER /v3/auth 확인: https://chat.ontechdev.com:8787/는200 result, https://chat.ontechdev.com/와 https://175.119.187.91:49333/는401 Authentication Failed. 현 배포 location-naver.js는 올바른 Client ID와 ncpKeyId 사용.
- 수정할 NAVER Web 서비스 URL: 포트 없는 https://chat.ontechdev.com 및 현재 앱의 IP 접속을 위한 https://175.119.187.91. 앱1.8.10 기본 주소는 여전히 IP:49333.
- NAVER 콘솔은 로그인 화면. Cloud Browser 보안 로그인 요청은 declined로 종료되어 등록값 변경은 하지 못함. 자격증명 재요청/우회 없이 중단. 다음 작업은 사용자가 로그인 재개를 요청하거나 콘솔에서 URL 등록을 완료한 뒤 인증 응답 확인.

# 완료 — 2026-10-18 서버1.15.34 자동 HTTPS / Inno Setup
- 공식 경로 D:\OnMakeSrc\OnTechChat, build.ps1 -Target Server -Validation HTTPSSetup 최종 성공: 2026-10-18 01:23:16 KST. 프로젝트 잠금 정상 해제.
- 사용자 요청대로 기존 자체 Setup 확인창과 Go EXE 직접 Setup 게시 경로를 제거. 유일한 배포 Setup 정의 installer/OnTechChat.iss, Inno Setup 6.7.3. 내부 서버·서비스 관리 실행 파일은 .build/OnTechChat.Runtime.exe로만 빌드하여 Inno에 포함.
- Inno 설치 마법사가 내부 --install-worker를 호출하고 실제 종료 코드를 확인. 실패는 완료 처리하지 않음. 기존 경로·서비스 계정·데이터 보존·업데이트 복원·APK 게시·트레이·단일 제거 항목/데이터 삭제 선택 유지.
- 공식 Inno 컴파일러 다운로드 Authenticode Valid, 발행자 Pyrsys B.V. 확인. 다운로드 SHA256 9c73c3bae7ed48d44112a0f48e66742c00090bdb5bef71d9d3c056c66e97b732. 컴파일러는 tools/InnoSetup, build.config.json에서 지정.
- 자동 HTTPS: 최고관리자 서버 설정 → 외부 접속 → 도메인 HTTPS 자동 설정. 별도 TCP443 수신기, TLS-ALPN-01 발급·갱신, 기존 서버 키로 암호화한 인증서/ACME 캐시. 기존 IP 접속·수신 포트·TLS 설정 유지.
- Cloudflare DNS 전용 A chat → 175.119.187.91, 외부 TCP443 → 실제 서버 TCP443 연결이 필요. 서버 PC localhost 설정의 전용 방화벽 버튼 제공. 공유기 포트 전달은 자동 변경하지 않음.
- 인증서 확인은 실제 도메인 SNI와 공인 CA 검증을 사용한 로컬 TLS 확인이며 인터넷 접속 성공으로 표시하지 않음. 운영 서버 설치/인증서 발급/외부 도메인 접속은 미수행.
- 최초 Server/Core 통과 후 Inno 변경은 핵심11개(HTTPS4 + 설치7)만 확인. settings.js 구문, 내부 x64 GUI/서버버전1.15.34/payload29개, Inno 스크립트 컴파일/최종 PE 버전 확인. 실제 UAC 설치·업데이트·제거·재부팅 시험은 미수행.
- 최종 Setup: output/OnTechChat-Server-Setup-v1.15.34-x64.exe / 34210905 bytes / SHA256 83ef409466935616912beb0cbf653370d47b261418fa20c0abd7e2964fdcf301.
- APK는 기존 output/OnTechChat-v1.8.10-arm64.apk (190) 유지. 28486666 bytes / SHA256 cf93b27506ce821f2849b150758e5c4f1bc0c6969cbffc335033b25b732d7b6e. 서명/package/version/정렬을 확인해 Setup에 포함.
- 다운로드: https://humanitarian-turbo-saving-boat.trycloudflare.com/server (2026-10-18 01:23:48 KST 공개 GET200, 파일명·크기·전체 SHA256 일치 확인).
- scripts/share.py 단일 임시 공유 실행: Python PID3564, cloudflared PID7500, MCP session4687ee02c8cc441b9b34f157995a20bf. PC/공유 프로세스 종료 시 링크도 종료. 최신 상태 .build/download.json.
- 로그: .build/logs/inno-setup-build.log, https-server-test.log, https-setup-test.log, release-1.15.34-download-verification.json.
- 편집 백업은 .build/https-edit-backups로 이동하여 소스/payload에 포함하지 않음. output에는 종류별 최신 APK/Setup 하나씩만 유지.
- 실제 서버는 별도 PC 175.119.187.91. HomePC_AMD는 개발 PC이며 운영 Caddy/DNS/방화벽/서비스/데이터를 변경하지 않음.
- GitHub 업로드 미완료: 연결 앱의 쓰기 요청403 기록 유지. 커밋/푸시/운영 배포 성공으로 보고하지 않음.
- 다음 순서: 실제 서버에서 새 Inno Setup 설치 → 도메인 HTTPS 설정/외부443 연결 → 도메인 접속 → NAVER 등록 URL을 실제 앱 주소에 맞춰 적용.

# 완료 — 2026-10-18 앱1.8.10(190) / 서버Setup1.15.33 — 네이버 지도
- 공식 원본 D:\OnMakeSrc\OnTechChat에 적용하고 build.ps1 -Target All -Validation Core 최종 성공: 2026-10-18 00:31:39 KST. 빌드 종료, 프로젝트 잠금 정상 해제.
- GPS 경로 기본 지도를 NAVER Dynamic Map으로 변경. 시작/마지막 지점, 경로 테두리, 정확도 원, 위치 선택, 카메라 복원, 대체 지도 및 기존 60초 기록/48시간 보관/15초 공유 권한 재검증 유지. 공개 Client ID만 사용하며 Secret은 사용하지 않음.
- 실제 SDK 검사에서 콜백이 전역 객체 등록보다 먼저 실행되는 문제를 발견해 microtask 이후 준비 판정으로 수정. JSONP 스타일 메타데이터와 .cur 리소스의 Android 허용 정책 보완. CSP 외부 스크립트는 SDK/인증/고정 메타데이터 파일로 제한. style-src-attr만 inline을 허용하고 inline script/style 요소/eval/wildcard는 계속 차단. SDK 오류 수집 POST도 차단 유지.
- Android 핵심201 + 알림22 + 서버 Core123 = 총346개 통과. 서버 opt-in5 skip. 서버 검사 안의 지도 생명주기 모의10개 포함(총합에 중복 가산하지 않음). 기존 Firebase obsolete 경고 유지.
- 실제 Chrome 검사 통과: 등록 도메인 https://chat.ontechdev.com:8787 원점을 로컬 프로젝트 fixture로 구성하고 실제 NAVER SDK/인증/메타데이터/타일을 요청함. 서울 모의 GPS 경로, 한글 지도, 타일25응답/로드 이미지29/SVG21, 위치 선택·카메라 복원·대체 지도 선택·정리 확인. 지도 필수 리소스 CSP 위반 없음; SDK telemetry POST 차단 확인. 실제 운영 도메인의 DNS/TLS/서버 배포 검증과는 다름.
- APK: output/OnTechChat-v1.8.10-arm64.apk / 28486666 bytes / SHA256 cf93b27506ce821f2849b150758e5c4f1bc0c6969cbffc335033b25b732d7b6e.
- Setup: output/OnTechChat-Server-Setup-v1.15.33-x64.exe / 41677312 bytes / SHA256 15ef5c6fef6a9e10418a1ea333284c932e470d755ee54404903f439ddfe3162f.
- APK730 entries, package com.ontechdev.chat, versionCode190, minSdk26, ARM64, 서명/16KiB 정렬 확인. 기존 인증서 SHA256 7fb644ceceefc9cfa7e6dbf775c1c21833bb62f79a792ef65cd2d82ddb2a99f1 유지.
- Setup x64 GUI/버전1.15.33/내부 payload27개 확인. 포함 APK1.8.10(190)의 크기·SHA256·서명 정보가 공식 APK와 일치. 편집 백업은 .build/naver-edit-backups로 이동하여 배포물에서 제외.
- 공식 scripts/share.py 단일 다운로드 실행. 공개 APK/Setup 실제 GET HTTP200, Content-Disposition 버전 파일명, 크기와 전체 SHA256 일치 확인.
- APK: https://acting-conservative-bicycle-commissions.trycloudflare.com/apk
- Setup: https://acting-conservative-bicycle-commissions.trycloudflare.com/server
- 임시 주소는 다운로드 작업과 PC가 실행 중일 때 유효. 최신 상태는 .build/download.json. 재부팅 자동 실행이나 영구 다운로드 서비스는 추가하지 않음.
- 검증 기록: .build/logs/release-1.15.33-verification.json, release-1.15.33-download-verification.json, naver-ui-verification.json, server-core-test.log 및 Android 빌드/서명 로그. 화면: .build/naver-map-ui.jpg.
- HomePC 접근 설정은 사용자 승인에 따라 공식 프로젝트 루트만 추가했고 파일 도구 접근을 확인함. 원본 HEAD b1fcb5751697a0b17006935ad507a7bb7e50eb94에서 기존 변경 없음/잠금 사용 가능 확인 후 적용. 이전 전달 패치는 초안이며 현재 공식 원본이 최종 수정본임.
- GitHub 원격 브랜치/커밋/PR 업로드는 수행하지 않음(아래 이전 연결 앱403 기록 참조).
- 운영 서버 설치·서비스 재시작·DNS/Cloudflare/Caddy/방화벽·사용자 데이터 변경 없음. 에뮬레이터/실제 휴대전화 설치·GPS 이동 검증 미수행. IP로 접속하는 경우 그 IP 원점도 NAVER Web 서비스 URL에 등록해야 함.
- 상세: docs/server/RELEASE-1.15.33.md. 아래 이전 패치 준비/접근 차단 기록은 해결 전 상태.

## 이전 패치 준비 — 2026-10-17 네이버 지도 / 앱1.8.10(190) · 서버Setup1.15.33
- 기준 main: c09bb1bb795edb675924f099ac70ee454d8fa24a. PR #2는 이 커밋에서 병합됨. 아래 10월14일 기록의 '병합 대기'는 당시 상태.
- 변경안은 OnTechChat-NaverMap-1.15.33.patch로 전달. GitHub feat/naver-gps-map-1.8.10 브랜치 생성은 연결 앱의 403 Resource not accessible by integration 응답으로 거부되어 push/PR 생성되지 않음. 네이버 지도 기본 렌더러, Android 외부 리소스 제한, CSP, 이전 APK/연결 실패 대체 지도 및 회귀 검사 추가. GPS 공유·보관·15초 재검증 정책 유지.
- Node.js 문법 및 메모리상 SDK 모의 검사 9개 통과. 공식 SDK/CDN URL 형식 조회 확인. Go/Android 공식 검사, APK/Setup 빌드·서명, 실제 브라우저/기기 지도 표시는 미검증.
- 2026-10-18 KST 정정: 사용자 화면에서 등록 URL http://chat.ontechdev.com:8787을 확인하고 같은 키로 수동 인증 요청한 결과 HTTP 200/result 성공. 앞선 IP 원점 요청은 등록 URL과 달랐음. 키 정상 확인, 실제 브라우저/Android 지도 렌더링 검증은 미완료.
- HomePC_AMD의 파일 허용 루트 제한 때문에 D:\OnMakeSrc\OnTechChat 접근이 거부되어 공식 build.ps1을 실행하지 못함. 허용 목록이나 빌드 경로/잠금을 우회하지 않음. 복제 작업 폴더/새 배포물은 만들지 않았음.
- 신규 APK/Setup/SHA256 없음. 기존 output 1.8.9/1.15.32 및 운영 서버·DNS·Caddy·사용자 데이터는 변경하지 않음.
- 상세·남은 빌드/검증: docs/server/RELEASE-1.15.33.md.

# GitHub 인증 및 동기화 완료 — 2026-10-14
- 원격 작업은 현재 실행 계정의 기본 GitHub CLI 프로필과 Windows keyring을 사용한다. GH_CONFIG_DIR로 다른 Windows 사용자의 인증 폴더를 지정하지 않는다.
- 인증 값은 소스/문서/hosts.yml에 저장하지 않았다. 만료 여부는 GitHub 응답으로 확인하며 HTTP401만으로 만료라고 단정하지 않는다.
- Git 작업은 저장된 GitHub CLI 인증을 credential helper로 사용한다. 전역 Git 설정을 바꾸지 않으려면 명령별로 -c credential.helper= -c 'credential.helper=!gh auth git-credential'를 지정한다.
- GPS 알림 및 지도 개선 커밋 push와 PR #2 생성 완료: https://github.com/OnTechnologyCo/OnTechChat/pull/2 (main 병합 대기).
- 이번 마무리는 인증 등록과 작업 기록 갱신이며 앱/서버 코드 변경은 없다. 2026-10-13 All/Core 빌드 검증 결과를 유지한다.

# 완료 — 2026-10-13 앱1.8.9(189) / 서버Setup1.15.32 — 가입 없는 벡터 지도
- 브이월드 API 키 방식은 사용자 요청에 따라 보류. 가입/API 키 없는 OpenFreeMap + MapLibre GL JS 5.24.0 벡터 지도 적용. OpenStreetMap 기반이며 국내 업체의 독자 데이터로 전환한 것은 아님.
- 한국어 우선 이름, 선명한 확대, 흰 테두리 경로, 시작/마지막 지점, 축척, 전체 경로/마지막 위치 제공. 기존 Leaflet 지도 선택 및 WebGL 실패/타일 지연 fallback, 자동 조회 시 카메라/기본 지도 선택 유지.
- 엔진/워커/스타일/아이콘 자체 포함. 외부는 고정 타일/폰트 GET만 허용. Android URL 정책과 CSP 확장, 외부 스크립트/unsafe-eval 허용 없음. 위치 공유·48시간 보관·15초 권한 재검증 정책 유지.
- 공식 build.ps1 -Target All -Validation Core 성공: 2026-10-13 23:12:09 KST. Android 핵심159 + 알림22 + 서버121 = 총302 통과. 서버 선택 검사5 skip. 기존 Firebase obsolete 경고 유지.
- 브라우저 실제 OpenFreeMap 타일/한글 지도/경로 렌더링 확인(서울 도심 모의 GPS, 렌더 피처1510, 지도 오류/CSP 위반0). 위치 선택·카메라 복원·기본 지도 선택·제공자 차단·WebGL 실패·dispose 및 기존 GPS 권한 회수/로그아웃/범위 검사 통과.
- 검사 기록: .build/logs/map-ui-verification.json, .build/map-ui.jpg, .build/logs/release-1.15.32-verification.json. 실기기 GPU/WebView·현장 이동·운영 설치 검증은 미실행.
- APK: output/OnTechChat-v1.8.9-arm64.apk / 28486666 bytes / SHA256 45d157cf7ed718d4b89b926f2e3e8c5a1f3a71a1f6e9cdf44afe577166c10010.
- Setup: output/OnTechChat-Server-Setup-v1.15.32-x64.exe / 41666560 bytes / SHA256 116950cdebf7142bf4022ba967c3babe4a68202c393918b384b21ae4d5dd36fa.
- APK730 entries, 기존 인증서 SHA256 7fb644ceceefc9cfa7e6dbf775c1c21833bb62f79a792ef65cd2d82ddb2a99f1. Setup x64 GUI/26개 payload 검증, 포함 APK1.8.9(189) 해시 일치.
- GitHub: 2026-10-14 실행 계정의 Windows 자격 증명 저장소(keyring)에 인증 등록. 새 프로세스에서 계정/API 인증 확인, GPS 알림6b78d15 및 지도a0ce184를 fix/keyless-gps-map-1.8.9에 push 완료. PR #2: https://github.com/OnTechnologyCo/OnTechChat/pull/2 (base main, 미병합). 운영 서비스/개인 데이터/방화벽 변경 없음.
- 상세: docs/server/RELEASE-1.15.32.md 및 MAP-LICENSES.md.

# 완료 — 2026-10-13 앱1.8.8(188) / 서버Setup1.15.31 — GPS 알림 숨기기
- GPS 기록 알림·앱 메뉴·알림 설정에 GPS 알림 숨기기 추가. 이동 기록 설정 상단에 현재 표시 상태/안내를 제공하고 작은 화면에서 스크롤 지원.
- 기존 GPS 전용 채널 ontech_location_history 유지. 사용자가 Android 설정에서 해당 알림만 한 번 끄면 됨. 알림을 숨기기 위해 위치 기록/공유를 중지하거나 채팅 알림을 끄지 않음. 시스템 실행 중인 앱 표시는 별도.
- 공식 All/Core 성공: Android130개/알림22개/서버120 PASS·5 opt-in SKIP. APK730개 항목, package188/기존 서명 유지/정렬 확인. Setup x64 GUI/payload24개/버전1.15.31 및 공식 APK188 포함 검증.
- 실제 Android 기기 연결 없음. 기기의 채널 설정 화면/숨김 적용 및 에뮬레이터 실행은 미검증. 운영 설치·서비스 변경 없음.
- 백업 .build/gps-notification-backup-20261013. 검증 .build/logs/release-1.15.31-verification.json.
- output/OnTechChat-Server-Setup-v1.15.31-x64.exe / 39866880 bytes / SHA256 affbaa7fd82119f49e681f821acf3ac4e4cb4e292c9632f1eab9d159bb72a965.
- output/OnTechChat-v1.8.8-arm64.apk / 28486666 bytes / SHA256 4dc33f4c3f62f43bb20c9ab4136382a16f9664518d83415941504deb27ccaca9.

# 완료 — 2026-10-10 앱1.8.7(187) / 서버Setup1.15.30 — 로그인 상태 보존
- 사용자: 친구/대화는 정상이나 상단 로그인 상태 불일치, 설정 진입 시 native 로그인 화면 복귀. 휴대전화 오류 로그는 미확보. 제보의 단일 원인을 실기기에서 확정하지는 않음.
- 확인한 코드 결함: SecureState 읽기/복호화/키 접근 오류를 빈 LocalState로 반환하고 배경 FCM 저장이 해당 상태로 덮어쓸 수 있었음. 빈 상태 반환을 제거하고 처음 성공한 상태 캐시, 복제본 조회, 저장 성공 후 상태 교체로 변경. IO 접근 실패를 파일 없음으로 처리하거나 기존 키 접근 실패 시 새 키로 덮어쓰기 금지.
- 기존 Android Keystore AES-GCM 저장 형식/키 이름/앱 서명을 유지. 읽기·쓰기 실패로 로그인과 설정을 초기화하지 않음. 명시 로그아웃 및 서버 SESSION_INVALID 권한 해제 유지.
- 서버 연결·로그인 유지 상단 표시와 FCM 알림 상태 분리. 쿠키 콜백의 managed 타입 판정 대신 실제 ontech_session 일치 확인, 이미 저장된 쿠키 재쓰기 방지. STATE_READ_FAILED/STATE_SAVE_FAILED/WEB_COOKIE_MISSING/WEB_COOKIE_TIMEOUT 구분. 민감정보 없이 오류 단계와 타입 기록.
- 공식 All/Core 성공. Android 핵심130개/알림22개, 서버 120 PASS/5 opt-in SKIP/실패0. 새 저장 실패·배경 동시 저장·명시 로그아웃·쿠키 확인 검증17개 추가.
- 깨끗한 APK 빌드 730 entries/libxamarin-app.so 1450320 bytes, 서명·정렬·package187/기존 인증서 확인. Setup x64 GUI/payload/version1.15.30 및 공식187 APK 포함 검증.
- 운영 설치·서비스 변경·휴대전화 실행·PC 재부팅 검증 미수행. 기본 서버 healthz1.15.29 응답 확인. 현재 SDK 에뮬레이터 실행 파일/이미지 없음.
- 임시 다운로드 https://volt-customers-stretch-task.trycloudflare.com. 공개 실제 GET200/파일명/크기/전체SHA256 모두 검증.
- 소스 백업 .build/session-state-backup-20261010. 검증 .build/logs/release-1.15.30-verification.json.
- output/OnTechChat-Server-Setup-v1.15.30-x64.exe / 39865344 bytes / SHA256 d8bb5df4f060867df240bd09923a75914183f0073c1bea955c1d9ad5a217a810.
- output/OnTechChat-v1.8.7-arm64.apk / 28486666 bytes / SHA256 37e75678b2f6f681f7373efe10c04278e6a92132d97191a996e912a2e85ef46a.

# 완료 — 2026-10-10 즉시 종료 보완 / 앱1.8.6(186)·서버1.15.29
- 사용자 증상: 앱을 누르면 열리자마자 종료. 직접 기기 오류 로그는 미확보.
- APK 패키지 누락: 1.8.4 730개 → 1.8.5 109개. 캐시 삭제·전체 빌드로 새 APK 730 개, libxamarin-app.so 1450320 bytes 및 필수 Kotlin/Firebase/AndroidX 리소스 확인. 공식 빌드에 캐시 재생성·필수 페이로드 검사 추가.
- FirebaseInitProvider 자동 초기화 대신 기존 Application의 명시 초기화를 예외 보호. 알림 채널 생성 보호. Activity 시작 실패 시 로그인 데이터 유지 및 진단 화면/복사 제공. 진단에는 토큰/계정/예외 메시지를 기록하지 않음.
- 기존 로그인 유지·자동 복구 유지. APK 기존 설치 위에 덮어 설치. 데이터 초기화/앱 삭제 필요 없음.
- 공식 All/Core 완료. Android 핵심113개/알림22개, 서버 120 개 PASS/5 개 opt-in SKIP/실패0. APK 서명/정렬/package186/기존 인증서, Setup x64 GUI/payload/--version1.15.29 검증 통과.
- output/OnTechChat-Server-Setup-v1.15.29-x64.exe / 39863296 bytes / SHA256 1f6a5195dcdd6c41193c7ffd2ea5f715f383fa839f08440d1b5729ccff650518.
- output/OnTechChat-v1.8.6-arm64.apk / 28486666 bytes / SHA256 b6e766b08a0e9b573984bb5d01c4431173f32643586f9619adc3ac00ed94b28e.
- 공개 다운로드 실제 GET HTTP200/파일명/크기/전체SHA256 검증. 임시 주소 https://bryant-walks-ranger-sao.trycloudflare.com.
- 백업 .build/startup-fix-backup-20261010. 검증 .build/logs/release-1.15.29-verification.json.
- 실제 휴대전화 실행·제보 재현·운영 설치 미수행. 현재 SDK에는 emulator 실행 파일/이미지가 없어 에뮬레이터 실행도 하지 않음. 패키지 누락은 확인했으나 제보 종료 원인 자체를 확정한 것은 아님.

# 완료 — 2026-10-10 앱1.8.5(185) / 서버Setup1.15.28 로그인 복구
- 웹 GET API 인증 실패와 로그인 시도에서 저장 세션으로 실제 복구. 이전 WebView 오류 콜백 무시. 연결 실패 동안5초 재시도.
- 쿠키 저장8초 제한, 늦은 콜백/로그아웃/계정 전환 뒤 화면 재열기 차단. 연결 진단 메뉴 추가, GPS 안내 분리.
- 공식 build.ps1: All/Core에서 Android 핵심113개/알림22개, APK 생성·서명·정렬·package185 확인. Server/Core 최종 성공. 서버 120 개 PASS/5 개 opt-in SKIP/실패0. Setup x64 GUI/payload21개/--version1.15.28 검증 통과.
- 패키징의 SYSTEM Git 소유권 검사는 재시도 프로세스에만 GIT_CONFIG_COUNT/safe.directory를 설정하여 해결. 전역Git 설정·Windows 계정 변경 없음.
- output/OnTechChat-Server-Setup-v1.15.28-x64.exe / 39350272 bytes / SHA256 e38047efc170d902e6e8110239ccc3d730fdbdf2c6b3a2d360dababf256a4eea.
- output/OnTechChat-v1.8.5-arm64.apk / 27838614 bytes / SHA256 5afd95fb0253b6deba6c24e566f6407e28c4a7eee8750b702ced4c738bd54373.
- 공개 APK/Setup 실제 GET 다운로드 HTTP200, 파일명/크기/전체SHA256 일치 확인. 현재 주소 https://bryant-walks-ranger-sao.trycloudflare.com. 임시 링크는 PC와 다운로드 작업 실행 동안 유효.
- 소스 백업 .build/login-recovery-backup-20261010. 검증 기록 .build/logs/release-1.15.28-verification.json.
- 실제 휴대전화 재현/PC재부팅/운영 설치 미수행. 기본 주소 /healthz는 기존서버1.15.27 정상 확인, 이번 앱과 호환.

# 1.15.27 / Android 1.8.4 완료 — 로그인 유지
- 2026-10-09: PC 재부팅/서버 재시작/앱 재실행/일시 연결 실패에도 로그인 유지. 직접 로그아웃 또는 비밀번호 변경·계정 사용 중지 등 명시적 권한 회수까지 서버 세션 영속 유지.
- 기존 유효 세션만 이전. 만료·로그아웃·회수한 토큰은 복구하지 않음. 다른 기기 로그인 때문에 기존 기기를 자동 로그아웃시키지 않음.
- Android 연결 대기 화면 및 5초 자동 복구, 정상 연결 중 30초 인증 확인, 암호화 토큰으로 WebView 쿠키 갱신. 프록시 401/403·5xx·잘못된 현재 비밀번호 입력으로 세션 삭제하지 않음.
- 서버 Core 120개 최상위 검사 통과, 5개 실계정 opt-in skip, 실패0, 151.981초. 재시작/이전/직접 로그아웃/계정 중지/다른 기기 로그인/비밀번호 오류의 새 회귀 포함.
- Android 핵심106개/알림22개 통과. Chrome 실제 headless UI9개 통과(자동 재연결 타이머 포함). JavaScript 구문 검사 통과.
- Android 서명/zipalign/package/version184 확인. 기존 인증서 SHA256 7fb644ceceefc9cfa7e6dbf775c1c21833bb62f79a792ef65cd2d82ddb2a99f1 유지.
- 공식 build.ps1로 Android 생성 후 Server/Core 최종 성공. Setup x64 GUI/내부 payload20개/--version1.15.27 및 최신 APK 포함 검증. manifest.json과 실제 파일 크기·SHA256 모두 일치.
- APK: output/OnTechChat-v1.8.4-arm64.apk / 28,482,570 bytes / SHA256 265842dae68742bf31fc99d435ff7afbddbf0e9904e57e9de0f4bd3464ba9fb0.
- Setup: output/OnTechChat-Server-Setup-v1.15.27-x64.exe / 39,859,200 bytes / SHA256 b519ec797c67c929af71755e9500d5458462ea3cbea2064ce247065d6b968bfd.
- SYSTEM 실행 환경 보완: 기존 NuGet 패키지 캐시와 원래 Android debug.keystore를 프로세스 환경으로 참조(복사/새 인증서 생성 없음). 샌드박스 테스트 agentTestEnv에 GOTMPDIR만 추가. 검증은 .build/system-test-temp를 SystemTemp로 사용하며 SYSTEM/Administrators 전용 DACL 적용.
- 변경: server auth_sessions(신규·검사), auth/store/app/maintenance/location_history/location_monitoring/web, Android MainActivity/ChatWeb/Core/검사/버전, 공식 빌드 Core 인증 검사 필터, 문서. 소스 백업 .build/login-persistence-backup-20261009.
- 검증 로그: .build/logs/server-core-test.log, Tests-test.log, NotificationPolicy.Tests-test.log, session-ui.log, android-arm64-*.log, server-payload-check.log. UI 검증 .build/verify-session-ui.mjs.
- 서버·앱 모두 업데이트해야 전체 적용. 운영 설치·서비스 재시작·실제 PC 재부팅·휴대전화 실행 검증은 수행하지 않음. 이미 앱의 로그인 정보가 삭제된 경우 최초 1회 재로그인 필요.
- 상세: docs/server/RELEASE-1.15.27.md.

# 1.15.26 완료 — 대화 목록 길게 누르기 팝업의 대화방 삭제
- 2026-10-03: 1:1/그룹/인증함/AI방의 길게 누르기 메뉴에 `대화방 삭제` 추가.
- DELETE /api/rooms/{room}: 현재 사용자 목록 숨김과 기존 본인 기록 비우기를 같은 저장 트랜잭션으로 처리. 상대방 원본과 공유방 참여 상태 보존. 나가기는 별도.
- 서버의 hidden_for 필드가 재접속/다른 기기에 유지됨. 새 메시지만 목록 복원; 1:1/AI 친구 재열기 시 삭제 경계는 유지.
- 메일 중복 전달 이력, AI 작업 취소/문맥 삭제/늦은 응답 차단 경계 유지.
- 삭제 관련 API/단위 회귀 통과(7.731초), Chrome headless 실제 팝업/포인터/취소/다른 방 삭제/실패복구 검사 21개 통과. 실제 Android 기기 검증은 하지 않음.
- 공식 build.ps1 -Target Server -Validation Core 성공. 서버 Core167.429초, x64 GUI/내부 payload19개/--version 1.15.26 확인. 전체 Full 검사를 수행한 것은 아님.
- output/OnTechChat-Server-Setup-v1.15.26-x64.exe / 39,853,056 bytes / SHA256 244504954870cffbed48e9c302795e06bae23226d8b534cad383900c4c3cff0c.
- 실제 해시와 manifest 일치. 공개 다운로드 HTTP200/Content-Disposition v1.15.26/Content-Length39853056 확인.
- Android1.8.3 유지. 운영 서비스/다른 PC 자동 설치 및 사용자 데이터 삭제는 수행하지 않음.
- 검증 파일: .build/verify-room-delete-ui.mjs, .build/logs/room-delete-ui.log, .build/logs/server-core-test.log.
- 소스 백업: .build/room-delete-backup-20261003-104642. 상세: docs/server/RELEASE-1.15.26.md.

# 이전 1.15.22 메일 수집 진단/전달 복구 기록
- 기존 빌드 완료 및 잠금 확인. 운영 PC와 메일 계정에는 접속하지 않음.
- 실제 수집 테스트 API, 단계별 결과, 본문 표시, 기존 설정 호환 및 회귀 검사 후 Setup 배포 예정.

# 작업 상태 — 2026-09-30 1.15.17 / Android1.8.3 완료
- 첨부 선택은 로컬 초안, 전송 시 업로드. 이미지 미리보기/파일 취소/방별 초안/부분 실패 재시도 구현.
- Android 및 웹 뒤로 가기: 방/설정 → 메인 → 앱 백그라운드. 방 탈퇴 없음.
- Chrome 실제 브라우저/테스트 API22개 통과, 모바일 폭 썸네일/전송 버튼 배치 확인.
- All/Core 빌드22:41 KST 성공. Android 핵심105/알림22개 통과, 서버 Core91개 통과/실계정 opt-in5개 skip/실패0/157.250초. 배포 파일 크기·SHA256과 manifest 일치 확인. 새 웹 정적 파일 제공 검사 포함.
- 실제 전체 웹 시작 검사 오류0/작업 화면 표시 확인. 네이티브 compile에서 FileChooser enum 이름을 SDK의 ChromeFileChooserMode로 수정한 뒤 최종 빌드 성공.
- APK 서명/정렬/package183/기존 인증서 유지, Setup x64 GUI/payload17개/실행 버전1.15.17 검증. Setup에 새 APK 내장.
- APK: output/OnTechChat-v1.8.3-arm64.apk / 28,482,570 bytes / SHA256 b022d7e84abf0bab8ab1ace95651f54231ef816b2f3b4a361d8da067b298eaf5.
- Setup: output/OnTechChat-Server-Setup-v1.15.17-x64.exe / 39,840,768 bytes / SHA256 d6ddb25b0b7a69e4277deab7f00553fd60c579da8099f69bb8fcaccf3a533602.
- 서버 UI와 앱을 모두 업데이트해야 전체 기능 적용. 운영 서비스 설치·재시작 없음. 실기기/에뮬레이터 실행 미검증.
- 변경: web composer.js/css/navigation.js/index/app/messenger, server app/ai_media_test, Android Navigation/MainActivity/Attachments/ChatWeb/manifest, 버전 및 문서. DB/환경변수 변경 없음.
- 상세: docs/server/RELEASE-1.15.17.md. 브라우저 검증/스크린샷은 .build/verify-composer.mjs 및 composer-preview.png.

## 이전 완료: 1.15.16 / Android1.8.2
- 앱1.8.1(181) → 1.8.2(182), 서버/Setup1.15.15 → 1.15.16. All/Core 빌드22:02 KST 완료, 진행 중 빌드 없음.
- 사용자1.8.1 업데이트 후 실행 불가 제보. 실제 기기 종료 로그 미확보, 제보 원인은 미확정.
- 정상 웹 화면 로드8초 후 자동 확인, 조회8초 제한, 업데이트 오류는 채팅과 분리.
- pending 설치까지 동일 작업 잠금, 설치 취소/권한 거부/설치창 진입 버전 기억, 수동 재시도와 다음 버전 허용.
- APK 해시/패키지 검증을 백그라운드로 이동. 불필요한 시작 상태 재저장 제거, 웹 생성 콜백 예외 처리 보완.
- Android 핵심105개/알림22개 통과. 신규4개 회귀에 시작 전 차단·중복 호출·오류 해제·버전 비교·취소 보관·수동 재시도·계정 경계 포함.
- 서버 Core91개 통과/실패0/실계정 opt-in5개 skip,157.291초. Setup x64 GUI, payload16개, 실행 버전1.15.16, APK182/서명/정렬 검증.
- Chrome+테스트 API에서 전체 웹 UI 시작 오류0/채팅 화면 표시 확인(.build/verify-startup.mjs). 실제 계정/운영 데이터 사용 없음.
- 연결된 Android 기기 및 설치된 에뮬레이터가 없어 네이티브 실행·업그레이드·제보 재현은 미검증. 기존 Firebase deprecated 경고 유지.
- APK: output/OnTechChat-v1.8.2-arm64.apk / 28,482,570 bytes / SHA256 78e35734b279a47a7525cbc16dab7ea100eca262dd36f003327698ab843b4806.
- Setup: output/OnTechChat-Server-Setup-v1.15.16-x64.exe / 39,827,968 bytes / SHA256 454177d666ce372d05ac65760e898da68b4da88eda57e4d66cfb5f8744c1c462.
- 실제 파일/manifest 일치. APK 기존 서명 유지, Setup에 최신 APK 포함. 운영 서버/개인 인증/사용자 기기는 설치·재시작하지 않음.
- 복구: 앱을 삭제하지 말고1.8.2 APK 직접 덮어 설치. 상세 변경/제한: docs/server/RELEASE-1.15.16.md.
- 변경: Android AppUpdates/MainActivity/ChatWeb/Core UpdateCheckGate/LocalState, 핵심 검사, 버전 및 배포 문서. DB 마이그레이션 없음.

## 이전 완료: 1.15.15 / Android1.8.1
- 서버1.15.14 → 1.15.15, Android1.8.0(180) → 1.8.1(181). 최종 All/Core 빌드21:03 KST 완료, 진행 중 빌드 없음.
- AI 파일/이미지 첨부와 Codex 실제 이미지 생성/수정 반환을 연결했습니다. PDF/Office 자동 해석 및 Claude CLI 이미지 입력은 미지원입니다.
- 채팅방별 AI 도움 체크 → 최근50개 자동 분석/요약·할 일·초안·근거 확인. 별도 범위 승인이나 자동 답장 전송 없음.
- 위치 허용 하나로1분 기록/48시간 보관/기존 공유 범위 적용. 최근1/2/6/12/24/48시간 지도 조회, 기본1시간, 별도 현재 위치 확인 버튼 제거.
- Android WebView에서 공식 OSM HTTPS 타일 GET 하위 요청만 허용합니다. 임의 외부 주소·페이지 이동·POST·잘못된 좌표는 차단합니다.
- POST_NOTIFICATIONS 거부가 기록을 막지 않도록 수정. GPS 이동 기록 채널만 숨기는 Android 설정 바로가기 제공. 앱이 사용자 채널 설정을 임의로 끄지는 않으며 시스템 실행 중인 앱 표시는 유지됩니다.
- 앱 열기 → 업데이트 확인/자동 다운로드/패키지·버전·크기·SHA256·서명 확인 → OS 설치 화면. 최초1.8.1 적용은 기존 업데이트 메뉴/직접 APK 설치가 필요합니다.
- 추가 요청: 친구·채팅·설정 하단 메뉴의 중복 safe-area 여백/강제 최소 높이를 제거하고 내부 여백 축소. Android 시스템 내비게이션 영역 유지. 앱 표시 버전/User-Agent도1.8.1로 갱신.
- 최종 Core91개 통과, 실패0/실계정 opt-in5개 skip, 서버153.523초. Android 핵심101개/알림22개 통과.
- 직전 동일 AI 코드의 opt-in 실제 GPT-6-Astra 이미지 수정 검사도 통과29.61초, 실제 생성 이미지1개 검증·회수. 해당 Core92개/skip4개/183.901초 기록은 .build/logs/v1.15.15-image-live-core.log에 보존.
- GPS UI/자동 AI 도움 모의 DOM·API 검사 및 JavaScript 구문 검사 통과. APK 서명/패키지/181/정렬, Setup x64 GUI/payload15개/실행 버전1.15.15 검증.
- APK: output/OnTechChat-v1.8.1-arm64.apk / 28,412,938 bytes / SHA256 b8a196fe11509f71f7de742a517dc83983ccec48256540193b1360cb4c9fad68.
- Setup: output/OnTechChat-Server-Setup-v1.15.15-x64.exe / 39,758,848 bytes / SHA256 b3bcb1e21d9c4ddddef9305a454041cbccd7ef4398062692221c85d653aacacc.
- 실제 파일과 output/manifest.json 일치. 최신 APK가 Setup 내부에 포함됨. 운영 서버/개인 데이터/다른 PC 설치·재시작은 수행하지 않음.
- 실제 휴대전화의 메뉴 여백·지도 렌더링·GPS 이동/화면꺼짐/제조사 절전·알림 숨김·자동 업데이트 설치 및 다른 PC 서비스 계정 호출은 미검증. 기존 Firebase deprecated 컴파일 경고 유지.
- 주요 신규 파일: server/ai_media.go 및 검사, conversation_auto.go 및 검사, web/conversation-auto.js, location-map.js, vendor/leaflet*. 기존 AI 호출/첨부/삭제, 위치 API/UI, Android 위치·업데이트·URL 정책, 버전/배포 문서 수정.
- 상세 사용법·제한: docs/server/RELEASE-1.15.15.md, LOCATION-HISTORY.md, CONVERSATION-HELP.md. DB 마이그레이션/비밀값 추가 없음.
- 이전 릴리스 내역은 아래에 보존합니다.

## 이전 완료: 1.15.14 / Android1.8.0
- 최신 사용자 지정: 60초 간격 측정, 측정 시각 기준 48시간 보관. 명시적 시작·중지·삭제, 기본 꺼짐, 친구 경로 공유 별도 동의.
- 서버1.15.13 → 1.15.14 / Android1.7.0(170) → 1.8.0(180). 최종 문서 포함 빌드 완료19:09 KST, 진행 중 빌드 없음.
- 로컬 암호화 큐/재전송/중복방지, 서버 계정·기기·세션·revision 검사, 만료 정리 및 만료 GPS를 포함한 이전 백업 제거.
- 시간 범위 경로도·시각·정확도·외부 지도 지점 열기. 3분 초과 공백/다른 기기/날짜변경선 구간 연결 금지. 별도 지도 타일 배경은 없음.
- 기기 재등록 시 설정 보존. 로그아웃/기기해제 후 친구 조회 차단, 이미 전송된 본인 기록은 48시간 보존. 삭제 후 이전 큐 업로드 차단.
- Android 핵심89개/알림22개 검사 통과. ARM64 빌드·서명·패키지·versionCode 검증 및 Setup 자동포함 완료.
- 최종 Core84개 통과(150.127초), 실패0/실제 계정 연동용4개 skip. GPS 검사4종에 권한·범위·재전송·만료·삭제·기기해제·시각오차·백업정리 포함.
- UI 모의검사·JavaScript 구문 통과. 실제 휴대전화 GPS 이동/화면꺼짐/재부팅/배터리/48시간 장기운영/타PC 설치는 미검증.
- 첫 Android 컴파일의 취소 예외 타입 모호성을 System.OperationCanceledException으로 수정 후 재빌드 통과. 기존 Firebase deprecated 경고는 유지.
- APK: output/OnTechChat-v1.8.0-arm64.apk / 28,478,474 bytes / SHA256 1eb12cae548428d78137c2800367ea8a4aa17882bc8d6c7a2123eae39ff10396.
- Setup: output/OnTechChat-Server-Setup-v1.15.14-x64.exe / 39,542,784 bytes / SHA256 50646d2498c35ce5a606411123496d9e5df1ab2305dc66c77cd26c64234779a1.
- 실제 파일과 output/manifest.json 일치, payload14개 및 --version1.15.14 확인. 기존 운영 서비스/개인 데이터/타PC에는 설치·재시작하지 않음.
- 상세 사용법/API/파일/제한: docs/server/LOCATION-HISTORY.md. 로그: .build/logs/server-core-test.log, Tests-test.log, NotificationPolicy.Tests-test.log, android-arm64-build.log, .build/verify-location-history.mjs.

## 이전 선택 대화 AI 도움 및 입력창 여백 1.15.13 완료
- 1.15.12 → 1.15.13 최종 설치기 생성 완료 (18:09 KST). .build/task.json completed, 진행 중인 빌드 없음.
- 신규 흐름: 친구/참여 대화 → 최근N/날짜범위 미리보기 승인 → 요약/할일/초안/질문 → 근거 원문 이동/초안 복사·입력. 자동전송 없음.
- 세션 사용자/참여권한을 매 조회·분석 전후·결과 사용 시 검사. 최고관리자도 미참여 대화 조회 불가. 원문 삭제/본인 비우기/권한 회수/만료 시 결과 폐기.
- 분석 전용 호출은 웹/파일/실행/MCP 도구를 제공하지 않음. 일반AI 웹검색·기존 도구는 유지. 메시지 속 명령으로 도구를 실행하지 않음.
- 최대200개/96KiB·UTF-8 분할·IANA 시간대·근거 ID 지원. 사용자별 임시 결과 최대15분 메모리 보관, 원문/결과 추가 영속저장 없음. DB/환경변수 변경 없음.
- Android 네이티브 inset의 웹 중복 적용 제거, composer 하단 여백 축소. Android APK1.7.0(170) 유지.
- Full 회귀 통과: 서버184.973초, 트레이0.476초, 설치기12.770초. 이후 UI 경합/한글 질문 한도 보완 후 최종 Core 80개 최상위 검사 통과(147.418초), 실패0, 실제 계정 연동용4개 skip.
- 신규 서버 검사5종: 접근권한/선택범위·도구, 삭제/비우기/권한회수, 처리중삭제/빈대화/AI오류, Unicode·CLI도구제한, 분할분석/만료 통과.
- UI 모의 DOM/API 및 JavaScript 구문 검사 통과. 404즉시폐기, 복사·삽입 전 검증, 이전 검사/늦은 분석응답의 결과 복원 방지 회귀 포함.
- 배포: output/OnTechChat-Server-Setup-v1.15.13-x64.exe / 39,485,440 bytes. 내부 payload13개·실행 버전 검증 완료.
- SHA256: 39735042c142ff9fe5f48160a08f59c8424955c9864b2053ccdd37446e82e04e (실제 파일과 manifest.json 일치).
- 변경 파일·API·사용법·제한: docs/server/CONVERSATION-HELP.md 및 API.md. 검증 로그: .build/logs/server-test.log, server-core-test.log, .build/verify-conversation-help.mjs.
- 운영 서비스/타PC 설치는 수행하지 않음. 실제 Android/모바일 브라우저 렌더링과 이번 선택대화 기능의 실제 AI 계정 응답은 미검증. AI 흐름은 모의 실행기와 CLI 전달인자로 검증함.

## 이전 1.15.12 완료 기록
- 사용자 보고: 다른 설치 PC에서 1.15.11 AI 대화 가능 확인 (2026-09-30 16:01 KST).
- 1.15.12 최종 Full 검사/설치기 빌드 완료 (2026-09-30 16:32 KST). 진행 중인 빌드 없음.
- Codex GPT-6-Astra 실제 web_search search/open_page/출처 응답 및 structured planner 검증 통과. ONTECH_TEST_CODEX_WEB=1로 최종 Full 검사에서도 재확인.
- 서버 전체 199.091초, 트레이 캐시 통과, 설치기 12.454초. payload 12개/실행 버전 1.15.12 검증 통과.
- RemoteControl 실제 설치 엔진을 격리 HTTP fixture에 연결해 initialize/tools/list/tools/call 통과. 역할 회수/일반 사용자 차단/비밀번호 암호화·조회 비노출/다른 PC 경로/프로토콜·페이지 처리 회귀 통과.
- JavaScript 구문 및 출처 링크 안전 렌더링 검사 통과. AI 상단 선택기 제거 및 사용자별 대화 삭제 회귀 통과.
- 배포: output/OnTechChat-Server-Setup-v1.15.12-x64.exe / 38,944,256 bytes.
- SHA256: 3586d6d6a20a9b684f79e1c2e8d4d8366e932bf2b3334e32180936e897524136 (manifest와 실제 파일 일치).
- 기록: .build/logs/server-test.log, v1.15.12-mcp-native.log, v1.15.12-web-diagnostic.jsonl.
- 운영 서비스/타 PC 설치/실제 RemoteControl 대상 연결은 변경하지 않음. MCP 연결은 설치 PC의 서버 설정 → AI 대화 → RemoteControl MCP에서 대상 Remote ID/PW를 등록하고 연결 확인 후 저장 후 적용.
- 실제 모델 검색 검증은 Codex에서 수행. Claude는 WebSearch/WebFetch 인자/지침 회귀 검증이며 실제 계정 호출 검증은 별도.
- 검색 초기 시험에서 code_mode_host 비활성 오류를 검출해 수정. 네이티브 셸/앱/개인 플러그인은 제한하고 검색 실행 호스트는 활성화. 설치기는 기존 allowlist로 해당 helper EXE도 공용 배치.
- 1차 통합 Full 검사의 유일한 실패는 실제 웹검색 검증이었으며, 나머지 대화 삭제/MCP 역할·비밀 저장·경로/프로토콜 검사는 통과.

## 이전 1.15.11 결과
- 1.15.11 Full 검증 및 설치기 빌드 완료 (2026-09-30 15:47 KST). 진행 중인 빌드 없음. 기존 운영 서비스/개인 인증은 변경하지 않음.
- 공용 실행 엔진 복사, 서비스 SID 전용 인증 홈, 설치 후 로그인 및 트레이 재로그인, 공용 경로 우선 처리 추가.

## 이전 1.15.10 상태
- 현재 1.15.10 Full 회귀/설치기 빌드 완료 (2026-09-30 14:58 KST). 진행 중인 빌드 없음. 이전 완료 결과는 아래에 보존.
- 변경: 트레이 Windows 서비스 로그온 안내, 업데이트 계정 보존, 현재 토큰 SID의 CLI 홈 환경. 운영 계정 변경/암호 수집 없음.
## 현재 작업
- 공식 루트: D:\OnMakeSrc\OnTechChat.
- 서버/설치기/트레이/웹 표시: 1.15.10. Android: 1.7.0 / versionCode 170 / ARM64.
- Full 회귀: 서버 180.348초 / 트레이 0.461초 / 설치기 9.920초 통과. 내부 payload 12개, 버전/x64 GUI 검사 및 배포 해시 일치.
- 운영 PC 설치/재시작은 수행하지 않았다.

## 수정 내용
- Codex의 tool_isolation_unavailable 일괄 차단을 제거하고 실제 비대화형 CLI 실행/JSONL 완료 응답 처리를 구현.
- GPT-6-Astra 모델/추론 값을 CLI에 전달. 모델/API로 임의 대체하지 않음.
- Claude/Codex 모두 같은 AI 채팅에서 일반 답변/구조화 도구 작업 자동 선택.
- 임시 CWD, 사용자 설정/규칙 제외, 읽기 전용, 승인 금지, 네이티브 도구 제한 적용.
- 작업 도구의 기존 소유권/역할/등록 프로그램/AppContainer 검사는 유지.
- 현재 설치 PC에서 CLI 경로 재탐색. npm codex.cmd는 네이티브 codex.exe로 해석.
- 설정/실행 파일 확인과 실제 인증/모델 호출 검사를 구분. CLI 로그인은 서버 실행 계정 기준.
- 타임아웃/취소 시 해당 CLI 프로세스 트리를 종료하고 출력 파이프 대기에 제한 적용.
- 본인 AI 대화 전체 비우기와 질문/답변 개별 삭제 추가. 전체 삭제 시 요청 이력/작업 내용/대기 알림 제거.
- 진행 중 요청 취소, 늦은 완료 응답 저장 차단, 다른 기기의 과거 메시지 병합 방지, 삭제된 작업 폴링 종료.
- 대화 삭제는 작업공간/기존 산출물 파일 삭제가 아님.
- AGENTS.md 및 기존 두 개발 프롬프트의 Codex 일괄 차단 지침 수정.
- 배포 안내: docs/server/AI-CLI-AND-CHAT.md.

## 검증
- 실제 Codex CLI 0.158.0 / GPT-6-Astra / low / structured planner: OK, 도구 요청 0개.
- 실제 호출 증거: .build/logs/v1.15.8-codex-live-test.log.
- 1차 Core 회귀 통과: AI, Agent, 사용자 권한, Windows AppContainer, 위치.
- JavaScript 문법 검사 통과.
- 다른 설치 PC 및 해당 Windows 서비스 계정 로그인은 직접 접속하지 않아 실검증하지 않음.
- Android 실기기 UI/UAC 업데이트/재부팅은 미검증. 기존 서버 WebView UI로 제공하며 APK 1.7.0 유지.
- CLI 플래그만으로 서버 계정 파일에 대한 완전한 사용자별 읽기 격리를 보장한다고 주장하지 않음.
- 최초 진단 테스트는 장시간 AppContainer 검사 중 중단되었으며 이후 전체 Core 회귀와 실제 CLI 검사는 통과했다.

## 빌드 규칙
- 유일한 진입점 build.ps1, 잠금 .build/project.lock 유지. archive는 수정하지 않음.
- output에는 종류별 최신 배포 파일 하나만 유지. manifest.json/SHA256SUMS.txt는 빌드가 생성.


## 이전 1.15.8 배포 기록
- 파일: output/OnTechChat-Server-Setup-v1.15.8-x64.exe
- 크기: 38,715,392 bytes
- SHA256: 387463d50bb820ea25a8d2a666e68f6abb41fa2d26ce22109ead320f3ba237b1
- manifest.json 및 SHA256SUMS.txt와 실제 파일 해시 일치.
- x64 Windows GUI 설치기, 내부 payload 12개 검증 및 --version 1.15.8 확인.
- 서버 Core 62개 최상위 검사 통과 (141.480초). GPT-6-Astra 실제 호출은 앞선 별도 기록의 성공 결과 사용, 반복 유료 호출하지 않음.
- Codex 자동 작업 라우팅/다른 PC 경로 재탐색/npm 네이티브 실행/삭제 권한/늦은 응답 저장 차단 검사 통과.
- JavaScript 문법 검사 통과. 배포 설치 안내의 버전과 CLI/삭제 안내가 payload에 포함됨을 확인.
- 기존 Android 1.7.0 APK(28,470,282 bytes, SHA256 362d3fa42b2fd2bad19baad251dfc06b7b33e012887281efaba708a5e6ef1f2b) 포함.
- 대상 다른 PC 설치 및 서비스 계정 인증 검증은 사용자가 업데이트 후 실제 호출 검사로 확인할 항목.

## 1.15.9 완료 — 다른 설치 PC의 CLI 검색
- 사용자 제공 진단: 개인 사용자 PowerShell에서는 Codex 실행 성공, OnTechChatServer는 LocalService로 실행 중.
- 해당 PC에 원격 접속한 것이 아니며, 개발 PC의 성공을 해당 PC 성공으로 간주하지 않음.
- 개인 계정 하드코딩 없이 네이티브 UTF-16 ProfileList/사용자 Environment 조회로 변경.
- 경로 직접 입력 및 실제 서버 실행 계정 표시, 경로 접근 권한 거부 구분 추가.
- 사용자 프로필/인증 파일/운영 서비스 설정은 변경하지 않음.
- 검증 및 1.15.9 빌드 완료. 타 PC의 실제 서비스 권한/CLI 인증은 원격 미검증.

## 최종 1.15.9 배포 결과
- output/OnTechChat-Server-Setup-v1.15.9-x64.exe
- 38,727,168 bytes
- SHA256 e73dadd7f851cc20e93359582d6cba7366af2d2d230373269104190cfe26859f
- 실제 파일과 manifest 일치, x64 GUI 및 내부 payload 12개 검증, --version 1.15.9 확인.
- Core 66개 검사 통과 (142.763초), 한글/공백/유니코드 경로, 네이티브 ProfileList, 명시 경로 우선 및 진단 정보 검사 포함.
- 서버 실행 환경이 개인 PowerShell과 다른 경우에도 계정명을 제품 코드에 고정하지 않는다.
- 테스트의 서비스 환경 모사는 환경 변수 차이 검증이며, 대상 다른 PC LocalService 토큰으로 실제 실행한 검증이 아니다.
- 운영 PC 서비스 계정/암호/사용자 프로필 ACL/인증 파일은 변경하지 않았으며, 개인 계정 로그인과 LocalService 인증 분리는 유지된다.

## 1.15.10 빌드 중 발견한 기존 검사 불일치
- 첫 Full 검사에서 TestMessengerDirectoryAndProfile만 실패. 공용 fixture는 superadmin인데 검사만 예전 문자열 admin을 기대하고 있었다.
- 디렉터리가 fixture 계정의 실제 Role을 그대로 반환하는지 검사하도록 수정. 제품 권한 로직은 변경하지 않음.
- Full 재검사 및 서버/트레이/설치기 검증 진행.

## 1.15.10 완료 — CLI 실행 계정 설정
- 파일: output/OnTechChat-Server-Setup-v1.15.10-x64.exe
- 크기: 38,733,312 bytes
- SHA256: b8451344704824ef326dd5b78cf5df43c7a2fc0a10b5daeba71f1cddb93b1453
- manifest.json/SHA256SUMS.txt와 실제 파일 해시 일치. 기존 APK 1.7.0 유지/포함.
- Full 서버·트레이·설치기 검사 모두 통과. JavaScript 문법 검사 통과.
- 회귀 추가: 업데이트 계정 보존/새 설치 기본값, SID 정확히 일치하는 프로필만 사용, 한글·다른 드라이브 CLI 홈 환경.
- 트레이 AI CLI 실행 계정 설정 메뉴는 안내 후 Windows 서비스 MMC를 연다. 자동 계정 전환/암호 수집을 하지 않는다.
- 업데이트 시 기존 서비스 obj/password를 재설정하지 않는다. CLI 자식 환경은 실행 토큰 계정의 HOME/USERPROFILE/AppData를 사용한다.
- 실제 대상 PC에서 설치 후 서비스 로그온 계정을 Codex 사용 Windows 계정으로 지정하고 해당 서비스를 다시 시작해야 한다.
- 이 방식은 서버 전체가 선택 계정 권한으로 실행됨을 안내한다. 계정명 하드코딩/인증 복사/개인 프로필 ACL 확대 없음.
- 대상 다른 PC의 UAC/로그온 변경/서비스 시작/GPT-6-Astra 응답은 직접 검증하지 않았다. 개발 PC 운영 서비스도 변경하지 않았다.

## 1.15.11 완료 — 공용 Codex 설치 및 서버용 인증
- 파일: output/OnTechChat-Server-Setup-v1.15.11-x64.exe
- 크기: 38,878,208 bytes
- SHA256: fe046e3f5ec126cec27874a9c7df7b9576895313097ffb8bda1ea8368b3c4b40
- manifest.json/SHA256SUMS.txt와 실제 파일 해시 일치. x64 GUI 및 payload 12개/--version 검사 통과.
- 서버 Full 179.008초 / 트레이 0.461초 통과. 설치기 최초 검사에서 아래 2건 발견 후 수정, 최종 12.541초 통과. 최종 서버/트레이는 동일 코드의 성공 캐시 사용.
- Codex 표준 bin 경로는 정션이었다. 열린 파일 핸들의 GetFinalPathNameByHandleW로 실제 로컬 엔진을 해석하고 복사한다. 목적지 링크/정션은 차단한다.
- ACL 회귀 검사에서 비관리자 테스트 실행자에게 쓰기 권한이 없는 임시 파일 정리 문제가 있었다. 검사 후 소유자 DACL을 복원하도록 수정. 최초 실패의 임시 파일도 정리 완료.
- 공용 엔진은 Program Files 제품 하위. 관리자만 수정, Windows Users 및 제품 서비스 SID는 읽기/실행만 허용.
- 인증 홈은 ProgramData 제품 하위 ai-cli/codex. 관리자/SYSTEM/제품 서비스 SID만 접근. 개인 인증/설정 파일은 복사하지 않는다.
- 설치 PC의 CLI 실행 엔진을 준비한다. 개발 PC CLI/인증은 설치 payload에 포함하지 않는다.
- 설치 완료 후 서버용 Codex 로그인 안내를 제공. 트레이 서버용 Codex 로그인 메뉴에서 재실행 가능.
- 서버용 로그인과 실제 Codex 실행에 동일한 CODEX_HOME 및 file credential store 적용. 공용 엔진은 저장된 개인 CLI 경로보다 우선.
- 개인 CODEX_HOME/API key/access token이 공용 실행 환경에 섞이지 않는 검사, 엔진만 복사하는 검사, 실제 Windows DACL 검사 통과.
- 실제 설치된 Codex 엔진을 임시 공용 경로로 복사한 --version 및 빈 서버 인증 홈 login status 검사 통과. 개인 로그인 재사용 없이 미로그인으로 판정.
- JavaScript 문법 검사 통과. Android 1.7.0 APK/대화 삭제/역할·소유자·작업공간/AppContainer 검사는 유지.
- 실제 대상 다른 PC의 UAC 설치/사용자 브라우저 로그인/GPT-6-Astra 응답은 원격 미검증. 이 개발 PC 운영 서비스와 개인 인증도 변경하지 않았다.
- 적용: 해당 PC에서 1.15.11 설치 → 서버용 Codex 로그인 1회 → AI 실제 호출. Windows 계정/암호 변경은 기본 흐름에 필요하지 않다.

## GPS 이동 기록 작업 착수 기록 — 상단 완료 결과 참조
- 사용자 최신 지정: 1분 측정, 로컬 미전송/서버 기록 모두 2일(48시간) 보관.
- 본인 기기 명시적 시작/중지, 별도 친구 경로 공유 동의. 기존 1회 위치조회 유지.
- 서버1.15.14/Android1.8.0(180) 예정. 원격 운영 배포 없이 공식 output에 빌드 예정.

## 2026-09-30 20:18 KST 작업 중 — 1.15.15
- AI 이미지/파일 첨부, 대화방별 자동 AI 도움 체크, GPS 최근1/2/6/12/24/48시간 조회(기본1시간) 구현 중.
- 소스 수정 중이며 빌드/운영 배포는 아직 하지 않음. 기존 APK1.8.0 유지 예정.

## GitHub 소스 관리 — 2026-10-09
- 비공개 저장소: https://github.com/OnTechnologyCo/OnTechChat
- 공식 로컬 경로 D:\OnMakeSrc\OnTechChat에 Git을 초기화하고 origin/main으로 연결한다.
- 소스 기준: Windows 서버 1.15.27 / Android 1.8.4(184). 기존 로그인 유지 수정이 포함된 상태를 등록한다.
- 소스, 웹 리소스, 테스트, 빌드 스크립트 및 문서를 버전 관리한다.
- 기존 .gitignore의 .build/output/archive/tools/bin/obj 및 서명키 제외를 유지한다. 공식 build.ps1이 재생성하는 installer/payload.zip도 제외한다.
- GitHub 등록 범위는 소스 버전 관리 설정과 업로드다. 앱 기능 변경, 추가 빌드, 설치 및 운영 서비스 재시작은 포함하지 않는다.
