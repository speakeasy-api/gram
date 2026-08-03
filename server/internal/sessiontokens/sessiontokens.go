// Package sessiontokens implements the product-neutral JWT and revocation
// primitives used by Gram-issued session tokens.
package sessiontokens

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RevocationChecker reports whether a JTI has been revoked.
type RevocationChecker interface {
	IsTokenRevoked(ctx context.Context, jti string) (bool, error)
}

// SessionClaims is the standard claim shape for Gram-issued user sessions.
type SessionClaims struct {
	jwt.RegisteredClaims

	ClientID string `json:"client_id,omitempty"`
}

// FirstPartyClientID identifies a token minted for a Gram surface without a
// registered OAuth client. It is deliberately not an HTTPS URL: URL-shaped
// client IDs are resolved as OAuth Client ID Metadata Documents.
const FirstPartyClientID = "client:first-party"

// Signer mints HS256-signed session JWTs. It is safe to share across goroutines.
type Signer struct {
	key []byte
}

// NewSigner builds a signer with the supplied HMAC secret.
func NewSigner(secret string) *Signer {
	return &Signer{key: []byte(secret)}
}

// MintParams describes one session token to sign.
type MintParams struct {
	Subject  urn.SessionSubject
	Audience string
	Issuer   string
	Lifetime time.Duration
	ClientID string
}

// Mint produces an HS256-signed JWT and its JTI.
func (s *Signer) Mint(params MintParams) (token string, jti string, err error) {
	now := time.Now()
	jti = uuid.NewString()

	claims := SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    params.Issuer,
			Subject:   params.Subject.String(),
			Audience:  jwt.ClaimStrings{params.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(params.Lifetime)),
			NotBefore: nil,
		},
		ClientID: params.ClientID,
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(s.key)
	if err != nil {
		return "", "", fmt.Errorf("sign session token: %w", err)
	}
	return signed, jti, nil
}

// Validate parses and verifies a signed token for the expected audience.
func (s *Signer) Validate(token, expectedAudience string) (*SessionClaims, error) {
	claims := SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			ExpiresAt: nil,
			NotBefore: nil,
			IssuedAt:  nil,
			ID:        "",
		},
		ClientID: "",
	}
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.key, nil
	}, jwt.WithAudience(expectedAudience))
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("token is not valid")
	}
	return &claims, nil
}

// ValidatedSession is a verified token's caller identity.
type ValidatedSession struct {
	Subject  urn.SessionSubject
	JTI      string
	ClientID string
}

// ValidateBearer verifies signature, expiry, audience, revocation, and subject.
// It fails closed when the revocation store is unavailable: admitting a token
// during a revocation-store outage would make revocation only intermittently
// effective.
func (s *Signer) ValidateBearer(ctx context.Context, token, expectedAudience string, revocation RevocationChecker) (ValidatedSession, error) {
	claims, err := s.Validate(token, expectedAudience)
	if err != nil {
		return ValidatedSession{}, err
	}
	if claims == nil {
		return ValidatedSession{}, errors.New("validate token: nil claims")
	}

	revoked, err := revocation.IsTokenRevoked(ctx, claims.ID)
	if err != nil {
		return ValidatedSession{}, fmt.Errorf("check revocation: %w", err)
	}
	if revoked {
		return ValidatedSession{}, errors.New("token is revoked")
	}

	subject, err := urn.ParseSessionSubject(claims.Subject)
	if err != nil {
		return ValidatedSession{}, fmt.Errorf("parse session subject: %w", err)
	}
	return ValidatedSession{Subject: subject, JTI: claims.ID, ClientID: claims.ClientID}, nil
}

// VerifiedJTI extracts a JTI after verifying the token's signature. It skips
// expiry validation so clients can revoke an expired token, but a valid Gram
// signature is required before the caller can affect the revocation cache.
func (s *Signer) VerifiedJTI(token string) (string, error) {
	claims := SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			ExpiresAt: nil,
			NotBefore: nil,
			IssuedAt:  nil,
			ID:        "",
		},
		ClientID: "",
	}
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithoutClaimsValidation())
	if err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	if claims.ID == "" {
		return "", errors.New("token missing jti claim")
	}
	return claims.ID, nil
}
