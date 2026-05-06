# go-grpc-kit Usage Guide (K8s Data Plane App)

`go-grpc-kit`는 gRPC 서버 및 클라이언트 구성을 위한 강력한 유틸리티(mTLS, 로깅 인터셉터, 최신 gRPC 클라이언트 스펙 대응)를 제공합니다. K8s Data Plane App(예: NodeVault, tori)에서 사용할 때는 본 라이브러리를 **통짜 프레임워크가 아닌 유틸리티 툴킷(Toolkit)**으로 활용하는 것을 강력히 권장합니다.

## 핵심 원칙
- **`server.Server()` 함수 사용 지양**: 내부적으로 `net.Listen`과 `grpc.Serve`를 강제하여 라이프사이클 관리가 유연하지 못합니다. (Graceful Shutdown, 다중 포트 바인딩 불가)
- **직접 `grpc.NewServer` 호출**: `server.DefaultServerOptions()` 및 `server.WithMTLS()` 등의 옵션만 추출하여 K8s App의 `main.go`에서 직접 서버를 구성합니다.

---

## 1. 서버 구성 예시 (Server)

K8s 환경에서는 앱 스스로 제어권을 쥐어야 합니다.

```go
package main

import (
	"log"
	"net"

	kit_server "github.com/HeaInSeo/go-grpc-kit/server"
	"google.golang.org/grpc"
	
	// 프로젝트별 Protobuf Import
	// pb "github.com/seoyhaein/tori/protos/ichthys/v1"
)

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 1. go-grpc-kit 의 장점인 기본 인터셉터(로깅 등) 및 제한 옵션만 골라서 적용
	opts := kit_server.DefaultServerOptions()
	
	// mTLS가 필요하다면 아래 옵션 주석 해제
	// mtlsOpt, err := kit_server.WithMTLSOption("server.crt", "server.key", "ca.crt")
	// if err != nil { log.Fatalf("failed to load mTLS credentials: %v", err) }
	// opts = append(opts, mtlsOpt)

	// 2. 표준 grpc.NewServer 에 옵션 주입
	grpcServer := grpc.NewServer(opts...)

	// 3. HealthCheck 등록 — h를 통해 런타임 상태 변경 가능 (SERVING / NOT_SERVING)
	h := kit_server.RegisterHealth(grpcServer)
	_ = h // e.g. h.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Reflection은 dev/test에서만 활성화할 것. 운영 환경에서는 서비스 구조가 외부에 노출됩니다.
	// kit_server.WithReflection()(grpcServer)

	// 4. 실제 비즈니스 서비스(Tori 등) 등록
	// pb.RegisterDataBlockServiceServer(grpcServer, &myServiceHandler{})

	log.Printf("Starting gRPC server on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
```

---

## 2. 클라이언트 구성 예시 (Client)

Deprecated 된 `grpc.DialContext` 대신, `go-grpc-kit/client`가 제공하는 최신 `grpc.NewClient` 기반의 안전한 블로킹 연결 래퍼(`Dial`)를 사용합니다. 파드(Pod) 기동 시 네트워크 딜레이를 극복하는 모범 사례입니다.

```go
package main

import (
	"context"
	"log"
	"time"

	kit_client "github.com/HeaInSeo/go-grpc-kit/client"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// K8s 내부 통신 시 DNS(Cilium Mesh) 사용
	target := "tori-service.default.svc.cluster.local:50051"

	// go-grpc-kit의 Dial 함수는 내부적으로 grpc.NewClient 및 WaitForStateChange를 사용하여 연결 수립 대기를 완벽히 지원합니다.
	conn, err := kit_client.Dial(ctx, target,
		// 인증서 없는 내부 통신 시
		kit_client.WithInsecure(),

		// mTLS 사용 시
		// kit_client.WithMTLS("client.crt", "client.key", "ca.crt"),

		// K8s 환경에서 IP로 접속하고 cert SAN이 hostname일 때
		// kit_client.WithServerName("nodevault.default.svc.cluster.local"),

		kit_client.WithBlock(), // 연결 상태(Ready)가 될 때까지 대기
	)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// 클라이언트 객체 생성 및 API 호출
	// client := pb.NewDataBlockServiceClient(conn)
	// resp, err := client.GetDataBlock(ctx, req)
}
```

## 장점
이 방식으로 `go-grpc-kit`을 사용하면 K8s 환경에서 다음과 같은 이점을 얻습니다.
1. `main.go`의 제어권을 놓치지 않으므로, 추후 **Graceful Shutdown**이나 HTTP/Metrics 서버를 병렬로 띄우기 용이합니다.
2. `go-grpc-kit` 내부에 잘 캡슐화된 **보안 로직(mTLS)**과 **로깅 인터셉터**를 복사-붙여넣기 없이 안전하게 재사용할 수 있습니다.
