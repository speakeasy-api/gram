package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// testTargetBaseURL is the platform host sessions are transferred to in these
// tests. The source is the server host.
var testTargetBaseURL = "https://" + testExtraPlatformHost

// transferSession stores a session for userInfo's first organization and
// returns a context authenticated with it on the server host.
func transferSession(t *testing.T, ctx context.Context, instance *testInstance, userInfo *MockUserInfo, impersonator string) (context.Context, sessions.Session) {
	t.Helper()

	session := sessions.Session{
		SessionID:             "source-" + t.Name(),
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  userInfo.Organizations[0].ID,
		WorkOSSessionID:       "workos-session-id",
		ImpersonatorEmail:     impersonator,
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	})
	return atHost(ctx, testServerURL.String()), session
}

func atHost(ctx context.Context, baseURL string) context.Context {
	return requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, baseURL))
}

func transferStart(ctx context.Context, instance *testInstance, sourceHost string) (*gen.TransferStartResult, error) {
	return instance.service.TransferStart(atHost(ctx, testTargetBaseURL), &gen.TransferStartPayload{ //nolint:wrapcheck // test helper returns the service error as is
		SourceHost: sourceHost,
		Redirect:   nil,
	})
}

func transferOut(ctx context.Context, instance *testInstance, nonce string) (*gen.TransferOutResult, error) {
	return instance.service.TransferOut(ctx, &gen.TransferOutPayload{ //nolint:wrapcheck // test helper returns the service error as is
		TargetHost:   testExtraPlatformHost,
		Nonce:        nonce,
		Redirect:     nil,
		SessionToken: nil,
	})
}

func transferIn(ctx context.Context, instance *testInstance, host, code string, nonce *string) (*gen.TransferInResult, error) {
	ctx = atHost(ctx, host)
	if nonce != nil {
		ctx = auth.TestTransferNonceContext(ctx, *nonce)
	}
	return instance.service.TransferIn(ctx, &gen.TransferInPayload{ //nolint:wrapcheck // test helper returns the service error as is
		Token:    code,
		Redirect: nil,
	})
}

// beginTransfer runs TransferStart on the target host and TransferOut on the
// source host. It returns the transfer code and the browser nonce.
func beginTransfer(t *testing.T, ctx context.Context, instance *testInstance) (string, string) {
	t.Helper()

	start, err := transferStart(ctx, instance, testServerURL.Host)
	require.NoError(t, err)

	out, err := transferOut(ctx, instance, start.TransferNonceCookie)
	require.NoError(t, err)

	location, err := url.Parse(out.Location)
	require.NoError(t, err)
	require.Equal(t, testExtraPlatformHost, location.Host)
	require.Equal(t, "/rpc/auth.transferIn", location.Path)
	code := location.Query().Get("token")
	require.NotEmpty(t, code)
	return code, start.TransferNonceCookie
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// newTransferInstance seeds a user and their organization. With member false
// the user is not a member of the organization.
func newTransferInstance(t *testing.T, member bool) (context.Context, *testInstance, *MockUserInfo) {
	t.Helper()
	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	memberID := ""
	if member {
		memberID = userInfo.UserID
	}
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], memberID))
	return ctx, instance, userInfo
}

func TestService_TransferStart(t *testing.T) {
	t.Parallel()

	ctx, instance, _ := newTransferInstance(t, true)

	t.Run("redirects to the source host's transferOut with the nonce", func(t *testing.T) {
		t.Parallel()

		result, err := instance.service.TransferStart(atHost(ctx, testTargetBaseURL), &gen.TransferStartPayload{
			SourceHost: testServerURL.Host,
			Redirect:   new("/projects?tab=1"),
		})
		require.NoError(t, err)
		require.NotEmpty(t, result.TransferNonceCookie)

		location, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Equal(t, testServerURL.Scheme, location.Scheme)
		require.Equal(t, testServerURL.Host, location.Host)
		require.Equal(t, "/rpc/auth.transferOut", location.Path)
		require.Equal(t, url.Values{
			"target_host": {testExtraPlatformHost},
			"nonce":       {result.TransferNonceCookie},
			"redirect":    {"/projects?tab=1"},
		}, location.Query())
	})

	t.Run("drops an off-site redirect", func(t *testing.T) {
		t.Parallel()

		result, err := instance.service.TransferStart(atHost(ctx, testTargetBaseURL), &gen.TransferStartPayload{
			SourceHost: testServerURL.Host,
			Redirect:   new("https://evil.example.com/"),
		})
		require.NoError(t, err)
		location, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.False(t, location.Query().Has("redirect"))
	})

	t.Run("rejects a source host that is not a platform host", func(t *testing.T) {
		t.Parallel()

		_, err := transferStart(ctx, instance, "evil.example.com")
		requireOopsCode(t, err, oops.CodeBadRequest)
	})

	t.Run("rejects the same host", func(t *testing.T) {
		t.Parallel()

		_, err := transferStart(ctx, instance, testExtraPlatformHost)
		requireOopsCode(t, err, oops.CodeBadRequest)
	})

	t.Run("requires a platform host", func(t *testing.T) {
		t.Parallel()

		_, err := instance.service.TransferStart(ctx, &gen.TransferStartPayload{
			SourceHost: testServerURL.Host,
			Redirect:   nil,
		})
		requireOopsCode(t, err, oops.CodeForbidden)
	})
}

