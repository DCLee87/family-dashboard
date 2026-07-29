# Elaboration 산출물

- 상태: 진행 중
- 시작일: 2026-07-13
- 종료일: 미정

## 목표

핵심 유스케이스를 상세화하고 실행 가능한 아키텍처를 통해 DS216+II 자원, 외부 접속 보안, 삼성 TV, 부모 모바일 PWA와 Web Push 위험을 검증한다.

## 예정 산출물

- 상세 유스케이스와 인수 기준
- 요구사항 추적표
- 아키텍처 기준선
- 데이터 모델
- 보안·인증·권한 설계
- 시놀로지 NAS 배포 검증 결과
- TV 및 주요 기기 UI 프로토타입
- 품질 측정·테스트 계획
- Construction 반복 계획

## 반복 계획

| 반복 | 목표 | 주요 검증 | 완료 산출물 |
|---|---|---|---|
| E1 | NAS 경량 실행 기준선 | 단일 컨테이너, SQLite, 메모리·CPU, 재시작·영속성 | 기술 스택 ADR, 실행 가능한 최소 골격, 자원 측정 결과 |
| E2 | 보안·외부 접속 기준선 | 위협 모델, HTTPS, 등록 기기, PIN, Reverse Proxy·VPN 비교 | 보안 설계, 기기 등록 프로토타입, 외부 접속 ADR |
| E3 | 사용자 기기 검증 | 삼성 TV 브라우저, iOS·Android PWA 설치, Web Push, 3 m 가독성 | 기기 호환성 보고서, UI 프로토타입, Push 시험 결과 |
| E4 | 도메인·아키텍처 기준선 | 일정·반복·예외·공개 범위·할 일·알림 데이터 흐름 | 상세 유스케이스, 데이터 모델, API 경계, 테스트 계획 |

## 완료 반복: E1

### 목표

DS216+II에서 실행 가능한 최소 아키텍처와 자원 예산을 검증한다.

### 작업

- [x] 기술 스택 후보 비교 및 ADR 작성
- [x] E1 최소 실행 구조 설계
- [x] 단일 애플리케이션 컨테이너 골격 구성
- [x] SQLite 영속 볼륨 구성
- [x] 상태 확인 endpoint와 최소 웹 화면 구현
- [x] 컨테이너 재시작·NAS 재부팅 후 자동 복구 확인
- [x] DS216+II 시험 배포·재시작·재생성·SQLite 영속성 확인
- [x] 유휴·정상 요청·단기 부하의 CPU와 메모리 측정
- [x] 24시간 메모리 증가 여부 확인

### E1 완료 기준

- [x] DS216+II에서 컨테이너가 정상 실행됨
- [x] 정상 사용 목표 메모리 256 MB 이내 달성 또는 목표 조정 근거 기록
- [x] 데이터가 컨테이너 재생성 후에도 유지됨
- [x] 재시작 후 수동 개입 없이 서비스가 복구됨
- [x] 다음 반복에서 사용할 아키텍처 후보가 ADR로 승인됨

### 현재 장애

- Mac Colima에서 arm64 이미지·Compose·SQLite 영속성 검증 완료
- Buildx로 linux/amd64 이미지와 NAS 전송 패키지 생성·에뮬레이션 실행 검증 완료
- DS216+II에서 재시작·재생성·영속성·초기 자원 측정 완료
- 24시간 CPU·메모리 관찰 완료: 평균 CPU 0.0483%, 최대 메모리 8.578 MiB
- 비정상 프로세스 종료 후 자동 복구·DB 무결성 시험 완료
- NAS 재부팅 후 컨테이너 자동 복구·SQLite 무결성 시험 완료
- 상태 API 60초 단기 부하 완료: 1,200/1,200 성공, 최대 메모리 11.060 MiB
- 세부 결과는 [E1 검증 기록](e1-verification.md)에서 관리

## 완료 기준선: E2

외부 접속을 활성화하지 않은 상태에서 위협 모델, HTTPS, 등록 기기, 관리자
PIN과 원격 접속 방식을 설계·검증한다.

- [E2 작업 계획](e2-plan.md)
- [E2 위협 모델 초안](../../architecture/e2-threat-model.md)
- [E2 보안 요구사항 추적표](e2-security-traceability.md)
- [E2 NAS 보안·외부 접속 검증 기록](e2-verification.md)
- [E2 외부 접속 방식 비교](../../architecture/e2-external-access-comparison.md)
- [E2 기기 인증 설계](../../architecture/e2-authentication-design.md)
- [ADR-024: E2 사설 외부 접속 기준선](../../decisions/ADR-024-e2-private-remote-access.md)

## 병행 반복: E3 모바일 범위

E2의 부모 모바일·공용 Android 태블릿 보안 검증을 기반으로 PWA 설치성과
Web Push 위험을 먼저 검증한다. 사용자가 후속으로 연기한 삼성 TV와 NAS
복구 시험은 완료 처리하지 않는다.

- [E3 모바일 PWA 검증 계획](e3-plan.md)

## 현재 반복: E4

Construction 구현 전에 일정의 시간·반복·예외·공개 범위 의미론과 데이터
모델, API 경계와 인수 시험을 기준선으로 만든다. 일정 기준선을 먼저
검토한 뒤 같은 회차 모델을 할 일과 알림 흐름에 확장한다.

- [E4 도메인·아키텍처 기준선 계획](e4-plan.md)
- [E4 도메인 데이터 모델과 API 경계](../../architecture/e4-domain-data-model.md)
- [E4 일정 상세 유스케이스](e4-schedule-use-cases.md)
- [E4 일정 요구사항 추적표](e4-schedule-traceability.md)
- [E4 할 일·알림 데이터 모델과 처리 흐름](../../architecture/e4-task-notification-model.md)
- [E4 할 일·알림 요구사항 추적표](e4-task-notification-traceability.md)
- [ADR-027: 일정 시간과 반복 회차 의미론](../../decisions/ADR-027-schedule-time-and-occurrence-semantics.md)
- [ADR-028: 할 일 회차와 알림 처리 의미론](../../decisions/ADR-028-task-occurrence-and-notification-semantics.md)

## 종료 조건

- [ ] 주요 기술 위험이 검증됨
- [ ] 핵심 아키텍처가 실행 가능한 형태로 검증됨
- [ ] 핵심 유스케이스와 인수 기준이 상세화됨
- [ ] Construction 범위와 반복 계획이 합의됨
