# E4 할 일·알림 데이터 모델과 처리 흐름

- 상태: 초안
- 작성일: 2026-07-29
- 적용 범위: 할 일, 반복 회차, 완료·건너뛰기, 일정·할 일 알림

## 핵심 불변 조건

1. 할 일에는 담당 가족이 한 명 이상 있어야 한다.
2. 반복은 `none`, `daily`, `weekly`만 지원한다.
3. 매주 반복은 ISO 요일이 하나 이상이어야 한다.
4. 마감 시각은 마감일 없이 존재할 수 없다.
5. 회차는 원본과 회차 키 조합당 하나만 존재한다.
6. 완료 회차는 완료 시각과 수행 기기를 함께 가지고, 미완료·건너뛴
   회차는 가지지 않는다.
7. 삭제된 원본과 회차, 완료·건너뛴 회차에는 활성 알림 작업이 없다.
8. 알림 작업은 한 기기와 한 Push 구독을 명시적으로 대상으로 한다.

## 논리 모델

### `tasks`

| 필드 | 의미 |
|---|---|
| `id` | 할 일 ID |
| `title`, `notes` | 제목과 선택 메모 |
| `priority` | `normal` 또는 `important` |
| `due_kind` | `none`, `date`, `datetime` |
| `due_date`, `local_due_time` | 시스템 시간대의 마감 |
| `recurrence_kind` | `none`, `daily`, `weekly` |
| `recurrence_start_date`, `recurrence_end_date` | 포함 날짜 범위 |
| `timezone_id` | 규칙 작성 당시 IANA 시간대 |
| `created_by_device_id` | 생성 기기 |
| `created_at`, `updated_at`, `deleted_at` | UTC 감사·휴지통 시각 |

### `task_assignees`

`task_id`와 `family_member_id`의 복합 기본 키를 사용한다. 여러 담당자가
있어도 회차 완료 상태는 하나다.

### `task_weekdays`

매주 반복 원본의 ISO 요일을 `(task_id, weekday)`로 저장한다.

### `task_occurrences`

| 필드 | 의미 |
|---|---|
| `id` | 회차 ID |
| `task_id` | 원본 할 일 |
| `occurrence_key` | 일회성 키 또는 원래 현지 날짜 |
| `due_at` | 마감 시각이 있으면 계산한 UTC 시각 |
| `due_date` | 날짜만 마감 또는 표시용 현지 날짜 |
| `status` | `pending`, `completed`, `skipped` |
| `completed_at`, `completed_by_device_id` | 완료 감사 정보 |
| `created_at`, `updated_at`, `deleted_at` | UTC 시각 |

`(task_id, occurrence_key)`는 고유하다. `overdue`와 완료 후 7일 숨김 여부는
조회 시 계산한다. 완료 기록은 자동 영구 삭제하지 않는다.

### `notification_rules`

| 필드 | 의미 |
|---|---|
| `id` | 알림 정의 ID |
| `subject_type`, `subject_id` | `schedule` 또는 `task` 원본 |
| `timing_kind` | 시작 상대, 마감 상대, 현지 고정 시각 |
| `offset_seconds` | 10분 전 등의 상대 값 |
| `local_time`, `day_offset` | 종일·날짜 마감용 현지 시각 |
| `enabled` | 알림 사용 여부 |
| `template_kind` | 공개 범위별 메시지 템플릿 |
| `created_at`, `updated_at` | UTC 감사 시각 |

### `notification_recipients`

알림 정의와 가족 구성원 또는 명시적 등록 기기를 연결한다. 실제 발송
시점에는 활성 등록 기기와 활성 Push 구독으로 확정한다. 공용 태블릿은
명시적으로 켠 경우에만 수신자가 된다.

### `notification_jobs`

| 필드 | 의미 |
|---|---|
| `id` | 발송 작업 ID |
| `rule_id` | 알림 정의 |
| `subject_type`, `subject_id` | 원본 식별 |
| `occurrence_key` | 최종 회차 키 |
| `device_id`, `push_subscription_id` | 발송 대상 |
| `scheduled_at` | UTC 예정 시각 |
| `status` | `pending`, `processing`, `sent`, `cancelled`, `failed` |
| `attempt_count`, `next_attempt_at` | 제한 재시도 |
| `deduplication_key` | 회차·정의·기기 고유 키 |
| `sent_at`, `last_error_code` | 결과 메타데이터 |

본문 원문은 작업에 저장하지 않는다. 발송 직전에 허용된 템플릿과 현재
도메인 데이터를 사용해 payload를 만든다.

