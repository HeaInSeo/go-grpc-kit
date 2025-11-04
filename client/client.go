package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Option defines a functional option for the Dial function
type Option func(*dialOptions)

type dialOptions struct {
	dialOpts []grpc.DialOption
	// TLS configuration (if any)
	tlsCfg *tls.Config
}

// WithInsecure disables transport security (for testing or plaintext communication)
func WithInsecure() Option {
	return func(o *dialOptions) {
		o.dialOpts = append(o.dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
}

// WithTLS configures the client to use TLS with the given CA certificate
// certFile is ignored (client auth not used)
// TODO panic 지우는 것 생각해보자.
func WithTLS(caFile string) Option {
	return func(o *dialOptions) {
		// Load CA cert
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			panic(fmt.Sprintf("failed to read CA certificate: %v", err))
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			panic("failed to append CA certificate to pool")
		}
		// Build TLS config
		tlsCfg := &tls.Config{
			RootCAs:    certPool,
			MinVersion: tls.VersionTLS12,
		}
		creds := credentials.NewTLS(tlsCfg)
		o.dialOpts = append(o.dialOpts, grpc.WithTransportCredentials(creds))
	}
}

// WithMTLS configures mutual TLS using client certificate, key and CA certificate
func WithMTLS(certFile, keyFile, caFile string) Option {
	return func(o *dialOptions) {
		// Load client certificate and key
		clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			panic(fmt.Sprintf("failed to load client key pair: %v", err))
		}
		// Load CA cert
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			panic(fmt.Sprintf("failed to read CA certificate: %v", err))
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			panic("failed to append CA certificate to pool")
		}
		// Build TLS config with mTLS
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      certPool,
			MinVersion:   tls.VersionTLS12,
		}
		creds := credentials.NewTLS(tlsCfg)
		o.dialOpts = append(o.dialOpts, grpc.WithTransportCredentials(creds))
	}
}

// WithDialOption allows passing a raw grpc.DialOption
func WithDialOption(opt grpc.DialOption) Option {
	return func(o *dialOptions) {
		o.dialOpts = append(o.dialOpts, opt)
	}
}

// Dial establishes a gRPC ClientConn using grpc.NewClient instead of DialContext.
// It respects grpc.WithBlock and will wait for readiness using the provided context.
func Dial(ctx context.Context, target string, opts ...Option) (*grpc.ClientConn, error) {
	// Collect options
	dcfg := &dialOptions{}
	for _, opt := range opts {
		opt(dcfg)
	}
	// Default to insecure if no credentials provided
	if len(dcfg.dialOpts) == 0 {
		dcfg.dialOpts = append(dcfg.dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Determine if blocking dial is requested
	block := false
	for _, o := range dcfg.dialOpts {
		// Inspect string repr to detect WithBlock (no other way)
		if fmt.Sprint(o) == "WithBlock()" {
			block = true
			break
		}
	}

	// Create ClientConn with grpc.NewClient
	cc, err := grpc.NewClient(target, dcfg.dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to new client %s: %w", target, err)
	}

	// Kick out of idle mode
	cc.Connect()

	// If blocking dial, wait until ready or context done
	if block {
		// Use context deadline if set, otherwise deadline is ctx
		for {
			s := cc.GetState()
			if s == connectivity.Ready {
				break
			}
			if !cc.WaitForStateChange(ctx, s) {
				return nil, fmt.Errorf("connection failed: %w", ctx.Err())
			}
		}
	}

	return cc, nil
}
