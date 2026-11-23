// Package e2econtrol is a control surface for end-to-end tests. It gives a test
// run two things it cannot get through the product:
//
//   - a known starting state for the one test organization (e2e.reseed), and
//     the names a test needs to address it (e2e.tenant);
//   - a signed-in dashboard session for the one test user, without driving the
//     identity provider's pages (e2e.signInLink, then e2e.signIn in the browser).
//
// No route takes an organization, a user or a destination from its caller: all
// three are fixed when the server starts. No route calls an identity provider.
//
// Where the surface may exist, and which organization and user it is fixed to
// there, is decided by its caller (attachE2EControl in server/cmd/gram): a
// local stack, or a pull request preview connected to its own database.
package e2econtrol

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// TokenHeader carries the control token on every call except e2e.signIn,
	// which a browser opens and which its one-time code authorizes.
	TokenHeader = "Gram-E2E-Token" //nolint:gosec // G101: a header name, not a credential

	// linkTTL is how long a sign-in link stays redeemable. The test opens it in
	// the step after it asks for it.
	linkTTL = 60 * time.Second

	minTokenLength = 32
)

// Tenant is the one organization end-to-end tests run in.
type Tenant struct {
	OrganizationID   string `json:"-"`
	OrganizationSlug string `json:"organization_slug"`
	ProjectSlug      string `json:"project_slug"`
	// SeededUserPrefix prefixes the ids of the seeded fictional members.
	SeededUserPrefix string `json:"seeded_user_prefix"`
	// CustomDomain is the verified custom domain the seed gives the
	// organization.
	CustomDomain string `json:"custom_domain"`
}

// Config fixes everything the surface acts on.
type Config struct {
	// Token is the control secret (GRAM_E2E_CONTROL_TOKEN).
	Token string
	// UserEmail is the only user e2e.signIn signs in.
	UserEmail string
	// UserID, when set, is the id that user must have.
	UserID string
	// Confined requires the user to be no platform admin and a member of the
	// tenant alone. Set wherever the tenant is not the developer's own.
	Confined bool
	// SiteURL is the dashboard origin: sign-in links are bound to it, and the
	// browser lands on it.
	SiteURL *url.URL
	// Build is the commit the server was built from. A test run reads it to
	// wait until a host serves the commit under test.
	Build string
	// Tenant is the organization Reseed resets and e2e.signIn signs in to.
	Tenant Tenant
	// Reseed replaces the tenant's data with the seed. It returns
	// demoseed.ErrSeedLocked while another seed run holds the seed lock.
	Reseed func(ctx context.Context) error
}

// Users reads a user as the database has them now, never from a cache: who
// they are, whether they are a platform admin, and the enabled organizations
// they belong to. A user that does not exist is (nil, nil).
type Users interface {
	UserByEmail(ctx context.Context, email string) (*sessions.CachedUserInfo, error)
}

// SessionStore issues the dashboard session. Implemented by sessions.Manager.
type SessionStore interface {
	SignInEnforced(ctx context.Context, organizationID string) bool
	InvalidateUserInfoCache(ctx context.Context, userID string) error
	Issue(ctx context.Context, w http.ResponseWriter, session sessions.Session, issuedAt time.Time) error
}

// Service serves the control surface.
type Service struct {
	logger      *slog.Logger
	cfg         Config
	tokenDigest [sha256.Size]byte
	users       Users
	sessions    SessionStore
	links       cache.Cache
	reseeding   sync.Mutex
}

// New builds the service. Its error is a refusal to start the server: the
// token is guessable, or the surface has no fixed target to act on.
func New(logger *slog.Logger, cfg Config, users Users, sessionStore SessionStore, links cache.Cache) (*Service, error) {
	switch {
	case len(cfg.Token) < minTokenLength:
		return nil, fmt.Errorf("e2e control: the control token must be at least %d bytes", minTokenLength)
	case cfg.UserEmail == "":
		return nil, errors.New("e2e control: no test user")
	case cfg.SiteURL == nil || cfg.Tenant.OrganizationID == "" || cfg.Tenant.OrganizationSlug == "" || cfg.Reseed == nil:
		return nil, errors.New("e2e control: no test organization, dashboard origin or reseed")
	}
	return &Service{
		logger:      logger.With(attr.SlogComponent("e2econtrol")),
		cfg:         cfg,
		tokenDigest: sha256.Sum256([]byte(cfg.Token)),
		users:       users,
		sessions:    sessionStore,
		links:       links,
		reseeding:   sync.Mutex{},
	}, nil
}

// Attach mounts the routes.
func Attach(mux goahttp.Muxer, s *Service) {
	o11y.AttachHandler(mux, http.MethodGet, "/rpc/e2e.tenant", oops.ErrHandle(s.logger, s.withToken(s.handleTenant)).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodPost, "/rpc/e2e.reseed", oops.ErrHandle(s.logger, s.withToken(s.handleReseed)).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodPost, "/rpc/e2e.signInLink", oops.ErrHandle(s.logger, s.withToken(s.handleSignInLink)).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, "/rpc/e2e.signIn", oops.ErrHandle(s.logger, s.handleSignIn).ServeHTTP)
}

func (s *Service) withToken(next func(http.ResponseWriter, *http.Request) error) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		got := sha256.Sum256([]byte(r.Header.Get(TokenHeader)))
		if subtle.ConstantTimeCompare(got[:], s.tokenDigest[:]) != 1 {
			return oops.C(oops.CodeUnauthorized)
		}
		return next(w, r)
	}
}

