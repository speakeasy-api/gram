package resourceas

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

// jwksDocument and jwk are the read side of RFC 7517. The keystore package
// owns the write side for this dev-idp's own key; these types exist because a
// resource can trust a foreign issuer, whose JWKS this server has to consume
// like any other client would.
type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// rsaPublicKey rebuilds an RSA public key from a JWK's modulus and exponent.
func (k jwk) rsaPublicKey() (*rsa.PublicKey, error) {
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

// parseIssuerURL validates an `iss` claim as an absolute http(s) URL before
// it is used to build a metadata request. Without this an issuer string could
// steer an outbound request somewhere unintended.
func parseIssuerURL(issuer string) (*url.URL, error) {
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
