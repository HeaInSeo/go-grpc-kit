package utils

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGenerateSelfSignedCA tests the generation of a self-signed CA certificate.
func TestGenerateSelfSignedCA(t *testing.T) {
	caCert, caKey, err := GenerateSelfSignedCA(1 * time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}

	if caCert == nil {
		t.Fatal("CA certificate is nil")
	}
	if caKey == nil {
		t.Fatal("CA private key is nil")
	}

	// Verify CA properties
	if !caCert.IsCA {
		t.Errorf("CA certificate is not marked as CA")
	}
	if caCert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("CA certificate does not have KeyUsageCertSign")
	}
	if caCert.Subject.CommonName != "Dev CA" {
		t.Errorf("CA certificate CommonName mismatch, got %s", caCert.Subject.CommonName)
	}
	if time.Now().After(caCert.NotAfter) || time.Now().Before(caCert.NotBefore) {
		t.Errorf("CA certificate validity period is incorrect")
	}

	// Test with options
	customSubject := pkix.Name{CommonName: "Custom CA"}
	caCertWithOptions, caKeyWithOptions, err := GenerateSelfSignedCA(1*time.Hour, WithSubject(customSubject), WithKeySize(2048))
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA with options failed: %v", err)
	}
	if caCertWithOptions.Subject.CommonName != "Custom CA" {
		t.Errorf("CA certificate CommonName with option mismatch, got %s", caCertWithOptions.Subject.CommonName)
	}
	if caKeyWithOptions.PublicKey.N.BitLen() != 2048 {
		t.Errorf("Expected 2048-bit key, got %d-bit", caKeyWithOptions.PublicKey.N.BitLen())
	}
}

// TestGenerateCert tests the generation of a leaf certificate signed by a CA.
func TestGenerateCert(t *testing.T) {
	caCert, caKey, err := GenerateSelfSignedCA(1 * time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}

	// Test basic generation
	cert, err := GenerateCert(caCert, caKey, 1*time.Hour,
		WithSubject(pkix.Name{CommonName: "test-cert"}),
		WithDNSNames("test-cert"),
		WithExtKeyUsage(x509.ExtKeyUsageServerAuth),
	)
	if err != nil {
		t.Fatalf("GenerateCert failed: %v", err)
	}
	if cert == nil || cert.Leaf == nil || cert.PrivateKey == nil {
		t.Fatal("Generated TLS certificate or its components are nil")
	}

	// Verify leaf certificate properties
	if cert.Leaf.IsCA {
		t.Errorf("Leaf certificate is marked as CA")
	}
	if cert.Leaf.Subject.CommonName != "test-cert" {
		t.Errorf("Leaf certificate CommonName mismatch, got %s", cert.Leaf.Subject.CommonName)
	}
	if cert.Leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Errorf("Leaf certificate ExtKeyUsage mismatch")
	}

	// Verify chain of trust (optional but good practice)
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	_, err = cert.Leaf.Verify(x509.VerifyOptions{
		DNSName:     "test-cert", // Assuming CommonName is used as DNSName here
		Roots:       roots,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, // For verification
	})
	if err != nil {
		t.Errorf("Leaf certificate verification failed: %v", err)
	}

	// Test with various options
	customSerial := big.NewInt(12345)
	customDNS := []string{"example.com", "www.example.com"}
	customIP := []net.IP{net.ParseIP("127.0.0.1")}
	customKeyUsage := x509.KeyUsageDigitalSignature
	customExtKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	customSubject := pkix.Name{CommonName: "another-cert", Organization: []string{"MyOrg"}}

	certWithOptions, err := GenerateCert(caCert, caKey, 1*time.Hour,
		WithKeySize(2048),
		WithSignatureAlgorithm(x509.SHA512WithRSA),
		WithSerialNumber(customSerial),
		WithSubject(customSubject),
		WithDNSNames(customDNS...),
		WithIPAddresses(customIP...),
		WithKeyUsage(customKeyUsage),
		WithExtKeyUsage(customExtKeyUsage...),
	)
	if err != nil {
		t.Fatalf("GenerateCert with many options failed: %v", err)
	}

	if certWithOptions.Leaf.SignatureAlgorithm != x509.SHA512WithRSA {
		t.Errorf("SignatureAlgorithm mismatch, got %v", certWithOptions.Leaf.SignatureAlgorithm)
	}
	if certWithOptions.Leaf.SerialNumber.Cmp(customSerial) != 0 {
		t.Errorf("SerialNumber mismatch, got %v", certWithOptions.Leaf.SerialNumber)
	}
	if certWithOptions.Leaf.Subject.CommonName != "another-cert" || certWithOptions.Leaf.Subject.Organization[0] != "MyOrg" {
		t.Errorf("Subject mismatch")
	}
	if len(certWithOptions.Leaf.DNSNames) != 2 || certWithOptions.Leaf.DNSNames[0] != "example.com" {
		t.Errorf("DNSNames mismatch")
	}
	if len(certWithOptions.Leaf.IPAddresses) != 1 || !certWithOptions.Leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Errorf("IPAddresses mismatch")
	}
	if certWithOptions.Leaf.KeyUsage != customKeyUsage {
		t.Errorf("KeyUsage mismatch")
	}
	if len(certWithOptions.Leaf.ExtKeyUsage) != 1 || certWithOptions.Leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("ExtKeyUsage mismatch")
	}
	pub := certWithOptions.PrivateKey.(*rsa.PrivateKey).PublicKey
	if pub.N.BitLen() != 2048 {
		t.Errorf("Expected 2048-bit key, got %d-bit", pub.N.BitLen())
	}
}

