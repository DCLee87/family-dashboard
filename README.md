# Family Dashboard

[![CI](https://github.com/DCLee87/family-dashboard/actions/workflows/ci.yml/badge.svg)](https://github.com/DCLee87/family-dashboard/actions/workflows/ci.yml)

가족 구성원이 일정, 할 일, 공지, 날씨와 위치 상태를 안전하게 공유하는 가정용 대시보드입니다.

## 현재 단계

- UP Construction: 일정·할 일 수직 조각 구현과 NAS 실기기 검증
- E1 완료: React·TypeScript PWA, Go 단일 서버, SQLite와 NAS 기준선
- E2 완료: 사설 HTTPS 외부 접속과 등록 기기 인증
- C3 완료: 일정·할 일 휴지통, 복원과 일정 알림
- C5 완료: 일정·할 일·공지·날씨와 기기별 위젯 통합
- C6 NAS 운영 중: 가족별 하단 탭, 가계부·대출·적금, 부모별 브라우저 로그인
- C7 NAS 운영 중: 월급 기간 이동, 예산·분류 분석, 대출·적금 납부 일정 고도화
- 시놀로지 2베이 NAS를 운영 서버로 사용하는 것을 목표로 함

## E1 로컬 실행

필요 도구는 Node.js 22+, Go 1.26+다. 전체 컨테이너 검증에는 Docker Compose가 추가로 필요하다.

```sh
cd web
npm install
npm run build
cd ..

go mod tidy
go test ./...
go run ./cmd/server
```

다른 터미널에서 상태를 확인한다.

```sh
curl http://localhost:8080/api/health
curl http://localhost:8080/api/runtime
```

컨테이너 실행:

```sh
mkdir -p runtime/data
docker compose up --build
```

E2 프로토타입에는 다음 상태 API가 추가됐다.

```sh
curl http://localhost:8080/api/setup/status
```

최초 설정 변경 API는 `FAMILY_DASHBOARD_LOCAL_NETWORKS`에 명시된 CIDR의
요청만 허용한다. 값이 없으면 기본 거부한다. HTTP 로컬 개발에서만
`FAMILY_DASHBOARD_SECURE_COOKIES=false`를 사용할 수 있으며, 실제 운영은
HTTPS와 Secure 쿠키를 사용한다. 초기 설정 코드는 최초 실행 시 보호된 운영
로그에 한 번 표시되므로 채팅, Git 또는 문서에 복사하지 않는다.

부모별 비밀번호 로그인은 구현됐지만 공개 HTTPS Reverse Proxy와 공격 차단
실기기 검증은 남아 있다. `8080`과 DSM 관리 포트를 인터넷에 직접 공개하지
않고, 검증 전 공유기 포트 포워딩도 추가하지 않는다.

## 문서

- [개발 프로세스](docs/00-development-process.md)
- [UP 단계별 산출물](docs/up/README.md)
- [비전과 범위](docs/01-vision-and-scope.md)
- [이해관계자](docs/02-stakeholders.md)
- [기기와 권한](docs/03-device-and-permissions.md)
- [기능 요구사항](docs/04-functional-requirements.md)
- [품질 요구사항](docs/05-quality-requirements.md)
- [위험 목록](docs/06-risk-register.md)
- [용어집](docs/07-glossary.md)
- [배포 환경](docs/08-deployment-environment.md)
- [시스템 구성](docs/09-system-context.md)
- [핵심 유스케이스 목록](docs/10-use-case-catalog.md)
- [E1 최소 실행 설계](docs/architecture/e1-minimal-runtime.md)
- [최초 설정 가이드](docs/guides/initial-setup.md)
- [설계 결정 기록](docs/decisions/ADR-001-adaptive-dashboard.md)
- [일정 기반 가족 상태 결정](docs/decisions/ADR-002-schedule-based-family-status.md)
- [일정 대상 가족 구조 결정](docs/decisions/ADR-003-multi-participant-schedule.md)
- [반복 일정 범위 결정](docs/decisions/ADR-004-recurring-schedule-scope.md)
- [겹치는 일정 처리 결정](docs/decisions/ADR-005-overlapping-schedules.md)
- [일정 공개 범위 결정](docs/decisions/ADR-006-schedule-visibility.md)
- [일정 삭제와 복구 결정](docs/decisions/ADR-007-schedule-deletion-and-recovery.md)
- [PIN 기반 관리자 모드 결정](docs/decisions/ADR-008-pin-admin-mode.md)
- [종일·여러 날 일정 결정](docs/decisions/ADR-009-all-day-and-multi-day-schedules.md)
- [PWA 시스템 구성 결정](docs/decisions/ADR-010-pwa-system-architecture.md)
- [Web Push 알림 결정](docs/decisions/ADR-011-web-push-notifications.md)
- [할 일 기본 구조 결정](docs/decisions/ADR-012-task-model.md)
- [반복 할 일 결정](docs/decisions/ADR-013-recurring-tasks.md)
- [할 일 알림 결정](docs/decisions/ADR-014-task-notifications.md)
- [완료 할 일 보관 결정](docs/decisions/ADR-015-completed-task-retention.md)
- [공지·메모 통합 결정](docs/decisions/ADR-016-board-items.md)
- [장소 기반 날씨 결정](docs/decisions/ADR-017-place-based-weather.md)
- [외부 접속 보안 원칙](docs/decisions/ADR-018-secure-remote-access.md)
- [부모 모바일 등록 결정](docs/decisions/ADR-019-parent-device-enrollment.md)
- [TV·공용 태블릿 등록 결정](docs/decisions/ADR-020-shared-device-enrollment.md)
- [최초 신뢰 PC 설정 결정](docs/decisions/ADR-021-initial-trusted-pc.md)
- [E1 기술 스택 결정](docs/decisions/ADR-022-e1-technology-stack.md)
- [E1 런타임 구현 결정](docs/decisions/ADR-023-e1-runtime-implementation.md)
- [E2 사설 외부 접속 결정](docs/decisions/ADR-024-e2-private-remote-access.md)
- [부모 브라우저 비밀번호 로그인 결정](docs/decisions/ADR-031-parent-browser-password-login.md)
- [E2 기기 인증 설계](docs/architecture/e2-authentication-design.md)
- [부모 모바일 Web Push 구독 결정](docs/decisions/ADR-026-web-push-subscriptions.md)
- [E2 보안 요구사항 추적표](docs/up/02-elaboration/e2-security-traceability.md)
- [E1 검증 기록](docs/up/02-elaboration/e1-verification.md)
- [Synology E1 검증 가이드](deploy/synology/README.md)

## 문서 작성 원칙

- 합의된 사항과 미결정 사항을 구분한다.
- 주소, 실제 위치, 비밀번호, PIN 등의 비밀값은 저장소에 기록하지 않는다.
- 중요한 설계 결정은 ADR로 이유와 함께 남긴다.
- 요구사항과 검증 기준에는 추적 가능한 식별자를 부여한다.
- UP 단계별 산출물과 완료 상태는 `docs/up/`에서 유지한다.

## 요구사항 식별자

- `FR`: 기능 요구사항
- `QR`: 품질 요구사항
- `UC`: 유스케이스
- `AC`: 인수 기준
- `RSK`: 위험
- `ADR`: 아키텍처 및 설계 결정
