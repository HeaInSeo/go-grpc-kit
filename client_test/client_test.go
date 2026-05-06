package client_test

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HeaInSeo/go-grpc-kit/client"
	"github.com/HeaInSeo/go-grpc-kit/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

// listenLocalhost starts a TCP listener on a free loopback port and returns the listener
// and a dial target of the form "localhost:PORT" (matching "localhost" SAN in test certs).
func listenLocalhost(t *testing.T) (net.Listener, string) {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	return lis, fmt.Sprintf("localhost:%d", port)
}

// TestDialInsecureSuccess verifies that Dial with insecure credentials can connect to a plain gRPC server.
func TestDialInsecureSuccess(t *testing.T) {
	lis, target := listenLocalhost(t)
	srv := grpc.NewServer()
	defer srv.Stop()
	go srv.Serve(lis)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := client.Dial(ctx, target, client.WithInsecure(), client.WithBlock())
	if err != nil {
		t.Fatalf("client.Dial failed: %v", err)
	}
	defer cc.Close()

	if cc.GetState() != connectivity.Ready {
		t.Errorf("expected state READY, got %v", cc.GetState())
	}
}

// TestDialInsecureTimeout verifies that Dial with insecure credentials times out when no server is listening.
func TestDialInsecureTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := client.Dial(ctx, "localhost:9", client.WithInsecure(), client.WithBlock())
	if err == nil {
		t.Fatal("expected error when dialing closed port, got nil")
	}
}

// TestDialNoCredentials verifies that Dial without any transport option returns an error.
func TestDialNoCredentials(t *testing.T) {
	ctx := context.Background()
	_, err := client.Dial(ctx, "localhost:9")
	if err == nil {
		t.Fatal("expected error when no credentials set, got nil")
	}
}

// TestDialDuplicateCredentials verifies that combining WithInsecure and WithTLS returns an error.
func TestDialDuplicateCredentials(t *testing.T) {
	ctx := context.Background()
	_, err := client.Dial(ctx, "localhost:9", client.WithInsecure(), client.WithTLS("/nonexistent/ca.pem"))
	if err == nil {
		t.Fatal("expected error when combining WithInsecure and WithTLS, got nil")
	}
}

// TestDialTLSSuccess verifies that Dial with TLS credentials can connect to a server-TLS-only server.
func TestDialTLSSuccess(t *testing.T) {
	caCert, caKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}
	serverCert, err := utils.GenerateServerCert(caCert, caKey, "localhost", time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	lis, target := listenLocalhost(t)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{*serverCert},
	})))
	defer srv.Stop()
	go srv.Serve(lis)

	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw}), 0644); err != nil {
		t.Fatalf("failed to write CA pem: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := client.Dial(ctx, target, client.WithTLS(caFile), client.WithBlock())
	if err != nil {
		t.Fatalf("TLS Dial failed: %v", err)
	}
	defer cc.Close()

	if cc.GetState() != connectivity.Ready {
		t.Errorf("expected state READY, got %v", cc.GetState())
	}
}

// TestDialMTLSSuccess verifies that Dial with mutual TLS credentials can connect to a mTLS-enabled server.
func TestDialMTLSSuccess(t *testing.T) {
	caCert, caKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}
	serverCert, err := utils.GenerateServerCert(caCert, caKey, "localhost", time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}
	clientCert, err := utils.GenerateClientCert(caCert, caKey, "client", time.Hour)
	if err != nil {
		t.Fatalf("GenerateClientCert failed: %v", err)
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(caCert)
	lis, target := listenLocalhost(t)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{*serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
	})))
	defer srv.Stop()
	go srv.Serve(lis)

	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw}), 0644); err != nil {
		t.Fatalf("failed to write CA pem: %v", err)
	}

	clientKey := clientCert.PrivateKey.(*rsa.PrivateKey)
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCert.Certificate[0]}), 0644); err != nil {
		t.Fatalf("failed to write client cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)}), 0600); err != nil {
		t.Fatalf("failed to write client key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := client.Dial(ctx, target, client.WithMTLS(certFile, keyFile, caFile), client.WithBlock())
	if err != nil {
		t.Fatalf("mTLS Dial failed: %v", err)
	}
	defer cc.Close()

	if cc.GetState() != connectivity.Ready {
		t.Errorf("expected state READY, got %v", cc.GetState())
	}
}

// TestDialWithServerName verifies WithServerName allows dialing by IP when cert SAN is a hostname.
func TestDialWithServerName(t *testing.T) {
	caCert, caKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}
	serverCert, err := utils.GenerateServerCert(caCert, caKey, "localhost", time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	// Listen on 127.0.0.1 explicitly so the dial target is an IP address.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	ipTarget := lis.Addr().String()

	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{*serverCert},
	})))
	defer srv.Stop()
	go srv.Serve(lis)

	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw}), 0644); err != nil {
		t.Fatalf("failed to write CA pem: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := client.Dial(ctx, ipTarget,
		client.WithTLS(caFile),
		client.WithServerName("localhost"),
		client.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Dial with WithServerName failed: %v", err)
	}
	defer cc.Close()

	if cc.GetState() != connectivity.Ready {
		t.Errorf("expected state READY, got %v", cc.GetState())
	}
}

