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
| 24시간 관찰 | 통과 | 2026-07-25 01:26~2026-07-26 01:26 KST, 60초 간격, 1,405개 샘플 |
| 비정상 프로세스 종료 | 통과 | 호스트에서 PID 1에 `SIGKILL`, 자동 재시작 후 `startCount` 4→5 |
| 비정상 종료 후 DB 무결성 | 통과 | 자동 복구 후 `--check-db` 성공 |
| NAS 재부팅 자동 복구 | 통과 | 부팅 후 `healthy`, `startCount` 5→6 |
| NAS 재부팅 후 DB 무결성 | 통과 | 자동 복구 후 `--check-db` 성공 |
| 상태 API 단기 부하 | 통과 | 동시 10개, 60초, 1,200/1,200 성공 |
| 단기 부하 CPU·메모리 | 통과 | 평균/최대 CPU 0.7323%/1.60%, 최대 메모리 11.060 MiB |

```text
이미지 크기: 10,403,137 bytes (NAS Docker 표시)
시작 시간: 약 2초
유휴 메모리: 3.789~5.781 MiB
정상 요청 메모리: 평균 10.492 MiB
최대 메모리: 11.060 MiB
평균·최대 CPU: 24시간 0.0483% / 1.71%, 단기 부하 0.7323% / 1.60%
상태 API 응답시간: 평균 11.455 ms, 최대 62.398 ms
재시작 복구시간: 약 2초
SQLite 무결성: 통과
24시간 메모리 변화: 5.781→7.258 MiB (+1.477 MiB), 평균 6.915 MiB, 최대 8.578 MiB
```

24시간 관찰은 손상된 행 없이 종료됐다. 관찰 전후 `startCount`가 3으로
유지되어 애플리케이션 재시작은 없었고, 종료 후 상태 API는
`{"database":"ok","status":"ok"}`를 반환했다. 측정값은 256 MB 목표보다
충분히 낮으며, 관찰 구간에서 지속적으로 증가하는 메모리 패턴은 확인되지
않았다.

비정상 종료 시험에서는 먼저 `docker compose kill`이 Docker의 수동 정지로
취급되어 `unless-stopped` 정책이 컨테이너를 자동 재시작하지 않는 것을
확인했다. 컨테이너를 다시 시작한 뒤 호스트에서 컨테이너 PID 1에
`SIGKILL`을 보내 실제 프로세스 장애를 모사했다. 이 경우 컨테이너가 자동
재시작됐고 상태 API와 SQLite 무결성 검사가 모두 통과했다.

단기 부하 시험은 Mac에서 `/api/health`와 `/api/runtime`에 동시 10개
클라이언트로 60초 동안 1,200회 요청했다. 모든 요청이 성공했고 컨테이너
재시작은 없었다. Docker 통계는 90초 동안 2초 간격으로 요청했으며 실제
수집 간격을 포함해 26개 샘플을 기록했다. E1에는 쓰기 API가 없으므로
SQLite 읽기·쓰기 혼합 부하는 도메인 쓰기 API가 추가되는 반복에서 수행한다.

## 남은 검증

- [x] DS216+II에서 사전 빌드한 linux/amd64 이미지 로드·실행
- [x] DS216+II에서 컨테이너 재생성 후 SQLite 영속성
- [x] DS216+II에서 정상 종료·재생성 후 DB 무결성
- [x] DS216+II에서 비정상 프로세스 종료 후 DB 무결성
- [x] NAS 재부팅 후 자동 복구
- [x] 초기 유휴 CPU·메모리·응답시간 측정
- [x] 24시간 메모리 증가 확인
- [x] 상태 API 단기 부하 CPU·메모리·응답시간 측정
