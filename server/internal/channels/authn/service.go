// Package authn owns the Channels authorization server and its credential store.
// It accepts only user-delegated sessions, never native agent or workload tokens.
package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

const Scope = "channels:connect"
const proofCookie = "__Host-channels-oauth-proof"
const accessLifetime = 15 * time.Minute
const grantLifetime = 30 * 24 * time.Hour

// Identity is an authenticated user and the URL-resolved organization/project,
// not an authorization decision. Admit rechecks live membership and project access.
type Identity struct {
	OrganizationID        string
	ProjectID             uuid.UUID
	BrowserOrganizationID string
	BrowserSessionID      string
	Principal             urn.Principal
}
type Config struct {
	Logger  *slog.Logger
	BaseURL *url.URL
	// DashboardURL is the explicitly configured browser proxy origin. It may
	// receive the existing local sign-in callback, but is never a token issuer.
	DashboardURL        *url.URL
	DB                  *pgxpool.Pool
	Signer              *sessiontokens.Signer
	AuthenticateBrowser func(*http.Request) (context.Context, error)
	Admit               func(context.Context, Identity) (context.Context, error)
}
type clientMetadataResolver interface {
	Resolve(context.Context, string, cimd.CacheState) (*cimd.CacheResult, error)
}
type Service struct {
	lastCleanup     atomic.Int64
	config          Config
	issuer, origin  string
	dashboardOrigin string
	clients         clientMetadataResolver
}

func New(c Config) (*Service, error) {
	if c.BaseURL == nil || c.BaseURL.Host == "" || c.BaseURL.User != nil || c.BaseURL.RawQuery != "" || c.BaseURL.Fragment != "" || (c.BaseURL.Path != "" && c.BaseURL.Path != "/") {
		return nil, errors.New("invalid Channels OAuth base URL")
	}
	if c.BaseURL.Scheme != "https" && (c.BaseURL.Scheme != "http" || (c.BaseURL.Hostname() != "localhost" && c.BaseURL.Hostname() != "127.0.0.1" && c.BaseURL.Hostname() != "::1")) {
		return nil, errors.New("channels OAuth requires HTTPS")
	}
	if c.DB == nil || c.Signer == nil || c.AuthenticateBrowser == nil || c.Admit == nil {
		return nil, errors.New("incomplete Channels OAuth configuration")
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
	dashboardOrigin := ""
	if c.DashboardURL != nil {
		dashboard := c.DashboardURL
		if dashboard.Host == "" || dashboard.User != nil || dashboard.Opaque != "" || dashboard.RawQuery != "" || dashboard.Fragment != "" || (dashboard.Path != "" && dashboard.Path != "/") || dashboard.RawPath != "" {
			return nil, errors.New("invalid channels OAuth dashboard origin")
		}
		if dashboard.Scheme != "https" && (dashboard.Scheme != "http" || (dashboard.Hostname() != "localhost" && dashboard.Hostname() != "127.0.0.1" && dashboard.Hostname() != "::1")) {
			return nil, errors.New("channels OAuth dashboard origin requires HTTPS")
		}
		dashboardOrigin = dashboard.Scheme + "://" + dashboard.Host
	}
	origin := c.BaseURL.Scheme + "://" + c.BaseURL.Host
	policy := guardian.NewDefaultPolicy(otel.GetTracerProvider())
	return &Service{lastCleanup: atomic.Int64{}, config: c, origin: origin, dashboardOrigin: dashboardOrigin, issuer: origin + "/channels/oauth", clients: cimd.NewResolver(policy, otel.GetMeterProvider(), c.Logger)}, nil
}
func (s *Service) Issuer() string { return s.issuer }
func (s *Service) Resource(orgSlug, projectSlug string) string {
	return s.origin + "/channels/" + orgSlug + "/" + projectSlug + "/mcp"
}
func (s *Service) ProtectedResourceURL(orgSlug, projectSlug string) string {
	return s.origin + "/.well-known/oauth-protected-resource/channels/" + orgSlug + "/" + projectSlug + "/mcp"
}
func (s *Service) Attach(mux interface {
	Handle(string, string, http.HandlerFunc)
}) {
	mux.Handle("GET", "/.well-known/oauth-protected-resource/channels/{orgSlug}/{projectSlug}/mcp", s.wrap(s.protectedResource))
	mux.Handle("GET", "/.well-known/oauth-authorization-server/channels/oauth", s.wrap(s.metadata))
	mux.Handle("POST", "/channels/oauth/register", s.wrap(s.register))
	mux.Handle("GET", "/channels/oauth/authorize", s.wrap(s.authorize))
	mux.Handle("POST", "/channels/oauth/authorize", s.wrap(s.consent))
	mux.Handle("POST", "/channels/oauth/token", s.wrap(s.token))
	mux.Handle("POST", "/channels/oauth/revoke", s.wrap(s.revoke))
}
func (s *Service) wrap(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h(w, r)
	}
}
func (s *Service) metadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"issuer": s.issuer, "authorization_endpoint": s.issuer + "/authorize", "token_endpoint": s.issuer + "/token", "registration_endpoint": s.issuer + "/register", "revocation_endpoint": s.issuer + "/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic", "client_secret_post"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{Scope}, "client_id_metadata_document_supported": true, "resource_indicators_supported": true})
}
func (s *Service) protectedResource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource")
	resource := s.origin + path
	if r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	if _, err := s.target(r.Context(), resource); err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"resource": resource, "authorization_servers": []string{s.issuer}, "bearer_methods_supported": []string{"header"}, "scopes_supported": []string{Scope}})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func timestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}
