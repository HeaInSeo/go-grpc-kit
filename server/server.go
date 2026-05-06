package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"

	globallog "github.com/HeaInSeo/go-grpc-kit/log"
	"github.com/HeaInSeo/go-grpc-kit/server/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

var logger = globallog.Log

func init() {
	// TODO: Prometheus 적용 예정
}

// RegisterServices is a callback that registers one or more gRPC services on a server.
type RegisterServices func(*grpc.Server)

// WithUnaryInterceptors registers additional Unary interceptors.
func WithUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.ServerOption {
	return grpc.ChainUnaryInterceptor(interceptors...)
}

// WithStreamInterceptors registers additional Stream interceptors.
func WithStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) grpc.ServerOption {
	return grpc.ChainStreamInterceptor(interceptors...)
}

// DefaultServerOptions returns base grpc.ServerOptions with logging interceptors and message size limits.
func DefaultServerOptions(opts ...grpc.ServerOption) []grpc.ServerOption {
	cfg := config.LoadServerConfig()
	base := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(loggingInterceptor),
		grpc.ChainStreamInterceptor(streamLoggingInterceptor),
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxSendMsgSize),
		grpc.MaxConcurrentStreams(cfg.MaxConcurrentStreams),
	}
	return append(base, opts...)
}

// WithTLSOption loads server TLS credentials from files and returns a ServerOption.
func WithTLSOption(certFile, keyFile string) (grpc.ServerOption, error) {
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
	}
	return grpc.Creds(creds), nil
}

// WithMTLSOption loads mTLS credentials (server cert + CA for client verification) and returns a ServerOption.
func WithMTLSOption(certFile, keyFile, caFile string) (grpc.ServerOption, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server key pair: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
		return nil, errors.New("failed to append CA cert to pool")
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS12,
	}
	return grpc.Creds(credentials.NewTLS(tlsConfig)), nil
}

// RegisterHealth registers the gRPC Health service and returns the *health.Server so callers
// can update serving status at runtime (e.g. STARTING → SERVING → NOT_SERVING on shutdown).
func RegisterHealth(grpcServer *grpc.Server) *health.Server {
	h := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, h)
	h.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	return h
}

// WithHealthCheck returns a RegisterServices callback that registers the gRPC Health service.
// Use RegisterHealth instead when you need to update the serving status at runtime.
func WithHealthCheck() RegisterServices {
	return func(grpcServer *grpc.Server) {
		RegisterHealth(grpcServer)
	}
}

// WithReflection returns a RegisterServices callback that registers gRPC reflection (dev/test only).
func WithReflection() RegisterServices {
	return func(grpcServer *grpc.Server) {
		reflection.Register(grpcServer)
	}
}

// Server starts a blocking gRPC server on the given address.
func Server(address string, opts []grpc.ServerOption, registerServices ...RegisterServices) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}
	grpcServer := grpc.NewServer(opts...)
	for _, reg := range registerServices {
		reg(grpcServer)
	}
	serveErr := grpcServer.Serve(lis)
	if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
		return serveErr
	}
	return nil
}

// ServerAsync starts a non-blocking gRPC server.
// Returns the *grpc.Server for lifecycle control and a channel that receives any serve error.
// The channel is closed when the server exits cleanly (GracefulStop/Stop).
func ServerAsync(address string, opts []grpc.ServerOption, registerServices ...RegisterServices) (*grpc.Server, <-chan error, error) {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %s: %w", address, err)
	}
	grpcServer := grpc.NewServer(opts...)
	for _, reg := range registerServices {
		reg(grpcServer)
	}
	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
		}
		close(errCh)
	}()
	return grpcServer, errCh, nil
}

func loggingInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	logger.Infof("Received request for %s", info.FullMethod)
	resp, err := handler(ctx, req)
	if err != nil {
		logger.Warnf("Method %s error: %v", info.FullMethod, err)
	}
	return resp, err
}

func streamLoggingInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	logger.Infof("[Stream] Start - %s", info.FullMethod)
	err := handler(srv, ss)
	if err != nil {
		logger.Warnf("[Stream] Method %s error: %v", info.FullMethod, err)
	}
	return err
}
