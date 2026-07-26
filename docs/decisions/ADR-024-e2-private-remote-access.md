# ADR-024: E2 사설 외부 접속 기준선

- 상태: 승인, 실기기 검증 필요
- 결정일: 2026-07-26

## 배경

ADR-018은 등록된 부모 기기의 HTTPS 외부 접속을 허용하되 VPN과 공개
Reverse Proxy 중 구현 방식을 E2에서 결정하도록 했다. E2 인증 프로토타입을
인터넷에 바로 공개하면 미완성 인증과 요청 제한이 가족 데이터의 공격면이
된다.

## 결정

- E2의 기본 외부 접속 계층은 Tailscale과 Tailscale Serve로 한다.
- NAS와 승인된 부모 기기만 같은 tailnet에 등록한다.
- Tailscale Serve는 NAS의 대시보드 HTTP 포트를 tailnet 전용 HTTPS로
  프록시한다.
- Tailscale Funnel은 사용하지 않으며 공개 인터넷 포트를 추가하지 않는다.
- Tailscale 접근은 네트워크 경계일 뿐이며 앱의 기기 등록·권한·폐기를
  대체하지 않는다.
- Tailscale 신원 헤더는 보조 감사 정보로만 사용하고 앱 인증의 유일한
  근거로 사용하지 않는다.
- TV와 공용 태블릿은 가정 내 네트워크 전용 정책을 유지한다.
- NAS와 tailnet 기기 이름은 인증서 투명성 로그에 노출돼도 민감정보가
  되지 않는 이름을 사용한다.
- 부모 모바일 사용성이나 Push 링크 요구를 충족하지 못할 때 공개 HTTPS
  Reverse Proxy를 별도 ADR로 재평가한다.

## 결과

- E2 인증을 공개 인터넷 공격면 없이 실제 외부 네트워크에서 검증할 수 있다.
- 부모 PC·모바일에 Tailscale 설치와 tailnet 로그인이 필요하다.
- Tailscale 계정, 클라이언트와 Synology 패키지 운영이 추가된다.
- NAS 패키지 버전, Serve 지원, 재부팅 자동 복구와 자원 사용량을 실측해야
  한다.
- 앱의 등록 기기 인증과 관리자 PIN 통제는 계속 구현해야 한다.

## 금지 사항

- `tailscale funnel` 활성화
- E2 중 라우터의 8080·DSM·Container Manager 포트 공개
- Tailscale 신원 헤더만으로 가족 데이터 또는 관리자 권한 부여
- 인증·거부 테스트 전에 실제 가족 데이터 입력

## 검증

- DS216+II 공식 패키지 설치 및 Serve 지원 버전 확인
- NAS 재부팅 후 tailnet·Serve 자동 복구
- Wi-Fi와 셀룰러 전환 후 부모 모바일 HTTPS 접속
- 미가입·권한 제거 tailnet 기기의 접속 거부
- 앱에서 폐기된 기기의 tailnet 내부 요청 거부
- Funnel 비활성 및 라우터 공개 포트 미추가 확인

## 근거

- [E2 외부 접속 방식 비교](../architecture/e2-external-access-comparison.md)
- [Tailscale의 Synology NAS 지원](https://tailscale.com/docs/integrations/synology)
- [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve)
- [Synology Reverse Proxy](https://kb.synology.com/en-global/DSM/help/DSM/AdminCenter/application_appportalias)