### `scheduler_watermarks`

회차 생성과 알림 재조정 작업별 마지막 완료 현지 날짜·UTC 시각을
저장한다. 작업 시작 시점이 아니라 트랜잭션 완료 후에만 전진한다.

## 기본 알림 생성

### 할 일

- 마감 시각 있음: 마감 1시간 전
- 마감일만 있음: 당일 시스템 시간대 오전 9시
- 마감 없음: 기본 알림 없음
- 중요 기한 지난 회차: 완료·건너뛰기·마감 변경 전까지 매일 오전 9시

기본 수신자는 담당자가 아빠만이면 아빠, 엄마만이면 엄마, 딸 포함 또는
여러 담당자면 부모 두 명이다.

### 일정

- 시간 일정: 시작 30분 전
- 종일 일정: 시작일 전날 시스템 시간대 오후 8시
- 추가 후보: 시작 시각, 10분·30분·1시간·1일 전

일정 대상 가족과 알림 수신자는 별도 연결로 저장한다. 부모 전용 일정은
`private_schedule` 템플릿을 사용한다.

## 처리 흐름

### 회차 생성

1. 예약 작업이 watermark 이후 필요한 날짜 범위를 계산한다.
2. 반복 규칙별 회차 키를 순서대로 만든다.
3. 고유 키 충돌을 성공으로 취급하는 멱등 삽입을 수행한다.
4. 회차별 알림 정의를 최종 마감·시작 시각에 적용한다.
5. 활성 수신 기기·구독별 알림 작업을 멱등 삽입한다.
6. 트랜잭션 성공 후 watermark를 전진한다.

### 도메인 변경 재조정

1. 일정 시간·회차 예외, 할 일 마감·완료 또는 알림 정의가 바뀐다.
2. 영향받은 미발송 작업을 `cancelled`로 바꾼다.
3. 현재 최종 회차와 수신 기기를 다시 계산한다.
4. 미래 작업만 새 고유 키로 만든다.
5. 이미 발송된 작업은 삭제하거나 발송 이력을 바꾸지 않는다.

### Push 발송

1. 발송 예정 시각이 지난 `pending` 작업을 제한 수만큼 선점한다.
2. 기기와 구독이 여전히 활성인지 확인한다.
3. 공개 범위 투영과 템플릿으로 payload를 만든다.
4. Push 공급자 결과를 `sent`, 재시도 또는 영구 실패로 기록한다.
5. 만료 구독 응답이면 구독을 비활성화하고 후속 작업을 취소한다.

## API 경계

| 메서드와 경로 | 역할 |
|---|---|
| `POST /api/v1/tasks` | 할 일·담당자·반복·기본 알림 생성 |
| `PUT /api/v1/tasks/{id}` | 원본과 미래 회차 규칙 수정 |
| `PUT /api/v1/tasks/{id}/occurrences/{key}/completion` | 완료·완료 취소 |
| `PUT /api/v1/tasks/{id}/occurrences/{key}/skip` | 한 회차 건너뛰기·취소 |
| `GET /api/v1/task-occurrences?from=&to=&assignee=` | 회차·기한 지남·완료 표시 |
| `PUT /api/v1/tasks/{id}/notification-rules` | 알림 시점·수신자 변경·끄기 |
| `PUT /api/v1/schedules/{id}/notification-rules` | 일정 알림 변경·끄기 |

쓰기는 등록 기기의 유효한 관리자 모드를 요구한다. 딸 보기 모드와 TV는
완료·알림 API를 사용할 수 없다.

## 원자성과 동시성

- 원본, 담당자, 요일과 기본 알림은 한 트랜잭션으로 생성한다.
- 완료 변경과 알림 취소는 한 트랜잭션에서 처리한다.
- 수정 API는 버전 기반 낙관적 동시성을 사용한다.
- 예약 작업과 사용자 작업은 고유 키와 조건부 상태 변경으로 경합을
  안전하게 종료한다.
- `processing` 상태가 제한 시간을 넘으면 재시도 가능한 작업으로
  회수하되 최대 시도 횟수를 넘지 않는다.

## 필수 시험

- 매일·매주 회차의 포함 날짜와 서버 중단 후 watermark 복구
- 같은 회차 동시 생성의 멱등성
- 완료·완료 취소·건너뛰기와 알림 재조정
- 중요/보통 기한 지남과 오전 9시 경계
- 기기 폐기·구독 만료 중 발송 경합
- 부모 전용 일정 payload와 로그의 금지 필드
- 발송 성공 후 재시작해도 동일 작업을 다시 선택하지 않음