func parseIdentity(org, principal string) (Identity, error) {
	p, err := urn.ParsePrincipal(principal)
	if err != nil || p.Type != urn.PrincipalTypeUser || org == "" {
		return Identity{}, errors.New("channels requires a user identity")
	}
	return Identity{OrganizationID: org, ProjectID: uuid.Nil, BrowserOrganizationID: "", BrowserSessionID: "", Principal: p}, nil
}
func (s *Service) browser(r *http.Request) (context.Context, Identity, error) {
	ctx, err := s.config.AuthenticateBrowser(r)
	if err != nil {
		return nil, Identity{}, err
	}
	a, ok := contextvalues.GetAuthContext(ctx)
	p, actor := contextvalues.AuthenticatedActor(ctx)
	if !ok || !actor || !contextvalues.HasValidatedGramSession(ctx) || contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) || p.Type != urn.PrincipalTypeUser || a.UserID != p.ID || a.ActiveOrganizationID == "" {
		return nil, Identity{}, errors.New("channels consent requires a browser user session")
	}
	target, found := TargetFromContext(r.Context())
	if !found {
		return nil, Identity{}, errors.New("channels target required")
	}
	id, err := parseIdentity(target.OrganizationID, p.String())
	id.ProjectID = target.ProjectID
	id.BrowserOrganizationID = a.ActiveOrganizationID
	if a.SessionID != nil {
		id.BrowserSessionID = *a.SessionID
	}
	if err != nil {
		return nil, Identity{}, err
	}
	ctx, err = s.config.Admit(ctx, id)
	return ctx, id, err
}

// Authenticate validates the exact issuer/audience and consults this service's
// live token, grant, and client records. Store outages fail closed.
func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	target, ok := TargetFromContext(ctx)
	if !ok {
		return Identity{}, errors.New("channels target required")
	}
	claims, err := s.config.Signer.ValidateExactAudience(token, target.Resource)
	if err != nil || claims.Issuer != s.issuer || claims.ID == "" {
		return Identity{}, errors.New("invalid Channels access token")
	}
	row, err := repo.New(s.config.DB).GetAccessIdentity(ctx, repo.GetAccessIdentityParams{Jti: claims.ID, OrganizationID: target.OrganizationID, ProjectID: target.ProjectID})
	if err != nil {
		return Identity{}, fmt.Errorf("load Channels access token: %w", err)
	}
	id, err := parseIdentity(row.OrganizationID, row.Principal)
	id.ProjectID = row.ProjectID
	if err != nil || claims.Subject != id.Principal.String() || claims.ClientID != row.ClientID || row.Resource != target.Resource {
		return Identity{}, errors.New("invalid Channels token identity")
	}
	if _, err = s.config.Admit(ctx, id); err != nil {
		return Identity{}, err
	}
	return id, nil
}
func parseForm(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		oauthError(w, 400, "invalid_request")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request")
		return false
	}
	for key, v := range r.PostForm {
		if key != "resource" && len(v) != 1 {
			oauthError(w, 400, "invalid_request")
			return false
		}
	}
	return true
}

// Cleanup is opportunistic and bounded, at most once per minute per process.
func (s *Service) cleanup(ctx context.Context) {
	now := time.Now().Unix()
	last := s.lastCleanup.Load()
	if now-last < 60 || !s.lastCleanup.CompareAndSwap(last, now) {
		return
	}
	if err := s.Cleanup(ctx); err != nil {
		s.config.Logger.ErrorContext(ctx, "Channels OAuth cleanup failed", attr.SlogError(err))
	}
}
