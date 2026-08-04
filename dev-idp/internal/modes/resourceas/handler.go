// Package resourceas serves the redeeming half of the cross-app access flow:
// the resource authorization servers that accept an Identity Assertion JWT
// Authorization Grant (ID-JAG) under the RFC 7523 jwt-bearer grant and hand
// back an access token for the MCP server behind them.
//
// Each row in xaa_resources is one such server, mounted at
// /resource-as/<slug>. They are rows rather than a singleton for two reasons:
// an ID-JAG names exactly one audience, so testing that a grant minted for
// one resource is refused at another needs a second resource to exist; and
// trust rules are per-resource, so modelling two trust domains needs two.
//
// The IdP half lives in internal/modes/oauth21. A resource here is under no
// obligation to trust it -- xaa_trust_rules decides, and pointing a resource
// at a foreign issuer is a supported configuration.
package resourceas

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/dev-idp/internal/cimd"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
	"github.com/speakeasy-api/gram/dev-idp/internal/xaa"
)

// Prefix is the URL prefix the dev-idp listener mounts this handler under.
// The resource slug is the next path segment.
const Prefix = xaa.ResourceASPrefix

const (
	accessTokenLifetime = 1 * time.Hour

	// maxFormBodyBytes caps /token and /introspect request bodies. An ID-JAG
	// assertion is the largest thing posted here and is a few KiB at most.
	maxFormBodyBytes = 64 << 10

	// idJAGClockSkew is how far in the future an ID-JAG's iat may sit and
	// still be accepted, covering ordinary clock drift between the issuing
	// IdP and this server.
	idJAGClockSkew = 60 * time.Second
)

// Config carries the static configuration for the resource servers.
type Config struct {
	// ExternalURL is the dev-idp's externally reachable base URL, with no
	// trailing slash and no prefix. Each resource's issuer identifier is
	// derived from it and the resource slug.
	ExternalURL string
}

// Handler serves every resource authorization server; which one a request
// lands on is the {slug} path wildcard.
type Handler struct {
	cfg      Config
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *sql.DB
	keystore *keystore.Keystore
	// httpClient dereferences CIMD client_id URLs and, for ID-JAGs from an
	// issuer that is not this dev-idp, that issuer's metadata and JWKS.
	httpClient *http.Client
}

func NewHandler(cfg Config, ks *keystore.Keystore, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
		cfg:        cfg,
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/resourceas"),
		logger:     logger.With(slog.String("component", "devidp.resource-as")),
		db:         db,
		keystore:   ks,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Handler returns the http.Handler to mount under Prefix (use
// http.StripPrefix). All registered paths are relative to that prefix.
//
// The two discovery documents are NOT mounted here: RFC 8414 and RFC 9728
// both place their well-known URI at the host root with the issuer's path
// component appended after. Wire those via RegisterRootRoutes.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{slug}/token", h.handleToken)
	mux.HandleFunc("POST /{slug}/introspect", h.handleIntrospect)
	return mux
}

// RegisterRootRoutes mounts the two discovery documents on the host-root mux.
// Per RFC 8414 §3 the well-known suffix goes after the host and the issuer's
// path component after that, so a resource with slug "chat" publishes its
// authorization-server metadata at
// "<host>/.well-known/oauth-authorization-server/resource-as/chat".
func (h *Handler) RegisterRootRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server"+Prefix+"/{slug}", h.handleASMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource"+Prefix+"/{slug}", h.handleProtectedResourceMetadata)
}

// issuer is the absolute URL identifying one resource's authorization server.
func (h *Handler) issuer(slug string) string {
	return xaa.ResourceASIssuer(h.cfg.ExternalURL, slug)
}

// =============================================================================
// Discovery — RFC 8414 + RFC 9728
// =============================================================================

