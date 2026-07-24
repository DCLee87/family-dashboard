# E1 검증 기록

- 구현 기준일: 2026-07-24
- 로컬 환경: macOS
- NAS 환경: DS216+II, DSM 7.2.2-72806 Update 9

## 구현 상태

- [x] React 최소 PWA와 30초 HTTP polling
- [x] Go HTTP 서버와 정적 화면 포함 구조
- [x] `/api/health`, `/api/runtime`
- [x] SQLite 초기 스키마, WAL과 무결성 검사
- [x] 다단계 Dockerfile과 Compose 정의
- [x] 비루트·읽기 전용 루트 파일시스템 설정
- [x] Synology 기본 검증·모니터링 스크립트 작성

## Mac 로컬 검증

| 항목 | 결과 | 비고 |
|---|---|---|
| `npm install` | 통과 | 감사 결과 알려진 취약점 0건 |
| `npm run build` | 통과 | Vite production build |
| `go test ./...` | 통과 | 공식 Go 1.26.5 임시 도구체인 사용 |
| `go build ./cmd/server` | 통과 | macOS arm64 실행 파일 생성 |
| `docker compose config` | 통과 | Docker Compose 5.3.1 |
| 로컬 arm64 이미지 빌드·실행 | 통과 | Colima Docker Engine 사용 |
| 컨테이너 healthcheck | 통과 | `healthy` 상태 확인 |
| 정상 재시작 | 통과 | `startCount` 5로 증가 |
| 컨테이너 재생성 | 통과 | `startCount` 6→7, SQLite 데이터 유지 |
| SQLite 무결성 검사 | 통과 | 재생성 후 `--check-db` 성공 |
| linux/amd64 Go 교차 컴파일 | 통과 | 정적 x86-64 ELF 생성 |
| linux/amd64 전체 이미지 빌드 | 통과 | Buildx 네이티브 빌드 단계와 Go 교차 컴파일 |
| linux/amd64 이미지 실행 | 통과 | Mac 에뮬레이션에서 API·SQLite 무결성 확인 |
| NAS 전송 패키지 생성 | 통과 | 이미지 tar, Compose와 검증 스크립트 포함 |
| endpoint 수동 확인 | 통과 | health, runtime, 정적 HTML 응답 확인 |

## NAS 실측

2026-07-25 01:21 KST에 사전 빌드한 linux/amd64 이미지를 DS216+II에 로드해 시험 배포했다.

| 항목 | 결과 | 비고 |
|---|---|---|
| linux/amd64 이미지 로드·실행 | 통과 | Container Manager Docker 24.0.2 |
| 상태 API와 웹 화면 | 통과 | 내부망 `8080`, 인증·HTTPS 없음 |
| 정상 재시작 | 통과 | `startCount` 1→2 |
| 컨테이너 강제 재생성 | 통과 | `startCount` 2→3, 데이터 유지 |
| SQLite 무결성 검사 | 통과 | 재생성 후 `--check-db` 성공 |
| 유휴 CPU·메모리 | 통과 | 초기 0.00%, 3.789~5.781 MiB |
| 24시간 관찰 | 진행 중 | 2026-07-25 01:26 KST 시작, 60초 간격 |

```text
이미지 크기: 10,403,137 bytes (NAS Docker 표시)
시작 시간: 약 2초
유휴 메모리: 3.789~5.781 MiB
정상 요청 메모리:
최대 메모리:
평균·최대 CPU: 첫 측정 0.00%
상태 API 응답시간: 내부망 요청 1초 미만
재시작 복구시간: 약 2초
SQLite 무결성: 통과
24시간 메모리 변화:
```

## 남은 검증

- [x] DS216+II에서 사전 빌드한 linux/amd64 이미지 로드·실행
- [x] DS216+II에서 컨테이너 재생성 후 SQLite 영속성
- [x] DS216+II에서 정상 종료·재생성 후 DB 무결성
- [ ] DS216+II에서 비정상 프로세스 종료 후 DB 무결성
- [ ] NAS 재부팅 후 자동 복구
- [x] 초기 유휴 CPU·메모리·응답시간 측정
- [ ] 24시간 메모리 증가 확인
