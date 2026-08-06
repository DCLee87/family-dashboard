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
6. TV·공용 태블릿은 Synology Reverse Proxy의 별도 LAN HTTPS 진입 경로를
   사용한다. 로컬 CA 개인키와 실제 주소는 Git에 저장하지 않는다.

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

Web Push를 처음 포함한 배포에서는 `push.env`에 VAPID 키를 권한 `0600`으로
한 번 생성한다. 이후 배포는 같은 파일을 보존하고 배포 전 백업에도 포함한다.
키 값을 화면, Git 또는 일반 로그에 복사하지 않는다. 키를 잃으면 기존 Push
구독은 다시 등록해야 하므로 실제 운영 전 별도 비밀 저장소에도 보관한다.

E1의 24시간·부하·재시작 검증은 이미 완료되었다. E2 배포 후에는 HTTPS,
최초 신뢰 기기 등록, 인증 쿠키와 기존 SQLite 데이터 마이그레이션을 별도로
검증한다.

## C1 일정 기능 배포

C1은 E2/E3 데이터베이스에 schema version 4를 적용하므로 기존 E2 배포
스크립트와 분리한다.

Mac에서 패키지를 만든다.

```sh
sh deploy/synology/build-c1-package.sh
```

기본 출력은 Git에서 무시되는 `runtime/synology-package-c1`이다. NAS에
`family-dashboard-c1` 디렉터리로 전송한 뒤 명시적인 배포 승인 후 다음을
실행한다.

```sh
sudo sh c1-deploy.sh
```

스크립트는 컨테이너를 멈춘 뒤 DB/WAL/SHM, Compose와 환경 파일을
`backups/pre-c1-<UTC 시각>`에 보존한다. 새 이미지로 `--check-db`를 먼저
실행하며 실패하면 서비스를 중지한 채 백업 경로를 출력한다.

되돌리기는 배포가 출력한 정확한 백업 경로를 명시해야 한다.

```sh
sudo sh c1-rollback.sh /volume1/docker/family-dashboard/backups/pre-c1-<UTC>
```

롤백은 현재 DB 파일을 백업본으로 교체하는 파괴적 작업이므로 새 C1 일정
데이터를 보존하지 않는다. 상태 확인과 화면 문제만으로 즉시 롤백하지 말고
먼저 로그와 `--check-db` 결과를 확인한다.

## C2 반복 일정 시험 배포

C2는 schema version 5와 매주 반복 일정 입력을 추가한다. 패키지는 다음과
같이 만들며 기본 출력 경로는 `runtime/synology-package-c2`이다.

```sh
sh deploy/synology/build-c2-package.sh
```

NAS의 `family-dashboard-c2` 디렉터리로 전송한 뒤 체크섬을 확인하고, 명시적인
시험 배포 승인 후 실행한다.

```sh
shasum -a 256 -c family-dashboard-c2-amd64.tar.sha256
sudo sh c2-deploy.sh
```

스크립트는 기존 설정과 SQLite 파일을 `backups/pre-c2-<UTC 시각>`에 보존한
뒤 DB 무결성 검사를 통과한 경우에만 C2 서비스를 시작한다. 되돌리기는 배포가
출력한 정확한 백업 경로를 `c2-rollback.sh`에 전달한다.

## 로컬 전용 기기의 LAN HTTPS

E2 실기기 환경에서는 Synology Reverse Proxy가 LAN HTTPS 요청을
`127.0.0.1:8080`으로 전달한다. Reverse Proxy의 소스 포트는 공유기에
포워딩하지 않는다. 인증서는 로컬 호스트 이름과 현재 NAS LAN 주소를
포함하고, 해당 로컬 CA 공개 인증서를 사용하는 기기에만 설치한다.

CA 개인키는 암호화본만 별도 보관한다. 평문 개인키, 인증서 암호, 실제 주소와
가족 정보는 Git이나 운영 문서에 기록하지 않는다. 구체적인 환경 값은
운영자가 관리하며 결정 근거는 ADR-025를 따른다.
