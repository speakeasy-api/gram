package agent

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/tunnel/identity"
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

const (
	// maxAssertionBytes bounds the assertion header before any parsing.
	// Assertions are well under 2 KiB; the margin covers long audiences.
	maxAssertionBytes = 8 << 10 // 8 KiB

	// maxJWKSBytes bounds a key set response. A handful of RSA keys is a
	// few KiB.
	maxJWKSBytes = 64 << 10 // 64 KiB

	// jwksFetchTimeout bounds one key set fetch.
	jwksFetchTimeout = 5 * time.Second

	// jwksFreshFor is how long fetched keys are trusted without revalidation,
	// matching the published contract and the endpoint's max-age.
	jwksFreshFor = 5 * time.Minute

	// jwksUnknownKeyRetry spaces refreshes triggered by an unknown key id
	// while cached keys are fresh, so forged kids cannot hammer the issuer.
	jwksUnknownKeyRetry = 30 * time.Second

	// jwksExpiredRetry spaces refreshes while no fresh keys are available.
	jwksExpiredRetry = 5 * time.Second
)

var (
	errAssertionMissing  = errors.New("caller assertion missing")
	errAssertionInvalid  = errors.New("caller assertion invalid")
	errVerificationKeys  = errors.New("caller assertion verification keys unavailable")
	errUnknownAssertKey  = errors.New("caller assertion signed by an unknown key")
	typedSubjectPattern  = regexp.MustCompile(`^(user|api_key|agent):.+$`)
	assertionVersionJSON = []byte("1")
)

// principal is who a session belongs to. A request from another principal
// cannot see, use or end the session.
type principal struct {
	issuer         string
	subject        string
	organizationID string
	mcpServerID    string
	// consent is true for discovery-only assertions that carry allowed_methods.
	consent bool
}

// callerAssertion is a verified assertion. Its upstream credential claim is
// kept raw: whether it can authorize a request is judged only after the
// principal is matched to a session.
type callerAssertion struct {
	principal      principal
	expiresAt      time.Time
	allowedMethods map[string]struct{}
	credential     json.RawMessage
}

func (a callerAssertion) permits(method string) bool {
	if !a.principal.consent {
		return true
	}
	_, ok := a.allowedMethods[method]
	return ok
}

type assertionClaims struct {
	jwt.RegisteredClaims

	Version            json.RawMessage `json:"version"`
	OrganizationID     *string         `json:"organization_id"`
	MCPServerID        *string         `json:"mcp_server_id"`
	AllowedMethods     json.RawMessage `json:"allowed_methods"`
	UpstreamCredential json.RawMessage `json:"upstream_credential"`
}

type assertionVerifier struct {
	issuer         string
	audience       string
	organizationID string
	keys           *jwksCache
	now            func() time.Time
}

func newAssertionVerifier(cfg CredentialsConfig, client *http.Client, now func() time.Time) *assertionVerifier {
	return &assertionVerifier{
		issuer:         cfg.Issuer,
		audience:       cfg.Audience,
		organizationID: cfg.OrganizationID,
		keys:           newJWKSCache(cfg.JWKSURL, client, now),
		now:            now,
	}
}

// assertionHeader returns the request's only assertion value. Several
// values, or any alias spelling alongside the header, are refused.
func assertionHeader(header http.Header) (string, error) {
	var values []string
	for name, vs := range header {
		if identity.ReservedHeader(name) {
			values = append(values, vs...)
		}
	}
	switch {
	case len(values) == 0:
		return "", errAssertionMissing
	case len(values) > 1 || len(values[0]) > maxAssertionBytes || values[0] == "":
		return "", errAssertionInvalid
	}
	return values[0], nil
}

func (v *assertionVerifier) verify(ctx context.Context, header http.Header) (callerAssertion, error) {
	raw, err := assertionHeader(header)
	if err != nil {
		return callerAssertion{}, err
	}

	var claims assertionClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(identity.ClockSkew),
		jwt.WithTimeFunc(v.now),
	)
	_, err = parser.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if typ, _ := token.Header["typ"].(string); typ != identity.TokenType {
			return nil, errAssertionInvalid
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, errAssertionInvalid
		}
		return v.keys.key(ctx, kid)
	})
	switch {
	case errors.Is(err, errVerificationKeys), errors.Is(err, errUnknownAssertKey):
		return callerAssertion{}, err
	case err != nil:
		return callerAssertion{}, fmt.Errorf("%w: %w", errAssertionInvalid, err)
	}
	return v.checkClaims(claims)
}

func (v *assertionVerifier) checkClaims(claims assertionClaims) (callerAssertion, error) {
	invalid := func(reason string) (callerAssertion, error) {
		return callerAssertion{}, fmt.Errorf("%w: %s", errAssertionInvalid, reason)
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return invalid("iat and exp are required")
	}
	if lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time); lifetime <= 0 || lifetime > identity.MaxLifetime {
		return invalid("lifetime out of range")
	}
	if len(claims.Audience) != 1 {
		return invalid("exactly one audience is required")
	}
	if !bytes.Equal(bytes.TrimSpace(claims.Version), assertionVersionJSON) {
		return invalid("unsupported version")
	}
	if claims.OrganizationID == nil || *claims.OrganizationID != v.organizationID {
		return invalid("organization mismatch")
	}
	if !typedSubjectPattern.MatchString(claims.Subject) {
		return invalid("untyped subject")
	}
	if claims.MCPServerID == nil {
		return invalid("mcp_server_id is required")
	}
	serverID, err := uuid.Parse(*claims.MCPServerID)
	if err != nil || serverID == uuid.Nil || serverID.String() != *claims.MCPServerID {
		return invalid("invalid mcp_server_id")
	}

	assertion := callerAssertion{
		principal: principal{
			issuer:         claims.Issuer,
			subject:        claims.Subject,
			organizationID: *claims.OrganizationID,
			mcpServerID:    *claims.MCPServerID,
			consent:        claims.AllowedMethods != nil,
		},
		expiresAt:      claims.ExpiresAt.Time,
		allowedMethods: nil,
		credential:     claims.UpstreamCredential,
	}
	if claims.AllowedMethods != nil {
		var methods []string
		if bytes.Equal(bytes.TrimSpace(claims.AllowedMethods), []byte("null")) || json.Unmarshal(claims.AllowedMethods, &methods) != nil {
			return invalid("allowed_methods must be an array of strings")
		}
		assertion.allowedMethods = make(map[string]struct{}, len(methods))
		for _, m := range methods {
			assertion.allowedMethods[m] = struct{}{}
		}
	}
	return assertion, nil
}

