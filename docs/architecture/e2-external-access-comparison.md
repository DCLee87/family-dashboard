# E2 외부 접속 방식 비교

- 상태: 권고 승인, ADR-024로 결정
- 작성일: 2026-07-26
- 결정 대상: ADR-018의 VPN·HTTPS Reverse Proxy 구현 방식

## 전제

- 외부 사용자는 등록된 부모 PC와 모바일로 제한한다.
- TV와 공용 태블릿은 가정 내 네트워크 전용이다.
- 앱 자체의 기기 등록·폐기·권한 검사는 접속 계층과 무관하게 유지한다.
- DSM 관리 화면과 Container Manager는 인터넷에 공개하지 않는다.
- E2 검증 전에는 라우터 포트 포워딩을 추가하지 않는다.

## 후보

### A. Tailscale + Tailscale Serve

NAS와 부모 기기를 같은 tailnet에 등록하고, Tailscale Serve가
`127.0.0.1:8080`을 tailnet 전용 HTTPS URL로 프록시한다.

장점:

- 라우터 인바운드 포트를 열지 않는다.
- tailnet 접근 정책과 앱 기기 인증을 겹쳐 적용할 수 있다.
- MagicDNS와 자동 발급 TLS 인증서를 사용할 수 있다.
- 부모 기기의 외부 IP가 바뀌어도 고정 tailnet 이름을 사용한다.
- E2 시험 중 실수로 서비스를 전체 인터넷에 노출할 가능성이 낮다.

부담과 위험:

- NAS와 모든 외부 부모 기기에 Tailscale 설치·로그인이 필요하다.
- Tailscale 계정·제어 계층과 클라이언트 가용성에 의존한다.
- Synology Package Center 버전은 최신 릴리스보다 늦을 수 있다.
- DSM 7 패키지는 일부 네트워크 기능과 Tailscale SSH에 제한이 있다.
- HTTPS 인증서의 기기·tailnet DNS 이름은 Certificate Transparency 로그에
  공개되므로 민감하지 않은 임의 이름을 사용해야 한다.
- `tailscale funnel`은 공개 인터넷 기능이므로 사용하지 않고 정책에서도
  비활성화한다.

### B. Synology Reverse Proxy + 공인 HTTPS

Synology Application Portal의 Reverse Proxy가 공개 HTTPS 요청을 내부
`8080`으로 전달한다.

장점:

- 부모 기기에 VPN 앱이 없어도 일반 브라우저와 PWA로 접속할 수 있다.
- 표준 도메인과 HTTPS URL을 사용할 수 있다.
- 향후 Push 링크와 제3자 연동이 단순하다.
- DSM에서 Reverse Proxy, HSTS, HTTP/2와 인증서 연결을 관리할 수 있다.

부담과 위험:

- 라우터 포트와 공개 DNS를 운영해야 하며 앱이 지속적으로 인터넷 공격면에
  놓인다.
- 기기 인증, 요청 제한, 감사와 보안 패치를 공개 전에 완성해야 한다.
- Synology의 일반 Let's Encrypt 방식은 도메인 검증·갱신을 위해 인터넷의
  80번 포트 접근이 필요하다.
- 프록시 전달 헤더를 신뢰할 주소를 엄격히 제한하지 않으면 로컬 기기 판정이
  우회될 수 있다.
- NAS 방화벽, 인증서 갱신, 도메인과 라우터 설정의 운영 부담이 증가한다.

## 비교

| 기준 | Tailscale + Serve | Synology Reverse Proxy |
|---|---|---|
| 공개 인바운드 포트 | 없음 | 일반적으로 443, 인증서 방식에 따라 80 |
| 외부 공격면 | tailnet 구성원 | 전체 인터넷 |
| 부모 기기 준비 | Tailscale 설치·로그인 | 브라우저만 필요 |
| 브라우저 HTTPS | Serve 자동 인증서 | DSM 인증서 설정 |
| 앱 자체 기기 인증 | 계속 필요 | 반드시 필요 |
| 공개 Push 링크 | VPN 연결 필요 | 직접 접근 가능 |
| 초기 E2 안전성 | 높음 | 인증 완성 전 낮음 |
| 운영 의존성 | Tailscale 계정·클라이언트 | DNS·CA·라우터·DSM |
| 장애 격리 | tailnet 접근 중단 가능 | 공개 DNS·인증서·회선 영향 |

## 권고안

E2 기본 경로는 **Tailscale + Tailscale Serve**로 한다.

1. 포트를 공개하지 않은 상태에서 HTTPS와 외부 기기 인증을 검증한다.
2. Tailscale 접근 권한은 외부 네트워크 경계이고, 앱의 기기 인증은 가족
   데이터 권한 경계로 분리한다.
3. Serve 대상은 가능하면 loopback으로 제한하고 전달 신원 헤더를 앱 인증의
   유일한 근거로 사용하지 않는다.
4. Funnel은 활성화하지 않는다.
5. 부모 모바일의 VPN 사용성과 Push 링크 동작이 요구를 충족하지 못할 때만
   공개 Reverse Proxy를 다시 평가한다.

이 권고는 ADR-024로 승인했다. NAS 패키지 설치 가능 여부, Tailscale Serve
지원 버전, iOS·Android 실사용성과 재부팅 후 자동 복구는 결정의 실기기
검증 항목으로 남긴다.

## E2 시험 항목

- [ ] DS216+II에서 공식 Tailscale 패키지 설치와 자원 사용량
- [ ] NAS 재부팅 후 tailnet 및 Serve 자동 복구
- [ ] 임의의 비민감 NAS 이름과 MagicDNS HTTPS
- [ ] 부모 모바일의 Wi-Fi·셀룰러 전환 후 접속
- [ ] tailnet 미가입 기기의 접속 거부
- [ ] Tailscale 권한 제거 후 즉시 접속 거부
- [ ] Funnel 비활성 상태 확인
- [ ] 앱 기기 폐기 후 tailnet 연결 상태에서도 가족 데이터 거부

## 공식 자료

- [Tailscale의 Synology NAS 지원](https://tailscale.com/docs/integrations/synology)
- [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve)
- [Tailscale HTTPS 인증서](https://tailscale.com/docs/how-to/set-up-https-certificates)
- [Synology Application Portal과 Reverse Proxy](https://kb.synology.com/en-global/DSM/help/DSM/AdminCenter/application_appportalias)
- [Synology 인증서와 Let's Encrypt](https://kb.synology.com/en-global/DSM/help/DSM/AdminCenter/connection_certificate)