type asMetadata struct {
	Issuer                            string   `json:"issuer"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`

	// AuthorizationGrantProfilesSupported is the field an MCP client reads to
	// decide whether this server speaks enterprise-managed authorization. It
	// looks for exactly xaa.GrantProfileIDJAG.
	AuthorizationGrantProfilesSupported []string `json:"authorization_grant_profiles_supported"`
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

func (h *Handler) handleASMetadata(w http.ResponseWriter, r *http.Request) {
	resource := h.lookupResource(r.Context(), w, r.PathValue("slug"))
	if resource == nil {
		return
	}

	iss := h.issuer(resource.Slug)
	writeJSON(w, http.StatusOK, asMetadata{
		Issuer:                            iss,
		TokenEndpoint:                     iss + "/token",
		IntrospectionEndpoint:             iss + "/introspect",
		GrantTypesSupported:               []string{xaa.GrantTypeJWTBearer},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   h.advertisedScopes(r.Context(), resource),
		ClientIDMetadataDocumentSupported: true,

		AuthorizationGrantProfilesSupported: []string{xaa.GrantProfileIDJAG},
	})
}

func (h *Handler) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	resource := h.lookupResource(r.Context(), w, r.PathValue("slug"))
	if resource == nil {
		return
	}

	writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource:             resource.ResourceIdentifier,
		AuthorizationServers: []string{h.issuer(resource.Slug)},
		ScopesSupported:      h.advertisedScopes(r.Context(), resource),
	})
}

// advertisedScopes is the union of every scope ceiling configured across this
// resource's trust rules. A rule with no ceiling contributes nothing, since
// "no ceiling" is not a scope. Best-effort: a read failure advertises none
// rather than failing the discovery document.
func (h *Handler) advertisedScopes(ctx context.Context, resource *repo.XaaResource) []string {
	rules, err := repo.New(h.db).ListXaaTrustRules(ctx, repo.ListXaaTrustRulesParams{
		After:      uuid.Nil,
		ResourceID: uuid.NullUUID{UUID: resource.ID, Valid: true},
		MaxRows:    100,
	})
	if err != nil {
		h.logger.WarnContext(ctx, "list trust rules for discovery", slog.Any("error", err))
		return []string{}
	}

	scopes := []string{}
	for _, rule := range rules {
		for _, s := range xaa.ScopeList(rule.AllowedScopes) {
			if !slices.Contains(scopes, s) {
				scopes = append(scopes, s)
			}
		}
	}
	slices.Sort(scopes)
	return scopes
}

