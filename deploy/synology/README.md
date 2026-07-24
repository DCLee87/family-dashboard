# Synology E1 검증

이 디렉터리는 DS216+II에서 E1 최소 런타임을 검증하기 위한 보조 도구다. 실제 가족 데이터를 넣기 전에 테스트 데이터로만 실행한다.

## 사전 준비

1. Container Manager에서 Compose 프로젝트를 실행할 수 있어야 한다.
2. 저장소의 `runtime/data` 디렉터리를 만들고 컨테이너 UID `10001`이 쓸 수 있게 설정한다.
3. NAS 내부망에서만 포트 `8080`에 접근한다. E1에는 인증과 HTTPS가 없다.
4. 외부 백업 대상과 복구 절차가 정해지기 전에는 실제 가족 데이터를 저장하지 않는다.

## 기본 검증

```sh
docker compose up -d --build
sh deploy/synology/e1-verify.sh
sh deploy/synology/e1-monitor.sh
```

`e1-monitor.sh`는 중단할 때까지 60초마다 컨테이너 상태와 자원 사용량을 기록한다. 24시간 시험 결과는 `docs/up/02-elaboration/e1-verification.md`에 옮겨 적는다.