func (s *Service) handleTenant(w http.ResponseWriter, _ *http.Request) error {
	return writeJSON(w, struct {
		Tenant
		Build string `json:"build"`
	}{Tenant: s.cfg.Tenant, Build: s.cfg.Build})
}

func (s *Service) handleReseed(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	// The seed lock covers the seed but not the fixtures that follow it, so
	// this process also runs one reseed at a time.
	if !s.reseeding.TryLock() {
		return oops.E(oops.CodeConflict, nil, "another reseed is in progress")
	}
	defer s.reseeding.Unlock()

	if err := s.cfg.Reseed(ctx); err != nil {
		if errors.Is(err, demoseed.ErrSeedLocked) {
			return oops.E(oops.CodeConflict, err, "another seed run is in progress")
		}
		// Not shareable: the caller learns only that it failed.
		return fmt.Errorf("e2e reseed: %w", err)
	}
	s.logger.InfoContext(ctx, "e2e test organization reseeded", attr.SlogOrganizationID(s.cfg.Tenant.OrganizationID))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleSignInLink(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	code, err := sessions.NewSessionID()
	if err != nil {
		return fmt.Errorf("e2e sign-in link: %w", err)
	}
	if err := s.links.Set(ctx, s.linkKey(code), true, linkTTL); err != nil {
		return fmt.Errorf("e2e sign-in link: store: %w", err)
	}
	s.logger.InfoContext(ctx, "e2e sign-in link minted")
	// A path, not a URL: every environment serves /rpc on the dashboard origin,
	// which the test's browser is already aimed at. "code" is a query
	// parameter the request log redacts.
	return writeJSON(w, map[string]string{"path": "/rpc/e2e.signIn?code=" + url.QueryEscape(code)})
}

func (s *Service) handleSignIn(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	var minted bool
	err := s.links.GetAndDelete(ctx, s.linkKey(r.URL.Query().Get("code")), &minted)
	switch {
	case errors.Is(err, redisCache.ErrCacheMiss), err == nil && !minted:
		return oops.E(oops.CodeForbidden, nil, "sign-in link is spent, expired or unknown")
	case err != nil:
		return fmt.Errorf("e2e sign-in: redeem link: %w", err)
	}
	// No identity provider authenticated this sign-in, so there is nothing to
	// record the way the login and invitation callbacks record theirs
	// (signins.RecordExchange). It gives no session where an organization
	// requires recorded sign-ins.
	if s.sessions.SignInEnforced(ctx, s.cfg.Tenant.OrganizationID) {
		return oops.E(oops.CodeForbidden, nil, "the test organization enforces recorded sign-ins, which this sign-in is not")
	}

	// Taken before reading the user so a sign-out everywhere that lands
	// meanwhile still ends this session.
	authenticatedAt := time.Now()
	user, err := s.users.UserByEmail(ctx, s.cfg.UserEmail)
	if err != nil {
		return fmt.Errorf("e2e sign-in: read test user: %w", err)
	}
	if reason := s.refusal(user); reason != "" {
		return oops.E(oops.CodeForbidden, nil, "%s", reason).LogError(ctx, s.logger)
	}
	// Requests are authorized from a cached copy of the user (staff bit,
	// organizations). Drop it, so the session is judged by the rows just read.
	if err := s.sessions.InvalidateUserInfoCache(ctx, user.UserID); err != nil {
		return fmt.Errorf("e2e sign-in: drop cached user: %w", err)
	}

	sessionID, err := sessions.NewSessionID()
	if err != nil {
		return fmt.Errorf("e2e sign-in: %w", err)
	}
	if err := s.sessions.Issue(ctx, w, sessions.Session{
		SessionID:             sessionID,
		UserID:                user.UserID,
		ActiveOrganizationID:  s.cfg.Tenant.OrganizationID,
		WorkOSSessionID:       "",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}, authenticatedAt); err != nil {
		return fmt.Errorf("e2e sign-in: issue session: %w", err)
	}
	s.logger.InfoContext(ctx, "e2e sign-in complete", attr.SlogUserID(user.UserID), attr.SlogOrganizationID(s.cfg.Tenant.OrganizationID))
	http.Redirect(w, r, s.cfg.SiteURL.JoinPath(s.cfg.Tenant.OrganizationSlug).String(), http.StatusSeeOther)
	return nil
}

// refusal says why user gets no session, or "" when they do. The reseed is
// what creates the test user and makes them a member, so each reason is also
// what a reseed repairs.
func (s *Service) refusal(user *sessions.CachedUserInfo) string {
	switch {
	case user == nil:
		return "the test user does not exist; reseed first"
	case s.cfg.UserID != "" && user.UserID != s.cfg.UserID:
		return "the test user's address belongs to another user"
	case !slices.ContainsFunc(user.Organizations, func(o sessions.Organization) bool { return o.ID == s.cfg.Tenant.OrganizationID }):
		return "the test user is not a member of the test organization; reseed first"
	case s.cfg.Confined && user.Admin:
		return "the test user is a platform admin, which it must never be here"
	case s.cfg.Confined && len(user.Organizations) != 1:
		return "the test user belongs to more than the test organization"
	}
	return ""
}

// linkKey is where a sign-in code's marker lives. The code is hashed, so the
// cache never holds a value that would sign anyone in, together with what it
// was minted for (dashboard origin, organization, user), so a server
// configured for anything else does not know it.
func (s *Service) linkKey(code string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{s.cfg.SiteURL.String(), s.cfg.Tenant.OrganizationID, s.cfg.UserEmail, code}, "\n")))
	return "e2econtrol:signin:" + hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, body any) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}
