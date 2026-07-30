package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	consentSlug       = "consent-server"
	consentApproveURL = "http://localhost:8080/callback?code=the-auth-code&state=client-state"
	consentDenyURL    = "http://localhost:8080/callback?error=access_denied&state=client-state"
)

// seedPendingConsent parks a PendingConsent in the same cache the service
// reads from, standing in for the upstream callback that normally creates it.
func seedPendingConsent(t *testing.T, env *oauthtest.OAuthServiceEnv, toolsetID uuid.UUID, useResultPage bool) string {
	t.Helper()

	consentID := uuid.NewString()
	storage := cache.NewTypedObjectCache[oauth.PendingConsent](testenv.NewLogger(t), env.Cache, cache.SuffixNone)

	require.NoError(t, storage.Store(t.Context(), oauth.PendingConsent{
		ID:            consentID,
		ToolsetID:     toolsetID,
		Code:          "the-auth-code",
		ApproveURL:    consentApproveURL,
		DenyURL:       consentDenyURL,
		ClientID:      "test-client",
		MCPSlug:       consentSlug,
		UseResultPage: useResultPage,
	}))

	return consentID
}

func postConsent(t *testing.T, env *oauthtest.OAuthServiceEnv, slug, consentID, action string) *httptest.ResponseRecorder {
	t.Helper()

	mux := goahttp.NewMuxer()
	oauth.Attach(mux, env.Service)

	form := url.Values{}
	form.Set("consent_id", consentID)
	form.Set("action", action)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"/oauth/"+slug+"/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestHandleConsentApproveRedirectsToClient(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	consentID := seedPendingConsent(t, env, uuid.New(), false)

	rec := postConsent(t, env, consentSlug, consentID, "approve")

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, consentApproveURL, rec.Header().Get("Location"),
		"approval hands the client the authorization code")
}

func TestHandleConsentDenyRedirectsWithAccessDenied(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	consentID := seedPendingConsent(t, env, uuid.New(), false)

	rec := postConsent(t, env, consentSlug, consentID, "deny")

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, consentDenyURL, rec.Header().Get("Location"),
		"denial returns access_denied instead of a code")
}

// Anything other than an explicit approve is a denial — a form replayed
// without the action field must not be treated as consent.
func TestHandleConsentMissingActionIsDenial(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	consentID := seedPendingConsent(t, env, uuid.New(), false)

	rec := postConsent(t, env, consentSlug, consentID, "")

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, consentDenyURL, rec.Header().Get("Location"))
}

func TestHandleConsentDenyRevokesGrant(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	grantStorage := cache.NewTypedObjectCache[oauth.Grant](testenv.NewLogger(t), env.Cache, cache.SuffixNone)
	require.NoError(t, grantStorage.Store(ctx, oauth.Grant{
		ToolsetID: toolsetID,
		Code:      "the-auth-code",
		ClientID:  "test-client",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}))

	consentID := seedPendingConsent(t, env, toolsetID, false)

	rec := postConsent(t, env, consentSlug, consentID, "deny")
	require.Equal(t, http.StatusFound, rec.Code)

	_, err := grantStorage.Get(ctx, oauth.GrantCacheKey(toolsetID, "the-auth-code"))
	require.Error(t, err, "a denied authorization must leave no redeemable code behind")
}

func TestHandleConsentApproveKeepsGrantRedeemable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	grantStorage := cache.NewTypedObjectCache[oauth.Grant](testenv.NewLogger(t), env.Cache, cache.SuffixNone)
	require.NoError(t, grantStorage.Store(ctx, oauth.Grant{
		ToolsetID: toolsetID,
		Code:      "the-auth-code",
		ClientID:  "test-client",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}))

	consentID := seedPendingConsent(t, env, toolsetID, false)

	rec := postConsent(t, env, consentSlug, consentID, "approve")
	require.Equal(t, http.StatusFound, rec.Code)

	grant, err := grantStorage.Get(ctx, oauth.GrantCacheKey(toolsetID, "the-auth-code"))
	require.NoError(t, err, "approval leaves the grant for the token endpoint to consume")
	require.Equal(t, "test-client", grant.ClientID)
}

func TestHandleConsentIsSingleUse(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	consentID := seedPendingConsent(t, env, uuid.New(), false)

	first := postConsent(t, env, consentSlug, consentID, "approve")
	require.Equal(t, http.StatusFound, first.Code)

	second := postConsent(t, env, consentSlug, consentID, "approve")
	require.NotEqual(t, http.StatusFound, second.Code,
		"a replayed consent form must not mint a second redirect for the same code")
}

func TestHandleConsentRejectsSlugMismatch(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	consentID := seedPendingConsent(t, env, uuid.New(), false)

	rec := postConsent(t, env, "some-other-server", consentID, "approve")

	require.NotEqual(t, http.StatusFound, rec.Code)
	require.Empty(t, rec.Header().Get("Location"))
}

func TestHandleConsentRejectsUnknownConsentID(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	rec := postConsent(t, env, consentSlug, uuid.NewString(), "approve")

	require.NotEqual(t, http.StatusFound, rec.Code)
	require.Empty(t, rec.Header().Get("Location"))
}

func TestHandleConsentRejectsMissingConsentID(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	rec := postConsent(t, env, consentSlug, "", "approve")

	require.NotEqual(t, http.StatusFound, rec.Code)
	require.Empty(t, rec.Header().Get("Location"))
}

// Gram-managed providers finish in a hosted status page that reports back to
// the opener rather than a bare 302, on both decisions.
func TestHandleConsentRendersResultPageForGramProvider(t *testing.T) {
	t.Parallel()
	env := newOAuthServiceTestEnv(t)

	approveID := seedPendingConsent(t, env, uuid.New(), true)
	approved := postConsent(t, env, consentSlug, approveID, "approve")
	require.Equal(t, http.StatusOK, approved.Code)
	require.Contains(t, approved.Header().Get("Content-Type"), "text/html")
	// The page HTML-escapes the URL it redirects to, so match the code alone.
	require.Contains(t, approved.Body.String(), "code=the-auth-code")

	denyID := seedPendingConsent(t, env, uuid.New(), true)
	denied := postConsent(t, env, consentSlug, denyID, "deny")
	require.Equal(t, http.StatusOK, denied.Code)
	require.Contains(t, denied.Body.String(), "access_denied")
}
