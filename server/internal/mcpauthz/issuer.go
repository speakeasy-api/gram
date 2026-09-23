// Package mcpauthz issues short-lived caller assertions for private MCP tunnels.
// These assertions identify the authenticated principal; the destination owns
// authorization and must verify the issuer, resource, tenant, and expiry.
package mcpauthz

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Header is reserved for Gram's signed caller assertion, without a Bearer prefix.
const Header = "SPEAKEASY_AUTHZ"

// JWKSPath is the public, issuer-wide verification-key endpoint.
const JWKSPath = "/.well-known/jwks.json"

// Lifetime bounds the bearer assertion's replay window.
const Lifetime = time.Minute

// Issuer holds an immutable signing key and precomputed public JWKS. A nil or
// zero issuer disables issuance and still serves an empty key set.
type Issuer struct {
	key    *rsa.PrivateKey
	kid    string
	issuer string
	jwks   []byte
	etag   string
}

// Target identifies the actual destination using server-owned metadata.
type Target struct {
	// OrganizationID is the destination owner's organization.
	OrganizationID string

	// ProjectID is the destination owner's project.
	ProjectID uuid.UUID

	// MCPServerID is the wrapper through which the destination is served.
	MCPServerID string

	// TunnelID is the immutable tunneled server ID, including on meta dispatch.
	TunnelID uuid.UUID
}

// New validates the complete configuration before serving traffic. Both key
// settings and issuer empty disables issuance; partial configuration is an error.
func New(privatePEM, publicPEM, issuerURL string, allowHTTP bool) (*Issuer, error) {
	if strings.TrimSpace(privatePEM) == "" && strings.TrimSpace(publicPEM) == "" && strings.TrimSpace(issuerURL) == "" {
		return &Issuer{key: nil, kid: "", issuer: "", jwks: nil, etag: ""}, nil
	}
	if strings.TrimSpace(privatePEM) == "" || strings.TrimSpace(publicPEM) == "" || strings.TrimSpace(issuerURL) == "" {
		return nil, errors.New("GRAM_AUTHZ_PRIVATE_KEY, GRAM_AUTHZ_PUBLIC_KEYS and GRAM_AUTHZ_ISSUER_URL are required")
	}
	u, err := url.Parse(issuerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Scheme != "https" && (!allowHTTP || u.Scheme != "http")) || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, errors.New("caller assertion issuer must be an HTTPS origin (HTTP allowed only locally)")
	}
	block, rest, err := decodePEM([]byte(privatePEM))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("GRAM_AUTHZ_PRIVATE_KEY must contain exactly one private PEM key")
	}
	if block.Type != "PRIVATE KEY" {
		return nil, errors.New("GRAM_AUTHZ_PRIVATE_KEY must be a PKCS#8 PEM key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	key, ok := parsed.(*rsa.PrivateKey)
	if err != nil || !ok || key.N.BitLen() < 2048 {
		return nil, errors.New("caller assertion private key must be RSA with at least 2048 bits")
	}
	if err := key.Validate(); err != nil {
		return nil, errors.New("invalid caller assertion RSA private key")
	}
	active, err := publicJWK(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	keys := make([]jose.JSONWebKey, 0)
	seen := make(map[string]bool)
	for remaining := []byte(publicPEM); len(bytes.TrimSpace(remaining)) > 0; {
		block, rest, err := decodePEM(remaining)
		if err != nil {
			return nil, errors.New("invalid GRAM_AUTHZ_PUBLIC_KEYS PEM bundle")
		}
		remaining = rest
		if block.Type != "PUBLIC KEY" {
			return nil, errors.New("GRAM_AUTHZ_PUBLIC_KEYS must contain only SubjectPublicKeyInfo PEM keys")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		pub, ok := parsed.(*rsa.PublicKey)
		if err != nil || !ok || pub.N.BitLen() < 2048 || pub.E < 3 || pub.E%2 == 0 {
			return nil, errors.New("caller assertion public keys must be RSA with at least 2048 bits")
		}
		jwk, err := publicJWK(pub)
		if err != nil {
			return nil, err
		}
		if !seen[jwk.KeyID] {
			keys = append(keys, jwk)
			seen[jwk.KeyID] = true
		}
	}
	if !seen[active.KeyID] {
		return nil, errors.New("active caller assertion public key is missing from GRAM_AUTHZ_PUBLIC_KEYS")
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].KeyID < keys[j].KeyID })
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("encode caller assertion JWKS: %w", err)
	}
	digest := sha256.Sum256(document)
	return &Issuer{key: key, kid: active.KeyID, issuer: strings.TrimRight(issuerURL, "/"), jwks: document,
		etag: `"` + base64.RawURLEncoding.EncodeToString(digest[:]) + `"`}, nil
}

