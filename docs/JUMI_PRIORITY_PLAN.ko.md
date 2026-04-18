# JUMI Priority Plan For `go-grpc-kit`

> 작성일: 2026-04-18
> 목적: `go-grpc-kit`를 JUMI의 내부 gRPC/mTLS 보조 계층으로 활용할 수 있을지 판단하고, 필요한 우선순위를 고정한다.

---

## 1. 현재 판단

`go-grpc-kit`의 핵심 가치는 gRPC transport security와 개발용 TLS/mTLS 유틸에 있다.

현재 확인된 유용한 축은 아래와 같다.

- client TLS / mTLS dial option
- server TLS / mTLS option
- health / reflection registration helper
- 개발용 self-signed certificate 생성 유틸

반면 현재 상태 그대로는 JUMI의 기반 라이브러리로 쓰기 어렵다.

- `go test ./...`가 깨진다.
- example/main 경로가 깨져 있다.
- 일부 옵션이 panic 기반이다.
- 테스트가 connection state 가정에 너무 강하게 묶여 있다.

이 문서는 JUMI 관점에서 `go-grpc-kit`의 보강 우선순위를 정리한다.

---

## 2. 최우선 원칙

`go-grpc-kit`가 JUMI에 기여하려면 아래 원칙을 먼저 만족해야 한다.

- library는 panic 대신 error를 반환해야 한다.
- build/test가 안정적으로 통과해야 한다.
- transport helper는 app semantics와 분리되어야 한다.
- JUMI는 gRPC 계약을 직접 소유하고, `go-grpc-kit`는 transport helper에 머물러야 한다.

---

## 3. 우선순위

### Priority 0. broken baseline 복구

목표:

- 저장소가 최소한 빌드/테스트 가능한 상태가 되도록 만든다.

해야 할 일:

- `main.go`의 깨진 `PeerNode` 시작 경로 수정 또는 제거
- 현재 실패하는 client/server 테스트 수정
- README의 현재 상태와 실제 코드 상태를 맞춤

완료 기준:

- `go test ./...`가 통과한다.
- example 또는 main path가 거짓 상태가 아니다.

### Priority 1. panic 제거

목표:

- TLS/mTLS 옵션이 library답게 error 기반으로 동작하도록 만든다.

해야 할 일:

- `WithTLS`, `WithMTLS`, server-side TLS helper에서 panic/log.Fatalf 제거
- option 생성과 client/server 생성 시점의 에러 전달 구조 정리
- 잘못된 cert/key/ca 파일에 대한 테스트 추가

완료 기준:

- 잘못된 입력이 프로세스 종료 대신 에러로 surface된다.

### Priority 2. transport helper 역할 고정

목표:

- 이 저장소가 app framework가 아니라 transport security helper라는 경계를 분명히 한다.

해야 할 일:

- `PeerNode`의 역할 재검토
- app registration과 transport helper 책임 분리
- health/reflection helper는 유지하되, business service 의존은 제거
- README에 non-goal 명시

완료 기준:

- JUMI가 이 저장소를 써도 service contract ownership이 흐려지지 않는다.

### Priority 3. mTLS/dev certificate usability 개선

목표:

- JUMI의 in-cluster gRPC 실험 및 개발 환경에서 재사용 가능한 수준으로 만든다.

해야 할 일:

- dev certificate 생성/저장 유틸 문서화
- server/client 예제 정리
- SAN, server name, cert rotation 가정 명시
- Cilium mesh 환경과 충돌하지 않는 사용 가이드 정리

완료 기준:

- 개발 환경에서 mTLS 실험을 반복 가능하게 수행할 수 있다.

### Priority 4. JUMI 적합성 판단

목표:

- JUMI가 이 저장소를 직접 의존할지, 일부 코드만 차용할지 판단한다.

판단 기준:

- helper만 필요하면 부분 차용이 더 낫다.
- 라이브러리 안정화 비용이 높으면 JUMI 내부 transport package를 직접 두는 편이 낫다.

현재 임시 판단:

- JUMI는 gRPC service contract를 직접 소유해야 한다.
- `go-grpc-kit`는 transport 보조 유틸 정도로만 보는 것이 안전하다.

---

## 4. JUMI 기준 사용 계획

지금 시점에서 JUMI가 참고하거나 부분 차용할 가치가 있는 부분은 아래와 같다.

- `client/client.go`의 TLS/mTLS dial 방향성
- `server/server.go`의 TLS/mTLS server option 방향성
- `utils/devtls.go`의 dev certificate 생성 유틸

지금 시점에서 기준선으로 삼기 어려운 부분은 아래와 같다.

- `main.go`
- `peernode` 중심 구조
- 현재 실패 중인 client/server 테스트 상태

---

## 5. 즉시 실행 순서

1. build/test 복구
2. panic/log.Fatalf 제거
3. transport helper 역할 재정의
4. mTLS/devtls 사용성 정리
5. JUMI 직접 의존 여부 재판단

---

## 6. 한 줄 결론

`go-grpc-kit`는 JUMI의 gRPC/mTLS 참고 구현으로는 유용하지만, 지금 단계에서 먼저 필요한 일은 transport helper답게 안정화하는 것이다.
