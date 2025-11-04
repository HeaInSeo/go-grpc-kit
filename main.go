package main

import (
	"log"

	"github.com/seoyhaein/go-grpc-kit/client"
	"github.com/seoyhaein/go-grpc-kit/peernode"
	"github.com/seoyhaein/go-grpc-kit/server"
)

// main.go
// 테스트 위해서 만들어 둠. 별도 프로젝트는 아직.

func main() {
	// 1) Server-side options (interceptors, limits 등)
	serverOpts := server.DefaultServerOptions()
	// 필요하다면 TLS/mTLS 추가
	// serverOpts = append(serverOpts, server.WithMTLS("certs/server.crt", "certs/server.key", "certs/ca.crt"))

	// 2) Client-side options (Dial 설정)
	clientOpts := []client.Option{client.WithInsecure()}

	// 3) Peer 목록
	peers := []string{"localhost:50052", "localhost:50053"}

	// 4) PeerNode 인스턴스 생성
	var node *peernode.PeerNode
	node = peernode.NewPeerNode(
		"peer1",    // Name
		":50051",   // Address
		peers,      // Peers to connect
		serverOpts, // ServerOptions
		clientOpts, // ClientOptions
		// 서비스 등록 콜백
		server.WithHealthCheck(),
		server.WithReflection(),
		/*func(s *grpc.Server) {
			pb.RegisterPeerServiceServer(s, node)
		},*/
	)

	// 5) gRPC 서버 시작
	if err := node.Start(); err != nil {
		log.Fatalf("failed to start PeerNode: %v", err)
	}
	log.Printf("PeerNode[%s] listening on %s", node.Name, node.Address)

	// 6) (선택) 다른 피어에 연결 테스트
	// time.Sleep(time.Second)
	// conns := node.ConnectAll(context.Background())
	// defer func() {
	// 	for _, c := range conns {
	// 		c.Close()
	// 	}
	// }()

	// 7) 종료 대기
	select {}
}
