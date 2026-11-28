package atecred

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"google.golang.org/grpc/credentials"
)

// NewServerTLSCredentials builds mutual-TLS 1.3 gRPC credentials for the ATE
// credential provider. It serves serverBundlePath (a Kubernetes PodCertificate
// combined PEM private-key + certificate-chain bundle) and requires every
// caller to present a client certificate chaining to clientCAFile whose URI
// SANs include expectedInjectorSAN. Both files reload automatically on
// rotation without restarting the pod.
func NewServerTLSCredentials(serverBundlePath, clientCAFile, expectedInjectorSAN string) (credentials.TransportCredentials, error) {
	if strings.TrimSpace(serverBundlePath) == "" {
		return nil, errors.New("server credential bundle path is required")
	}
	if strings.TrimSpace(clientCAFile) == "" {
		return nil, errors.New("client CA bundle path is required")
	}
	if strings.TrimSpace(expectedInjectorSAN) == "" {
		return nil, errors.New("expected injector SPIFFE identity is required")
	}

	certLoader := &reloadingBundle{path: serverBundlePath, mu: sync.Mutex{}, info: nil, cert: nil}
	if _, err := certLoader.get(); err != nil {
		return nil, fmt.Errorf("load server credential bundle: %w", err)
	}
	caLoader := &reloadingCAPool{path: clientCAFile, mu: sync.Mutex{}, info: nil, pool: nil}
	if _, err := caLoader.get(); err != nil {
		return nil, fmt.Errorf("load client CA bundle: %w", err)
	}

	verifySAN := verifyClientURISAN(expectedInjectorSAN)
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			pool, err := caLoader.get()
			if err != nil {
				return nil, err
			}
			return &tls.Config{
				MinVersion:       tls.VersionTLS13,
				GetCertificate:   func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return certLoader.get() },
				ClientAuth:       tls.RequireAndVerifyClientCert,
				ClientCAs:        pool,
				VerifyConnection: verifySAN,
			}, nil
		},
	}
	return credentials.NewTLS(cfg), nil
}

func verifyClientURISAN(expectedSAN string) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("client certificate is required")
		}
		leaf := state.PeerCertificates[0]
		for _, uri := range leaf.URIs {
			if uri.String() == expectedSAN {
				return nil
			}
		}
		return fmt.Errorf("client certificate URI SANs do not include expected injector identity %q", expectedSAN)
	}
}

type reloadingBundle struct {
	path string

	mu   sync.Mutex
	info os.FileInfo
	cert *tls.Certificate
}

func (b *reloadingBundle) get() (*tls.Certificate, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	info, err := os.Stat(b.path)
	if err != nil {
		return nil, fmt.Errorf("stat credential bundle: %w", err)
	}
	if b.cert != nil && b.info != nil && os.SameFile(b.info, info) && info.ModTime().Equal(b.info.ModTime()) && info.Size() == b.info.Size() {
		return b.cert, nil
	}

	parsed, err := parseCredentialBundleFile(b.path)
	if err != nil {
		return nil, err
	}
	b.info = info
	b.cert = parsed
	return parsed, nil
}

// parseCredentialBundleFile parses a single PEM file containing a private key
// and one or more certificates (the Kubernetes PodCertificate bundle format).
func parseCredentialBundleFile(path string) (*tls.Certificate, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path is operator-configured TLS credential bundle path.
	if err != nil {
		return nil, fmt.Errorf("read credential bundle: %w", err)
	}
	var keyPEM, certPEM []byte
	rest := raw
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		encoded := pem.EncodeToMemory(block)
		if strings.Contains(block.Type, "PRIVATE KEY") {
			keyPEM = append(keyPEM, encoded...)
		} else if block.Type == "CERTIFICATE" {
			certPEM = append(certPEM, encoded...)
		}
	}
	if len(keyPEM) == 0 || len(certPEM) == 0 {
		return nil, errors.New("credential bundle must contain both a private key and a certificate chain")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse credential bundle key pair: %w", err)
	}
	return &cert, nil
}

type reloadingCAPool struct {
	path string

	mu   sync.Mutex
	info os.FileInfo
	pool *x509.CertPool
}

func (p *reloadingCAPool) get() (*x509.CertPool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	info, err := os.Stat(p.path)
	if err != nil {
		return nil, fmt.Errorf("stat client CA bundle: %w", err)
	}
	if p.pool != nil && p.info != nil && os.SameFile(p.info, info) && info.ModTime().Equal(p.info.ModTime()) && info.Size() == p.info.Size() {
		return p.pool, nil
	}

	raw, err := os.ReadFile(p.path)
	if err != nil {
		return nil, fmt.Errorf("read client CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("client CA bundle contains no valid PEM certificates")
	}
	p.info = info
	p.pool = pool
	return pool, nil
}
