// Canonical Gram session-token signer. Replaces the chat-session JWT path in
// `server/internal/auth/chatsessions/jwt.go`.
//
// Until the chat-session removal lands, both signers share `GRAM_JWT_SIGNING_KEY`
// and the `chat_session_revoked:{jti}` revocation cache (per spike §4.5) so a
// `userSessions.revoke` lookup hits the same key the chat-session validator
// reads. The two signers are intentionally separate code paths so the
// chat-session signer can be deleted in isolation.
//
// Schema is OIDC-shaped registered claims only (per `schemas/jwt.go`): no
// Gram-specific extras live on the JWT. Org / project / etc. resolve from the
// session record in Postgres (`user_sessions`), keyed by the refresh-token
// hash on token exchange or by JTI on access-token validation.

package usersessions

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SessionClaims is the unified JWT claim shape for Gram-issued user sessions
// (and, as the chat-session path retires, all Gram session tokens). Carries
// only the standard OIDC registered claims.
type SessionClaims struct {
	jwt.RegisteredClaims
}

// Signer mints HS256-signed user-session JWTs. Constructed once at server
// boot from the GRAM_JWT_SIGNING_KEY env var; safe to share across goroutines.
type Signer struct {
	key []byte
}

// NewSigner builds a user-session JWT signer with the supplied HMAC secret.
func NewSigner(secret string) *Signer {
	return &Signer{key: []byte(secret)}
}

// Mint produces an HS256-signed JWT carrying the supplied subject + audience
// + issuer + lifetime. Returns the signed token plus the JTI so the caller
// can persist it on the user_sessions row for later revocation.
func (s *Signer) Mint(subject urn.SessionSubject, audience, issuer string, lifetime time.Duration) (token string, jti string, err error) {
	now := time.Now()
	jti = uuid.NewString()

	claims := SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    issuer,
			Subject:   subject.String(),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
			NotBefore: nil,
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(s.key)
	if err != nil {
		return "", "", fmt.Errorf("sign user-session token: %w", err)
	}
	return signed, jti, nil
}

// Validate parses + verifies an HS256 SessionClaims JWT. Verifies signature,
// expiry/notBefore (via the jwt library), and that the audience contains the
// expected value (typically the toolset slug). Revocation (jti against the
// shared `chat_session_revoked:{jti}` cache) is the caller's responsibility —
// this signer doesn't reach into Redis.
func (s *Signer) Validate(token, expectedAudience string) (*SessionClaims, error) {
	claims := SessionClaims{RegisteredClaims: jwt.RegisteredClaims{}} //nolint:exhaustruct // ParseWithClaims populates the fields.
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

// ParseUnverifiedJTI extracts the `jti` claim from a token without verifying
// the signature. Used by /revoke to push the JTI into the revocation cache —
// the token's authenticity is established by the client_secret check in the
// revoke handler, not by signature verification (RFC 7009 doesn't require it).
func (s *Signer) ParseUnverifiedJTI(token string) (string, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := SessionClaims{RegisteredClaims: jwt.RegisteredClaims{}} //nolint:exhaustruct // ParseUnverified populates the fields.
	if _, _, err := parser.ParseUnverified(token, &claims); err != nil {
		return "", fmt.Errorf("parse unverified token: %w", err)
	}
	if claims.ID == "" {
		return "", errors.New("token missing jti claim")
	}
	return claims.ID, nil
}
