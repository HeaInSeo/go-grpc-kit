package server_test

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/HeaInSeo/go-grpc-kit/server"
	"github.com/HeaInSeo/go-grpc-kit/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// captureServer records the PeerIdentity seen during each RPC.
type captureServer struct {
	lastIdentity *server.PeerIdentity
	lastErr      error
}

func (s *captureServer) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	identity, err := server.PeerIdentityFromContext(ctx)
	s.lastIdentity = identity
	s.lastErr = err
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (s *captureServer) Watch(_ *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	return status.Errorf(codes.Unimplemented, "Watch not implemented")
}

// startCaptureServer starts a real mTLS server that captures peer identity, optionally with an interceptor.
func startCaptureServer(t *testing.T, interceptor grpc.UnaryServerInterceptor) (*captureServer, *x509.Certificate, *rsa.PrivateKey, string, func()) {
	t.Helper()

	caCert, caKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("CA generation failed: %v", err)
	}
	serverCert, err := utils.GenerateServerCert(caCert, caKey, "localhost", time.Hour)
	if err != nil {
		t.Fatalf("server cert generation failed: %v", err)
	}

	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(caCert)
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS12,
	}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}

	srvOpts := []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}
	if interceptor != nil {
		srvOpts = append(srvOpts, grpc.ChainUnaryInterceptor(interceptor))
	}

	grpcSrv := grpc.NewServer(srvOpts...)
	svc := &captureServer{}
	grpc_health_v1.RegisterHealthServer(grpcSrv, svc)
	go func() { _ = grpcSrv.Serve(lis) }()

	addr := lis.Addr().String()
	stop := func() { grpcSrv.GracefulStop() }
	return svc, caCert, caKey, addr, stop
}

// dialWithClientCN generates a client cert with the given CN and dials the mTLS server.
func dialWithClientCN(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, address, cn string) (*grpc.ClientConn, error) {
	t.Helper()

	clientCert, err := utils.GenerateClientCert(caCert, caKey, cn, time.Hour)
	if err != nil {
		t.Fatalf("client cert generation failed: %v", err)
	}

	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(caCert)
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*clientCert},
		RootCAs:      rootCAs,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
}

// TestPeerIdentityFromContext verifies that the server can extract the client CN from context.
func TestPeerIdentityFromContext(t *testing.T) {
	svc, caCert, caKey, addr, stop := startCaptureServer(t, nil)
	defer stop()

	conn, err := dialWithClientCN(t, caCert, caKey, addr, "my-service")
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	_, err = grpc_health_v1.NewHealthClient(conn).Check(
		context.Background(), &grpc_health_v1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("RPC failed: %v", err)
	}

	if svc.lastErr != nil {
		t.Fatalf("PeerIdentityFromContext returned error: %v", svc.lastErr)
	}
	if svc.lastIdentity == nil {
		t.Fatal("PeerIdentity is nil")
	}
	if svc.lastIdentity.CommonName != "my-service" {
		t.Errorf("expected CN %q, got %q", "my-service", svc.lastIdentity.CommonName)
	}
}

// TestRequireClientCN_Allowed verifies that an allowed CN passes the interceptor.
func TestRequireClientCN_Allowed(t *testing.T) {
	interceptor := server.RequireClientCN("allowed-service", "other-allowed")
	_, caCert, caKey, addr, stop := startCaptureServer(t, interceptor)
	defer stop()

	conn, err := dialWithClientCN(t, caCert, caKey, addr, "allowed-service")
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	_, err = grpc_health_v1.NewHealthClient(conn).Check(
		context.Background(), &grpc_health_v1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("RPC with allowed CN should succeed, got: %v", err)
	}
}

// TestRequireClientCN_Denied verifies that a disallowed CN is rejected by the interceptor.
func TestRequireClientCN_Denied(t *testing.T) {
	interceptor := server.RequireClientCN("allowed-service")
	_, caCert, caKey, addr, stop := startCaptureServer(t, interceptor)
	defer stop()

	conn, err := dialWithClientCN(t, caCert, caKey, addr, "evil-service")
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	_, err = grpc_health_v1.NewHealthClient(conn).Check(
		context.Background(), &grpc_health_v1.HealthCheckRequest{},
	)
	if err == nil {
		t.Fatal("RPC with denied CN should fail, but it succeeded")
	}
	if code := status.Code(err); code != codes.Unknown {
		t.Logf("RPC rejected with code %v (expected Unknown/authorization error): %v", code, err)
	}
}

// TestPeerCertificateFromContext_NoPeer verifies that PeerCertificateFromContext returns an error
// when the context carries no peer info (e.g. not called from a gRPC handler).
func TestPeerCertificateFromContext_NoPeer(t *testing.T) {
	_, err := server.PeerCertificateFromContext(context.Background())
	if err == nil {
		t.Fatal("expected error for context with no peer info, got nil")
	}
}

// TestPeerIdentityFromContext_NoPeer verifies that PeerIdentityFromContext returns an error
// when the context carries no peer info.
func TestPeerIdentityFromContext_NoPeer(t *testing.T) {
	_, err := server.PeerIdentityFromContext(context.Background())
	if err == nil {
		t.Fatal("expected error for context with no peer info, got nil")
	}
}
