package e2econtrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testToken  = "zz-e2e-not-a-secret-zz-e2e-not-a-secret"
	testEmail  = "e2e@example.test"
	testUserID = "user-e2e"
	testOrgID  = "org-e2e"
	testSite   = "https://site.example.test"
)

var (
	testOrg  = sessions.Organization{ID: testOrgID, Slug: "e2e-org"}
	otherOrg = sessions.Organization{ID: "org-other", Slug: "other"}
)

// fakeUsers is the database: the users it holds, by address.
type fakeUsers struct {
	asked []string
	users map[string]*sessions.CachedUserInfo
}

func (f *fakeUsers) UserByEmail(_ context.Context, email string) (*sessions.CachedUserInfo, error) {
	f.asked = append(f.asked, email)
	return f.users[email], nil
}

type fakeSessions struct {
	issued      []sessions.Session
	invalidated []string
	enforced    bool
}

func (f *fakeSessions) SignInEnforced(context.Context, string) bool { return f.enforced }

func (f *fakeSessions) InvalidateUserInfoCache(_ context.Context, userID string) error {
	// Before the session exists, or its first request could be judged by a stale copy.
	if len(f.issued) == 0 {
		f.invalidated = append(f.invalidated, userID)
	}
	return nil
}

func (f *fakeSessions) Issue(_ context.Context, w http.ResponseWriter, session sessions.Session, _ time.Time) error {
	f.issued = append(f.issued, session)
	// The real cookie's attributes are sessions.Manager's, and tested there.
	http.SetCookie(w, &http.Cookie{Name: "issued", Value: session.SessionID})
	return nil
}

type harness struct {
	mux      goahttp.Muxer
	redis    *miniredis.Miniredis
	links    cache.Cache
	users    *fakeUsers
	sessions *fakeSessions
	reseeds  int
	reseed   func(context.Context) error
}

func validConfig(t *testing.T, h *harness, site string) e2econtrol.Config {
	t.Helper()
	siteURL, err := url.Parse(site)
	require.NoError(t, err)
	return e2econtrol.Config{
		Token:     testToken,
		UserEmail: testEmail,
		UserID:    "",
		Confined:  false,
		SiteURL:   siteURL,
		Build:     "0123abc",
		Tenant:    e2econtrol.Tenant{OrganizationID: testOrgID, OrganizationSlug: "e2e-org", ProjectSlug: "default", SeededUserPrefix: "user_e2e_"},
		Reseed: func(ctx context.Context) error {
			h.reseeds++
			return h.reseed(ctx)
		},
	}
}

// newHarness serves the surface for the dashboard origin site, with sign-in
// links kept in a real Redis adapter so single use and expiry are the store's.
// Its test user is a platform admin with a second organization, which is what
// a developer is on their own stack.
func newHarness(t *testing.T) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	h := &harness{
		mux:   nil,
		redis: mr,
		links: cache.NewRedisCacheAdapter(client),
		users: &fakeUsers{asked: nil, users: map[string]*sessions.CachedUserInfo{
			testEmail: {UserID: testUserID, Email: testEmail, Admin: true, Organizations: []sessions.Organization{otherOrg, testOrg}},
		}},
		sessions: &fakeSessions{issued: nil, invalidated: nil, enforced: false},
		reseeds:  0,
		reseed:   func(context.Context) error { return nil },
	}
	h.mux = h.serve(t, nil)
	return h
}

// serve mounts one more server on the harness's cache, users and session
// store, configured as the default one except for what mutate changes.
func (h *harness) serve(t *testing.T, mutate func(*e2econtrol.Config)) goahttp.Muxer {
	t.Helper()
	cfg := validConfig(t, h, testSite)
	if mutate != nil {
		mutate(&cfg)
	}
	service, err := e2econtrol.New(testenv.NewLogger(t), cfg, h.users, h.sessions, h.links)
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	e2econtrol.Attach(mux, service)
	return mux
}

