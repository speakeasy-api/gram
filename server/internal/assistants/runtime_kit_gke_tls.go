package assistants

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// newKitGKETLSCertificate creates fresh, runtime-scoped server credentials.
// The self-signed leaf is trusted directly, rather than acting as a CA.
func newKitGKETLSCertificate(serverName string) ([]byte, []byte, error) {
	if serverName == "" {
		return nil, nil, fmt.Errorf("kit TLS server name is required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate Kit TLS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate Kit TLS serial: %w", err)
	}
	// Keep serial numbers positive, including the extremely unlikely zero draw.
	serial.Add(serial, big.NewInt(1))
	now := time.Now()
	//nolint:exhaustruct // Only server-leaf fields are set; CA and extension defaults must remain disabled.
	leaf := &x509.Certificate{
		SerialNumber:          serial,
		DNSNames:              []string{serverName},
		NotBefore:             now,
		NotAfter:              now.Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kit TLS certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode Kit TLS key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: nil, Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: nil, Bytes: keyDER}), nil
}

// kitGKETLSConfig trusts only the supplied runtime certificate.
func kitGKETLSConfig(certPEM []byte, serverName string) (*tls.Config, error) {
	if serverName == "" {
		return nil, fmt.Errorf("kit TLS server name is required")
	}
	block, rest := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("kit TLS requires exactly one PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Kit TLS certificate: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, DNSName: serverName, Intermediates: nil, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, MaxConstraintComparisions: 0, CertificatePolicies: nil}); err != nil {
		return nil, fmt.Errorf("verify Kit TLS certificate: %w", err)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: serverName,
	}, nil
}