// TestGenerateServerCert tests the generation of a server-specific certificate.
func TestGenerateServerCert(t *testing.T) {
	caCert, caKey, err := GenerateSelfSignedCA(1 * time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}

	serverHost := "my-server.com"
	serverCert, err := GenerateServerCert(caCert, caKey, serverHost, 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	if serverCert.Leaf.Subject.CommonName != serverHost {
		t.Errorf("Server certificate CommonName mismatch")
	}
	if len(serverCert.Leaf.DNSNames) != 1 || serverCert.Leaf.DNSNames[0] != serverHost {
		t.Errorf("Server certificate DNSNames mismatch")
	}
	if serverCert.Leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Errorf("Server certificate ExtKeyUsage mismatch")
	}
	if serverCert.Leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 || serverCert.Leaf.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		t.Errorf("Server certificate KeyUsage mismatch")
	}
}

// TestGenerateClientCert tests the generation of a client-specific certificate.
func TestGenerateClientCert(t *testing.T) {
	caCert, caKey, err := GenerateSelfSignedCA(1 * time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}

	clientName := "my-client"
	clientCert, err := GenerateClientCert(caCert, caKey, clientName, 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateClientCert failed: %v", err)
	}

	if clientCert.Leaf.Subject.CommonName != clientName {
		t.Errorf("Client certificate CommonName mismatch")
	}
	if len(clientCert.Leaf.DNSNames) != 0 { // Client certs typically don't need DNSNames
		t.Errorf("Client certificate has unexpected DNSNames: %v", clientCert.Leaf.DNSNames)
	}
	if clientCert.Leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("Client certificate ExtKeyUsage mismatch")
	}
	if clientCert.Leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Errorf("Client certificate KeyUsage mismatch")
	}
}

// TestWriteCertPEM tests writing a certificate to a PEM file.
func TestWriteCertPEM(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")

	_, _, der, err := makeCert(nil, nil, 1*time.Hour, WithIsCA(true)) // Create a dummy DER cert
	if err != nil {
		t.Fatalf("Failed to create dummy cert for testing WriteCertPEM: %v", err)
	}

	err = WriteCertPEM(certPath, der)
	if err != nil {
		t.Fatalf("WriteCertPEM failed: %v", err)
	}

	// Verify file existence and content
	fileInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("Failed to stat certificate file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0644 { // Permissions check
		t.Errorf("Certificate file permissions mismatch, got %o", fileInfo.Mode().Perm())
	}

	content, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("Failed to read certificate file: %v", err)
	}
	block, _ := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Errorf("Decoded block is nil or not of type CERTIFICATE")
	}
	if !bytes.Equal(block.Bytes, der) {
		t.Errorf("Written certificate DER mismatch")
	}
}

// TestWriteKeyPEM tests writing a private key to a PEM file.
func TestWriteKeyPEM(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key for testing WriteKeyPEM: %v", err)
	}

	err = WriteKeyPEM(keyPath, key)
	if err != nil {
		t.Fatalf("WriteKeyPEM failed: %v", err)
	}

	// Verify file existence and content
	fileInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0600 { // Permissions check
		t.Errorf("Key file permissions mismatch, got %o", fileInfo.Mode().Perm())
	}

	content, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}
	block, _ := pem.Decode(content)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Errorf("Decoded block is nil or not of type RSA PRIVATE KEY")
	}
	parsedKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Errorf("Failed to parse private key from PEM: %v", err)
	}
	if parsedKey.N.Cmp(key.N) != 0 { // Simple comparison of public moduli
		t.Errorf("Written private key mismatch")
	}
}