func TestService_TransferOut_RequiresNonce(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")

	_, err := transferOut(ctx, instance, "")
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_Transfer_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, source := transferSession(t, ctx, instance, userInfo, "")

	code, nonce := beginTransfer(t, ctx, instance)
	require.NotContains(t, code, source.SessionID)

	result, err := transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	require.NoError(t, err)
	require.Equal(t, testTargetBaseURL, result.Location)
	require.NotEqual(t, source.SessionID, result.SessionToken)

	minted, err := instance.sessionManager.GetSession(ctx, result.SessionToken)
	require.NoError(t, err)
	require.Equal(t, source.UserID, minted.UserID)
	require.Equal(t, source.ActiveOrganizationID, minted.ActiveOrganizationID)
	require.Equal(t, source.WorkOSSessionID, minted.WorkOSSessionID)
	require.Empty(t, minted.ImpersonatorEmail)

	_, err = transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestService_TransferIn_BrowserBinding(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")
	code, nonce := beginTransfer(t, ctx, instance)

	// A browser without the nonce cookie, such as a victim sent the link by
	// an attacker, cannot redeem the code.
	_, err := transferIn(ctx, instance, testTargetBaseURL, code, nil)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// Nor can a browser holding another transfer's nonce.
	_, err = transferIn(ctx, instance, testTargetBaseURL, code, new("another-transfer-nonce"))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// Neither attempt consumed the code.
	_, err = transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	require.NoError(t, err)
}

func TestService_TransferOut_RejectsImpersonation(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "support@example.com")

	_, err := transferOut(ctx, instance, "nonce")
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_TransferIn_WrongHostDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")
	code, nonce := beginTransfer(t, ctx, instance)

	_, err := transferIn(ctx, instance, testServerURL.String(), code, &nonce)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	_, err = transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	require.NoError(t, err)
}

func TestService_TransferIn_NonMemberDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, false)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")
	code, nonce := beginTransfer(t, ctx, instance)

	_, err := transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	requireOopsCode(t, err, oops.CodeForbidden)

	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))
	_, err = transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	require.NoError(t, err)
}

func TestService_TransferIn_NoActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, false)
	userInfo.Organizations[0].ID = ""
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")
	code, nonce := beginTransfer(t, ctx, instance)

	result, err := transferIn(ctx, instance, testTargetBaseURL, code, &nonce)
	require.NoError(t, err)

	minted, err := instance.sessionManager.GetSession(ctx, result.SessionToken)
	require.NoError(t, err)
	require.Empty(t, minted.ActiveOrganizationID)
}

// TestTransfer_HTTPCookies drives the target host's HTTP endpoints to check
// how the nonce cookie is set, read, and cleared.
func TestTransfer_HTTPCookies(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	sourceCtx, _ := transferSession(t, ctx, instance, userInfo, "")

	mux := goahttp.NewMuxer()
	auth.Attach(mux, instance.service)
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(atHost(r.Context(), testTargetBaseURL)))
	})

	serve := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, testTargetBaseURL+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		target.ServeHTTP(rec, req)
		return rec
	}
	findCookie := func(rec *httptest.ResponseRecorder, name string) *http.Cookie {
		for _, line := range rec.Header().Values("Set-Cookie") {
			c, err := http.ParseSetCookie(line)
			require.NoError(t, err)
			if c.Name == name {
				return c
			}
		}
		return nil
	}

	start := serve("/rpc/auth.transferStart?"+url.Values{"source_host": {testServerURL.Host}}.Encode(), nil)
	require.Equal(t, http.StatusTemporaryRedirect, start.Code)
	nonceCookie := findCookie(start, constants.SessionTransferNonceCookie)
	require.NotNil(t, nonceCookie)
	require.NotEmpty(t, nonceCookie.Value)
	require.True(t, nonceCookie.Secure)
	require.True(t, nonceCookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, nonceCookie.SameSite)
	require.Equal(t, "/", nonceCookie.Path)
	require.Empty(t, nonceCookie.Domain)
	require.Equal(t, constants.SessionTransferNonceCookieMaxAgeSeconds, nonceCookie.MaxAge)

	out, err := transferOut(sourceCtx, instance, nonceCookie.Value)
	require.NoError(t, err)
	outURL, err := url.Parse(out.Location)
	require.NoError(t, err)
	transferInPath := outURL.RequestURI()

	// Without the cookie the code is refused, and the cookie is still cleared.
	refused := serve(transferInPath, nil)
	require.Equal(t, http.StatusUnauthorized, refused.Code)
	cleared := findCookie(refused, constants.SessionTransferNonceCookie)
	require.NotNil(t, cleared)
	require.Negative(t, cleared.MaxAge)

	// With the cookie the session is established and the cookie cleared.
	accepted := serve(transferInPath, &http.Cookie{Name: constants.SessionTransferNonceCookie, Value: nonceCookie.Value})
	require.Equal(t, http.StatusTemporaryRedirect, accepted.Code)
	require.NotNil(t, findCookie(accepted, constants.SessionCookie))
	cleared = findCookie(accepted, constants.SessionTransferNonceCookie)
	require.NotNil(t, cleared)
	require.Negative(t, cleared.MaxAge)
}
