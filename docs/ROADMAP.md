# go-grpc-kit 개선 로드맵 및 설계 고려사항

> 현재 상태: **v0 — 기능 완성, 운영 배포 전 단계**
> 업데이트: 2026-05-06

---

## 현재 구현 상태 요약

| 영역 | 상태 | 비고 |
|------|------|------|
| 모듈 이름 / 빌드 기준선 | ✅ 완료 | `github.com/HeaInSeo/go-grpc-kit` |
| 서버 TLS / mTLS 옵션 | ✅ 완료 | `WithTLSOption`, `WithMTLSOption` (error 반환) |
| 클라이언트 TLS / mTLS / ServerName | ✅ 완료 | `WithTLS`, `WithMTLS`, `WithServerName`, `WithTLSConfig` |
| 옵션 가드레일 | ✅ 완료 | 중복/잘못된 조합 시 error 반환 |
| mTLS 테스트 (성공 + 실패 케이스) | ✅ 완료 | cert 없음, 잘못된 CA, server CA mismatch |
| Peer identity 추출 | ✅ 완료 | `PeerCertificateFromContext`, `PeerIdentityFromContext`, `RequireClientCN` |
| `RegisterHealth` (상태 변경 가능) | ✅ 완료 | `*health.Server` 반환 |
| `ServerAsync` errCh | ✅ 완료 | `(*grpc.Server, <-chan error, error)` |
| devtls IP SAN 처리 | ✅ 완료 | IP 호스트 → IP SAN, DNS 호스트 → DNS SAN |
| devtls CA nil / isCA 검증 | ✅ 완료 | `GenerateCert` 진입 시 사전 검증 |
| RSA key size 최소 2048 | ✅ 완료 | 2048 미만 시 error |
| server/config global viper 제거 | ✅ 완료 | 로컬 viper 인스턴스 사용 |
| `peernode` 패키지 제거 | ✅ 완료 | transport helper와 app framework 혼용 제거 |

---

## 개선이 필요한 영역

### 1. Cert Rotation / Reloadable TLS Config

**현재 문제**
`WithMTLSOption`, `WithTLSOption`은 서버 기동 시점에 인증서를 한 번만 읽습니다.
인증서 갱신 시 서버를 재시작해야 하며, zero-downtime rotation이 불가능합니다.

**권장 설계**
```go
// tls.Config의 GetCertificate / GetConfigForClient 훅을 활용한 동적 로딩
func WithReloadableMTLS(certFile, keyFile, caFile string) (grpc.ServerOption, error)

// 또는 watcher 기반
type CertWatcher struct { ... }
func NewCertWatcher(certFile, keyFile string) (*CertWatcher, error)
func (w *CertWatcher) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)
```

**참고**: cert-manager + Kubernetes Secret 연동 시 파일이 갱신되므로 `fsnotify` 기반 watcher가 실용적입니다.

---

### 2. SPIFFE / SVID 기반 Peer Authorization

**현재 상태**
`RequireClientCN`은 X.509 CommonName 기반 인가만 지원합니다.
SPIFFE 환경(Istio, SPIRE)에서는 identity가 URI SAN(`spiffe://trust-domain/path`)으로 표현됩니다.

**권장 API**
```go
// URI SAN 기반 인가 인터셉터
func RequireClientSAN(allowedSANs ...string) grpc.UnaryServerInterceptor

// SPIFFE ID 기반 인가 인터셉터
func RequireSPIFFEID(allowedIDs ...string) grpc.UnaryServerInterceptor

// 사용 예
server.WithUnaryInterceptors(
    server.RequireSPIFFEID(
        "spiffe://cluster.local/ns/default/sa/nodevault",
    ),
)
```

---

### 3. `ServerAsync`의 실제 listen 주소 반환

**현재 문제**
`ServerAsync`는 내부에서 `net.Listen("tcp", address)`하지만, `:0`처럼 동적 포트를 사용할 때
실제 바인딩된 주소를 caller가 알 수 없습니다.
테스트에서는 `net.Listen` + `grpc.NewServer` + `grpc.Serve`를 직접 조합해야 하는 이유입니다.

**권장 API 변경**
```go
type RunningServer struct {
    Server *grpc.Server
    Addr   net.Addr
    ErrCh  <-chan error
}

func ServerAsync(address string, opts []grpc.ServerOption, ...) (*RunningServer, error)
```

