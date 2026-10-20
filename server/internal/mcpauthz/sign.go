package mcpauthz

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// URL is the issuer origin tokens signed by this issuer carry.
func (s *Issuer) URL() string { return s.issuer }

// SignRS256 signs claims with the active platform key under the given typ
// header, so verifiers can tell token kinds signed by the same key apart.
func (s *Issuer) SignRS256(claims jwt.Claims, typ string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["typ"] = typ
	token.Header["kid"] = s.kid
	raw, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("sign %s: %w", typ, err)
	}
	return raw, nil
}

// ParseRS256 verifies raw against every published platform key, requiring the
// typ header, this issuer, the single audience, and an expiry.
func (s *Issuer) ParseRS256(raw string, claims jwt.Claims, typ, audience string) error {
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Header["typ"] != typ {
			return nil, errors.New("unexpected token type")
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errors.New("missing key id")
		}
		key, ok := s.publicKeys.Key(kid)
		if !ok {
			return nil, errors.New("unknown key id")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithIssuer(s.issuer), jwt.WithAudience(audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return fmt.Errorf("verify %s: %w", typ, err)
	}
	if !token.Valid {
		return fmt.Errorf("verify %s: invalid token", typ)
	}
	return nil
}
