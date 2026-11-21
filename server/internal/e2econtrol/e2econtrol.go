// Package e2econtrol is a control surface for end-to-end tests that run
// against a deployed, non-production environment (a per-PR preview or dev) and
// against a local stack through the same calls.
//
// It gives a test run two things it cannot get through the product:
//
//   - a signed-in dashboard session for the one configured test identity,
//     without driving the identity provider's hosted pages (e2e.signInLink,
//     then e2e.signIn in the browser);
//   - a known starting state for the one test organization (e2e.reseed), and
//     that organization's identifiers (e2e.tenant) so a test names nothing
//     environment-specific.
//
// It is off unless a control token is configured, and New refuses to build it
// in production whatever the configuration says.
package e2econtrol

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// TokenHeader carries the control token on every call except e2e.signIn,
	// which a browser opens and which is authorized by its one-time ticket.
	TokenHeader = "Gram-E2E-Token" //nolint:gosec // G101: a header name, not a credential

	// productionEnvironment is the one environment this surface never exists in.
	productionEnvironment = "prod"

	// ticketTTL is how long a sign-in link stays redeemable. The test opens it
	// in the step after it asks for it.
	ticketTTL = 60 * time.Second

	minTokenLength = 32
)

// ErrNotConfigured reports that no reseeder is wired for this environment.
var ErrNotConfigured = errors.New("e2e reseed is not configured for this environment")

// ErrSeedBusy reports that another seed run holds the seed lock.
var ErrSeedBusy = errors.New("another seed run is in progress")

// Config is the surface's configuration. A zero Token disables it.
type Config struct {
	// Environment is the deployment environment (GRAM_ENVIRONMENT).
	Environment string
	// Token is the shared control secret (GRAM_E2E_CONTROL_TOKEN).
	Token string
	// UserEmail is the only identity e2e.signIn signs in (GRAM_E2E_USER_EMAIL).
	UserEmail string
	// SiteURL is the dashboard origin the browser lands on after sign-in.
	SiteURL string
	// ServerURL is this server's own origin, the base of sign-in links.
	ServerURL string
	// Build is the commit this server was built from. A test run reads it from
	// e2e.tenant to wait until a deployment serves the commit under test.
	Build string
}

// Enabled reports whether the surface should be mounted.
func (c Config) Enabled() bool { return c.Token != "" }

// Tenant identifies the organization end-to-end tests run in. Tests read it
// from e2e.tenant instead of hard-coding slugs or seeded ids.
type Tenant struct {
	OrganizationID   string `json:"organization_id"`
	OrganizationSlug string `json:"organization_slug"`
	ProjectSlug      string `json:"project_slug"`
	// SeededUserPrefix prefixes the ids of the seeded fictional members.
	SeededUserPrefix string `json:"seeded_user_prefix"`
	// APIKeyName is the name of the seeded API key.
	APIKeyName string `json:"api_key_name"`
}

// Identity is the part of identity.Resolver the sign-in needs. It is the same
// pair the invitation callback uses: a server-created Magic Auth code, then
// the bootstrap every identity-provider login runs.
type Identity interface {
	AuthenticateWithMagicAuth(ctx context.Context, email string) (*identity.IDPUserInfo, error)
	CompleteIDPLogin(ctx context.Context, idpUser *identity.IDPUserInfo, opts identity.IDPLoginOptions) (identity.IDPLoginResult, error)
}

// SessionStore stores the dashboard session. Implemented by sessions.Manager.
type SessionStore interface {
	StoreSessionIssuedAt(ctx context.Context, session sessions.Session, issuedAt time.Time) error
}

// TicketStore holds one-time sign-in tickets. Implemented by cache.Cache.
type TicketStore interface {
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	GetAndDelete(ctx context.Context, key string, value any) error
}

// Reseeder brings the test organization to its known state and names it.
type Reseeder interface {
	// Reseed replaces the organization's data. It returns ErrSeedBusy when
	// another seed run holds the lock.
	Reseed(ctx context.Context) error
	// Tenant returns the organization's identifiers. It does not write.
	Tenant() Tenant
}

