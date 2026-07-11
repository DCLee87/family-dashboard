# Family Dashboard

가족 구성원이 일정, 할 일, 공지, 날씨와 위치 상태를 안전하게 공유하는 가정용 대시보드입니다.

## 현재 단계

- UP Inception: 비전, 범위, 핵심 요구사항 및 위험 분석
- 구현 및 기술 스택은 미정
- 시놀로지 2베이 NAS를 운영 서버로 사용하는 것을 목표로 함

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
