package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// Sessions move from the server host (the source) to the extra platform host
// (the target), which serves its own dashboard.
var testTargetBaseURL = "https://" + testExtraPlatformHost

const testTransferRedirect = "/test-org/mcp?tab=logs#recent"

// loginAt is the login fallback on the dashboard at siteURL.
func loginAt(siteURL, redirect string) string {
	return siteURL + "/login?" + url.Values{"redirect": {redirect}}.Encode()
}

// targetLogin is the login fallback on the target host.
func targetLogin(redirect string) string { return loginAt(testTargetBaseURL, redirect) }

// sourceLogin is the login fallback on the source host's dashboard.
func sourceLogin(redirect string) string { return loginAt(testSiteURL.String(), redirect) }

func atHost(ctx context.Context, baseURL string) context.Context {
	return requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, baseURL))
}

// browser is a cookie jar standing in for one browser's cookies on the
// target host.
type browser struct {
	mu      sync.Mutex
	cookies map[string]string
}

var _ auth.TransferCookieJar = (*browser)(nil)

func newBrowser() *browser { return &browser{mu: sync.Mutex{}, cookies: map[string]string{}} }

func (b *browser) Get(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cookies[name]
}

func (b *browser) Set(name, value string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cookies[name] = value
}

func (b *browser) Clear(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.cookies, name)
}

func (b *browser) clone() *browser {
	b.mu.Lock()
	defer b.mu.Unlock()
	return &browser{mu: sync.Mutex{}, cookies: maps.Clone(b.cookies)}
}

func (b *browser) transferCookies() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	found := map[string]string{}
	for name, value := range b.cookies {
		if strings.HasPrefix(name, constants.TransferInNonceCookiePrefix) {
			found[name] = value
		}
	}
	return found
}

type transferFixture struct {
	instance *testInstance
	userInfo *MockUserInfo
	source   sessions.Session
}

// sourceCtx is a request on the source host carrying the session cookie.
func (f *transferFixture) sourceCtx(ctx context.Context) context.Context {
	return contextvalues.SetSessionTokenInContext(atHost(ctx, testServerURL.String()), f.source.SessionID)
}

type transferOptions struct {
	member       bool
	defaultHost  *string
	impersonator string
	supportOrgID string
}

func defaultTransferOptions() transferOptions {
	return transferOptions{member: true, defaultHost: new(testTargetBaseURL), impersonator: "", supportOrgID: ""}
}

// newTransfer seeds a user, their organization with opts.defaultHost, and a
// session for it on the source host.
func newTransfer(t *testing.T, opts transferOptions) (context.Context, *transferFixture) {
	t.Helper()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	org := userInfo.Organizations[0]
	memberID := ""
	if opts.member {
		memberID = userInfo.UserID
	}
	require.NoError(t, instance.createTestOrganization(ctx, org, memberID))
	require.NoError(t, orgRepo.New(instance.conn).SetOrganizationDefaultHostForTest(ctx, orgRepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: conv.PtrToPGText(opts.defaultHost),
		ID:          org.ID,
	}))

	source := sessions.Session{
		SessionID:             "source-" + strings.ReplaceAll(t.Name(), "/", "-"),
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  org.ID,
		WorkOSSessionID:       "workos-session-id",
		ImpersonatorEmail:     opts.impersonator,
		SupportOrganizationID: opts.supportOrgID,
		SupportExpiresAt:      time.Time{},
	}
	if opts.supportOrgID != "" {
		source.SupportExpiresAt = time.Now().Add(time.Hour)
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, source))

	return ctx, &transferFixture{instance: instance, userInfo: userInfo, source: source}
}

// start runs transferIn start mode in b and returns the nonce from the transferOut
// URL it redirects to.
func (f *transferFixture) start(ctx context.Context, t *testing.T, b *browser) string {
	t.Helper()
	result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(atHost(ctx, testTargetBaseURL), b), startPayload(testServerURL.Host, new(testTransferRedirect)))
	require.NoError(t, err)
	location, err := url.Parse(result.Location)
	require.NoError(t, err)
	require.Equal(t, "/rpc/auth.transferOut", location.Path, "start mode fell back: %s", location)
	return location.Query().Get("nonce")
}