func do(t *testing.T, mux http.Handler, method, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	if token != "" {
		req.Header.Set(e2econtrol.TokenHeader, token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func signInLink(t *testing.T, mux goahttp.Muxer) string {
	t.Helper()
	res := do(t, mux, http.MethodPost, "/rpc/e2e.signInLink", testToken)
	require.Equal(t, http.StatusOK, res.Code)
	var link map[string]string
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &link))
	require.Len(t, link, 1, "a link is a path and nothing else")
	require.True(t, strings.HasPrefix(link["path"], "/rpc/e2e.signIn?code="), "path %q", link["path"])
	return link["path"]
}

func TestNewRefusesIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*e2econtrol.Config){
		"no token":           func(c *e2econtrol.Config) { c.Token = "" },
		"guessable token":    func(c *e2econtrol.Config) { c.Token = "short" },
		"no test user":       func(c *e2econtrol.Config) { c.UserEmail = "" },
		"no dashboard":       func(c *e2econtrol.Config) { c.SiteURL = nil },
		"no organization id": func(c *e2econtrol.Config) { c.Tenant.OrganizationID = "" },
		"no reseed":          func(c *e2econtrol.Config) { c.Reseed = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig(t, nil, testSite)
			mutate(&cfg)
			_, err := e2econtrol.New(testenv.NewLogger(t), cfg, nil, nil, nil)
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
		for _, token := range []string{"", "wrong-token-wrong-token-wrong-token-wrong", testToken + "x", testToken[:len(testToken)-1]} {
			res := do(t, h.mux, call.method, call.path, token)
			require.Equal(t, http.StatusUnauthorized, res.Code, "%s %s with token %q", call.method, call.path, token)
		}
	}
	require.Zero(t, h.reseeds)
	require.Empty(t, h.redis.Keys(), "no sign-in link is minted without the token")
}

