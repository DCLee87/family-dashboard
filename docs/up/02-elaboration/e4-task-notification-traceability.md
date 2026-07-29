# E4 할 일·알림 요구사항 추적표

- 상태: 초안
- 작성일: 2026-07-29
- 요구사항 기준: `docs/04-functional-requirements.md`
- 설계 기준: ADR-012~015, ADR-028,
  `docs/architecture/e4-task-notification-model.md`

## 검증 유형

| 코드 | 유형 |
|---|---|
| UT | 회차·기한·수신자·알림 계산 단위 시험 |
| DB | SQLite 제약·트랜잭션·watermark 시험 |
| IT | HTTP 권한·상태 전이·응답 계약 통합 시험 |
| PUSH | 가짜 공급자와 실제 Android Web Push 시험 |
| NAS | 중단·재시작·시간 경계 실기기 시험 |
| REV | payload·로그·DB 민감정보 검토 |

## 할 일 요구사항

| 요구사항 | 모델·처리 통제 | 예정 검증 |
|---|---|---|
| FR-TODO-001, FR-TODO-002, FR-TODO-003, FR-TODO-004 | 관리자 쓰기 API, `task_assignees` 최소 1행 | DB: 누락·중복, IT: 권한별 쓰기 |
| FR-TODO-005, FR-TODO-006, FR-TODO-007, FR-TODO-008 | `due_kind`, 날짜·시각 제약, 우선순위 기본값 | DB: 필드 조합, IT: 기본값·왕복 |
| FR-TODO-009, FR-TODO-010, FR-TODO-011, FR-TODO-012, FR-TODO-013 | 회차 상태와 완료 감사, 관리자 전용 전이 | DB: 상태 제약, IT: 완료·취소·딸 거부 |
| FR-TODO-014, FR-TODO-015, FR-TODO-016, FR-TODO-017, FR-TODO-018, FR-TODO-019, FR-TODO-020 | 일·주 반복, 회차 키, 독립 상태와 건너뛰기 | UT: 기간·요일·독립성, DB: 고유 회차 |
| FR-TODO-021, FR-TODO-022, FR-TODO-023 | 마감 유형별 기본 `notification_rules` | UT: 1시간 전·오전 9시·없음 |
| FR-TODO-024, FR-TODO-025, FR-TODO-026, FR-TODO-027 | 담당자 기반 기본 수신자와 사용자 변경 | UT: 가족 조합, IT: 변경·끄기 |
| FR-TODO-028, FR-TODO-029, FR-TODO-030 | 완료·취소·회차별 알림 재조정 | DB: 원자 취소, UT: 미래만 재예약 |
| FR-TODO-031, FR-TODO-032, FR-TODO-033, FR-TODO-034, FR-TODO-035, FR-TODO-036 | 계산된 기한 지남, 중요 오전 9시 재알림 | UT: 우선순위·완료·마감 변경·다음 회차 |
| FR-TODO-037, FR-TODO-038, FR-TODO-039, FR-TODO-040, FR-TODO-041 | 완료 시각 기반 기본 화면·기록 조회·복원 | UT: 7일 경계, IT: 기록·미완료 복원 |
| FR-TODO-042, FR-TODO-043, FR-TODO-044, FR-TODO-045 | 회차/전체 휴지통과 30일 정리 | DB: 원자 삭제, IT: 복원·영구 삭제·권한 |

## 일정·기기 알림 요구사항

| 요구사항 | 모델·처리 통제 | 예정 검증 |
|---|---|---|
| FR-NTF-001, FR-NTF-002, FR-NTF-003 | 복수 `notification_rules`, 상대 시점 후보 | UT: 후보 전체, IT: 추가·삭제·끄기 |
| FR-NTF-004, FR-NTF-005, FR-NTF-006 | 활성 Push 구독, 태블릿 opt-in, TV 화면 강조 | IT/PUSH: 기기별 허용, UI: TV 비Push |
| FR-NTF-007, FR-NTF-008, FR-NTF-009 | 최종 일정 회차 기반 작업 재조정 | UT/DB: 변경·취소와 미발송 작업 |
| FR-NTF-010, FR-NTF-011 | 비민감 템플릿, Web Push 전용 | REV: payload·로그, IT: 다른 채널 없음 |
| FR-NTF-012, FR-NTF-013, FR-NTF-014, FR-NTF-015, FR-NTF-016 | 일정 대상과 별도 수신자, 가족 조합 기본값 | UT: 대상 조합, DB: 별도 연결 |
| FR-NTF-017, FR-NTF-018 | 정의 변경·끄기와 명시적 등록 기기 선택 | IT: 다중 모바일 선택·폐기 |
| FR-NTF-019, FR-NTF-020, FR-NTF-021, FR-NTF-022 | 시간 일정 30분 전, 종일 전날 20시, 복수 설정 | UT: 시스템 시간대·날짜 경계 |

## 데이터 흐름 시험 묶음

1. `task_occurrence_test`: 일회성·매일·매주·포함 종료일·건너뛰기
2. `task_completion_test`: 완료·취소·기기 감사·경합
3. `task_overdue_test`: 날짜/시각 마감·우선순위·오전 9시 재알림
4. `scheduler_recovery_test`: watermark, 긴 중단, 멱등 재시작
5. `notification_reconciliation_test`: 시각 변경·완료·취소·기기 폐기
6. `notification_delivery_test`: 선점·성공·제한 재시도·만료 구독
7. `notification_privacy_test`: 부모 전용 payload·오류·로그 금지 필드

## 현재 추적 공백

- TV의 다가오는 일정 강조는 후속 TV 실기기 반복에서 검증한다.
- Push 공급자의 실제 중복 가능성과 Android 절전 중 지연은 장시간 모바일
  시험에서 관찰한다.
- 할 일과 일정 편집 UI의 최종 화면 흐름은 Construction 수직 조각에서
  상세화한다.
