# E1 최소 실행 설계

## 목적

전체 가족 기능을 구현하기 전에 DS216+II에서 단일 컨테이너, Go, 정적 React 화면과 SQLite가 자원·재시작·영속성 기준을 충족하는지 검증한다.

## E1 범위

포함:

- Go HTTP 서버
- React 최소 PWA 화면
- 상태 확인 API
- SQLite 연결과 마이그레이션 1개
- 외부 데이터 볼륨
- 컨테이너 자동 재시작
- 구조화된 최소 로그
- CPU·메모리 측정

제외:

- 실제 PIN과 기기 등록
- 외부 접속
- Web Push
- 전체 일정·할 일·공지 기능
- 날씨 공급자 연동
- 완성된 디자인 시스템

## 논리 구조

```text
Browser / PWA
  ├─ GET /                 정적 React 화면
  ├─ GET /api/health       프로세스와 DB 상태
  └─ GET /api/runtime      버전·업타임, 민감정보 제외

Go process
  ├─ static file handler
  ├─ API handler
  ├─ SQLite connection
  ├─ migration runner
  └─ graceful shutdown

NAS volume
  ├─ /data/family-dashboard.db
  ├─ /config/
  └─ /backups/
```

## 저장소 구조 후보

```text
family-dashboard/
├─ cmd/server/
├─ internal/
│  ├─ app/
│  ├─ database/
│  └─ httpapi/
├─ migrations/
├─ web/
│  ├─ src/
│  └─ public/
├─ deploy/synology/
├─ docs/
├─ Dockerfile
├─ compose.yaml
├─ go.mod
└─ package metadata
```

## 상태 확인 계약 초안

`GET /api/health`

```json
{
  "status": "ok",
  "database": "ok"
}
```

원칙:

- 인증정보, 파일 경로, 내부 오류 스택과 환경변수는 응답하지 않는다.
- SQLite 확인 실패 시 HTTP 오류 상태와 일반화된 응답을 제공한다.
- Container Manager 상태 확인에서 사용할 수 있어야 한다.

## SQLite E1 검증

- 앱 시작 시 순서가 보장된 마이그레이션 실행
- WAL 사용 여부와 NAS 파일시스템 동작 확인
- 동시 읽기와 단일 쓰기 부하 시험
- 컨테이너 강제 종료 후 무결성 확인
- 컨테이너 재생성 후 데이터 유지 확인
- 백업 중 일관된 SQLite 스냅샷 생성 방식 검토

## 컨테이너 원칙

- 다단계 빌드
- 비루트 사용자
- 읽기 전용 루트 파일시스템을 우선 검토
- `/data`, `/config`, `/backups`만 외부 볼륨
- `restart: unless-stopped`
- 런타임 이미지에 빌드 도구 미포함
- CPU·메모리 제한값은 실측 후 compose에 확정

## 측정 시나리오

1. 컨테이너 시작 직후
2. 30분 유휴
3. 상태 API 연속 요청
4. SQLite 읽기·쓰기 혼합 요청
5. 컨테이너 정상 재시작
6. 컨테이너 강제 종료 후 재시작
7. NAS 재부팅 후 자동 복구
8. 24시간 유휴·간헐 요청

기록 항목:

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

## E1 인수 기준

- DS216+II에서 amd64 이미지가 실행됨
- 정상 사용 메모리 목표 256 MB 이내 또는 조정 근거 승인
- 상태 API와 최소 화면이 가정 내 네트워크에서 응답함
- 컨테이너 재생성 후 SQLite 데이터가 유지됨
- 정상·비정상 종료 후 DB 무결성이 유지됨
- NAS 재부팅 후 수동 개입 없이 서비스가 복구됨
- 24시간 시험에서 지속적인 메모리 증가가 없음