func TestTenantNamesTheTestOrganizationAndTheBuild(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	res := do(t, h.mux, http.MethodGet, "/rpc/e2e.tenant", testToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"organization_slug":"e2e-org","project_slug":"default","seeded_user_prefix":"user_e2e_","build":"0123abc"}`, res.Body.String())
	require.Zero(t, h.reseeds, "reading the tenant must not reseed")
}

func TestReseed(t *testing.T) {
	t.Parallel()

	t.Run("runs the one configured reseed, whatever the caller sends", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		res := do(t, h.mux, http.MethodPost, "/rpc/e2e.reseed?organization_id=org-other&slug=other", testToken)
		require.Equal(t, http.StatusNoContent, res.Code)
		require.Equal(t, 1, h.reseeds)
	})

	t.Run("answers 409 while a seed run holds the seed lock", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.reseed = func(context.Context) error { return demoseed.ErrSeedLocked }
		require.Equal(t, http.StatusConflict, do(t, h.mux, http.MethodPost, "/rpc/e2e.reseed", testToken).Code)
	})

	t.Run("answers 409 while another reseed is running in this process", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		running, release := make(chan struct{}), make(chan struct{})
		h.reseed = func(context.Context) error {
			close(running)
			<-release
			return nil
		}
		first := make(chan int)
		go func() { first <- do(t, h.mux, http.MethodPost, "/rpc/e2e.reseed", testToken).Code }()
		<-running
		require.Equal(t, http.StatusConflict, do(t, h.mux, http.MethodPost, "/rpc/e2e.reseed", testToken).Code)
		close(release)
		require.Equal(t, http.StatusNoContent, <-first)
	})

	t.Run("hides the cause of a failed seed", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.reseed = func(context.Context) error { return errors.New("postgres: connection string with a password") }
		res := do(t, h.mux, http.MethodPost, "/rpc/e2e.reseed", testToken)
		require.Equal(t, http.StatusInternalServerError, res.Code)
		require.NotContains(t, res.Body.String(), "password")
	})
}

func TestSignInLinkSignsTheTestUserIntoTheTestOrganizationOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	path := signInLink(t, h.mux)

	res := do(t, h.mux, http.MethodGet, path, "")
	require.Equal(t, http.StatusSeeOther, res.Code)
	require.Equal(t, testSite+"/e2e-org", res.Header().Get("Location"))
	require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", res.Header().Get("Referrer-Policy"))
	require.Equal(t, []string{testEmail}, h.users.asked, "only the configured user is ever signed in")

	require.Len(t, h.sessions.issued, 1)
	require.Equal(t, []string{testUserID}, h.sessions.invalidated, "the cached copy of the user is dropped before the session exists")
	session := h.sessions.issued[0]
	require.Equal(t, testUserID, session.UserID)
	require.Equal(t, testOrgID, session.ActiveOrganizationID)
	require.Empty(t, session.WorkOSSessionID, "no identity provider took part")
	require.Empty(t, session.ImpersonatorEmail)
	require.Empty(t, session.SupportOrganizationID)

	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, session.SessionID, cookies[0].Value)

	again := do(t, h.mux, http.MethodGet, path, "")
	require.Equal(t, http.StatusForbidden, again.Code, "a link is spent on first use")
	require.Len(t, h.sessions.issued, 1)
}

func TestSignInLinkExpiresAfterAMinute(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	path := signInLink(t, h.mux)
	h.redis.FastForward(59 * time.Second)
	require.Equal(t, http.StatusSeeOther, do(t, h.mux, http.MethodGet, path, "").Code)

	path = signInLink(t, h.mux)
	h.redis.FastForward(61 * time.Second)
	require.Equal(t, http.StatusForbidden, do(t, h.mux, http.MethodGet, path, "").Code)
	require.Len(t, h.sessions.issued, 1)
}

func TestSignInLinkStoresNoRedeemableValue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	code := strings.TrimPrefix(signInLink(t, h.mux), "/rpc/e2e.signIn?code=")
	keys := h.redis.Keys()
	require.Len(t, keys, 1)
	require.NotContains(t, keys[0], code)
	stored, err := h.redis.Get(keys[0])
	require.NoError(t, err)
	require.NotContains(t, stored, code)
}

// A second server sharing the first one's cache does not know a code minted
// for a different dashboard origin, organization or user. Previews share one
// cache with each other and with their environment.
func TestSignInLinkIsBoundToWhatItWasMintedFor(t *testing.T) {
	t.Parallel()

	otherSite, err := url.Parse("https://other.example.test")
	require.NoError(t, err)
	cases := map[string]func(*e2econtrol.Config){
		"another dashboard origin": func(c *e2econtrol.Config) { c.SiteURL = otherSite },
		"another organization":     func(c *e2econtrol.Config) { c.Tenant.OrganizationID = "org-other" },
		"another user":             func(c *e2econtrol.Config) { c.UserEmail = "someone-else@example.test" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			other := h.serve(t, mutate)

			path := signInLink(t, h.mux)
			require.Equal(t, http.StatusForbidden, do(t, other, http.MethodGet, path, "").Code)
			require.Empty(t, h.users.asked)
			require.Equal(t, http.StatusSeeOther, do(t, h.mux, http.MethodGet, path, "").Code, "the refusal elsewhere did not spend it")
		})
	}
}

func TestSignInRefusesUnknownCodes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	for _, path := range []string{"/rpc/e2e.signIn", "/rpc/e2e.signIn?code=", "/rpc/e2e.signIn?code=made-up"} {
		// The control token is no substitute for a code.
		require.Equal(t, http.StatusForbidden, do(t, h.mux, http.MethodGet, path, testToken).Code, path)
	}
	require.Empty(t, h.users.asked)
	require.Empty(t, h.sessions.issued)
}

func TestSignInGivesNoSessionWhereSignInsMustBeRecorded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.sessions.enforced = true

	res := do(t, h.mux, http.MethodGet, signInLink(t, h.mux), "")
	require.Equal(t, http.StatusForbidden, res.Code)
	require.Empty(t, h.users.asked)
	require.Empty(t, h.sessions.issued)
}

// The user is read when the link is redeemed, so each case is the database as
// it stands then, whatever it was when the server started.
func TestSignInGivesNoSessionUnlessTheUserIsTheTestOrganizationsMember(t *testing.T) {
	t.Parallel()

	cases := map[string]*sessions.CachedUserInfo{
		"no such user":             nil,
		"not a member":             {UserID: testUserID, Email: testEmail, Organizations: []sessions.Organization{otherOrg}},
		"a member of nothing":      {UserID: testUserID, Email: testEmail, Organizations: nil},
		"the address of another":   {UserID: "user-someone-else", Email: testEmail, Organizations: []sessions.Organization{testOrg}},
		"confined: platform admin": {UserID: testUserID, Email: testEmail, Admin: true, Organizations: []sessions.Organization{testOrg}},
		"confined: a second org":   {UserID: testUserID, Email: testEmail, Organizations: []sessions.Organization{testOrg, otherOrg}},
	}
	for name, user := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.users.users[testEmail] = user
			confined := h.serve(t, func(c *e2econtrol.Config) {
				c.UserID = testUserID
				c.Confined = true
			})

			res := do(t, confined, http.MethodGet, signInLink(t, confined), "")
			require.Equal(t, http.StatusForbidden, res.Code)
			require.Empty(t, h.sessions.issued)
			require.Empty(t, res.Result().Cookies())
		})
	}
}

func TestConfinedSignInSignsInTheSoleMember(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.users.users[testEmail] = &sessions.CachedUserInfo{UserID: testUserID, Email: testEmail, Organizations: []sessions.Organization{testOrg}}
	confined := h.serve(t, func(c *e2econtrol.Config) {
		c.UserID = testUserID
		c.Confined = true
	})

	require.Equal(t, http.StatusSeeOther, do(t, confined, http.MethodGet, signInLink(t, confined), "").Code)
	require.Len(t, h.sessions.issued, 1)
	require.Equal(t, testOrgID, h.sessions.issued[0].ActiveOrganizationID)
}
