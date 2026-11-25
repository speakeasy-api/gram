// Package jwk builds public signing keys for publication.
package jwk

import (
	"crypto"
	"encoding/base64"
	"fmt"

	"github.com/go-jose/go-jose/v4"
)

// NewPublicKey assigns an RFC 7638 SHA-256 thumbprint as the key ID.
// Callers supply a validated public key and its signing algorithm.
func NewPublicKey(key crypto.PublicKey, algorithm jose.SignatureAlgorithm) (jose.JSONWebKey, error) {
	public := jose.JSONWebKey{
		Key:                         key,
		KeyID:                       "",
		Algorithm:                   string(algorithm),
		Use:                         "sig",
		Certificates:                nil,
		CertificatesURL:             nil,
		CertificateThumbprintSHA1:   nil,
		CertificateThumbprintSHA256: nil,
	}
	thumbprint, err := public.Thumbprint(crypto.SHA256)
	if err != nil {
		return jose.JSONWebKey{}, fmt.Errorf("derive key thumbprint: %w", err)
	}
	public.KeyID = base64.RawURLEncoding.EncodeToString(thumbprint)
	return public, nil
}
