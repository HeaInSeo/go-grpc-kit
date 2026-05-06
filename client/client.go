package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Option defines a functional option for Dial.
type Option func(*dialOptions)

type dialOptions struct {
	dialOpts                []grpc.DialOption
	tlsCfg                  *tls.Config // deferred; finalized in Dial with serverName
	hasTransportCredentials bool
	serverName              string
	err                     error
	block                   bool
}

// WithInsecure disables transport security. Use only in development or testing.
func WithInsecure() Option {
	return func(o *dialOptions) {
		if o.hasTransportCredentials {
			o.err = errors.New("transport credentials already set; cannot combine WithInsecure with other TLS options")
			return
		}
		o.dialOpts = append(o.dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		o.hasTransportCredentials = true
	}
}

// WithTLS configures one-way TLS using the given CA certificate file.
func WithTLS(caFile string) Option {
	return func(o *dialOptions) {
		if o.hasTransportCredentials {
			o.err = errors.New("transport credentials already set; cannot combine multiple TLS options")
			return
		}
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			o.err = fmt.Errorf("failed to read CA certificate: %w", err)
			return
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			o.err = errors.New("failed to append CA certificate to pool")
			return
		}
		o.tlsCfg = &tls.Config{
			RootCAs:    certPool,
			MinVersion: tls.VersionTLS12,
		}
		o.hasTransportCredentials = true
	}
}

// WithMTLS configures mutual TLS using client certificate, key, and CA certificate files.
func WithMTLS(certFile, keyFile, caFile string) Option {
	return func(o *dialOptions) {
		if o.hasTransportCredentials {
			o.err = errors.New("transport credentials already set; cannot combine multiple TLS options")
			return
		}
		clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			o.err = fmt.Errorf("failed to load client key pair: %w", err)
			return
		}
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			o.err = fmt.Errorf("failed to read CA certificate: %w", err)
			return
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			o.err = errors.New("failed to append CA certificate to pool")
			return
		}
		o.tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      certPool,
			MinVersion:   tls.VersionTLS12,
		}
		o.hasTransportCredentials = true
	}
}

// WithTLSConfig injects a pre-built *tls.Config directly, for advanced use cases.
// WithServerName applied after this option will still override ServerName.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(o *dialOptions) {
		if o.hasTransportCredentials {
			o.err = errors.New("transport credentials already set; cannot combine multiple TLS options")
			return
		}
		if cfg == nil {
			o.err = errors.New("tls.Config must not be nil")
			return
		}
		o.tlsCfg = cfg
		o.hasTransportCredentials = true
	}
}

// WithServerName overrides the ServerName used in TLS handshake.
// Useful when dialing by IP address or a hostname that differs from the server cert SAN.
// Must be used together with WithTLS, WithMTLS, or WithTLSConfig.
func WithServerName(name string) Option {
	return func(o *dialOptions) {
		o.serverName = name
	}
}

// WithDialOption allows passing a raw grpc.DialOption for advanced use cases.
func WithDialOption(opt grpc.DialOption) Option {
	return func(o *dialOptions) {
		o.dialOpts = append(o.dialOpts, opt)
	}
}

// WithBlock makes Dial wait until the connection reaches Ready state or the context expires.
func WithBlock() Option {
	return func(o *dialOptions) {
		o.block = true
	}
}

// Dial creates a gRPC ClientConn. Transport credentials must be set explicitly;
// use WithInsecure() for plaintext connections in dev/test.
func Dial(ctx context.Context, target string, opts ...Option) (*grpc.ClientConn, error) {
	dcfg := &dialOptions{}
	for _, opt := range opts {
		opt(dcfg)
		if dcfg.err != nil {
			return nil, dcfg.err
		}
	}

	if !dcfg.hasTransportCredentials {
		return nil, errors.New("no transport credentials set; use WithTLS, WithMTLS, WithTLSConfig, or WithInsecure")
	}

	// WithServerName only makes sense with a TLS config.
	if dcfg.serverName != "" && dcfg.tlsCfg == nil {
		return nil, errors.New("WithServerName requires WithTLS, WithMTLS, or WithTLSConfig; not compatible with WithInsecure")
	}

	// Finalize TLS credentials: apply ServerName override, then build grpc credential.
	if dcfg.tlsCfg != nil {
		if dcfg.serverName != "" {
			dcfg.tlsCfg.ServerName = dcfg.serverName
		}
		dcfg.dialOpts = append(dcfg.dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(dcfg.tlsCfg)))
	}

	cc, err := grpc.NewClient(target, dcfg.dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", target, err)
	}

	cc.Connect()

	if dcfg.block {
		for {
			s := cc.GetState()
			if s == connectivity.Ready {
				break
			}
			if !cc.WaitForStateChange(ctx, s) {
				_ = cc.Close()
				return nil, fmt.Errorf("connection to %s failed: %w", target, ctx.Err())
			}
		}
	}

	return cc, nil
}
