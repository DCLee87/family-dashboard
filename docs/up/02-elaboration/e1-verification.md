# E1 검증 기록

- 구현 기준일: 2026-07-24
- 로컬 환경: macOS
- NAS 환경: DS216+II, 검증 전

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
| `docker compose build` | 미실행 | 현재 Mac에 Docker 없음 |
| endpoint 수동 확인 | 통과 | health, runtime, 정적 HTML 응답 확인 |

## NAS 실측

아래 항목은 DS216+II에서 직접 측정하기 전까지 완료하지 않는다.

```text
이미지 크기:
시작 시간:
유휴 메모리:
정상 요청 메모리:
최대 메모리:
평균·최대 CPU:
상태 API 응답시간:
재시작 복구시간:
SQLite 무결성:
24시간 메모리 변화:
```

## 남은 검증

- [ ] Docker 이미지 및 Compose 구문 검증
- [ ] 컨테이너 재생성 후 SQLite 영속성
- [ ] 정상·강제 종료 후 DB 무결성
- [ ] NAS 재부팅 후 자동 복구
- [ ] CPU·메모리·응답시간 측정
- [ ] 24시간 메모리 증가 확인
