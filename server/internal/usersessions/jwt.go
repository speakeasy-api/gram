// Canonical Gram session-token signer. Replaces the chat-session JWT path in
// `server/internal/auth/chatsessions/jwt.go`.
//
// Until the chat-session removal lands, both signers share `GRAM_JWT_SIGNING_KEY`
// and the `chat_session_revoked:{jti}` revocation cache so a
// `userSessions.revoke` lookup hits the same key the chat-session validator
// reads. The two signers are intentionally separate code paths so the
// chat-session signer can be deleted in isolation.
//
// Schema is standard claims only: the OIDC registered set plus `client_id`
// from RFC 9068 §2.2 (JWT Profile for OAuth 2.0 Access Tokens). No
// Gram-specific extras live on the JWT. Org / project / etc. resolve from the
// session record in Postgres (`user_sessions`), keyed by the refresh-token
// hash on token exchange or by JTI on access-token validation.

package usersessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RevocationChecker reports whether a JTI has been revoked. The user-session
// flow and the chat-session flow share a revocation keyspace today
// (`chat_session_revoked:{jti}`); this interface is the seam so the signer
// itself doesn't need to know about Redis or the chat-session manager.
type RevocationChecker interface {
	IsTokenRevoked(ctx context.Context, jti string) (bool, error)
}

// TokenRevoker pushes a JTI into the revocation cache the RevocationChecker
// reads. Declared as an interface rather than taking *chatsessions.Manager
// directly so revoke handlers can be tested against a failing revoker without
// standing up an unreachable Redis, which costs seconds per call (1s
// DialTimeout plus go-redis retries) and so taxes every run of these tests
// once per seeded session.
type TokenRevoker interface {
	RevokeToken(ctx context.Context, jti string) error
}

// SessionClaims is the unified JWT claim shape for Gram-issued user sessions
// (and, as the chat-session path retires, all Gram session tokens). Carries
// the standard OIDC registered claims plus the OAuth client the token was
// issued to.
type SessionClaims struct {
	jwt.RegisteredClaims

	// ClientID identifies the OAuth client this token was issued to (RFC 9068
	// §2.2). Unlike the client identity an MCP client reports at initialize,
	// this one is established by the authorization flow rather than
	// self-reported.
	//
	// Mint paths with no registered client stamp FirstPartyClientID instead.
	// Empty only on tokens minted before this claim existed, so readers must
	// still treat it as optional.
	ClientID string `json:"client_id,omitempty"`
}

// FirstPartyClientID is the `client_id` stamped on sessions minted for our own
// surfaces, which have no registered OAuth client because they never run the
// authorization dance.
//
// A deliberate departure from RFC 9068, where `client_id` names a registered
// client. Leaving it empty is indistinguishable from "unknown", and a reader
// asking who called cannot tell a first-party session from an old token minted
// before the claim existed. A URN says it outright.
//
// Deliberately not a resolvable URL: an `https://…` value would be read as an
// OAuth Client ID Metadata Document, which requires the client_id to equal a
// URL serving that document (see `user_session_clients.client_id_metadata_uri`).
// This resolves to nothing and should never be looked up.
const FirstPartyClientID = "client:first-party"

// JWTSigningKeyFlag is the CLI flag (and env var via GRAM_JWT_SIGNING_KEY)
// that supplies the HMAC secret to NewSigner at server boot. Defining the
// flag name here keeps the start/worker command wiring and the signer's
// expectations in sync.
const JWTSigningKeyFlag = "jwt-signing-key"

// Signer mints HS256-signed user-session JWTs. Constructed once at server
// boot from the GRAM_JWT_SIGNING_KEY env var; safe to share across goroutines.
type Signer struct {
	key []byte
}

// NewSigner builds a user-session JWT signer with the supplied HMAC secret.
func NewSigner(secret string) *Signer {
	return &Signer{key: []byte(secret)}
}

// MintParams describes one session token to sign. A struct rather than
// positional arguments so every caller has to state which OAuth client the
// token belongs to — several mint paths legitimately have none.
type MintParams struct {
	Subject  urn.SessionSubject
	Audience string
	Issuer   string
	Lifetime time.Duration
	// ClientID is the OAuth client the token is issued to, or empty for mint
	// paths with no client (e.g. API-key exchange).
	ClientID string
}

// Mint produces an HS256-signed JWT from the supplied params. Returns the
// signed token plus the JTI so the caller can persist it on the user_sessions
// row for later revocation.
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
	claims := SessionClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    "",
		Subject:   "",
		Audience:  nil,
		ExpiresAt: nil,
		NotBefore: nil,
		IssuedAt:  nil,
		ID:        "",
	}, ClientID: ""}
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

// ValidatedSession is what a Bearer token resolves to once it has been
// verified: who is calling, which token it was, and which OAuth client holds
// it.
type ValidatedSession struct {
	Subject urn.SessionSubject
	JTI     string
	// ClientID is the OAuth client the token was issued to, empty when the
	// token carries no `client_id` claim.
	ClientID string
}

// ValidateBearer bundles the full /mcp/{slug} Bearer validation path:
// signature + expiry + audience match (via Validate), revocation cache
// lookup (via the RevocationChecker), and URN parse of the `sub` claim.
//
// Callers stay free of JWT primitives and revocation plumbing — they only
// need a Signer + a RevocationChecker (typically *chatsessions.Manager
// while the unified revocation keyspace is shared with chat sessions).
func (s *Signer) ValidateBearer(ctx context.Context, token, expectedAudience string, revocation RevocationChecker) (ValidatedSession, error) {
	claims, err := s.Validate(token, expectedAudience)
	if err != nil {
		return ValidatedSession{}, err
	}
	if claims == nil {
		return ValidatedSession{}, errors.New("validate token: nil claims")
	}
	// Fail closed on revocation-cache errors: if we can't tell whether a
	// token is revoked (Redis outage, network blip), refuse the token
	// rather than admit a possibly-revoked one. The alternative — silently
	// allowing access when the revocation backend is degraded — turns the
	// /revoke endpoint into a sometimes-effective primitive, which is the
	// worst possible failure mode for a security control.
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

// ParseUnverifiedJTI extracts the `jti` claim from a token without verifying
// the signature. Used by /revoke to push the JTI into the revocation cache —
// the token's authenticity is established by the client_secret check in the
// revoke handler, not by signature verification (RFC 7009 doesn't require it).
func (s *Signer) ParseUnverifiedJTI(token string) (string, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := SessionClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    "",
		Subject:   "",
		Audience:  nil,
		ExpiresAt: nil,
		NotBefore: nil,
		IssuedAt:  nil,
		ID:        "",
	}, ClientID: ""}
	if _, _, err := parser.ParseUnverified(token, &claims); err != nil {
		return "", fmt.Errorf("parse unverified token: %w", err)
	}
	if claims.ID == "" {
		return "", errors.New("token missing jti claim")
	}
	return claims.ID, nil
}