// =============================================================================
// /token — RFC 7523 jwt-bearer, carrying an ID-JAG
// =============================================================================

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "failed to parse form")
		return
	}

	resource := h.lookupResource(ctx, w, r.PathValue("slug"))
	if resource == nil {
		return
	}

	if got := r.Form.Get("grant_type"); got != xaa.GrantTypeJWTBearer {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("this authorization server only accepts %s", xaa.GrantTypeJWTBearer))
		return
	}

	assertion := r.Form.Get("assertion")
	if assertion == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "assertion is required and must be an ID-JAG")
		return
	}

	claims, err := h.verifyIDJAG(ctx, resource, assertion)
	if err != nil {
		h.logger.InfoContext(ctx, "reject id-jag",
			slog.String("resource", resource.Slug),
			slog.Any("error", err),
		)
		oauthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// The client_id claim names who the IdP authorized to redeem this grant.
	// Whoever is actually calling must be that client.
	if err := h.authenticateClient(ctx, r, claims); err != nil {
		oauthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}

	queries := repo.New(h.db)

	// Single use, claimed before the token is minted so a concurrent replay
	// loses the race rather than getting a second token.
	if _, err := queries.ClaimXaaRedeemedJag(ctx, repo.ClaimXaaRedeemedJagParams{
		Issuer:     claims.Issuer,
		Jti:        claims.ID,
		ResourceID: resource.ID,
		ExpiresAt:  claims.ExpiresAt.Time,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "this ID-JAG has already been redeemed")
			return
		}
		h.logger.ErrorContext(ctx, "claim id-jag", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to record redemption")
		return
	}

	userID, err := h.resolveSubject(ctx, queries, claims)
	if err != nil {
		h.logger.ErrorContext(ctx, "resolve id-jag subject", slog.Any("error", err))
		oauthError(w, http.StatusBadRequest, "invalid_grant", "could not resolve the subject this grant names")
		return
	}

	// The trust rule's ceiling is applied on top of whatever the IdP already
	// granted: this server narrows, it never widens.
	rule, err := queries.GetXaaTrustRuleForIssuer(ctx, repo.GetXaaTrustRuleForIssuerParams{
		ResourceID:    resource.ID,
		TrustedIssuer: claims.Issuer,
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "reload trust rule", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load trust rule")
		return
	}
	granted := xaa.NarrowScope(claims.Scope, rule.AllowedScopes)

	access := "xaa_" + randomHex(32)
	if _, err := queries.CreateXaaResourceToken(ctx, repo.CreateXaaResourceTokenParams{
		Token:      access,
		ResourceID: resource.ID,
		UserID:     userID,
		ClientID:   claims.ClientID,
		Audience:   resource.ResourceIdentifier,
		Scope:      granted,
		ExpiresAt:  time.Now().Add(accessTokenLifetime),
	}); err != nil {
		h.logger.ErrorContext(ctx, "issue resource access token", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(accessTokenLifetime.Seconds()),
		Scope:       granted,
	})
}

// verifyIDJAG runs the full acceptance check on an assertion and returns its
// claims. Every failure is phrased for a developer reading the response body,
// because that is the whole point of this server existing.
func (h *Handler) verifyIDJAG(ctx context.Context, resource *repo.XaaResource, assertion string) (xaa.Claims, error) {
	// Parse unverified first: the issuer decides which key verifies the
	// signature, and the issuer is inside the token.
	var claims xaa.Claims
	unverified, _, err := jwt.NewParser().ParseUnverified(assertion, &claims)
	if err != nil {
		return xaa.Claims{}, fmt.Errorf("assertion is not a JWT: %w", err)
	}

	// The typ header is what distinguishes an ID-JAG from an ordinary
	// id_token. Without this check any id_token the IdP ever signed would be
	// redeemable here.
	if typ, _ := unverified.Header["typ"].(string); typ != xaa.JWTType {
		return xaa.Claims{}, fmt.Errorf("assertion typ header is %q, want %q", typ, xaa.JWTType)
	}

	issuerURL := h.issuer(resource.Slug)
	if !slices.Contains(claims.Audience, issuerURL) {
		return xaa.Claims{}, fmt.Errorf("assertion aud %v does not name this authorization server (%s)", claims.Audience, issuerURL)
	}

	if claims.ID == "" {
		return xaa.Claims{}, errors.New("assertion has no jti, so it cannot be enforced as single-use")
	}

	rule, err := repo.New(h.db).GetXaaTrustRuleForIssuer(ctx, repo.GetXaaTrustRuleForIssuerParams{
		ResourceID:    resource.ID,
		TrustedIssuer: claims.Issuer,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return xaa.Claims{}, fmt.Errorf("this resource has no trust rule for issuer %q", claims.Issuer)
		}
		return xaa.Claims{}, fmt.Errorf("load trust rule: %w", err)
	}
	if !rule.Enabled {
		return xaa.Claims{}, fmt.Errorf("the trust rule for issuer %q is disabled", claims.Issuer)
	}

	if err := allowedByTrustRule(rule, claims.ClientID); err != nil {
		return xaa.Claims{}, err
	}

	// Only now, with the issuer known to be trusted, resolve its key. Doing
	// it in this order means an untrusted issuer never causes an outbound
	// request.
	key, err := h.verificationKey(ctx, claims.Issuer, unverified)
	if err != nil {
		return xaa.Claims{}, fmt.Errorf("resolve signing key for issuer %q: %w", claims.Issuer, err)
	}

	var verified xaa.Claims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(claims.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(idJAGClockSkew),
	)
	if _, err := parser.ParseWithClaims(assertion, &verified, func(*jwt.Token) (any, error) { return key, nil }); err != nil {
		return xaa.Claims{}, fmt.Errorf("assertion did not verify: %w", err)
	}

	if resource.ResourceIdentifier != "" && verified.Resource != "" && verified.Resource != resource.ResourceIdentifier {
		return xaa.Claims{}, fmt.Errorf("assertion resource %q is not the resource behind this authorization server (%s)", verified.Resource, resource.ResourceIdentifier)
	}

	return verified, nil
}

// allowedByTrustRule checks a client id against a rule's allowlist. An empty
// or absent list means any client the issuer vouched for is acceptable.
func allowedByTrustRule(rule repo.XaaTrustRule, clientID string) error {
	raw := strings.TrimSpace(rule.AllowedClientIds)
	if raw == "" || raw == "[]" {
		return nil
	}
	var allowed []string
	if err := json.Unmarshal([]byte(raw), &allowed); err != nil {
		return fmt.Errorf("trust rule allowed_client_ids is not a JSON array: %w", err)
	}
	if len(allowed) > 0 && !slices.Contains(allowed, clientID) {
		return fmt.Errorf("client %q is not on this trust rule's allowlist", clientID)
	}
	return nil
}

// verificationKey resolves the RSA public key that should have signed an
// assertion from `issuer`.
//
// When the issuer is this dev-idp's own oauth2-1 server the key is taken
// straight from the keystore -- the alternative would be the process making
// an HTTP request to itself. Any other issuer is resolved the way a real
// server would: RFC 8414 metadata, then the jwks_uri it names, then the key
// matching the assertion's kid. Nothing is cached, because a dev-idp sees a
// handful of redemptions and a stale key is a worse failure than a slow one.
func (h *Handler) verificationKey(ctx context.Context, issuer string, token *jwt.Token) (*rsa.PublicKey, error) {
	if issuer == strings.TrimRight(h.cfg.ExternalURL, "/")+"/oauth2-1" {
		return h.keystore.PublicKey(), nil
	}

	kid, _ := token.Header["kid"].(string)
	jwksURI, err := h.discoverJWKSURI(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return h.fetchJWKSKey(ctx, jwksURI, kid)
}

func (h *Handler) discoverJWKSURI(ctx context.Context, issuer string) (string, error) {
	parsed, err := parseIssuerURL(issuer)
	if err != nil {
		return "", err
	}
	metadataURL := parsed.Scheme + "://" + parsed.Host + "/.well-known/oauth-authorization-server" + parsed.Path

	var doc struct {
		JwksURI string `json:"jwks_uri"`
	}
	if err := h.getJSON(ctx, metadataURL, &doc); err != nil {
		return "", fmt.Errorf("fetch authorization server metadata: %w", err)
	}
	if doc.JwksURI == "" {
		return "", fmt.Errorf("issuer metadata at %s declares no jwks_uri", metadataURL)
	}
	return doc.JwksURI, nil
}

func (h *Handler) fetchJWKSKey(ctx context.Context, jwksURI, kid string) (*rsa.PublicKey, error) {
	var doc jwksDocument
	if err := h.getJSON(ctx, jwksURI, &doc); err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}

	for _, key := range doc.Keys {
		if kid != "" && key.Kid != kid {
			continue
		}
		if key.Kty != "RSA" {
			continue
		}
		pub, err := key.rsaPublicKey()
		if err != nil {
			return nil, fmt.Errorf("decode jwk %q: %w", key.Kid, err)
		}
		return pub, nil
	}
	return nil, fmt.Errorf("no RSA key with kid %q in %s", kid, jwksURI)
}

// authenticateClient checks that whoever is calling is the client the ID-JAG
// was minted for.
//
// There is no shared secret in this profile: the assertion is already bound
// to one audience and is single-use, and MCP clients are commonly public. So
// the check is that the presented client_id equals the one the IdP put in the
// grant. When that id is a Client ID Metadata Document URL it is dereferenced
// too, which is the CIMD draft's own requirement and catches a client
// pointing at a document that does not describe it.
func (h *Handler) authenticateClient(ctx context.Context, r *http.Request, claims xaa.Claims) error {
	presented := r.Form.Get("client_id")
	if presented == "" {
		return errors.New("client_id is required")
	}
	if presented != claims.ClientID {
		return fmt.Errorf("client_id %q is not the client this grant was issued to", presented)
	}
	if cimd.IsClientID(presented) {
		if _, err := cimd.Fetch(ctx, h.httpClient, presented); err != nil {
			return fmt.Errorf("client metadata document is not usable: %w", err)
		}
	}
	return nil
}

// resolveSubject maps an ID-JAG's subject onto a local user row.
//
// A grant from this dev-idp's own IdP carries a users.id as `sub`, so that
// path is a direct lookup. A grant from a foreign issuer carries an id
// meaningful only in that issuer's namespace, so the `email` claim is what
// identifies the person -- and a user who has never been seen here is
// created, which is what a real resource app does on first cross-domain
// access.
func (h *Handler) resolveSubject(ctx context.Context, queries *repo.Queries, claims xaa.Claims) (uuid.UUID, error) {
	if id, err := uuid.Parse(claims.Subject); err == nil {
		if _, err := queries.GetUser(ctx, id); err == nil {
			return id, nil
		}
	}

	if claims.Email == "" {
		return uuid.Nil, fmt.Errorf("subject %q is not a local user and the grant carries no email to match on", claims.Subject)
	}
	user, err := queries.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
		ID:          uuid.New(),
		Email:       claims.Email,
		DisplayName: claims.Email,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert user for foreign subject: %w", err)
	}
	return user.ID, nil
}

