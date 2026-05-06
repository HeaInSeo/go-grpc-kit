package server

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// PeerCertificateFromContext extracts the first verified client certificate from the incoming
// gRPC connection. Only populated when the server is configured with mTLS
// (tls.RequireAndVerifyClientCert). Returns an error if no cert is available.
func PeerCertificateFromContext(ctx context.Context) (*x509.Certificate, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("no peer info in context")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, errors.New("peer auth info is not TLS")
	}
	chains := tlsInfo.State.VerifiedChains
	if len(chains) == 0 || len(chains[0]) == 0 {
		return nil, errors.New("no verified client certificate in TLS state")
	}
	return chains[0][0], nil
}

// PeerIdentity holds identity fields extracted from a client certificate.
type PeerIdentity struct {
	CommonName string
	DNSNames   []string
	URIs       []string // URI SANs, e.g. SPIFFE IDs (spiffe://trust-domain/path)
}

// PeerIdentityFromContext extracts identity information from the client certificate in context.
// Returns an error under the same conditions as PeerCertificateFromContext.
func PeerIdentityFromContext(ctx context.Context) (*PeerIdentity, error) {
	cert, err := PeerCertificateFromContext(ctx)
	if err != nil {
		return nil, err
	}
	uris := make([]string, 0, len(cert.URIs))
	for _, u := range cert.URIs {
		uris = append(uris, u.String())
	}
	return &PeerIdentity{
		CommonName: cert.Subject.CommonName,
		DNSNames:   cert.DNSNames,
		URIs:       uris,
	}, nil
}

// RequireClientCN returns a gRPC unary interceptor that rejects requests whose client
// certificate CommonName is not in the allowed list.
// Must be used on a server configured with mTLS (RequireAndVerifyClientCert).
func RequireClientCN(allowed ...string) grpc.UnaryServerInterceptor {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, cn := range allowed {
		allowedSet[cn] = struct{}{}
	}
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		identity, err := PeerIdentityFromContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("authorization failed: %w", err)
		}
		if _, ok := allowedSet[identity.CommonName]; !ok {
			return nil, fmt.Errorf("authorization failed: client CN %q is not allowed", identity.CommonName)
		}
		return handler(ctx, req)
	}
}
