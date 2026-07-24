# Synology E1 검증

이 디렉터리는 DS216+II에서 E1 최소 런타임을 검증하기 위한 보조 도구다. 실제 가족 데이터를 넣기 전에 테스트 데이터로만 실행한다.

## 사전 준비

1. Container Manager에서 Compose 프로젝트를 실행할 수 있어야 한다.
2. 배포 디렉터리의 `data` 디렉터리를 만들고 컨테이너 UID `10001`이 쓸 수 있게 설정한다.
3. NAS 내부망에서만 포트 `8080`에 접근한다. E1에는 인증과 HTTPS가 없다.
4. 외부 백업 대상과 복구 절차가 정해지기 전에는 실제 가족 데이터를 저장하지 않는다.

## Mac에서 NAS 패키지 생성

DS216+II의 1 GB 메모리에서 Node.js와 Go 빌드를 실행하지 않는다. 개발 Mac에서 `linux/amd64` 이미지를 만들고 이미지 보관 파일과 Compose 파일을 NAS로 옮긴다.

```sh
sh deploy/synology/build-package.sh
```

기본 출력 경로는 `runtime/synology-package`이며 Git에서 무시된다.

## NAS에서 실행

패키지 디렉터리를 NAS로 복사한 뒤:

```sh
mkdir -p data
sudo chown 10001:10001 data
docker load -i family-dashboard-e1-amd64.tar
docker compose up -d
sh e1-verify.sh
sh e1-monitor.sh
```

`e1-monitor.sh`는 기본적으로 24시간 동안 60초마다 컨테이너 상태와 자원 사용량을 기록한다. `MONITOR_DURATION_SECONDS`와 `MONITOR_INTERVAL_SECONDS`로 시험 시간을 조정할 수 있다. 결과는 `docs/up/02-elaboration/e1-verification.md`에 옮겨 적는다.