// =============================================================================
// /introspect — RFC 7662
// =============================================================================

type introspectionResponse struct {
	Active    bool   `json:"active"`
	Scope     string `json:"scope,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	Username  string `json:"username,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Exp       int64  `json:"exp,omitempty"`
	Sub       string `json:"sub,omitempty"`
	Aud       string `json:"aud,omitempty"`
	Iss       string `json:"iss,omitempty"`
}

// handleIntrospect makes the audience restriction observable. Without it a
// caller could see that a token was issued but not that it is bound to one
// MCP server, which is the property this profile turns on.
func (h *Handler) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "failed to parse form")
		return
	}

	resource := h.lookupResource(ctx, w, r.PathValue("slug"))
	if resource == nil {
		return
	}

	// RFC 7662 §2.2: an unknown or expired token is a 200 with active=false,
	// not an error.
	inactive := introspectionResponse{
		Active: false, Scope: "", ClientID: "", Username: "",
		TokenType: "", Exp: 0, Sub: "", Aud: "", Iss: "",
	}

	token := r.Form.Get("token")
	if token == "" {
		writeJSON(w, http.StatusOK, inactive)
		return
	}

	queries := repo.New(h.db)
	stored, err := queries.GetActiveXaaResourceToken(ctx, repo.GetActiveXaaResourceTokenParams{
		Token:      token,
		ResourceID: resource.ID,
		Ts:         time.Now(),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, inactive)
		return
	}

	username := ""
	if user, err := queries.GetUser(ctx, stored.UserID); err == nil {
		username = user.Email
	}

	writeJSON(w, http.StatusOK, introspectionResponse{
		Active:    true,
		Scope:     stored.Scope,
		ClientID:  stored.ClientID,
		Username:  username,
		TokenType: "Bearer",
		Exp:       stored.ExpiresAt.Unix(),
		Sub:       stored.UserID.String(),
		Aud:       stored.Audience,
		Iss:       h.issuer(resource.Slug),
	})
}

// =============================================================================
// Helpers
// =============================================================================

// lookupResource resolves the {slug} wildcard to a resource row, writing the
// error response itself and returning nil when there is no such resource.
func (h *Handler) lookupResource(ctx context.Context, w http.ResponseWriter, slug string) *repo.XaaResource {
	resource, err := repo.New(h.db).GetXaaResourceBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			oauthError(w, http.StatusNotFound, "invalid_request", fmt.Sprintf("no resource authorization server is registered at %q", slug))
			return nil
		}
		h.logger.ErrorContext(ctx, "load resource", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load resource")
		return nil
	}
	return &resource
}

func (h *Handler) getJSON(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, maxFormBodyBytes)).Decode(into); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError writes an OAuth-shaped error envelope (RFC 6749 §5.2).
func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}