func (f *transferFixture) out(ctx context.Context, t *testing.T, payload *gen.TransferOutPayload) string {
	t.Helper()
	result, err := f.instance.service.TransferOut(ctx, payload)
	require.NoError(t, err)
	return result.Location
}

// startPayload is transferIn's start mode.
func startPayload(sourceHost string, redirect *string) *gen.TransferInPayload {
	return &gen.TransferInPayload{SourceHost: &sourceHost, Code: nil, Redirect: redirect}
}

// callbackPayload is transferIn's callback mode.
func callbackPayload(code string, redirect *string) *gen.TransferInPayload {
	return &gen.TransferInPayload{SourceHost: nil, Code: &code, Redirect: redirect}
}

func outPayload(nonce string) *gen.TransferOutPayload {
	return &gen.TransferOutPayload{
		TargetHost:   new(testExtraPlatformHost),
		Nonce:        &nonce,
		Redirect:     new(testTransferRedirect),
		SessionToken: nil,
	}
}

// codeFrom returns the transfer code from a transferOut redirect.
func codeFrom(t *testing.T, location string) string {
	t.Helper()
	parsed, err := url.Parse(location)
	require.NoError(t, err)
	require.Equal(t, testExtraPlatformHost, parsed.Host, "transferOut fell back: %s", location)
	require.Equal(t, "/rpc/auth.transferIn", parsed.Path)
	require.Equal(t, testTransferRedirect, parsed.Query().Get("redirect"))
	code := parsed.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

// begin runs transferIn start mode and transferOut in a new browser and returns the
// transfer code, the nonce, and the browser.
func (f *transferFixture) begin(ctx context.Context, t *testing.T) (string, string, *browser) {
	t.Helper()
	b := newBrowser()
	nonce := f.start(ctx, t, b)
	return codeFrom(t, f.out(f.sourceCtx(ctx), t, outPayload(nonce))), nonce, b
}

// in runs transferIn on host in browser b.
func (f *transferFixture) in(ctx context.Context, t *testing.T, host, code string, b *browser) *gen.TransferInResult {
	t.Helper()
	result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(atHost(ctx, host), b), callbackPayload(code, new(testTransferRedirect)))
	require.NoError(t, err)
	return result
}

// requireRefused checks a transferIn result is the login fallback with no
// session.
func requireRefused(t *testing.T, result *gen.TransferInResult) {
	t.Helper()
	require.Equal(t, targetLogin(testTransferRedirect), result.Location)
	require.Nil(t, result.SessionCookie)
	require.Nil(t, result.SessionToken)
}

// requireAccepted checks a transferIn result established a new session for
// the source session's user and organization.
func (f *transferFixture) requireAccepted(ctx context.Context, t *testing.T, result *gen.TransferInResult) {
	t.Helper()
	require.Equal(t, testTargetBaseURL+testTransferRedirect, result.Location)
	require.NotNil(t, result.SessionCookie)
	require.Equal(t, result.SessionCookie, result.SessionToken)
	require.NotEqual(t, f.source.SessionID, *result.SessionCookie)

	minted, err := f.instance.sessionManager.GetSession(ctx, *result.SessionCookie)
	require.NoError(t, err)
	require.Equal(t, sessions.Session{
		SessionID:             *result.SessionCookie,
		ActiveOrganizationID:  f.source.ActiveOrganizationID,
		UserID:                f.source.UserID,
		WorkOSSessionID:       f.source.WorkOSSessionID,
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}, minted)
}

func nonceHash(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}

