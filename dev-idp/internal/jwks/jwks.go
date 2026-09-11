// Package jwks is the read side of RFC 7517: turning a published JWKS
// document back into verification keys.
//
// internal/keystore owns the write side for the dev-idp's own key. This
// package exists because the dev-idp also has to consume keys it did not
// produce -- a resource authorization server verifying an ID-JAG from a
// foreign issuer, and the IdP verifying a private_key_jwt client assertion
// signed by an app's own key.
package jwks

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

// Document is a JWKS as published at a jwks_uri or registered inline.
type Document struct {
	Keys []Key `json:"keys"`
}

// Key is one JWK. Only the RSA fields are modelled; the dev-idp signs and
// verifies RS256 exclusively.
type Key struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// Parse decodes a JWKS document.
func Parse(raw []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("decode jwks: %w", err)
	}
	return doc, nil
}

// FindRSA returns the RSA public key matching `kid`. An empty `kid` matches
// the first RSA key, which is what a single-key document means in practice.
func (d Document) FindRSA(kid string) (*rsa.PublicKey, error) {
	for _, key := range d.Keys {
		if kid != "" && key.Kid != kid {
			continue
		}
		if key.Kty != "RSA" {
			continue
		}
		pub, err := key.RSAPublicKey()
		if err != nil {
			return nil, fmt.Errorf("decode jwk %q: %w", key.Kid, err)
		}
		return pub, nil
	}
	return nil, fmt.Errorf("no RSA key with kid %q", kid)
}

// RSAPublicKey rebuilds an RSA public key from a JWK's modulus and exponent.
func (k Key) RSAPublicKey() (*rsa.PublicKey, error) {
	if k.N == "" || k.E == "" {
		return nil, errors.New("jwk is missing n or e")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(k.N, "="))
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(k.E, "="))
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	exponent := new(big.Int).SetBytes(eBytes)
	if !exponent.IsInt64() || exponent.Int64() > int64(^uint32(0)) || exponent.Int64() < 3 {
		return nil, fmt.Errorf("implausible RSA exponent %s", exponent)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(exponent.Int64()),
	}, nil
}

// ParseIssuerURL validates an `iss` claim as an absolute http(s) URL before it
// is used to build a metadata request. Without this an issuer string could
// steer an outbound request somewhere unintended.
func ParseIssuerURL(issuer string) (*url.URL, error) {
	parsed, err := url.Parse(issuer)
	if err != nil {
		return nil, fmt.Errorf("issuer %q is not a URL: %w", issuer, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("issuer %q is not an http(s) URL", issuer)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("issuer %q has no host", issuer)
	}
	return parsed, nil
}
