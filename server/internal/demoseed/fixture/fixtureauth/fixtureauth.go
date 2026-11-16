// Package fixtureauth holds the credentials, paths and token format of the
// demo seed's MCP fixture (internal/demoseed/fixture), which every Speakeasy
// server hosts under PathPrefix. They are checked into the repository on
// purpose: the fixture serves fake data only, and the demo seed imports this
// package to mint tokens and clients the fixture accepts, so seeded
// connections work right after seeding, locally and in the public demo org.
//
// Nothing here is a secret, and nothing here may guard real data.
package fixtureauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SharedToken is the shared credential the fixture's shared-token endpoint
// accepts as `Authorization: Bearer <SharedToken>`. It stands in for a service
// account key that every caller of a server uses.
const SharedToken = "fixture-shared-token-not-a-secret"

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
	// PathDCR is where the fixture's second authorization server lives. It
	// takes only dynamically registered clients (no client ID metadata
	// documents), so a sign-in set up against it exercises DCR, while
	// PathOAuthMCP's server is used through a metadata document client.
	PathDCR = "/dcr"
	// PathAutoMCP is the DCR authorization server's protected resource, for
	// a server whose sign-in is set up through discovery and dynamic client
	// registration. It is relative to PathDCR.
	PathAutoMCP = "/mcp/auto"
	// PathSharedMCP takes the shared token or a caller's own token.
	PathSharedMCP = "/mcp/shared"
	// PathOwnTokenMCP lets anyone connect and list tools, but every tool call
	// needs the caller's own token in the OwnTokenHeader request header, like
	// an API that takes a personal API key.
	PathOwnTokenMCP = "/mcp/own-token" //nolint:gosec // a URL path, not a credential
	// PathOpenMCP needs no credentials at all.
	PathOpenMCP = "/mcp/open"
	PathAPI     = "/acme-api"
)

// OwnTokenHeader is the request header PathOwnTokenMCP reads the caller's own
// token from.
const OwnTokenHeader = "X-Upstream-Token" //nolint:gosec // a header name, not a credential

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

// RegisterClient registers a client with the authorization server at issuer,
// as its dynamic client registration endpoint does. The client id is a signed
// token describing the client, so registrations survive a restart without
// storage, and the seed registers clients without calling the endpoint.
func RegisterClient(issuer, name string, redirectURIs []string, now time.Time) (string, error) {
	return Sign(Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "client",
			Audience:  nil,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: nil,
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * 365 * 24 * time.Hour)),
			ID:        NewID(),
		},
		Kind:          KindClient,
		Scope:         "",
		ClientID:      "",
		RedirectURI:   "",
		RedirectURIs:  redirectURIs,
		ClientName:    name,
		CodeChallenge: "",
		Resource:      "",
	})
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
			NotBefore: nil,
			ID:        NewID(),
		},
		Kind:          KindAccess,
		Scope:         scope,
		ClientID:      clientID,
		RedirectURI:   "",
		RedirectURIs:  nil,
		ClientName:    "",
		CodeChallenge: "",
		Resource:      resource,
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
			NotBefore: nil,
			ID:        NewID(),
		},
		Kind:          KindRefresh,
		Scope:         scope,
		ClientID:      clientID,
		RedirectURI:   "",
		RedirectURIs:  nil,
		ClientName:    "",
		CodeChallenge: "",
		Resource:      resource,
	})
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpires: refreshExp, Scope: scope}, nil
}

// OAuthResource is the resource identifier of the fixture's OAuth-protected
// MCP endpoint for a fixture served at baseURL.
func OAuthResource(baseURL string) string { return Resource(baseURL, PathOAuthMCP) }

// PathPrefix is where the Speakeasy server serves the fixture. It sits under
// /rpc because the deployed ingress sends only some paths, /rpc among them,
// to the API server; the rest reach the dashboard.
const PathPrefix = "/rpc/demoseed"

// BaseURL is the fixture's external URL, and its main authorization
// server's issuer, on a Speakeasy server at serverURL.
func BaseURL(serverURL *url.URL) string {
	return strings.TrimRight(serverURL.String(), "/") + PathPrefix
}

// DCRIssuer is the DCR-only authorization server's issuer, under the
// fixture served at baseURL.
func DCRIssuer(baseURL string) string { return strings.TrimRight(baseURL, "/") + PathDCR }

// AutoResource is the resource identifier of the DCR server's MCP endpoint.
func AutoResource(baseURL string) string { return Resource(DCRIssuer(baseURL), PathAutoMCP) }

// Resource is the resource identifier of the fixture endpoint at path.
func Resource(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

// HasScope reports whether scopes contains want.
func HasScope(scopes []string, want string) bool { return slices.Contains(scopes, want) }
