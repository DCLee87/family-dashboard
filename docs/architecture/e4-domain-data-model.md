# E4 도메인 데이터 모델과 API 경계

- 상태: 초안
- 작성일: 2026-07-29
- 적용 범위: 일정, 반복 회차, 공개 범위와 일정 기반 예상 상태

## 목적

E4의 첫 기준선은 일정 기능을 구현하기 전에 도메인 규칙, 영속 모델과
권한별 API 응답의 경계를 맞추는 것이다. 현재 E2 기기 인증과 관리자
모드를 그대로 사용하며, 새 인증 체계를 추가하지 않는다.

## 핵심 불변 조건

1. 일정에는 대상 가족 구성원이 한 명 이상 있어야 한다.
2. 시간 일정은 종료가 시작보다 늦고, 종일 일정은 종료일이 시작일보다
   빠르지 않아야 한다.
3. 일정 유형은 `timed` 또는 `all_day` 중 하나이며 두 유형의 시간 필드를
   섞어 저장하지 않는다.
4. 공개 범위는 `family`, `tv_summary`, `parents_only` 중 하나이며 기본값은
   `family`다.
5. 반복 규칙은 `none` 또는 `weekly`만 지원한다.
6. 매주 반복은 요일이 하나 이상이고 시작일이 필수다.
7. 회차 예외는 반복 원본과 원래 회차 키 조합당 하나만 존재한다.
8. 삭제된 일정과 취소된 회차는 일반 조회·겹침·상태 계산에서 제외한다.
9. 부모 전용 상세는 권한 없는 API 응답에 존재하지 않아야 한다.

## 논리 모델

### `family_members`

| 필드 | 의미 |
|---|---|
| `id` | 변경되지 않는 내부 ID |
| `slug` | 초기값 `dad`, `mom`, `daughter`인 고유 식별자 |
| `display_name` | 화면 표시 이름 |
| `role` | `parent` 또는 `child` |
| `active` | 신규 일정 선택 가능 여부 |
| `created_at`, `updated_at` | UTC 감사 시각 |

가족 구성원을 코드의 고정 배열로 취급하지 않는다. 초기 설정에서 기본
세 행을 만들되 일정과 할 일은 내부 ID를 참조한다.

### `schedules`

| 필드 | 의미 |
|---|---|
| `id` | 일정 ID |
| `title` | 필수 제목 |
| `location_name` | 선택 장소명 |
| `notes` | 선택 상세 |
| `visibility` | `family`, `tv_summary`, `parents_only` |
| `time_kind` | `timed`, `all_day` |
| `starts_at`, `ends_at` | 일회성 시간 일정의 UTC 시각 |
| `start_date`, `end_date` | 일회성 종일 일정의 포함 날짜 |
| `status_starts_at`, `status_ends_at` | 종일 일정의 선택적 상태 반영 UTC 구간 |
| `recurrence_kind` | `none`, `weekly` |
| `recurrence_start_date`, `recurrence_end_date` | 포함 날짜 범위 |
| `local_start_time`, `local_end_time` | 매주 반복 시간 일정의 현지 시각 |
| `timezone_id` | 반복 규칙 작성 당시 IANA 시간대 |
| `created_by_device_id` | 생성 기기 |
| `created_at`, `updated_at` | UTC 감사 시각 |
| `deleted_at` | 휴지통 이동 시각 |

일회성 일정과 반복 일정은 같은 aggregate로 다루되 데이터베이스
제약으로 허용되는 필드 조합을 제한한다. `deleted_at` 이후 30일 보관과
영구 삭제는 ADR-007을 따른다.

### `schedule_participants`

| 필드 | 의미 |
|---|---|
| `schedule_id` | 일정 ID |
| `family_member_id` | 가족 구성원 ID |

복합 기본 키로 중복 대상을 막는다. 일정 저장 트랜잭션은 참여자가 없는
상태로 완료될 수 없다.

### `schedule_weekdays`

| 필드 | 의미 |
|---|---|
| `schedule_id` | 반복 일정 ID |
| `weekday` | ISO 요일 `1`(월요일)부터 `7`(일요일) |

`recurrence_kind=weekly`인 일정에만 존재한다.

### `schedule_occurrence_exceptions`

| 필드 | 의미 |
|---|---|
| `id` | 예외 ID |
| `schedule_id` | 반복 원본 ID |
| `occurrence_key` | 원래 현지 시작 날짜·시각 |
| `kind` | `cancelled` 또는 `overridden` |
| `title`, `location_name`, `notes` | 변경 시 선택적 대체 값 |
| `starts_at`, `ends_at` | 변경된 시간 일정의 UTC 구간 |
| `start_date`, `end_date` | 변경된 종일 일정의 포함 날짜 |
| `created_by_device_id` | 변경 기기 |
| `created_at`, `updated_at` | UTC 감사 시각 |