// TestSavePEM tests saving both certificate and key files.
func TestSavePEM(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test_save.crt")
	keyPath := filepath.Join(tmpDir, "test_save.key")

	_, priv, der, err := makeCert(nil, nil, 1*time.Hour, WithIsCA(true)) // Dummy cert/key
	if err != nil {
		t.Fatalf("Failed to create dummy cert/key for testing SavePEM: %v", err)
	}

	err = SavePEM(certPath, keyPath, der, priv)
	if err != nil {
		t.Fatalf("SavePEM failed: %v", err)
	}

	// Verify both files exist (content verification is done by WriteCertPEM/WriteKeyPEM tests)
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Errorf("Certificate file was not created by SavePEM")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Errorf("Key file was not created by SavePEM")
	}
}

// TestGenerateServerCert_IPSAN verifies that an IP address host gets an IP SAN, not a DNS SAN.
func TestGenerateServerCert_IPSAN(t *testing.T) {
	caCert, caKey, err := GenerateSelfSignedCA(time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA failed: %v", err)
	}
	cert, err := GenerateServerCert(caCert, caKey, "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert with IP host failed: %v", err)
	}
	if len(cert.Leaf.IPAddresses) != 1 || cert.Leaf.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("expected IP SAN 127.0.0.1, got IPAddresses=%v DNSNames=%v", cert.Leaf.IPAddresses, cert.Leaf.DNSNames)
	}
	if len(cert.Leaf.DNSNames) != 0 {
		t.Errorf("expected no DNS SANs for IP host, got %v", cert.Leaf.DNSNames)
	}
}

// TestGenerateCert_NilCA verifies that GenerateCert rejects nil CA inputs.
func TestGenerateCert_NilCA(t *testing.T) {
	caCert, caKey, _ := GenerateSelfSignedCA(time.Hour)

	if _, err := GenerateCert(nil, caKey, time.Hour); err == nil {
		t.Error("expected error for nil caCert")
	}
	if _, err := GenerateCert(caCert, nil, time.Hour); err == nil {
		t.Error("expected error for nil caKey")
	}
}

// TestGenerateCert_NonCAIssuer verifies that GenerateCert rejects a non-CA certificate as issuer.
func TestGenerateCert_NonCAIssuer(t *testing.T) {
	caCert, caKey, _ := GenerateSelfSignedCA(time.Hour)
	// Generate a leaf cert, then try to use it as a CA.
	leafCert, err := GenerateServerCert(caCert, caKey, "localhost", time.Hour)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}
	leafX509 := leafCert.Leaf
	_, err = GenerateCert(leafX509, caKey, time.Hour)
	if err == nil {
		t.Error("expected error when using non-CA cert as issuer")
	}
}

// TestWithKeySize_TooSmall verifies that key sizes below 2048 are rejected.
func TestWithKeySize_TooSmall(t *testing.T) {
	_, _, err := GenerateSelfSignedCA(time.Hour, WithKeySize(1024))
	if err == nil {
		t.Error("expected error for 1024-bit key size")
	}
}

// TestErrorPaths tests various error conditions to increase coverage.
func TestErrorPaths(t *testing.T) {
	_, _, _, err := makeCert(nil, nil, 1*time.Hour, WithKeySize(10))
	if err == nil {
		t.Error("makeCert with invalid key size did not return error")
	}

	// Test makeCert with serial number generation error (simulated via rand.Int failure if possible)
	// This is hard to test directly without mocking crypto/rand.
	// Current coverage for serial number error path is through CA serial generation within makeCert.

	// Test makeCert with CreateCertificate error (hard to simulate without breaking templates/keys)
	// This path is usually covered by successful execution.

	// Test makeCert with ParseCertificate error (hard to simulate without malformed DER)
	// This path is usually covered by successful execution.

	// Test WriteCertPEM and WriteKeyPEM with invalid paths/permissions
	badPath := "/nonexistent/path/test.crt" // Assuming this path does not exist and cannot be created
	err = WriteCertPEM(badPath, []byte("dummy"))
	if err == nil {
		t.Errorf("WriteCertPEM with bad path did not return an error")
	}

	badPath = "/nonexistent/path/test.key"
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	err = WriteKeyPEM(badPath, key)
	if err == nil {
		t.Errorf("WriteKeyPEM with bad path did not return an error")
	}
}
