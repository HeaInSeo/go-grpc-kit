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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// healthServerImpl implements the gRPC Health service for tests.
type healthServerImpl struct{}

func (s *healthServerImpl) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (s *healthServerImpl) Watch(_ *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	return status.Errorf(codes.Unimplemented, "Watch not implemented")
}

// startPlainServer starts a plaintext gRPC server on a free port and returns it with its address.
func startPlainServer(t *testing.T) (*grpc.Server, string) {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	opts := server.DefaultServerOptions()
	grpcSrv := grpc.NewServer(opts...)
	server.WithHealthCheck()(grpcSrv)
	go func() { _ = grpcSrv.Serve(lis) }()
	return grpcSrv, lis.Addr().String()
}

// startMTLSServer starts a gRPC server with real mTLS (RequireAndVerifyClientCert).
// Returns the server, CA cert, CA key, address, and a channel for serve errors.
func startMTLSServer(t *testing.T) (*grpc.Server, *x509.Certificate, *rsa.PrivateKey, string, <-chan error) {
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

	grpcSrv := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))
	grpc_health_v1.RegisterHealthServer(grpcSrv, &healthServerImpl{})

	errCh := make(chan error, 1)
	go func() { errCh <- grpcSrv.Serve(lis) }()

	return grpcSrv, caCert, caKey, lis.Addr().String(), errCh
}

// clientTLSConfig builds a tls.Config for connecting to the mTLS server.
func clientTLSConfig(caCert *x509.Certificate, clientCert *tls.Certificate) *tls.Config {
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(caCert)
	cfg := &tls.Config{
		RootCAs:    rootCAs,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}
	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}
	return cfg
}

// TestServerHealth verifies plaintext health check over insecure gRPC.
func TestServerHealth(t *testing.T) {
	grpcSrv, address := startPlainServer(t)
	defer grpcSrv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := grpc_health_v1.NewHealthClient(conn).Check(
		context.Background(), &grpc_health_v1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING, got %v", resp.Status)
	}
}

// TestServerHealth_MTLS_Success verifies mTLS connection with a valid client cert.
func TestServerHealth_MTLS_Success(t *testing.T) {
	srv, caCert, caKey, address, errCh := startMTLSServer(t)
	defer func() {
		srv.GracefulStop()
		if err := <-errCh; err != nil && err != grpc.ErrServerStopped {
			t.Errorf("server shutdown error: %v", err)
		}
	}()

	clientCert, err := utils.GenerateClientCert(caCert, caKey, "client", time.Hour)
	if err != nil {
		t.Fatalf("client cert generation failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLSConfig(caCert, clientCert))),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("mTLS dial failed: %v", err)
	}
	defer conn.Close()

	resp, err := grpc_health_v1.NewHealthClient(conn).Check(
		context.Background(), &grpc_health_v1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("Health.Check failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING, got %v", resp.Status)
	}
}

// TestServerHealth_MTLS_NoClientCert verifies the server rejects connections without a client cert.
func TestServerHealth_MTLS_NoClientCert(t *testing.T) {
	srv, caCert, _, address, errCh := startMTLSServer(t)
	defer func() {
		srv.GracefulStop()
		<-errCh
	}()

	// TLS config with no client certificate
	tlsCfg := clientTLSConfig(caCert, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err == nil {
		t.Fatal("expected connection to fail without client cert, but it succeeded")
	}
}

// TestServerHealth_MTLS_WrongCA verifies the server rejects a client cert signed by an unknown CA.
func TestServerHealth_MTLS_WrongCA(t *testing.T) {
	srv, caCert, _, address, errCh := startMTLSServer(t)
	defer func() {
		srv.GracefulStop()
		<-errCh
	}()

	// Generate a separate CA and sign a client cert with it
	wrongCA, wrongCAKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("wrong CA generation failed: %v", err)
	}
	wrongClientCert, err := utils.GenerateClientCert(wrongCA, wrongCAKey, "evil-client", time.Hour)
	if err != nil {
		t.Fatalf("wrong client cert generation failed: %v", err)
	}

	// Use server's CA for root (so server cert validates), but present wrong client cert
	tlsCfg := clientTLSConfig(caCert, wrongClientCert)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err == nil {
		t.Fatal("expected connection to fail with wrong CA client cert, but it succeeded")
	}
}

// TestServerHealth_MTLS_WrongServerCA verifies the client rejects a server cert from an unknown CA.
func TestServerHealth_MTLS_WrongServerCA(t *testing.T) {
	srv, caCert, caKey, address, errCh := startMTLSServer(t)
	defer func() {
		srv.GracefulStop()
		<-errCh
	}()

	// Use a different CA as the client's trusted root.
	// Server cert is signed by the real CA, so the client won't trust it.
	wrongCA, wrongCAKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("wrong CA generation failed: %v", err)
	}

	// Client cert must be signed by the real CA so the server accepts it;
	// only the root of trust for the server cert is wrong.
	clientCert, err := utils.GenerateClientCert(caCert, caKey, "client", time.Hour)
	if err != nil {
		t.Fatalf("client cert generation failed: %v", err)
	}
	_ = wrongCAKey // used only to confirm wrongCA is independent

	tlsCfg := clientTLSConfig(wrongCA, clientCert)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err == nil {
		t.Fatal("expected connection to fail with wrong server CA, but it succeeded")
	}
}

// TestRegisterHealth verifies that RegisterHealth returns a *health.Server whose status can be updated.
func TestRegisterHealth(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	opts := server.DefaultServerOptions()
	grpcSrv := grpc.NewServer(opts...)
	h := server.RegisterHealth(grpcSrv)
	go func() { _ = grpcSrv.Serve(lis) }()
	defer grpcSrv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	hc := grpc_health_v1.NewHealthClient(conn)

	// Default status should be SERVING.
	resp, err := hc.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Health.Check failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING, got %v", resp.Status)
	}

	// Update to NOT_SERVING and verify.
	h.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	resp, err = hc.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Health.Check after NOT_SERVING failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Errorf("expected NOT_SERVING, got %v", resp.Status)
	}
}

// TestServerAsync_ErrCh verifies that ServerAsync's error channel closes cleanly on GracefulStop.
func TestServerAsync_ErrCh(t *testing.T) {
	opts := server.DefaultServerOptions()
	grpcSrv, errCh, err := server.ServerAsync("localhost:0", opts, server.WithHealthCheck())
	if err != nil {
		t.Fatalf("ServerAsync failed: %v", err)
	}
	grpcSrv.GracefulStop()

	// errCh must close without sending an error on clean shutdown.
	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			t.Errorf("unexpected error from errCh: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("errCh did not close after GracefulStop")
	}
}