// TestDialWithTLSConfig verifies WithTLSConfig accepts a pre-built *tls.Config.
func TestDialWithTLSConfig(t *testing.T) {
	caCert, caKey, err := utils.GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}
	serverCert, err := utils.GenerateServerCert(caCert, caKey, "localhost", time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	lis, target := listenLocalhost(t)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{*serverCert},
	})))
	defer srv.Stop()
	go srv.Serve(lis)

	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(caCert)
	customCfg := &tls.Config{
		RootCAs:    rootCAs,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := client.Dial(ctx, target, client.WithTLSConfig(customCfg), client.WithBlock())
	if err != nil {
		t.Fatalf("Dial with WithTLSConfig failed: %v", err)
	}
	defer cc.Close()

	if cc.GetState() != connectivity.Ready {
		t.Errorf("expected state READY, got %v", cc.GetState())
	}
}

// --- Guardrail tests: invalid option combinations ---

func TestDial_InsecureAndTLS(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9",
		client.WithInsecure(),
		client.WithTLS("/nonexistent/ca.pem"),
	)
	if err == nil {
		t.Fatal("expected error combining WithInsecure and WithTLS")
	}
}

func TestDial_TLSAndMTLS(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9",
		client.WithTLS("/nonexistent/ca.pem"),
		client.WithMTLS("/a", "/b", "/c"),
	)
	if err == nil {
		t.Fatal("expected error combining WithTLS and WithMTLS")
	}
}

func TestDial_TLSAndTLSConfig(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9",
		client.WithTLS("/nonexistent/ca.pem"),
		client.WithTLSConfig(&tls.Config{}),
	)
	if err == nil {
		t.Fatal("expected error combining WithTLS and WithTLSConfig")
	}
}

func TestDial_InsecureAndTLSConfig(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9",
		client.WithInsecure(),
		client.WithTLSConfig(&tls.Config{}),
	)
	if err == nil {
		t.Fatal("expected error combining WithInsecure and WithTLSConfig")
	}
}

func TestDial_NilTLSConfig(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9",
		client.WithTLSConfig(nil),
	)
	if err == nil {
		t.Fatal("expected error for nil tls.Config")
	}
}

func TestDial_ServerNameWithInsecure(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9",
		client.WithInsecure(),
		client.WithServerName("somehost"),
	)
	if err == nil {
		t.Fatal("expected error combining WithServerName and WithInsecure")
	}
}

func TestDial_NoCredentials(t *testing.T) {
	_, err := client.Dial(context.Background(), "localhost:9")
	if err == nil {
		t.Fatal("expected error when no credentials set")
	}
}
