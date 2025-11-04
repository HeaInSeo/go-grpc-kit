package peernode

import (
	"context"
	"fmt"
	"log"

	"github.com/seoyhaein/go-grpc-kit/client"
	globallog "github.com/seoyhaein/go-grpc-kit/log"
	"github.com/seoyhaein/go-grpc-kit/server"
	"google.golang.org/grpc"
)

var logger = globallog.Log

// PeerNode encapsulates both gRPC server and client roles for P2P communication.
type PeerNode struct {
	Name             string
	Address          string
	Peers            []string                  // peer addresses
	ServerOptions    []grpc.ServerOption       // server-side options
	ClientOptions    []client.Option           // client-side dialing options
	registerServices []server.RegisterServices // gRPC services to register
}

// NewPeerNode constructs a PeerNode with given configurations.
func NewPeerNode(name, addr string, peers []string, serverOpts []grpc.ServerOption, clientOpts []client.Option, registerServices ...server.RegisterServices) *PeerNode {
	return &PeerNode{
		Name:             name,
		Address:          addr,
		Peers:            peers,
		ServerOptions:    serverOpts,
		ClientOptions:    clientOpts,
		registerServices: registerServices,
	}
}

func (n *PeerNode) ServerStart() error {
	if len(n.registerServices) == 0 {
		return fmt.Errorf("no services registered for PeerNode %s", n.Name)
	}
	go func() {
		if err := server.Server(n.Address, n.ServerOptions, n.registerServices...); err != nil {
			logger.Fatalf("PeerNode[%s] server error: %v", n.Name, err)
		}
	}()
	logger.Printf("PeerNode[%s] gRPC server started at %s", n.Name, n.Address)
	return nil
}

// ServerStartAsync 비동기로 gRPC 서버를 띄우고 *grpc.Server 리턴
func (n *PeerNode) ServerStartAsync() (*grpc.Server, error) {
	if len(n.registerServices) == 0 {
		return nil, fmt.Errorf("no services registered for PeerNode %s", n.Name)
	}

	grpcServer, err := server.ServerAsync(n.Address, n.ServerOptions, n.registerServices...)
	if err != nil {
		return nil, fmt.Errorf("PeerNode[%s] async start failed: %w", n.Name, err)
	}

	logger.Printf("PeerNode[%s] async gRPC server started at %s", n.Name, n.Address)
	return grpcServer, nil
}

// ConnectClients establishes connections to all configured peers.
func (n *PeerNode) ConnectClients(ctx context.Context) []*grpc.ClientConn {
	cons := make([]*grpc.ClientConn, 0, len(n.Peers))
	for _, target := range n.Peers {
		con, err := client.Dial(ctx, target, n.ClientOptions...)
		if err != nil {
			log.Printf("[%s] Dial to %s failed: %v", n.Name, target, err)
			continue
		}
		cons = append(cons, con)
	}
	return cons
}