func decodePEM(data []byte) (*pem.Block, []byte, error) {
	data = bytes.TrimSpace(data)
	if !bytes.HasPrefix(data, []byte("-----BEGIN ")) {
		return nil, nil, errors.New("expected PEM block")
	}
	block, rest := pem.Decode(data)
	if block == nil || len(block.Headers) != 0 || bytes.Count(data[:len(data)-len(rest)], []byte("-----BEGIN ")) != 1 {
		return nil, nil, errors.New("invalid PEM block")
	}
	return block, rest, nil
}

func publicJWK(key *rsa.PublicKey) (jose.JSONWebKey, error) {
	jwk := jose.JSONWebKey{Key: key, KeyID: "", Algorithm: string(jose.RS256), Use: "sig",
		Certificates: nil, CertificatesURL: nil, CertificateThumbprintSHA1: nil, CertificateThumbprintSHA256: nil}
	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return jose.JSONWebKey{}, fmt.Errorf("compute caller assertion key ID: %w", err)
	}
	jwk.KeyID = base64.RawURLEncoding.EncodeToString(thumbprint)
	return jwk, nil
}

// ReservedHeader matches both the wire spelling and the common dash alias.
func ReservedHeader(name string) bool {
	return strings.EqualFold(name, Header) || strings.EqualFold(name, "Speakeasy-Authz")
}

// Strip removes all case variants, including noncanonical Header map entries.
func Strip(header http.Header) {
	for name := range header {
		if ReservedHeader(name) {
			delete(header, name)
		}
	}
}

// Middleware serves public keys before session/custom-domain middleware and
// removes reserved inbound headers before downstream instrumentation sees them.
func (s *Issuer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Strip(r.Header)
		if r.URL.Path != JWKSPath {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body := []byte(`{"keys":[]}`)
		etag := `"disabled"`
		if s != nil && len(s.jwks) != 0 {
			body, etag = s.jwks, s.etag
		}
		w.Header().Set("Content-Type", "application/jwk-set+json")
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		w.Header().Set("ETag", etag)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	})
}

// Enabled reports whether startup configured an active signing key.
func (s *Issuer) Enabled() bool { return s != nil && s.key != nil }

// Mint returns no assertion for unsupported provenance or a disabled issuer.
// The caller must restrict this to private tunnel destinations. A matching
// owner organization is required even when some other route admitted a caller.
func (s *Issuer) Mint(ctx context.Context, target Target) (string, error) {
	if s == nil || s.key == nil {
		return "", nil
	}
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
		target.ProjectID == uuid.Nil || target.TunnelID == uuid.Nil || target.MCPServerID == "" ||
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
	claims := jwt.MapClaims{
		"iss": s.issuer, "sub": principalType + ":" + subject, "principal_type": principalType,
		"aud": urn.NewTunneledMcpServer(target.TunnelID).String(),
		"iat": now.Unix(), "exp": expires.Unix(), "jti": uuid.NewString(),
		"organization_id": target.OrganizationID, "project_id": target.ProjectID.String(),
		"mcp_server_id": target.MCPServerID, "tunneled_mcp_server_id": target.TunnelID.String(),
		"version": 1, "purpose": "mcp_request",
	}
	if principalType == "user" && auth.UserID == subject && auth.Email != nil && *auth.Email != "" {
		claims["email"] = *auth.Email
	}
	if identity.Kind() == mcpidentity.KindConsentDiscovery {
		claims["purpose"] = "mcp_discovery"
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