func TestService_TransferIn_StartMode(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	target := atHost(ctx, testTargetBaseURL)

	t.Run("redirects to the source host's transferOut and sets a per-transfer cookie", func(t *testing.T) {
		t.Parallel()

		b := newBrowser()
		result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(target, b), startPayload(testServerURL.Host, new(testTransferRedirect)))
		require.NoError(t, err)

		location, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Equal(t, testServerURL.Scheme, location.Scheme)
		require.Equal(t, testServerURL.Host, location.Host)
		require.Equal(t, "/rpc/auth.transferOut", location.Path)
		nonce := location.Query().Get("nonce")
		require.NotEmpty(t, nonce)
		require.Equal(t, url.Values{
			"target_host": {testExtraPlatformHost},
			"nonce":       {nonce},
			"redirect":    {testTransferRedirect},
		}, location.Query())

		require.Equal(t, map[string]string{
			constants.TransferInNonceCookiePrefix + nonceHash(nonce)[:16]: nonce,
		}, b.transferCookies())
	})

	t.Run("accepts the dashboard host as the source and sends it to the server host", func(t *testing.T) {
		t.Parallel()

		result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(target, newBrowser()), startPayload(testSiteURL.Host, nil))
		require.NoError(t, err)
		location, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Equal(t, testServerURL.Host, location.Host)
		require.Equal(t, "/rpc/auth.transferOut", location.Path)
	})

	t.Run("issues a fresh nonce and cookie each time", func(t *testing.T) {
		t.Parallel()

		b := newBrowser()
		first, second := f.start(ctx, t, b), f.start(ctx, t, b)
		require.NotEqual(t, first, second)
		require.Len(t, b.transferCookies(), 2)
	})

	redirects := []struct {
		name     string
		redirect *string
		want     string
	}{
		{"keeps a path, query and hash", new("/a/b?c=d#e"), "/a/b?c=d#e"},
		{"defaults to the root", nil, "/"},
		{"drops an absolute URL", new("https://evil.example.com/x"), "/"},
		{"drops a protocol-relative URL", new("//evil.example.com/x"), "/"},
		{"drops a backslash URL", new(`/\evil.example.com`), "/"},
	}
	for _, tt := range redirects {
		t.Run("redirect "+tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(target, newBrowser()), startPayload(testServerURL.Host, tt.redirect))
			require.NoError(t, err)
			location, err := url.Parse(result.Location)
			require.NoError(t, err)
			require.Equal(t, tt.want, location.Query().Get("redirect"))
		})
	}

	targetOrigin := originAt(requestorigin.SurfacePlatform, testTargetBaseURL)
	failures := []struct {
		name       string
		origin     *requestorigin.Origin // nil: the request has no origin
		sourceHost string
		want       string
	}{
		{"source host is not a platform host", &targetOrigin, "evil.example.com", targetLogin("/")},
		{"source host is this host", &targetOrigin, testExtraPlatformHost, targetLogin("/")},
		{"source host is empty", &targetOrigin, "", targetLogin("/")},
		{"request is not on a platform host", nil, testServerURL.Host, sourceLogin("/")},
		{"request is on a custom domain", new(originAt(requestorigin.SurfaceCustomDomain, "https://mcp.customer.example")), testServerURL.Host, sourceLogin("/")},
	}
	for _, tt := range failures {
		t.Run("falls back to login when "+tt.name, func(t *testing.T) {
			t.Parallel()

			reqCtx := ctx
			if tt.origin != nil {
				reqCtx = requestorigin.WithContext(ctx, *tt.origin)
			}
			b := newBrowser()
			result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(reqCtx, b), &gen.TransferInPayload{SourceHost: &tt.sourceHost, Code: nil, Redirect: nil})
			require.NoError(t, err)
			require.Equal(t, tt.want, result.Location)
			require.Empty(t, b.transferCookies())
		})
	}
}

