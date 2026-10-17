// Package mcpauthz issues short-lived caller assertions for private MCP tunnels.
// Destinations can verify assertions to use caller claims in their access policy.
package mcpauthz

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/identity"
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

// Issuer holds an immutable signing key initialized by New.
type Issuer struct {
	key        *rsa.PrivateKey
	kid        string
	issuer     string
	publicKeys *jwks.Set
}

// Target identifies the actual destination using server-owned metadata.
type Target struct {
	// OrganizationID is the destination owner's organization.
	OrganizationID string

	// ProjectID is the destination owner's project.
	ProjectID uuid.UUID

	// TunnelID is the immutable tunneled server ID, including on meta dispatch.
	TunnelID uuid.UUID

	// MCPServerID is the mcp_servers row wrapping the tunnel that the caller
	// reached: on meta dispatch, the selected member's row, never the meta
	// gateway's. The tunnel agent binds a session to it.
	MCPServerID uuid.UUID

	// ResourceIdentifier is the destination's saved audience. Empty uses TunnelID.
	ResourceIdentifier string
}

// ValidateIssuerOrigin reports whether issuerURL is a bare HTTPS origin, the
// form GRAM_AUTHZ_ISSUER_URL must take. allowHTTP admits HTTP for local use.
func ValidateIssuerOrigin(issuerURL string, allowHTTP bool) error {
	u, err := url.Parse(issuerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Scheme != "https" && (!allowHTTP || u.Scheme != "http")) || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return errors.New("caller assertion issuer must be an HTTPS origin (HTTP allowed only locally)")
	}
	return nil
}

// New returns a signer. All three settings are required, and the configuration
// must contain valid keys and issuer.
func New(privatePEM, publicPEM, issuerURL string, allowHTTP bool) (*Issuer, error) {
	if strings.TrimSpace(privatePEM) == "" || strings.TrimSpace(publicPEM) == "" || strings.TrimSpace(issuerURL) == "" {
		return nil, errors.New("GRAM_AUTHZ_PRIVATE_KEY, GRAM_AUTHZ_PUBLIC_KEYS and GRAM_AUTHZ_ISSUER_URL are required (run `mise run zero:tunnel-identity` locally)")
	}
	if err := ValidateIssuerOrigin(issuerURL, allowHTTP); err != nil {
		return nil, err
	}
	block, rest := pem.Decode([]byte(privatePEM))
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("GRAM_AUTHZ_PRIVATE_KEY must contain exactly one PKCS#8 PEM key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	key, ok := parsed.(*rsa.PrivateKey)
	if err != nil || !ok {
		return nil, errors.New("caller assertion private key must be RSA")
	}
	active, err := jwks.PublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("identify caller assertion key: %w", err)
	}
	publicKeys, err := jwks.Parse(publicPEM)
	if err != nil {
		return nil, fmt.Errorf("parse GRAM_AUTHZ_PUBLIC_KEYS: %w", err)
	}
	if !publicKeys.Contains(active.KeyID) {
		return nil, errors.New("active caller assertion public key is missing from GRAM_AUTHZ_PUBLIC_KEYS")
	}
	return &Issuer{key: key, kid: active.KeyID, issuer: strings.TrimRight(issuerURL, "/"), publicKeys: publicKeys}, nil
}

// Strip removes all case variants, including noncanonical Header map entries.
func Strip(header http.Header) {
	for name := range header {
		if identity.ReservedHeader(name) {
			delete(header, name)
		}
	}
}

// Mint returns no assertion for unsupported provenance.
// The caller must restrict this to private tunnel destinations. A matching
// owner organization is required even when some other route admitted a caller.
//
// cred describes the bearer forwarded alongside the assertion, and must be
// derived from the credential Speakeasy resolved for the request, never from
// a configured header. Nil omits the upstream_credential claim.
func (s *Issuer) Mint(ctx context.Context, target Target, cred *identity.UpstreamCredential) (string, error) {
	caller, ok := mcpidentity.FromContext(ctx)
	if !ok {
		return "", nil
	}
	var subject, principalType string
	switch caller.Kind() {
	case mcpidentity.KindUserSession, mcpidentity.KindConsentDiscovery:
		principalType, subject = "user", caller.UserID()
	case mcpidentity.KindAPIKey:
		principalType, subject = "api_key", caller.APIKeyID()
	case mcpidentity.KindAgent:
		principalType, subject = "agent", caller.AgentID()
	default:
		return "", nil
	}
	if subject == "" {
		return "", nil
	}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.ActiveOrganizationID != target.OrganizationID || target.OrganizationID == "" ||
		target.ProjectID == uuid.Nil || target.TunnelID == uuid.Nil ||
		(auth.ProjectID != nil && *auth.ProjectID != target.ProjectID) {
		return "", errors.New("caller assertion destination does not match authenticated tenant")
	}
	if target.MCPServerID == uuid.Nil {
		return "", errors.New("caller assertion destination has no MCP server")
	}
	now := time.Now()
	expires := now.Add(identity.MaxLifetime)
	if deadline := caller.ExpiresAt(); !deadline.IsZero() && deadline.Before(expires) {
		expires = deadline
	}
	if expires.Unix() <= now.Unix() {
		return "", errors.New("caller assertion source credential expired")
	}
	audience := target.ResourceIdentifier
	if audience == "" {
		audience = urn.NewTunneledMcpServer(target.TunnelID).String()
	}
	claims := jwt.MapClaims{
		"iss": s.issuer, "sub": principalType + ":" + subject, "aud": audience,
		identity.ClaimOrganizationID: target.OrganizationID,
		identity.ClaimMCPServerID:    target.MCPServerID.String(),
		"iat":                        now.Unix(), "exp": expires.Unix(), "jti": uuid.NewString(), identity.ClaimVersion: identity.Version,
	}
	if cred != nil {
		claims[identity.ClaimUpstreamCredential] = cred
	}
	if auth.OrganizationSlug != "" {
		claims["organization_slug"] = auth.OrganizationSlug
	}
	if principalType == "user" && auth.UserID == subject && auth.Email != nil && *auth.Email != "" {
		claims["email"] = *auth.Email
	}
	if caller.Kind() == mcpidentity.KindConsentDiscovery {
		claims[identity.ClaimAllowedMethods] = []string{"server/discover", "initialize", "notifications/initialized", "ping", "tools/list"}
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	token.Header["typ"] = identity.TokenType
	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", errors.New("sign caller assertion")
	}
	return signed, nil
}