// Service serves the control surface.
type Service struct {
	logger   *slog.Logger
	cfg      Config
	identity Identity
	sessions SessionStore
	tickets  TicketStore
	reseeder Reseeder
	now      func() time.Time
}

// New builds the service. It returns an error, which the server treats as a
// refusal to start, when the configuration would enable the surface in
// production or leave it guessable.
func New(logger *slog.Logger, cfg Config, idp Identity, sessionStore SessionStore, tickets TicketStore, reseeder Reseeder) (*Service, error) {
	if !cfg.Enabled() {
		return nil, errors.New("e2e control: no control token configured")
	}
	if cfg.Environment == productionEnvironment || cfg.Environment == "" {
		return nil, fmt.Errorf("e2e control: refusing to enable in environment %q", cfg.Environment)
	}
	if len(cfg.Token) < minTokenLength {
		return nil, fmt.Errorf("e2e control: the control token must be at least %d characters", minTokenLength)
	}
	if !strings.Contains(cfg.UserEmail, "@") {
		return nil, errors.New("e2e control: a test user email is required")
	}
	for name, raw := range map[string]string{"site URL": cfg.SiteURL, "server URL": cfg.ServerURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, fmt.Errorf("e2e control: %s %q is not an absolute http(s) URL", name, raw)
		}
	}
	return &Service{
		logger:   logger.With(attr.SlogComponent("e2econtrol")),
		cfg:      cfg,
		identity: idp,
		sessions: sessionStore,
		tickets:  tickets,
		reseeder: reseeder,
		now:      time.Now,
	}, nil
}

// Attach mounts the routes.
func Attach(mux goahttp.Muxer, s *Service) {
	o11y.AttachHandler(mux, http.MethodPost, "/rpc/e2e.reseed", s.authorized(s.handleReseed))
	o11y.AttachHandler(mux, http.MethodGet, "/rpc/e2e.tenant", s.authorized(s.handleTenant))
	o11y.AttachHandler(mux, http.MethodPost, "/rpc/e2e.signInLink", s.authorized(s.handleSignInLink))
	o11y.AttachHandler(mux, http.MethodGet, "/rpc/e2e.signIn", s.handleSignIn)
}

func (s *Service) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := sha256.Sum256([]byte(r.Header.Get(TokenHeader)))
		want := sha256.Sum256([]byte(s.cfg.Token))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

type tenantResponse struct {
	Tenant
	Environment string `json:"environment"`
	Build       string `json:"build"`
	UserEmail   string `json:"user_email"`
}

type reseedResponse struct {
	tenantResponse
	DurationMS int64 `json:"duration_ms"`
}

func (s *Service) tenant() (tenantResponse, bool) {
	if s.reseeder == nil {
		return tenantResponse{Tenant: Tenant{OrganizationID: "", OrganizationSlug: "", ProjectSlug: "", SeededUserPrefix: "", APIKeyName: ""}, Environment: "", Build: "", UserEmail: ""}, false
	}
	return tenantResponse{Tenant: s.reseeder.Tenant(), Environment: s.cfg.Environment, Build: s.cfg.Build, UserEmail: s.cfg.UserEmail}, true
}

func (s *Service) handleTenant(w http.ResponseWriter, _ *http.Request) {
	tenant, ok := s.tenant()
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": ErrNotConfigured.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (s *Service) handleReseed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant, ok := s.tenant()
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": ErrNotConfigured.Error()})
		return
	}
	started := s.now()
	if err := s.reseeder.Reseed(ctx); err != nil {
		if errors.Is(err, ErrSeedBusy) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": ErrSeedBusy.Error()})
			return
		}
		s.logger.ErrorContext(ctx, "e2e reseed failed", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reseed failed"})
		return
	}
	elapsed := s.now().Sub(started)
	s.logger.InfoContext(ctx, "e2e tenant reseeded", attr.SlogOrganizationID(tenant.OrganizationID))
	writeJSON(w, http.StatusOK, reseedResponse{tenantResponse: tenant, DurationMS: elapsed.Milliseconds()})
}