`(schedule_id, occurrence_key)`는 고유하다. 예외는 대상 가족과 공개 범위를
초기 버전에서 변경하지 않는다. 그 변경이 필요하면 원본과 별개의
일회성 일정을 만든다.

## 조회용 최종 회차

조회 서비스는 다음 순서로 `ScheduleOccurrence`를 만든다.

1. 요청 날짜 범위와 교차하는 일회성 일정을 선택한다.
2. 매주 반복 규칙을 시스템 시간대의 달력에서 요청 범위만큼 펼친다.
3. 원래 현지 시작으로 회차 키를 만든다.
4. 취소 예외를 제거하고 변경 예외를 덮어쓴다.
5. 휴지통 일정과 비활성 가족을 정책에 따라 제외한다.
6. 요청 기기·사용자 권한에 맞게 상세를 제거하거나 비식별 상태로
   변환한다.

무기한 반복을 데이터베이스에 무한히 미리 생성하지 않는다. 모든 목록
API는 최대 조회 범위를 가져야 하며 첫 구현은 62일을 상한으로 한다.

## API 경계

초기 경로는 `/api/v1` 아래에 둔다. 오류 응답은 안정적인 `code`,
사용자 표시용 `message`와 선택적 `fields`를 가진다.

### 관리자 쓰기 API

| 메서드와 경로 | 역할 |
|---|---|
| `POST /api/v1/schedules` | 일정과 대상 가족을 한 트랜잭션으로 생성 |
| `PUT /api/v1/schedules/{id}` | 원본 또는 일회성 일정 전체 수정 |
| `DELETE /api/v1/schedules/{id}` | 전체 일정을 휴지통으로 이동 |
| `PUT /api/v1/schedules/{id}/occurrences/{key}` | 한 회차 변경 |
| `DELETE /api/v1/schedules/{id}/occurrences/{key}` | 한 회차 취소 |
| `POST /api/v1/schedules/{id}/restore` | 휴지통 일정 복원 |

모든 쓰기 API는 등록 기기와 유효한 관리자 모드를 요구하고 사용 기기와
시각을 감사 기록에 남긴다. 겹침은 저장 거부가 아니라
`overlap_warning`과 겹친 회차 목록으로 응답한다. 클라이언트는 최초
응답의 확인 토큰을 같은 요청에 실어 명시적으로 저장을 계속한다.

### 읽기 API

| 메서드와 경로 | 역할 |
|---|---|
| `GET /api/v1/schedule-occurrences?from=&to=&member=` | 권한별 최종 회차 목록 |
| `GET /api/v1/schedules/{id}` | 권한별 일정 상세 |
| `GET /api/v1/family-status?at=` | 가족별 예상 상태와 겹침 표시 |
| `GET /api/v1/family-members` | 일정 입력·필터용 가족 구성원 |

날짜 범위는 필수이고 시스템 시간대의 날짜로 해석한다. 응답에는
`timezone`과 각 회차의 `occurrenceKey`를 포함한다.

## 공개 범위 투영

서버가 인증된 기기 프로필을 기준으로 다음 투영 중 하나를 선택한다.
클라이언트가 요청한 프로필 문자열은 신뢰하지 않는다.

| 권한 | `family` | `tv_summary` | `parents_only` |
|---|---|---|---|
| 부모 개인 기기 | 상세 | 상세 | 상세 |
| 딸 개인 보기 | 상세 | 상세 | 목록 제외, 상태만 비식별 |
| 공용 태블릿 보기 | 상세 | `개인 일정` 요약 | 목록 제외, 상태만 비식별 |
| TV | 상세 | `개인 일정` 요약 | 목록 제외, 상태만 비식별 |

요약·비식별 응답은 원본 객체를 일부 마스킹하지 않고 허용 필드만으로 새
DTO를 만든다. 제목, 실제 장소, 메모, 내부 일정 ID와 날씨 지역처럼
상세를 추론할 수 있는 필드를 넣지 않는다.

## 트랜잭션과 동시성

- 일정 원본, 참여자, 요일과 감사 이벤트는 한 SQLite 트랜잭션에서
  저장한다.
- 쓰기 요청은 현재 `updated_at` 또는 버전 값을 요구하는 낙관적
  동시성 검사를 사용한다.
- 충돌 시 마지막 저장이 이기는 방식으로 덮어쓰지 않고 `409 Conflict`를
  반환한다.
- 회차 펼치기와 공개 범위 투영은 동일한 서비스 계층에서 처리해 API별
  규칙 복제를 막는다.

## 구현 순서

1. SQLite 마이그레이션과 제약 조건
2. 회차 생성·예외 적용 순수 함수
3. 공개 범위 투영과 예상 상태 순수 함수
4. 저장소와 관리자 쓰기 API
5. 읽기 API
6. PC 관리자 일정 입력 UI
7. 모바일·태블릿 보기 UI

이 문서는 구현 구조를 제한하지만 현재 E4 문서 작업 자체는 DB
마이그레이션이나 운영 데이터 변경을 수행하지 않는다.