func TestService_TransferOut_Refusals(t *testing.T) {
	t.Parallel()

	sourceCtx := func(ctx context.Context, f *transferFixture) context.Context { return f.sourceCtx(ctx) }
	withNonce := func(*transferFixture) *gen.TransferOutPayload { return outPayload("nonce") }
	withTarget := func(host string) func(*transferFixture) *gen.TransferOutPayload {
		return func(*transferFixture) *gen.TransferOutPayload {
			p := outPayload("nonce")
			p.TargetHost = &host
			return p
		}
	}

	tests := []struct {
		name    string
		opts    transferOptions
		ctx     func(ctx context.Context, f *transferFixture) context.Context
		payload func(f *transferFixture) *gen.TransferOutPayload
		want    string
	}{
		{
			name: "no session",
			opts: defaultTransferOptions(),
			ctx: func(ctx context.Context, f *transferFixture) context.Context {
				return atHost(ctx, testServerURL.String())
			},
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
		{
			name: "unknown session",
			opts: defaultTransferOptions(),
			ctx: func(ctx context.Context, f *transferFixture) context.Context {
				return contextvalues.SetSessionTokenInContext(atHost(ctx, testServerURL.String()), "unknown")
			},
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
		{
			name: "missing nonce",
			opts: defaultTransferOptions(),
			ctx:  sourceCtx,
			payload: func(*transferFixture) *gen.TransferOutPayload {
				p := outPayload("")
				p.Nonce = nil
				return p
			},
			want: targetLogin(testTransferRedirect),
		},
		{
			name: "missing target",
			opts: defaultTransferOptions(),
			ctx:  sourceCtx,
			payload: func(*transferFixture) *gen.TransferOutPayload {
				p := outPayload("nonce")
				p.TargetHost = nil
				return p
			},
			want: sourceLogin(testTransferRedirect),
		},
		{
			name:    "empty nonce",
			opts:    defaultTransferOptions(),
			ctx:     sourceCtx,
			payload: func(*transferFixture) *gen.TransferOutPayload { return outPayload("") },
			want:    targetLogin(testTransferRedirect),
		},
		{
			name:    "target is not a platform host",
			opts:    defaultTransferOptions(),
			ctx:     sourceCtx,
			payload: withTarget("evil.example.com"),
			want:    sourceLogin(testTransferRedirect),
		},
		{
			name:    "target is empty",
			opts:    defaultTransferOptions(),
			ctx:     sourceCtx,
			payload: withTarget(""),
			want:    sourceLogin(testTransferRedirect),
		},
		{
			name:    "target is this host",
			opts:    defaultTransferOptions(),
			ctx:     sourceCtx,
			payload: withTarget(testServerURL.Host),
			want:    sourceLogin(testTransferRedirect),
		},
		{
			name: "request is not on a platform host",
			opts: defaultTransferOptions(),
			ctx: func(ctx context.Context, f *transferFixture) context.Context {
				return contextvalues.SetSessionTokenInContext(ctx, f.source.SessionID)
			},
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
		{
			name:    "organization has no default host",
			opts:    transferOptions{member: true, defaultHost: nil, impersonator: "", supportOrgID: ""},
			ctx:     sourceCtx,
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
		{
			name:    "organization's default host is another host",
			opts:    transferOptions{member: true, defaultHost: new(testServerURL.String()), impersonator: "", supportOrgID: ""},
			ctx:     sourceCtx,
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
		{
			name:    "impersonation session",
			opts:    transferOptions{member: true, defaultHost: new(testTargetBaseURL), impersonator: "support@example.com", supportOrgID: ""},
			ctx:     sourceCtx,
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
		{
			name:    "support session",
			opts:    transferOptions{member: true, defaultHost: new(testTargetBaseURL), impersonator: "", supportOrgID: "org-123"},
			ctx:     sourceCtx,
			payload: withNonce,
			want:    targetLogin(testTransferRedirect),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newTransfer(t, tt.opts)
			require.Equal(t, tt.want, f.out(tt.ctx(ctx, f), t, tt.payload(f)))
		})
	}
}

func TestService_TransferOut_OrganizationLessSession(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	orgLess := f.source
	orgLess.SessionID = "org-less-session"
	orgLess.ActiveOrganizationID = ""
	require.NoError(t, f.instance.sessionManager.StoreSession(ctx, orgLess))

	orgLessCtx := contextvalues.SetSessionTokenInContext(atHost(ctx, testServerURL.String()), orgLess.SessionID)
	require.Equal(t, targetLogin(testTransferRedirect), f.out(orgLessCtx, t, outPayload("nonce")))
}

func TestService_TransferOut_HeaderSession(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	b := newBrowser()
	nonce := f.start(ctx, t, b)

	// No session cookie, only the session header.
	payload := outPayload(nonce)
	payload.SessionToken = new(f.source.SessionID)
	code := codeFrom(t, f.out(atHost(ctx, testServerURL.String()), t, payload))

	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, code, b))
}

func TestService_TransferOut_StoresOnlyTheNonceHash(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, nonce, _ := f.begin(ctx, t)

	var record map[string]any
	require.NoError(t, f.instance.nonceStore.Get(ctx, "session_transfer:"+nonceHash(code), &record))
	require.Equal(t, map[string]any{
		"UserID":               f.source.UserID,
		"ActiveOrganizationID": f.source.ActiveOrganizationID,
		"WorkOSSessionID":      f.source.WorkOSSessionID,
		"SourceHost":           testServerURL.Host,
		"TargetHost":           testExtraPlatformHost,
		"NonceHash":            nonceHash(nonce),
	}, record)
}

func TestService_Transfer_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, b := f.begin(ctx, t)
	require.NotContains(t, code, f.source.SessionID)

	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, code, b))
	require.Empty(t, b.transferCookies(), "the nonce cookie is cleared")

	// The source session is untouched.
	_, err := f.instance.sessionManager.GetSession(ctx, f.source.SessionID)
	require.NoError(t, err)
}