type signInLinkResponse struct {
	// URL is opened once by the test's browser. It is spent on first use and
	// expires after ExpiresInSeconds, so it is safe to appear in test evidence.
	URL string `json:"url"`
	// Path is URL without its origin, for a browser already aimed at the
	// dashboard: every environment serves /rpc on the dashboard origin.
	Path             string `json:"path"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

func (s *Service) handleSignInLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create a ticket"})
		return
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.tickets.Set(ctx, ticketKey(ticket), true, ticketTTL); err != nil {
		s.logger.ErrorContext(ctx, "store e2e sign-in ticket", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store the ticket"})
		return
	}
	path := "/rpc/e2e.signIn?ticket=" + url.QueryEscape(ticket)
	writeJSON(w, http.StatusOK, signInLinkResponse{URL: strings.TrimRight(s.cfg.ServerURL, "/") + path, Path: path, ExpiresInSeconds: int(ticketTTL.Seconds())})
}

func (s *Service) handleSignIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ticket := r.URL.Query().Get("ticket")
	var live bool
	if ticket == "" || s.tickets.GetAndDelete(ctx, ticketKey(ticket), &live) != nil || !live {
		http.Error(w, "sign-in link is spent, expired or unknown", http.StatusForbidden)
		return
	}

	// Taken before authenticating so a sign-out everywhere that lands
	// meanwhile still ends this session.
	authenticatedAt := s.now()
	idpUser, err := s.identity.AuthenticateWithMagicAuth(ctx, s.cfg.UserEmail)
	if err != nil || idpUser == nil {
		s.logger.ErrorContext(ctx, "e2e sign-in: identity provider refused the test user", attr.SlogError(err))
		http.Error(w, "the identity provider refused the test user", http.StatusBadGateway)
		return
	}
	login, err := s.identity.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: false, Attribution: nil})
	if err != nil || login.UserInfo == nil {
		s.logger.ErrorContext(ctx, "e2e sign-in: login bootstrap failed", attr.SlogError(err))
		http.Error(w, "could not complete the login", http.StatusInternalServerError)
		return
	}

	// The session only ever enters the test organization. A test user who is
	// not a member of it gets no session at all.
	var active *sessions.Organization
	if tenant, ok := s.tenant(); ok {
		for i := range login.UserInfo.Organizations {
			if login.UserInfo.Organizations[i].ID == tenant.OrganizationID {
				active = &login.UserInfo.Organizations[i]
			}
		}
	} else if len(login.UserInfo.Organizations) == 1 {
		active = &login.UserInfo.Organizations[0]
	}
	if active == nil {
		http.Error(w, "the test user is not a member of the test organization", http.StatusConflict)
		return
	}

	sessionID, err := sessions.NewSessionID()
	if err != nil {
		http.Error(w, "could not create a session", http.StatusInternalServerError)
		return
	}
	session := sessions.Session{
		SessionID:             sessionID,
		UserID:                login.UserID,
		ActiveOrganizationID:  active.ID,
		WorkOSSessionID:       idpUser.WorkOSSessionID,
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	if err := s.sessions.StoreSessionIssuedAt(ctx, session, authenticatedAt); err != nil {
		s.logger.ErrorContext(ctx, "e2e sign-in: store session", attr.SlogError(err))
		http.Error(w, "could not store the session", http.StatusInternalServerError)
		return
	}

	//nolint:exhaustruct_v5 // only desired fields, as the login callback sets them
	http.SetCookie(w, &http.Cookie{
		Name:     constants.SessionCookie,
		Value:    sessionID,
		MaxAge:   constants.SessionCookieMaxAgeSeconds,
		Path:     "/",
		Secure:   s.cfg.Environment != "local",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	s.logger.InfoContext(ctx, "e2e sign-in complete", attr.SlogUserID(login.UserID), attr.SlogOrganizationID(active.ID))
	http.Redirect(w, r, strings.TrimRight(s.cfg.SiteURL, "/")+"/"+url.PathEscape(active.Slug), http.StatusSeeOther)
}

// ticketKey stores a ticket under its hash, so the cache never holds a value
// that would sign anyone in.
func ticketKey(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return "e2econtrol:signin:" + hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		return
	}
}
