# E4 일정 요구사항 추적표

- 상태: 초안
- 작성일: 2026-07-29
- 요구사항 기준: `docs/04-functional-requirements.md`
- 설계 기준: ADR-002~009, ADR-027,
  `docs/architecture/e4-domain-data-model.md`

## 검증 유형

| 코드 | 유형 |
|---|---|
| UT | 회차·겹침·상태·투영 단위 시험 |
| DB | SQLite 제약·마이그레이션·트랜잭션 시험 |
| IT | HTTP 인증·검증·응답 계약 통합 시험 |
| UI | PC·모바일·태블릿 화면 시험 |
| NAS | 백업 후 실제 이미지·기존 DB 업그레이드 시험 |
| REV | 모델·로그·응답의 민감정보 검토 |

## 일정 요구사항

| 요구사항 | 유스케이스 | 모델·API 통제 | 예정 검증 |
|---|---|---|---|
| FR-SCH-001, FR-SCH-002, FR-SCH-003 | UC-010, 011 | 관리자 쓰기 API, 일정 핵심 필드 | IT: 관리자 여부·필드 왕복, UI: 입력·수정 |
| FR-SCH-004, FR-SCH-005, FR-SCH-006, FR-SCH-007 | UC-010, 012 | `schedule_participants` 최소 1행, 복합 키 | DB: 누락·중복 거부, IT: 가족별 조회 |
| FR-SCH-008, FR-SCH-009, FR-SCH-010 | UC-010, 012 | `weekly`, ISO 요일, 포함 시작·종료일 | UT: 요일·기간 경계, DB: 허용 조합 |
| FR-SCH-011, FR-SCH-012, FR-SCH-013, FR-SCH-014 | UC-011 | 회차 키와 `cancelled`·`overridden` 고유 예외 | UT: 회차 독립성, DB: 중복 예외 거부 |
| FR-SCH-015, FR-SCH-016, FR-SCH-017, FR-SCH-018, FR-SCH-019 | UC-010, 015 | 반개구간 겹침, 확인 토큰, 겹침 DTO | UT: 경계·동률, IT: 경고 후 저장 |
| FR-SCH-020, FR-SCH-021, FR-SCH-022, FR-SCH-023, FR-SCH-024, FR-SCH-025 | UC-010, 014 | 서버 기기 프로필 투영, 기본 `family` | IT: 권한 행렬·위조 거부, REV: DTO 누출 |
| FR-SCH-026, FR-SCH-027, FR-SCH-028, FR-SCH-029, FR-SCH-030, FR-SCH-031, FR-SCH-032 | UC-013 | `deleted_at`, 30일 보존, 회차/전체 구분 | DB: 원자 삭제, IT: 복원·권한, UI: 확인 |
| FR-SCH-033, FR-SCH-034, FR-SCH-035, FR-SCH-036 | UC-010, 012 | `timed`/`all_day` 배타 제약, 포함 날짜 | UT: 날짜 펼치기, UI: 유형 구분 |

## 예상 상태 요구사항

| 요구사항 | 유스케이스 | 모델·API 통제 | 예정 검증 |
|---|---|---|---|
| FR-LOC-001, FR-LOC-002, FR-LOC-003, FR-LOC-004, FR-LOC-005, FR-LOC-006 | UC-020 | 일정 회차만 입력으로 사용하는 상태 서비스 | UT: 일정 없음·기본 상태, REV: GPS 필드 부재 |
| FR-LOC-007, FR-LOC-008 | UC-015, 020 | 최신 시작과 ADR-027 동률, 겹침 표시 | UT: 후보 순서 전체 조합 |
| FR-LOC-009, FR-LOC-010, FR-LOC-011, FR-LOC-012 | UC-014, 020 | 상세 DTO와 비식별 상태 DTO 분리 | IT: 권한 행렬, REV: 금지 필드 검색 |
| FR-LOC-013, FR-LOC-014 | UC-010, 020 | 선택적 종일 상태 반영 UTC 구간 | UT: 구간 유무·종료 경계, DB: 쌍 제약 |

## ADR 결정 추적

| 결정 | 구현 위치 | 예정 검증 |
|---|---|---|
| ADR-002 일정 기반 상태 | `family-status` 도메인 서비스 | UT: GPS 없이 상태 계산 |
| ADR-003 복수 대상 가족 | 참여자 연결 테이블 | DB: 복합 키, IT: 모든 대상 조회 |
| ADR-004 초기 반복 범위 | 주간 회차 생성기 | UT: 허용·비허용 반복 |
| ADR-005 겹침 허용 | 겹침 검사와 확인 토큰 | UT/IT: 경고 후 저장 |
| ADR-006 공개 범위 | 서버 측 응답 투영 | IT/REV: 권한별 원문 부재 |
| ADR-007 휴지통 | 소프트 삭제와 정리 작업 | DB/IT: 30일 경계·복원 |
| ADR-009 종일 일정 | 포함 날짜와 선택 상태 구간 | UT/UI: 여러 날 표시 |
| ADR-027 시간·회차 의미론 | 회차 생성·구간·동률 공통 함수 | UT: 시간 경계·회차 키·동률 |

## API 계약 추적

| API | 주요 요구사항 | 필수 실패 시험 |
|---|---|---|
| `POST /api/v1/schedules` | FR-SCH-001~025, 033~036 | 무관리자, 대상 없음, 잘못된 구간, 겹침 미확인 |
| `PUT /api/v1/schedules/{id}` | FR-SCH-001, 008~025 | 오래된 버전, 예외 충돌, 상세 직접 접근 |
| `PUT /api/v1/schedules/{id}/occurrences/{key}` | FR-SCH-012~013 | 잘못된 키, 중복 예외, 겹침 미확인 |
| `DELETE /api/v1/schedules/{id}/occurrences/{key}` | FR-SCH-011, 031 | 무관리자, 없는 키, 경합 |
| `DELETE /api/v1/schedules/{id}` | FR-SCH-026~032 | 무관리자, 중복 삭제, 오래된 버전 |
| `POST /api/v1/schedules/{id}/restore` | FR-SCH-027~030 | 30일 초과, 영구 삭제됨, 무관리자 |
| `GET /api/v1/schedule-occurrences` | FR-SCH-002~025, 033~036 | 범위 없음·초과, 폐기 기기, 권한별 누출 |
| `GET /api/v1/family-status` | FR-LOC-001~014 | 폐기 기기, 비식별 누출, 종료 경계 |

## Construction 진입용 자동화 묶음

1. `schedule_occurrence_test`: 단일·주간·취소·변경·포함 종료일
2. `schedule_overlap_test`: 맞닿음·교차·자정·복수 대상·동률
3. `schedule_projection_test`: 부모·딸·태블릿·TV의 세 공개 범위
4. `family_status_test`: 최신 시작·동률·종일 상태·비식별 상태
5. `schedule_repository_test`: 제약·롤백·낙관적 잠금·휴지통
6. `schedule_http_test`: 관리자 쓰기와 권한별 읽기 계약
7. `schedule_migration_test`: 기존 E3 DB에서 적용·재적용·무결성

## 현재 추적 공백

- 실제 테이블과 migration 번호는 Construction 첫 반복에서 확정한다.
- 겹침 확인 토큰의 서명·만료 형식은 HTTP 구현 전에 별도 보안 검토한다.
- 일정 변경을 TV에 60초 안에 반영하는 FR-DSH-011은 대시보드 갱신
  수직 조각에서 연결 시험한다.
- 일정 알림 FR-NTF 요구사항은 할 일·알림 데이터 흐름과 함께 E4 후속
  추적표에서 다룬다.