func TestService_TransferIn_ReplayedCode(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, b := f.begin(ctx, t)
	replay := b.clone()

	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, code, b))
	requireRefused(t, f.in(ctx, t, testTargetBaseURL, code, replay))
}

func TestService_TransferIn_RedirectIsSanitized(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, b := f.begin(ctx, t)

	result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(atHost(ctx, testTargetBaseURL), b), callbackPayload(code, new("//evil.example.com/x")))
	require.NoError(t, err)
	require.Equal(t, testTargetBaseURL+"/", result.Location)
	require.NotNil(t, result.SessionCookie)
}

func TestService_TransferIn_UnknownCode(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	requireRefused(t, f.in(ctx, t, testTargetBaseURL, "unknown-code", newBrowser()))
}

func TestService_TransferIn_ExpiredCode(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, b := f.begin(ctx, t)

	// Simulate the 60 second TTL expiring the record. Redis is shared with
	// parallel tests, so delete only this record.
	require.NoError(t, f.instance.nonceStore.Delete(ctx, "session_transfer:"+nonceHash(code)))

	requireRefused(t, f.in(ctx, t, testTargetBaseURL, code, b))
}

// A code presented on another host is refused but stays redeemable by the
// right browser on the right host.
func TestService_TransferIn_WrongHostDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, right := f.begin(ctx, t)

	result := f.in(ctx, t, testServerURL.String(), code, right.clone())
	require.Nil(t, result.SessionCookie)
	require.Equal(t, sourceLogin(testTransferRedirect), result.Location)

	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, code, right))
}

// A browser without the transfer's nonce cookie is refused, and the code is
// burned: TransferOut is a GET, so a victim can be made to issue a code bound
// to someone else's nonce, and that code must not stay redeemable if its URL
// leaks.
func TestService_TransferIn_BrowserMismatchBurnsCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		browser func(nonce string) *browser
	}{
		{"missing nonce cookie", func(string) *browser { return newBrowser() }},
		{"empty nonce cookie", func(nonce string) *browser {
			b := newBrowser()
			b.Set(constants.TransferInNonceCookiePrefix+nonceHash(nonce)[:16], "")
			return b
		}},
		{"wrong nonce cookie", func(nonce string) *browser {
			b := newBrowser()
			b.Set(constants.TransferInNonceCookiePrefix+nonceHash(nonce)[:16], "another-nonce")
			return b
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newTransfer(t, defaultTransferOptions())
			code, nonce, right := f.begin(ctx, t)

			requireRefused(t, f.in(ctx, t, testTargetBaseURL, code, tt.browser(nonce)))
			requireRefused(t, f.in(ctx, t, testTargetBaseURL, code, right))
		})
	}
}

func TestService_TransferIn_TwoTransfersInOneBrowser(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())

	// Two tabs start a transfer each before either finishes.
	b := newBrowser()
	nonceA, nonceB := f.start(ctx, t, b), f.start(ctx, t, b)
	codeA := codeFrom(t, f.out(f.sourceCtx(ctx), t, outPayload(nonceA)))
	codeB := codeFrom(t, f.out(f.sourceCtx(ctx), t, outPayload(nonceB)))

	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, codeB, b))
	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, codeA, b))
	require.Empty(t, b.transferCookies())
}

func TestService_TransferIn_AnotherTransfersCookieIsRefused(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	_, _, browserA := f.begin(ctx, t)
	codeB, _, browserB := f.begin(ctx, t)

	// Transfer A's cookie, even renamed to B's cookie name, does not satisfy
	// transfer B.
	renamed := newBrowser()
	for _, value := range browserA.transferCookies() {
		for name := range browserB.transferCookies() {
			renamed.Set(name, value)
		}
	}
	requireRefused(t, f.in(ctx, t, testTargetBaseURL, codeB, renamed))

	// A browser holding only A's cookie under A's name has no cookie for B.
	codeB2, _, browserB2 := f.begin(ctx, t)
	requireRefused(t, f.in(ctx, t, testTargetBaseURL, codeB2, browserA.clone()))
	requireRefused(t, f.in(ctx, t, testTargetBaseURL, codeB2, browserB2)) // the refused code is burned
}

