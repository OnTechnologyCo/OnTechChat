# 서버 1.15.33 / Android 1.8.10(190): 네이버 지도

서버 Setup 1.15.33과 Android 1.8.10(190)을 함께 업데이트하세요. Setup에는 같은 버전의 공식 ARM64 APK가 포함됩니다. 빌드·서명·배포 해시와 실제 검증 범위는 docs/WORK_STATUS.md 및 output/manifest.json을 확인하세요.

## 변경

- GPS 이동 경로의 기본 배경을 NAVER Dynamic Map으로 변경합니다. 화면을 열 때 SDK를 한 번 로드하며 공개 Client ID는 server/web/location-naver.js의 historyNaverKey 한 곳에서 관리합니다. Client Secret은 사용하지 않습니다.
- 경로 테두리, 시작/마지막 지점, 정확도 원, 측정 시각, 전체 경로/마지막 위치, 자동 조회 시 카메라 복원을 지원합니다.
- 기존 1/2/6/12/24/48시간 선택, 기본 최근 1시간, 15초 공유 권한 재검증 및 화면 닫기/숨김 정리는 변경하지 않습니다.
- SDK/인증/초기화 실패 시 이유를 표시하고 기존 OpenFreeMap/Leaflet 대체 지도를 사용합니다. 화면을 닫은 뒤 늦게 도착하는 SDK/인증 이벤트는 지도를 되살리지 않습니다.
- WebView User-Agent의 고정 1.8.7 표기를 실제 설치 패키지 버전으로 변경해 지도 지원 버전을 올바르게 판정합니다.
- Android 1.8.9 이하에서는 앱 업데이트 안내와 기존 지도를 표시합니다. 서버와 APK를 함께 업데이트해야 네이버 리소스를 앱에서 로드할 수 있습니다.
- Android는 HTTPS GET 하위 리소스에 한해 고정 NAVER SDK/인증 URL, 지도 스타일/타일/아이콘 경로를 허용합니다. 외부 탐색/POST/자격증명 URL/임의 호스트는 차단합니다.
- CSP는 공식 SDK·인증 및 고정 지도 스타일 JSONP 파일만 외부 스크립트로 허용합니다. 네이버 지도의 동적 배치에 필요한 style-src-attr만 unsafe-inline을 허용하며, 인라인 스크립트·인라인 style 요소·eval·와일드카드는 허용하지 않습니다. SDK 오류 수집 POST는 차단을 유지합니다. 채팅 페이지의 외부 Referer는 원점까지만 전달합니다. SDK는 채팅 페이지 안에서 실행되는 외부 코드입니다.
- 네이버 로고·지도 데이터 저작권 표시는 유지합니다. 기존 대체 지도 라이선스도 보존합니다.

## NAVER Cloud 입력

2026-10-18 사용자 화면에서 Client ID와 등록 URL http://chat.ontechdev.com:8787을 확인했습니다. 이 URL로 수동 인증 요청을 보내 HTTP 200/result 성공을 확인했습니다. IP 직접 접속은 별도 IP 등록이 필요합니다. 공식 가이드는 등록 URL에서 포트·경로를 제외하도록 안내하지만, 현재 등록값으로 인증은 성공했으므로 임의로 콘솔 설정을 변경하지 않았습니다.

이 앱은 Android 네이티브 지도 SDK가 아닌 HTTPS WebView의 JavaScript 지도를 사용하므로 이 연동에는 Android 패키지명이나 iOS Bundle ID 대신 Web 서비스 URL을 사용합니다. 기존 HTTPS 접속·인증서를 유지해야 하며, NAVER URL 등록만으로 HTTP 서버가 HTTPS가 되지는 않습니다. 기본 내부 HTTP 8787 포트와 외부 HTTPS 주소를 혼동하지 마세요.

Cloudflare 도메인은 API 전환의 필수 조건이 아닙니다. DNS/프록시/방화벽/TLS는 이번 변경에서 수정하지 않았습니다.

## 검증

- 공식 빌드 진입점은 build.ps1 -Target All -Validation Core입니다. Android URL 허용·차단 검사, 서버 CSP/정적 리소스 검사와 Node.js 지도 생명주기 검사 10개를 포함합니다.
- 실제 SDK 검사에서 초기 콜백이 전역 객체 등록보다 먼저 실행되는 경우를 확인하여 microtask 이후 준비 완료를 판정합니다. JSONP 스타일 메타데이터와 지도 커서의 Android 허용 경로를 보완했습니다.
- SDK 모의 검사와 실제 지도 렌더링, 실기기 설치·GPS 검증은 서로 구분합니다. 실행 결과는 docs/WORK_STATUS.md에 기록합니다.
- 네이버 인증은 등록 원점 http://chat.ontechdev.com:8787/에서 HTTP 200/result 성공을 확인했습니다. 다른 접속 원점은 해당 URL을 NAVER Cloud에 등록해야 합니다.
- 운영 서버 설치, DNS/프록시/TLS 변경 및 실제 휴대전화 설치는 이 소스 변경과 별개입니다.

## 공식 참고 문서

- [NAVER Maps SDK 시작하기](https://navermaps.github.io/maps.js.ncp/docs/tutorial-2-Getting-Started.html): ncpKeyId와 callback.
- [Application 등록](https://guide.ncloud-docs.com/docs/application-maps-app-vpc)
- [Maps 문제 해결](https://guide.ncloud-docs.com/docs/application-maps-troubleshoot): 서비스 URL에서 포트·경로 제외.
- [Map API](https://navermaps.github.io/maps.js.ncp/docs/naver.maps.Map.html): init, fitBounds, destroy.
