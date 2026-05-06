package utils

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// Option configures certificate generation parameters.
type Option func(*options)

type options struct {
	// common fields
	keySize            int
	signatureAlgorithm x509.SignatureAlgorithm
	serialNumber       *big.Int
	subject            pkix.Name
	dnsNames           []string
	ipAddresses        []net.IP
	// leaf-specific
	keyUsage    x509.KeyUsage
	extKeyUsage []x509.ExtKeyUsage
	isCA        bool
}

// WithKeySize sets the RSA key size (in bits).
func WithKeySize(size int) Option {
	return func(o *options) {
		o.keySize = size
	}
}

// WithIsCA marks the certificate as a CA (Certificate Authority).
func WithIsCA(isCA bool) Option {
	return func(o *options) {
		o.isCA = isCA
	}
}

// WithSignatureAlgorithm sets the X.509 signature algorithm.
func WithSignatureAlgorithm(algo x509.SignatureAlgorithm) Option {
	return func(o *options) {
		o.signatureAlgorithm = algo
	}
}

// WithSerialNumber supplies a custom serial number for the certificate.
func WithSerialNumber(sn *big.Int) Option {
	return func(o *options) {
		o.serialNumber = sn
	}
}

// WithSubject sets the certificate's subject (CommonName, Organization, etc.).
func WithSubject(subject pkix.Name) Option {
	return func(o *options) {
		o.subject = subject
	}
}

// WithDNSNames adds DNS names to the certificate's SAN extension.
func WithDNSNames(names ...string) Option {
	return func(o *options) {
		o.dnsNames = names
	}
}

// WithIPAddresses adds IP addresses to the certificate's SAN extension.
func WithIPAddresses(ips ...net.IP) Option {
	return func(o *options) {
		o.ipAddresses = ips
	}
}

// WithKeyUsage sets KeyUsage for leaf certificates.
func WithKeyUsage(usage x509.KeyUsage) Option {
	return func(o *options) {
		o.keyUsage = usage
	}
}

// WithExtKeyUsage sets ExtKeyUsage for leaf certificates.
func WithExtKeyUsage(usages ...x509.ExtKeyUsage) Option {
	return func(o *options) {
		o.extKeyUsage = usages
	}
}