func TestService_TransferIn_NonMemberDoesNotConsume(t *testing.T) {
	t.Parallel()

	// The code is issued for an organization the user is not a member of, as
	// if the membership was removed after transferOut issued it.
	ctx, f := newTransfer(t, transferOptions{member: false, defaultHost: new(testTargetBaseURL), impersonator: "", supportOrgID: ""})
	nonce := "browser-nonce"
	code, err := sessions.NewTransferManager(f.instance.nonceStore).Create(ctx, f.source, nonce, testServerURL.Host, testExtraPlatformHost)
	require.NoError(t, err)
	b := newBrowser()
	b.Set(constants.TransferInNonceCookiePrefix+nonceHash(nonce)[:16], nonce)

	requireRefused(t, f.in(ctx, t, testTargetBaseURL, code, b.clone()))

	require.NoError(t, f.instance.createTestOrganization(ctx, f.userInfo.Organizations[0], f.userInfo.UserID))
	f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, code, b))
}

func TestService_TransferIn_MembershipLookupErrorDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, b := f.begin(ctx, t)

	f.instance.conn.Close()
	requireRefused(t, f.in(ctx, t, testTargetBaseURL, code, b))

	// The code is still there for a retry.
	_, err := sessions.NewTransferManager(f.instance.nonceStore).Lookup(ctx, code, testExtraPlatformHost)
	require.NoError(t, err)
}

func TestService_TransferIn_ConcurrentRedemption(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())
	code, _, b := f.begin(ctx, t)

	const attempts = 8
	results := make([]*gen.TransferInResult, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		// Each request carries the same cookie, as parallel requests from one
		// browser would.
		reqCtx := auth.WithTransferCookieJar(atHost(ctx, testTargetBaseURL), b.clone())
		wg.Go(func() {
			results[i], errs[i] = f.instance.service.TransferIn(reqCtx, callbackPayload(code, nil))
		})
	}
	wg.Wait()

	accepted := 0
	for i := range attempts {
		require.NoError(t, errs[i])
		if results[i].SessionCookie != nil {
			accepted++
		}
	}
	require.Equal(t, 1, accepted)
}

