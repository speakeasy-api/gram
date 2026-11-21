package e2econtrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testToken = "zz-e2e-not-a-secret-zz-e2e-not-a-secret"
	testEmail = "e2e@example.test"
	testOrgID = "org-e2e"
)

type fakeIdentity struct {
	emails        []string
	organizations []sessions.Organization
}

func (f *fakeIdentity) AuthenticateWithMagicAuth(_ context.Context, email string) (*identity.IDPUserInfo, error) {
	f.emails = append(f.emails, email)
	return &identity.IDPUserInfo{Sub: "user_idp", Email: email, WorkOSSessionID: "session_idp"}, nil
}

func (f *fakeIdentity) CompleteIDPLogin(_ context.Context, idpUser *identity.IDPUserInfo, _ identity.IDPLoginOptions) (identity.IDPLoginResult, error) {
	return identity.IDPLoginResult{
		UserID:   "user-gram",
		UserInfo: &sessions.CachedUserInfo{UserID: "user-gram", Email: idpUser.Email, Organizations: f.organizations},
	}, nil
}

type fakeSessions struct{ stored []sessions.Session }

func (f *fakeSessions) StoreSessionIssuedAt(_ context.Context, session sessions.Session, _ time.Time) error {
	f.stored = append(f.stored, session)
	return nil
}

type fakeTickets struct {
	mu   sync.Mutex
	keys map[string]bool
}

func (f *fakeTickets) Set(_ context.Context, key string, _ any, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys[key] = true
	return nil
}

func (f *fakeTickets) GetAndDelete(_ context.Context, key string, value any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.keys[key] {
		return errors.New("not found")
	}
	delete(f.keys, key)
	if live, ok := value.(*bool); ok {
		*live = true
	}
	return nil
}

type fakeReseeder struct {
	err   error
	calls int
}

func (f *fakeReseeder) Reseed(context.Context) error {
	f.calls++
	return f.err
}

func (f *fakeReseeder) Tenant() e2econtrol.Tenant {
	return e2econtrol.Tenant{OrganizationID: testOrgID, OrganizationSlug: "e2e-org", ProjectSlug: "default", SeededUserPrefix: "user_e2e_", APIKeyName: "seed-key"}
}

type harness struct {
	server   *httptest.Server
	identity *fakeIdentity
	sessions *fakeSessions
	reseeder *fakeReseeder
}

// reply is what a test reads from one response, after its body is closed.
type reply struct {
	status   int
	location string
	cookies  []*http.Cookie
	body     []byte
}

func validConfig(serverURL string) e2econtrol.Config {
	return e2econtrol.Config{Environment: "dev", Token: testToken, UserEmail: testEmail, SiteURL: "https://site.example.test", ServerURL: serverURL, Build: "0123abc"}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	mux := goahttp.NewMuxer()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	h := &harness{
		server: server,
		identity: &fakeIdentity{emails: nil, organizations: []sessions.Organization{
			{ID: "org-other", Slug: "other"},
			{ID: testOrgID, Slug: "e2e-org"},
		}},
		sessions: &fakeSessions{stored: nil},
		reseeder: &fakeReseeder{err: nil, calls: 0},
	}
	service, err := e2econtrol.New(testenv.NewLogger(t), validConfig(server.URL), h.identity, h.sessions, &fakeTickets{mu: sync.Mutex{}, keys: map[string]bool{}}, h.reseeder)
	require.NoError(t, err)
	e2econtrol.Attach(mux, service)
	return h
}

func (h *harness) do(t *testing.T, method, path, token string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, h.server.URL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set(e2econtrol.TokenHeader, token)
	}
	// Redirects are asserted, not followed.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return reply{status: res.StatusCode, location: res.Header.Get("Location"), cookies: res.Cookies(), body: body}
}

func TestNewRefusesUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*e2econtrol.Config){
		"production":           func(c *e2econtrol.Config) { c.Environment = "prod" },
		"unnamed environment":  func(c *e2econtrol.Config) { c.Environment = "" },
		"no token":             func(c *e2econtrol.Config) { c.Token = "" },
		"guessable token":      func(c *e2econtrol.Config) { c.Token = "short" },
		"no test user":         func(c *e2econtrol.Config) { c.UserEmail = "" },
		"relative site URL":    func(c *e2econtrol.Config) { c.SiteURL = "/dashboard" },
		"server URL not a URL": func(c *e2econtrol.Config) { c.ServerURL = "localhost" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig("https://server.example.test")
			mutate(&cfg)
			_, err := e2econtrol.New(testenv.NewLogger(t), cfg, nil, nil, nil, nil)
			require.Error(t, err)
		})
	}
}