// jwksCache holds the issuer's verification keys. Keys are trusted for
// jwksFreshFor after a successful fetch or revalidation; after that, a
// failed revalidation rejects every assertion rather than trusting keys the
// issuer may have withdrawn.
type jwksCache struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	etag        string
	freshUntil  time.Time
	lastAttempt time.Time
	// fetching is closed when the in-flight fetch finishes; nil when idle.
	fetching chan struct{}
}

func newJWKSCache(url string, client *http.Client, now func() time.Time) *jwksCache {
	return &jwksCache{
		url:         url,
		client:      client,
		now:         now,
		mu:          sync.Mutex{},
		keys:        nil,
		etag:        "",
		freshUntil:  time.Time{},
		lastAttempt: time.Time{},
		fetching:    nil,
	}
}

// jwksHTTPClient never follows redirects: the key set URL is pinned.
func jwksHTTPClient() *http.Client {
	return &http.Client{
		Timeout: jwksFetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *jwksCache) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	for refreshed := false; ; refreshed = true {
		c.mu.Lock()
		now := c.now()
		fresh := now.Before(c.freshUntil)
		if fresh {
			if key, ok := c.keys[kid]; ok {
				c.mu.Unlock()
				return key, nil
			}
		}
		if refreshed {
			c.mu.Unlock()
			if fresh {
				return nil, errUnknownAssertKey
			}
			return nil, errVerificationKeys
		}
		if wait := c.fetching; wait != nil {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, errVerificationKeys
			}
		}
		minGap := jwksExpiredRetry
		if fresh {
			minGap = jwksUnknownKeyRetry
		}
		if !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < minGap {
			c.mu.Unlock()
			if fresh {
				return nil, errUnknownAssertKey
			}
			return nil, errVerificationKeys
		}
		done := make(chan struct{})
		c.fetching = done
		c.lastAttempt = now
		etag := c.etag
		c.mu.Unlock()

		keys, newETag, notModified, err := c.fetch(etag)

		c.mu.Lock()
		if err == nil {
			if !notModified {
				c.keys, c.etag = keys, newETag
			}
			c.freshUntil = c.now().Add(jwksFreshFor)
		}
		c.fetching = nil
		close(done)
		c.mu.Unlock()
	}
}

// fetch runs detached from any one request so a cancelled caller cannot fail
// the fetch other requests are waiting on.
func (c *jwksCache) fetch(etag string) (map[string]*rsa.PublicKey, string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), jwksFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("build key set request: %w", err)
	}
	req.Header.Set("Accept", "application/jwk-set+json, application/json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("fetch key set: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotModified:
		if etag == "" {
			return nil, "", false, errors.New("key set not modified without a cached copy")
		}
		return nil, etag, true, nil
	case http.StatusOK:
	default:
		return nil, "", false, fmt.Errorf("fetch key set: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, "", false, fmt.Errorf("read key set: %w", err)
	}
	if len(body) > maxJWKSBytes {
		return nil, "", false, errors.New("key set too large")
	}
	keys, err := parseJWKS(body)
	if err != nil {
		return nil, "", false, err
	}
	return keys, resp.Header.Get("ETag"), false, nil
}

// parseJWKS keeps RSA signing keys whose kid is their RFC 7638 thumbprint,
// the only form the issuer publishes.
func parseJWKS(body []byte) (map[string]*rsa.PublicKey, error) {
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("decode key set: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, key := range set.Keys {
		pub, ok := key.Key.(*rsa.PublicKey)
		if !ok || (key.Use != "" && key.Use != "sig") || (key.Algorithm != "" && key.Algorithm != jwt.SigningMethodRS256.Alg()) {
			continue
		}
		canonical, err := jwks.PublicKey(pub)
		if err != nil || canonical.KeyID != key.KeyID {
			continue
		}
		keys[key.KeyID] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("key set has no usable RSA signing keys")
	}
	return keys, nil
}

// bearerToken returns the request's only Authorization bearer token.
func bearerToken(header http.Header) (string, bool) {
	var values []string
	for name, vs := range header {
		if strings.EqualFold(name, "Authorization") {
			values = append(values, vs...)
		}
	}
	if len(values) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || len(token) > maxBearerBytes || !b64TokenPattern.MatchString(token) {
		return "", false
	}
	return token, true
}

// maxBearerBytes bounds an upstream access token. Large JWT access tokens
// stay well under it.
const maxBearerBytes = 16 << 10 // 16 KiB

// b64TokenPattern is RFC 6750's b64token.
var b64TokenPattern = regexp.MustCompile(`^[A-Za-z0-9\-._~+/]+=*$`)