// TestTransfer_HTTP runs start, out and in over HTTP, carrying cookies between
// the steps the way a browser does.
func TestTransfer_HTTP(t *testing.T) {
	t.Parallel()

	ctx, f := newTransfer(t, defaultTransferOptions())

	mux := goahttp.NewMuxer()
	auth.Attach(mux, f.instance.service)
	serve := func(baseURL, rawURL string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		handler := middleware.SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux.ServeHTTP(w, r.WithContext(atHost(r.Context(), baseURL)))
		}))
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	setCookies := func(rec *httptest.ResponseRecorder, prefix string) []*http.Cookie {
		var found []*http.Cookie
		for _, line := range rec.Header().Values("Set-Cookie") {
			c, err := http.ParseSetCookie(line)
			require.NoError(t, err)
			if strings.HasPrefix(c.Name, prefix) {
				found = append(found, c)
			}
		}
		return found
	}

	// 1. The target host's start mode binds the transfer to this browser.
	start := serve(testTargetBaseURL, testTargetBaseURL+"/rpc/auth.transferIn?"+url.Values{
		"source_host": {testServerURL.Host},
		"redirect":    {testTransferRedirect},
	}.Encode())
	require.Equal(t, http.StatusTemporaryRedirect, start.Code)
	nonceCookies := setCookies(start, constants.TransferInNonceCookiePrefix)
	require.Len(t, nonceCookies, 1)
	nonceCookie := nonceCookies[0]
	require.True(t, strings.HasPrefix(nonceCookie.Name, "__Host-"))
	require.NotEmpty(t, nonceCookie.Value)
	require.Equal(t, constants.TransferInNonceCookiePrefix+nonceHash(nonceCookie.Value)[:16], nonceCookie.Name)
	require.True(t, nonceCookie.Secure)
	require.True(t, nonceCookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, nonceCookie.SameSite)
	require.Equal(t, "/", nonceCookie.Path)
	require.Empty(t, nonceCookie.Domain)
	require.Equal(t, constants.TransferInNonceCookieMaxAgeSeconds, nonceCookie.MaxAge)

	// 2. The source host issues the code for the browser's session cookie.
	sessionCookie := &http.Cookie{Name: constants.SessionCookie, Value: f.source.SessionID}
	out := serve(testServerURL.String(), start.Header().Get("Location"), sessionCookie)
	require.Equal(t, http.StatusTemporaryRedirect, out.Code)
	transferInURL := out.Header().Get("Location")
	require.True(t, strings.HasPrefix(transferInURL, testTargetBaseURL+"/rpc/auth.transferIn?code="), transferInURL)

	// 3a. Without the nonce cookie the target refuses, writes no session,
	// clears the transfer's cookie, and burns the code.
	refused := serve(testTargetBaseURL, transferInURL)
	require.Equal(t, http.StatusTemporaryRedirect, refused.Code)
	require.Equal(t, targetLogin(testTransferRedirect), refused.Header().Get("Location"))
	require.Empty(t, setCookies(refused, constants.SessionCookie))
	cleared := setCookies(refused, nonceCookie.Name)
	require.Len(t, cleared, 1)
	require.Negative(t, cleared[0].MaxAge)

	// That burned the code, so the right cookie no longer helps.
	burned := serve(testTargetBaseURL, transferInURL, &http.Cookie{Name: nonceCookie.Name, Value: nonceCookie.Value})
	require.Equal(t, targetLogin(testTransferRedirect), burned.Header().Get("Location"))

	// 3b. A fresh transfer with the nonce cookie establishes the session and
	// clears the cookie.
	start = serve(testTargetBaseURL, testTargetBaseURL+"/rpc/auth.transferIn?"+url.Values{
		"source_host": {testServerURL.Host},
		"redirect":    {testTransferRedirect},
	}.Encode())
	nonceCookies = setCookies(start, constants.TransferInNonceCookiePrefix)
	require.Len(t, nonceCookies, 1)
	nonceCookie = nonceCookies[0]
	out = serve(testServerURL.String(), start.Header().Get("Location"), sessionCookie)
	transferInURL = out.Header().Get("Location")
	accepted := serve(testTargetBaseURL, transferInURL, &http.Cookie{Name: nonceCookie.Name, Value: nonceCookie.Value})
	require.Equal(t, http.StatusTemporaryRedirect, accepted.Code)
	require.Equal(t, testTargetBaseURL+testTransferRedirect, accepted.Header().Get("Location"))
	minted := setCookies(accepted, constants.SessionCookie)
	require.Len(t, minted, 1)
	require.NotEqual(t, f.source.SessionID, minted[0].Value)
	_, err := f.instance.sessionManager.GetSession(ctx, minted[0].Value)
	require.NoError(t, err)
	cleared = setCookies(accepted, nonceCookie.Name)
	require.Len(t, cleared, 1)
	require.Negative(t, cleared[0].MaxAge)
}

// Each mode check must refuse without starting a transfer or using up the
// code, so the real callback still succeeds afterwards.
func TestService_TransferIn_Modes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload func(code string) *gen.TransferInPayload
	}{
		{"both source_host and code", func(code string) *gen.TransferInPayload {
			return &gen.TransferInPayload{SourceHost: new(testServerURL.Host), Code: &code, Redirect: new(testTransferRedirect)}
		}},
		{"neither source_host nor code", func(string) *gen.TransferInPayload {
			return &gen.TransferInPayload{SourceHost: nil, Code: nil, Redirect: new(testTransferRedirect)}
		}},
		{"a tampered code never restarts a transfer", func(string) *gen.TransferInPayload {
			return callbackPayload("tampered-code", new(testTransferRedirect))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newTransfer(t, defaultTransferOptions())
			code, _, b := f.begin(ctx, t)

			probe := b.clone()
			result, err := f.instance.service.TransferIn(auth.WithTransferCookieJar(atHost(ctx, testTargetBaseURL), probe), tt.payload(code))
			require.NoError(t, err)
			requireRefused(t, result)
			require.NotContains(t, result.Location, "transferOut")
			require.Equal(t, b.transferCookies(), probe.transferCookies(), "no cookie set or cleared")

			f.requireAccepted(ctx, t, f.in(ctx, t, testTargetBaseURL, code, b))
		})
	}
}