func TestControlCallsNeedTheToken(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/rpc/e2e.reseed"},
		{http.MethodGet, "/rpc/e2e.tenant"},
		{http.MethodPost, "/rpc/e2e.signInLink"},
	} {
		for _, token := range []string{"", "wrong-token-wrong-token-wrong-token-wrong"} {
			got := h.do(t, call.method, call.path, token)
			require.Equal(t, http.StatusUnauthorized, got.status, "%s %s with token %q", call.method, call.path, token)
		}
	}
	require.Zero(t, h.reseeder.calls)
}

func TestTenantNamesTheTestOrganization(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	res := h.do(t, http.MethodGet, "/rpc/e2e.tenant", testToken)
	require.Equal(t, http.StatusOK, res.status)
	var got map[string]any
	require.NoError(t, json.Unmarshal(res.body, &got))
	require.Equal(t, "e2e-org", got["organization_slug"])
	require.Equal(t, "default", got["project_slug"])
	require.Equal(t, "user_e2e_", got["seeded_user_prefix"])
	require.Equal(t, "dev", got["environment"])
	require.Equal(t, "0123abc", got["build"])
	require.Zero(t, h.reseeder.calls, "reading the tenant must not reseed")
}

func TestReseed(t *testing.T) {
	t.Parallel()

	t.Run("replaces the tenant's data and names it", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		res := h.do(t, http.MethodPost, "/rpc/e2e.reseed", testToken)
		require.Equal(t, http.StatusOK, res.status)
		require.Equal(t, 1, h.reseeder.calls)
		var got map[string]any
		require.NoError(t, json.Unmarshal(res.body, &got))
		require.Equal(t, "e2e-org", got["organization_slug"])
	})

	t.Run("reports a seed already in progress as a conflict", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.reseeder.err = e2econtrol.ErrSeedBusy
		res := h.do(t, http.MethodPost, "/rpc/e2e.reseed", testToken)
		require.Equal(t, http.StatusConflict, res.status)
	})

	t.Run("hides the cause of a failed seed", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.reseeder.err = errors.New("postgres: connection string with a password")
		res := h.do(t, http.MethodPost, "/rpc/e2e.reseed", testToken)
		require.Equal(t, http.StatusInternalServerError, res.status)
		require.NotContains(t, string(res.body), "password")
	})
}

func signInLink(t *testing.T, h *harness) string {
	t.Helper()
	res := h.do(t, http.MethodPost, "/rpc/e2e.signInLink", testToken)
	require.Equal(t, http.StatusOK, res.status)
	var link struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(res.body, &link))
	require.True(t, strings.HasPrefix(link.Path, "/rpc/e2e.signIn?ticket="), "path %q", link.Path)
	require.Equal(t, h.server.URL+link.Path, link.URL)
	return link.Path
}

func TestSignInLinkSignsTheTestUserIntoTheTestOrganizationOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	path := signInLink(t, h)

	res := h.do(t, http.MethodGet, path, "")
	require.Equal(t, http.StatusSeeOther, res.status)
	require.Equal(t, "https://site.example.test/e2e-org", res.location)
	require.Equal(t, []string{testEmail}, h.identity.emails, "only the configured identity is ever signed in")

	require.Len(t, h.sessions.stored, 1)
	session := h.sessions.stored[0]
	require.Equal(t, "user-gram", session.UserID)
	require.Equal(t, testOrgID, session.ActiveOrganizationID)
	require.Equal(t, "session_idp", session.WorkOSSessionID)

	var cookie *http.Cookie
	for _, c := range res.cookies {
		if c.Name == constants.SessionCookie {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	require.Equal(t, session.SessionID, cookie.Value)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)

	again := h.do(t, http.MethodGet, path, "")
	require.Equal(t, http.StatusForbidden, again.status, "a link is spent on first use")
	require.Len(t, h.sessions.stored, 1)
}

func TestSignInRefusesUnknownTickets(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	for _, path := range []string{"/rpc/e2e.signIn", "/rpc/e2e.signIn?ticket=", "/rpc/e2e.signIn?ticket=made-up"} {
		res := h.do(t, http.MethodGet, path, testToken)
		require.Equal(t, http.StatusForbidden, res.status, path)
	}
	require.Empty(t, h.identity.emails)
	require.Empty(t, h.sessions.stored)
}

func TestSignInGivesNoSessionOutsideTheTestOrganization(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.identity.organizations = []sessions.Organization{{ID: "org-other", Slug: "other"}}

	res := h.do(t, http.MethodGet, signInLink(t, h), "")
	require.Equal(t, http.StatusConflict, res.status)
	require.Empty(t, h.sessions.stored)
	require.Empty(t, res.cookies)
}
