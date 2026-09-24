// Package jwks validates and serves public verification keys for tunnel caller assertions.
package jwks

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-jose/go-jose/v4"
)

// Path is the public verification-key endpoint on the tunnel gateway.
const Path = "/.well-known/jwks.json"

// Set is an immutable public key set and its cacheable HTTP representation.
type Set struct {
	keyIDs   map[string]bool
	document []byte
	etag     string
}

// Parse accepts only SubjectPublicKeyInfo RSA PEM blocks. Empty input produces
// an empty key set for gateways without local caller-assertion configuration.
func Parse(publicPEM string) (*Set, error) {
	keys := make([]jose.JSONWebKey, 0)
	seen := make(map[string]bool)
	for remaining := bytes.TrimSpace([]byte(publicPEM)); len(remaining) > 0; {
		block, rest := pem.Decode(remaining)
		if block == nil {
			return nil, errors.New("invalid public key PEM bundle")
		}
		remaining = bytes.TrimSpace(rest)
		if block.Type != "PUBLIC KEY" {
			return nil, errors.New("public key bundle must contain only SubjectPublicKeyInfo PEM keys")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		pub, ok := parsed.(*rsa.PublicKey)
		if err != nil || !ok {
			return nil, errors.New("public key bundle must contain RSA keys")
		}
		key, err := PublicKey(pub)
		if err != nil {
			return nil, err
		}
		if !seen[key.KeyID] {
			keys = append(keys, key)
			seen[key.KeyID] = true
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].KeyID < keys[j].KeyID })
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("encode public JWKS: %w", err)
	}
	digest := sha256.Sum256(document)
	return &Set{keyIDs: seen, document: document, etag: `"` + base64.RawURLEncoding.EncodeToString(digest[:]) + `"`}, nil
}

// PublicKey identifies an RSA verification key by its RFC 7638 SHA-256 thumbprint.
func PublicKey(key *rsa.PublicKey) (jose.JSONWebKey, error) {
	if key == nil || key.N == nil || key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
		return jose.JSONWebKey{}, errors.New("public keys must be RSA with at least 2048 bits and an odd exponent")
	}
	jwk := jose.JSONWebKey{Key: key, KeyID: "", Algorithm: string(jose.RS256), Use: "sig",
		Certificates: nil, CertificatesURL: nil, CertificateThumbprintSHA1: nil, CertificateThumbprintSHA256: nil}
	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return jose.JSONWebKey{}, fmt.Errorf("compute public key ID: %w", err)
	}
	jwk.KeyID = base64.RawURLEncoding.EncodeToString(thumbprint)
	return jwk, nil
}

// Contains reports whether the bundle publishes the given key ID.
func (s *Set) Contains(keyID string) bool { return s.keyIDs[keyID] }

// ServeHTTP serves the public bundle without requiring authentication.
func (s *Set) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("ETag", s.etag)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Header.Get("If-None-Match") == s.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write(s.document)
	}
}
