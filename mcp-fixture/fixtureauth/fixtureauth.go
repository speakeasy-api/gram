// Package fixtureauth holds the credentials and token format of the local MCP
// fixture server (mcp-fixture). They are checked into the repository on
// purpose: the fixture is a throwaway development server, and the local demo
// seed imports this package to mint tokens the fixture accepts, so seeded
// connections work right after `mise run seed`.
//
// Nothing here is a secret. Never reuse these values outside local
// development.
package fixtureauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SharedToken is the shared credential the fixture's shared-token endpoint
// accepts as `Authorization: Bearer <SharedToken>`. It stands in for a service
// account key that every caller of a server uses.
const SharedToken = "fixture-shared-token-not-a-secret"

// TunnelKey is the tunnel agent key the fixture daemon connects to the local
// tunnel gateway with. The local seed stores its hash on the Tunnelled
// Private Server, so the tunnel is live right after seeding.
const TunnelKey = "gram_tunnel_05360ec0d82da35bbbe6ce936c7697e31b7c13d19d5be23effb52b7664440477"

// SharedIdentity is the identity a request authenticated with SharedToken
// acts as.
const SharedIdentity = "shared-service-account"

// signingKey signs every token the fixture issues (HS256). It is public so
// the seed can mint tokens without talking to a running fixture.
var signingKey = []byte("speakeasy-mcp-fixture-signing-key-not-a-secret")

// Token kinds, carried in the `typ` claim so one kind can never stand in for
// another.
const (
	KindAccess  = "access"
	KindRefresh = "refresh"
	KindCode    = "code"
	KindClient  = "client"
)

// Scopes the fixture's authorization server offers.
const (
	ScopeNotesRead  = "notes:read"
	ScopeNotesWrite = "notes:write"
)

// DefaultScopes is granted when a client asks for no scope.
var DefaultScopes = []string{ScopeNotesRead, ScopeNotesWrite}

// Paths, relative to the fixture's base URL.
const (
	// PathOAuthMCP takes the fixture's own per-user OAuth tokens.
	PathOAuthMCP = "/mcp/oauth"
	// PathAutoMCP is a second OAuth-protected resource, for a server whose
	// sign-in an admin sets up through discovery and client registration.
	PathAutoMCP = "/mcp/auto"
	// PathSharedMCP takes the shared token or a caller's own token.
	PathSharedMCP = "/mcp/shared"
	// PathOpenMCP needs no credentials at all.
	PathOpenMCP = "/mcp/open"
	PathAPI     = "/acme-api"
)

// PersonalTokenTTL is how long a token from the personal token page lives.
const PersonalTokenTTL = 30 * 24 * time.Hour

// Claims is every claim a fixture token can carry. Unused claims are empty.
type Claims struct {
	jwt.RegisteredClaims
	Kind          string   `json:"typ"`
	Scope         string   `json:"scope,omitempty"`
	ClientID      string   `json:"client_id,omitempty"`
	RedirectURI   string   `json:"redirect_uri,omitempty"`
	RedirectURIs  []string `json:"redirect_uris,omitempty"`
	ClientName    string   `json:"client_name,omitempty"`
	CodeChallenge string   `json:"code_challenge,omitempty"`
	Resource      string   `json:"resource,omitempty"`
}

// Email returns the signed-in person a token belongs to.
func (c Claims) Email() string { return c.Subject }

// Scopes splits the scope claim.
func (c Claims) Scopes() []string { return strings.Fields(c.Scope) }

// Sign signs claims as an HS256 JWT.
func Sign(c Claims) (string, error) {
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(signingKey)
	if err != nil {
		return "", fmt.Errorf("sign fixture %s token: %w", c.Kind, err)
	}
	return signed, nil
}

// ErrWrongKind is returned when a valid token is of another kind than asked.
var ErrWrongKind = errors.New("token is of the wrong kind")

// Parse verifies a fixture token's signature, expiry and kind.
func Parse(token, kind string) (Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		return signingKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return Claims{}, fmt.Errorf("parse fixture %s token: %w", kind, err)
	}
	if c.Kind != kind {
		return Claims{}, fmt.Errorf("parse fixture %s token: %w (%s)", kind, ErrWrongKind, c.Kind)
	}
	return c, nil
}

// NewID returns a random token id.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// TokenPair is what the token endpoint returns and what the seed stores.
type TokenPair struct {
	AccessToken     string
	AccessExpiresAt time.Time
	RefreshToken    string
	RefreshExpires  time.Time
	Scope           string
}

// RefreshTTL is how long a refresh token lives.
const RefreshTTL = 30 * 24 * time.Hour

// IssueTokens mints an access and refresh token for email at issuer, bound to
// resource and clientID. accessTTL sets how long the access token lives.
func IssueTokens(issuer, resource, clientID, email string, scopes []string, accessTTL time.Duration, now time.Time) (TokenPair, error) {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	scope := strings.Join(scopes, " ")
	accessExp := now.Add(accessTTL)
	access, err := Sign(Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   email,
			Audience:  jwt.ClaimStrings{resource},
			ExpiresAt: jwt.NewNumericDate(accessExp),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        NewID(),
		},
		Kind:     KindAccess,
		Scope:    scope,
		ClientID: clientID,
		Resource: resource,
	})
	if err != nil {
		return TokenPair{}, err
	}
	refreshExp := now.Add(RefreshTTL)
	refresh, err := Sign(Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   email,
			Audience:  jwt.ClaimStrings{issuer},
			ExpiresAt: jwt.NewNumericDate(refreshExp),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        NewID(),
		},
		Kind:     KindRefresh,
		Scope:    scope,
		ClientID: clientID,
		Resource: resource,
	})
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpires: refreshExp, Scope: scope}, nil
}

// OAuthResource is the resource identifier of the fixture's OAuth-protected
// MCP endpoint for a fixture served at baseURL.
func OAuthResource(baseURL string) string { return Resource(baseURL, PathOAuthMCP) }

// Resource is the resource identifier of the fixture endpoint at path.
func Resource(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

// HasScope reports whether scopes contains want.
func HasScope(scopes []string, want string) bool { return slices.Contains(scopes, want) }
