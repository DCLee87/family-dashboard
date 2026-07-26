# Synology E2 배포

이 디렉터리는 DS216+II에서 E2 보안 런타임을 배포하기 위한 보조 도구다.
E1 데이터 볼륨은 유지하지만, 외부 백업과 복구 절차가 검증되기 전에는 실제
가족 데이터를 유일본으로 저장하지 않는다.

## 사전 준비

1. Container Manager에서 Compose 프로젝트를 실행할 수 있어야 한다.
2. 배포 디렉터리의 `data` 디렉터리를 만들고 컨테이너 UID `10001`이 쓸 수 있게 설정한다.
3. Tailscale Serve가 NAS의 loopback `8080`을 비공개 HTTPS로 프록시해야 한다.
4. Compose의 `FAMILY_DASHBOARD_LOCAL_NETWORKS`에는 실제 Docker 프록시
   소스 CIDR을 명시한다. 빈 값은 최초 설정 변경 API를 기본 거부한다.
5. `8080`은 `127.0.0.1`에만 바인딩하며 공유기 포트 포워딩이나 Tailscale
   Funnel을 사용하지 않는다.

## Mac에서 NAS 패키지 생성

DS216+II의 1 GB 메모리에서 Node.js와 Go 빌드를 실행하지 않는다. 개발 Mac에서 `linux/amd64` 이미지를 만들고 이미지 보관 파일과 Compose 파일을 NAS로 옮긴다.

```sh
sh deploy/synology/build-package.sh
```

기본 출력 경로는 `runtime/synology-package`이며 Git에서 무시된다.

## NAS에서 실행

패키지 디렉터리를 NAS의 사용자 홈으로 복사한 뒤:

```sh
sudo sh e2-deploy.sh
```

스크립트는 컨테이너를 정지한 상태에서 SQLite의 DB/WAL/SHM과 기존 Compose를
`backups/pre-e2-<UTC 시각>`에 복사한다. 이후 E2 이미지를 로드하고, 호스트의
`8080`을 loopback에만 바인딩하며, 실제 Compose 네트워크 CIDR을 최초 설정
허용 범위로 기록한다. 백업 디렉터리를 확인하기 전에는 삭제하지 않는다.

E1의 24시간·부하·재시작 검증은 이미 완료되었다. E2 배포 후에는 HTTPS,
최초 신뢰 기기 등록, 인증 쿠키와 기존 SQLite 데이터 마이그레이션을 별도로
검증한다.