이 변경은 breaking change이므로 v1 출시 시점에 함께 적용하는 것이 좋습니다.

---

### 4. Stream interceptor 기반 Peer Authorization

**현재 상태**
`RequireClientCN`은 Unary interceptor만 지원합니다.
Streaming RPC에서는 `grpc.StreamServerInterceptor`가 필요합니다.

**권장 API**
```go
func RequireClientCNStream(allowed ...string) grpc.StreamServerInterceptor
```

---

### 5. `WithBlock()` 재연결 정책

**현재 문제**
`client.Dial`의 `WithBlock()` 구현은 `WaitForStateChange` 루프를 사용합니다.
연결이 `TRANSIENT_FAILURE`로 빠지면 gRPC 내부 backoff 정책에 따라 재시도하지만,
caller가 재시도 정책을 제어할 수 없습니다.

**권장 개선**
```go
client.WithBackoffConfig(grpc.ConnectParams{
    Backoff: backoff.Config{
        BaseDelay:  100 * time.Millisecond,
        MaxDelay:   5 * time.Second,
    },
    MinConnectTimeout: 5 * time.Second,
})
```

또는 `WithBlock()` 내부에서 `TRANSIENT_FAILURE` 상태를 감지해 즉시 반환할지 재시도할지 선택하는 옵션 추가.

---

### 6. `server/config` 설계 개선

**현재 상태**
`LoadServerConfig()`가 호출될 때마다 새로운 viper 인스턴스를 만들고 환경 변수를 읽습니다.
`DefaultServerOptions()` 안에서 매번 호출되므로 비용이 발생합니다.

**권장 방향**
```go
// 명시적 config 주입 방식
type ServerConfig struct {
    MaxRecvMsgSize      int
    MaxSendMsgSize      int
    MaxConcurrentStreams uint32
}

func DefaultServerConfig() ServerConfig  // 기본값만 반환
func DefaultServerOptions(cfg ...ServerConfig) []grpc.ServerOption
```

viper 의존성 자체를 이 패키지에서 제거하고, config 파싱은 앱 쪽에서 담당하게 하는 것이 툴킷 철학에 맞습니다.

---

### 7. `WithInsecure` 네이밍

**현재 상태**
`WithInsecure()`는 gRPC 생태계에서 deprecated 방향으로 가고 있는 명칭입니다.

**권장 변경**
```go
// 명시적으로 의도를 드러내는 이름
func WithPlaintext() Option  // 또는
func WithNoTLS() Option
```

v1 시점에 `WithInsecure`를 deprecated 처리하고 `WithPlaintext`로 전환하는 것을 고려하세요.

---

## 테스트에서 보완이 필요한 케이스

| 테스트 | 현재 상태 | 비고 |
|--------|-----------|------|
| `WithBlock()` + TRANSIENT_FAILURE 재시도 동작 | ❌ 없음 | 재시도 정책 확인 |
| Streaming RPC에서 peer identity 추출 | ❌ 없음 | Stream interceptor 구현 후 추가 |
| Cert rotation 중 연결 유지 | ❌ 없음 | Reloadable TLS 구현 후 추가 |
| expired cert 연결 거부 | ❌ 없음 | devtls의 `validFor` 를 과거로 설정해 테스트 |
| `clientAuth EKU` 없는 인증서 거부 | ❌ 없음 | `WithExtKeyUsage` 생략 시 동작 확인 |

---

## 의존성 고려사항

| 항목 | 현재 | 고려 방향 |
|------|------|-----------|
| `github.com/spf13/viper` | server/config에서 사용 | config 패키지 개선 시 제거 가능 |
| `google.golang.org/grpc v1.67.3` | 사용 중 | grpc-go는 빠르게 변경되므로 주기적 업데이트 필요 |
| RSA only | devtls | ECDSA P-256 / Ed25519 지원 추가 시 성능/크기 개선 |

---

## v1 출시 전 체크리스트

- [ ] `ServerAsync` → `RunningServer` 반환으로 breaking change 적용
- [ ] `WithInsecure` → `WithPlaintext` deprecation 처리
- [ ] `RequireClientSAN` / `RequireSPIFFEID` 구현
- [ ] Reloadable TLS config 구현
- [ ] server/config에서 viper 의존성 제거
- [ ] Stream interceptor 기반 peer authorization
- [ ] ECDSA / Ed25519 cert 지원 (devtls)
- [ ] expired cert / EKU 없는 cert 거부 테스트 추가
