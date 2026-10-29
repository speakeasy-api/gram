package assistants

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

func TestKitGKETLSCertificate(t *testing.T) {
	t.Parallel()
	const name = "runtime-a.kit.internal"
	certPEM, keyPEM, err := newKitGKETLSCertificate(name)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cert.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Fatal("expected ECDSA public key")
	}
	if cert.IsCA || len(cert.DNSNames) != 1 || cert.DNSNames[0] != name {
		t.Fatal("expected runtime-scoped server leaf")
	}
	if cert.NotAfter.Sub(cert.NotBefore) != 30*24*time.Hour {
		t.Fatal("expected 30-day certificate lifetime")
	}
	if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
		t.Fatalf("expected self-signed certificate: %v", err)
	}
	nextCert, nextKey, err := newKitGKETLSCertificate(name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(certPEM, nextCert) || bytes.Equal(keyPEM, nextKey) {
		t.Fatal("each bootstrap must rotate certificate and key")
	}
	if _, _, err := newKitGKETLSCertificate(""); err == nil {
		t.Fatal("accepted empty server name")
	}
}

func TestKitGKETLSConfig(t *testing.T) {
	t.Parallel()
	const name = "runtime-a.kit.internal"
	certPEM, keyPEM, err := newKitGKETLSCertificate(name)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)

	config, err := kitGKETLSConfig(certPEM, name)
	if err != nil {
		t.Fatal(err)
	}
	if config.MinVersion != tls.VersionTLS12 || config.InsecureSkipVerify || config.ServerName != name {
		t.Fatal("unexpected TLS security configuration")
	}
	t.Run("trusted runtime", func(t *testing.T) {
		t.Parallel()
		transport := &http.Transport{TLSClientConfig: config}
		defer transport.CloseIdleConnections()
		client := &guardian.HTTPClient{Transport: transport, Timeout: 5 * time.Second}
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("unexpected status: %d", response.StatusCode)
		}
	})
	t.Run("wrong hostname", func(t *testing.T) {
		t.Parallel()
		if _, err := kitGKETLSConfig(certPEM, "runtime-b.kit.internal"); err == nil {
			t.Fatal("accepted wrong hostname")
		}
		wrong := config.Clone()
		wrong.ServerName = "runtime-b.kit.internal"
		transport := &http.Transport{TLSClientConfig: wrong}
		defer transport.CloseIdleConnections()
		client := &guardian.HTTPClient{Transport: transport, Timeout: 5 * time.Second}
		if response, err := client.Get(server.URL); err == nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
			t.Fatal("TLS accepted wrong hostname")
		}
	})
	t.Run("different bootstrap", func(t *testing.T) {
		t.Parallel()
		otherPEM, _, err := newKitGKETLSCertificate(name)
		if err != nil {
			t.Fatal(err)
		}
		otherConfig, err := kitGKETLSConfig(otherPEM, name)
		if err != nil {
			t.Fatal(err)
		}
		transport := &http.Transport{TLSClientConfig: otherConfig}
		defer transport.CloseIdleConnections()
		client := &guardian.HTTPClient{Transport: transport, Timeout: 5 * time.Second}
		if response, err := client.Get(server.URL); err == nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
			t.Fatal("TLS accepted untrusted bootstrap certificate")
		}
	})
	for label, material := range map[string][]byte{
		"empty":                 nil,
		"garbage":               []byte("not a certificate"),
		"key":                   keyPEM,
		"malformed DER":         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: nil, Bytes: []byte("bad")}),
		"multiple certificates": append(append([]byte{}, certPEM...), certPEM...),
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if _, err := kitGKETLSConfig(material, name); err == nil {
				t.Fatal("accepted invalid certificate material")
			}
		})
	}
	if _, err := kitGKETLSConfig(certPEM, ""); err == nil {
		t.Fatal("accepted empty server name")
	}
}