// internal helper to generate a certificate template and private key.
func makeCert(parent *x509.Certificate, parentKey *rsa.PrivateKey, validFor time.Duration, opts ...Option) (*x509.Certificate, *rsa.PrivateKey, []byte, error) {
	o := &options{
		keySize:            2048,
		signatureAlgorithm: x509.SHA256WithRSA,
		subject:            pkix.Name{CommonName: "localhost"},
		dnsNames:           []string{"localhost"},
		keyUsage:           x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		extKeyUsage:        nil,
	}
	// if parent is self (CA), mark isCA and adjust defaults
	if parent == nil {
		o.isCA = true
		o.keyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		o.subject = pkix.Name{CommonName: "Dev CA"}
		o.extKeyUsage = nil // CA certs typically don't have ExtKeyUsage
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.keySize < 2048 {
		return nil, nil, nil, fmt.Errorf("RSA key size must be at least 2048 bits, got %d", o.keySize)
	}

	priv, err := rsa.GenerateKey(rand.Reader, o.keySize)
	if err != nil {
		return nil, nil, nil, err
	}
	// serial number
	var serial *big.Int
	if o.serialNumber != nil {
		serial = o.serialNumber
	} else {
		// If it's a CA, generate a large random serial number
		if o.isCA {
			maX := new(big.Int).Lsh(big.NewInt(1), 128)
			serial, err = rand.Int(rand.Reader, maX)
			if err != nil {
				return nil, nil, nil, err
			}
		} else {
			// MODIFIED: For leaf certificates, also generate a large random serial number
			// for more robust uniqueness, similar to CA serials.
			maX := new(big.Int).Lsh(big.NewInt(1), 128)
			serial, err = rand.Int(rand.Reader, maX)
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               o.subject,
		NotBefore:             now,
		NotAfter:              now.Add(validFor),
		KeyUsage:              o.keyUsage,
		ExtKeyUsage:           o.extKeyUsage,
		BasicConstraintsValid: true,
		IsCA:                  o.isCA,
		SignatureAlgorithm:    o.signatureAlgorithm,
		DNSNames:              o.dnsNames,
		IPAddresses:           o.ipAddresses,
	}
	// self-sign if CA
	parentCert := tmpl
	parentPriv := priv
	if !o.isCA {
		parentCert = parent
		parentPriv = parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &priv.PublicKey, parentPriv)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	return cert, priv, der, nil
}

// GenerateSelfSignedCA creates a self-signed CA certificate and returns it with its private key.
func GenerateSelfSignedCA(validFor time.Duration, opts ...Option) (*x509.Certificate, *rsa.PrivateKey, error) {
	cert, priv, _, err := makeCert(nil, nil, validFor, opts...)
	if err != nil {
		return nil, nil, err
	}
	return cert, priv, nil
}

// GenerateCert creates and signs a leaf certificate using a given CA certificate/key.
// caCert and caKey must not be nil, and caCert must be a CA certificate.
// Provide WithExtKeyUsage to define the certificate's purpose (ServerAuth, ClientAuth, etc.).
func GenerateCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, validFor time.Duration, opts ...Option) (*tls.Certificate, error) {
	if caCert == nil {
		return nil, errors.New("ca certificate is nil")
	}
	if caKey == nil {
		return nil, errors.New("ca private key is nil")
	}
	if !caCert.IsCA {
		return nil, errors.New("ca certificate is not a CA")
	}
	cert, priv, der, err := makeCert(caCert, caKey, validFor, opts...)
	if err != nil {
		return nil, err
	}
	tlsCert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        cert,
	}
	return &tlsCert, nil
}

// GenerateServerCert creates a server-only certificate signed by the given CA.
// host may be a DNS name or an IP address; the SAN is set accordingly.
func GenerateServerCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, host string, validFor time.Duration) (*tls.Certificate, error) {
	opts := []Option{
		WithSubject(pkix.Name{CommonName: host}),
		WithKeyUsage(x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment),
		WithExtKeyUsage(x509.ExtKeyUsageServerAuth),
	}
	if ip := net.ParseIP(host); ip != nil {
		opts = append(opts, WithDNSNames(), WithIPAddresses(ip))
	} else {
		opts = append(opts, WithDNSNames(host))
	}
	return GenerateCert(caCert, caKey, validFor, opts...)
}

// GenerateClientCert creates a client-only certificate signed by the given CA.
func GenerateClientCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, clientName string, validFor time.Duration) (*tls.Certificate, error) {
	return GenerateCert(caCert, caKey, validFor,
		WithSubject(pkix.Name{CommonName: clientName}),
		WithKeyUsage(x509.KeyUsageDigitalSignature),
		WithExtKeyUsage(x509.ExtKeyUsageClientAuth),
		WithDNSNames(), // override default SAN
	)
}

// WriteCertPEM writes a certificate DER to a PEM file (mode 0644).
func WriteCertPEM(filename string, certDER []byte) error {
	buf := new(bytes.Buffer)
	if err := pem.Encode(buf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}
	return os.WriteFile(filename, buf.Bytes(), 0644)
}

// WriteKeyPEM writes an RSA private key to a PEM file (mode 0600).
func WriteKeyPEM(filename string, key *rsa.PrivateKey) error {
	buf := new(bytes.Buffer)
	if err := pem.Encode(buf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return err
	}
	return os.WriteFile(filename, buf.Bytes(), 0600)
}

// SavePEM writes DER bytes and private key to cert and key PEM files.
func SavePEM(certPath, keyPath string, certDER []byte, key *rsa.PrivateKey) error {
	if err := WriteCertPEM(certPath, certDER); err != nil {
		return err
	}
	return WriteKeyPEM(keyPath, key)
}
