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
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

// Header is reserved for Gram's signed caller assertion, without a Bearer prefix.
const Header = "X-Speakeasy-Identity"

// Lifetime bounds the bearer assertion's replay window.
const Lifetime = time.Minute

// Issuer holds an immutable signing key initialized by New.
type Issuer struct {
	key    *rsa.PrivateKey
	kid    string
	issuer string
}

// Target identifies the actual destination using server-owned metadata.
type Target struct {
	// OrganizationID is the destination owner's organization.
	OrganizationID string

	// ProjectID is the destination owner's project.
	ProjectID uuid.UUID

	// TunnelID is the immutable tunneled server ID, including on meta dispatch.
	TunnelID uuid.UUID

	// ResourceIdentifier is the destination's saved audience. Empty uses TunnelID.
	ResourceIdentifier string
}

// New returns a signer when all three settings are present. Missing settings
// disable signing; a complete configuration must contain valid keys and issuer.
func New(privatePEM, publicPEM, issuerURL string, allowHTTP bool) (*Issuer, error) {
	if strings.TrimSpace(privatePEM) == "" || strings.TrimSpace(publicPEM) == "" || strings.TrimSpace(issuerURL) == "" {
		return nil, nil
	}
	u, err := url.Parse(issuerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Scheme != "https" && (!allowHTTP || u.Scheme != "http")) || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, errors.New("caller assertion issuer must be an HTTPS origin (HTTP allowed only locally)")
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
	return &Issuer{key: key, kid: active.KeyID, issuer: strings.TrimRight(issuerURL, "/")}, nil
}

// ReservedHeader also matches spellings with underscores for any dash: some
// servers fold underscores into dashes, which would let a forged alias through.
func ReservedHeader(name string) bool {
	return strings.EqualFold(strings.ReplaceAll(name, "_", "-"), Header)
}

// Strip removes all case variants, including noncanonical Header map entries.
func Strip(header http.Header) {
	for name := range header {
		if ReservedHeader(name) {
			delete(header, name)
		}
	}
}

// Mint returns no assertion for unsupported provenance.
// The caller must restrict this to private tunnel destinations. A matching
// owner organization is required even when some other route admitted a caller.
func (s *Issuer) Mint(ctx context.Context, target Target) (string, error) {
	identity, ok := mcpidentity.FromContext(ctx)
	if !ok {
		return "", nil
	}
	var subject, principalType string
	switch identity.Kind() {
	case mcpidentity.KindUserSession, mcpidentity.KindConsentDiscovery:
		principalType, subject = "user", identity.UserID()
	case mcpidentity.KindAPIKey:
		principalType, subject = "api_key", identity.APIKeyID()
	case mcpidentity.KindAgent:
		principalType, subject = "agent", identity.AgentID()
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
	now := time.Now()
	expires := now.Add(Lifetime)
	if deadline := identity.ExpiresAt(); !deadline.IsZero() && deadline.Before(expires) {
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
		"iat": now.Unix(), "exp": expires.Unix(), "jti": uuid.NewString(), "version": 1,
	}
	if principalType == "user" && auth.UserID == subject && auth.Email != nil && *auth.Email != "" {
		claims["email"] = *auth.Email
	}
	if identity.Kind() == mcpidentity.KindConsentDiscovery {
		claims["allowed_methods"] = []string{"server/discover", "initialize", "notifications/initialized", "ping", "tools/list"}
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	token.Header["typ"] = "speakeasy-authz+jwt"
	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", errors.New("sign caller assertion")
	}
	return signed, nil
}
