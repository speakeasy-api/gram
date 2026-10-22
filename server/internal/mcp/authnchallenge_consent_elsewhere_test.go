package mcp_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// finishElsewhereFixture is a private toolset with a pending consent
// challenge whose client listens at redirectURI.
type finishElsewhereFixture struct {
	ti      *testInstance
	slug    string
	stateID string
	csrf    string
}

func newFinishElsewhereFixture(t *testing.T, redirectURI string) finishElsewhereFixture {
	t.Helper()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, toolset.ID.String())
	subject := urn.NewUserSubject(mockidp.MockUserID)
	stateID := uuid.NewString()
	csrf := "csrf-" + uuid.NewString()

	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            client.ClientID,
		RedirectURI:         redirectURI,
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           csrf,
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))
	return finishElsewhereFixture{ti: ti, slug: toolset.McpSlug.String, stateID: stateID, csrf: csrf}
}

func (f finishElsewhereFixture) serve(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", f.slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleConsent(w, req))
	return w
}

func (f finishElsewhereFixture) get(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.serve(t, httptest.NewRequest(http.MethodGet, "/mcp/"+f.slug+"/connect?state="+f.stateID, nil))
}

func (f finishElsewhereFixture) approve(t *testing.T, finishElsewhere bool) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{"state": {f.stateID}, "csrf_token": {f.csrf}, "action": {"approve"}}
	if finishElsewhere {
		form.Set("finish_elsewhere", "1")
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return f.serve(t, req)
}

func TestHandleConsentGet_OffersFinishElsewhereForLoopbackRedirect(t *testing.T) {
	t.Parallel()

	w := newFinishElsewhereFixture(t, "http://127.0.0.1:51734/callback").get(t)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `name="finish_elsewhere"`)
	require.Contains(t, w.Body.String(), "Finishing on another machine?")
}

func TestHandleConsentGet_HidesFinishElsewhereForWebRedirect(t *testing.T) {
	t.Parallel()

	w := newFinishElsewhereFixture(t, "https://client.example.com/callback").get(t)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), `name="finish_elsewhere"`)
}

// Ticked, an approval renders the authorization response for the person to
// deliver from the client's machine instead of redirecting this browser.
func TestHandleConsentPost_FinishElsewhereShowsCallbackCommand(t *testing.T) {
	t.Parallel()

	w := newFinishElsewhereFixture(t, "http://127.0.0.1:51734/callback").approve(t, true)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Header().Get("Location"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	body := w.Body.String()
	require.Contains(t, body, "Finish on the other machine")
	_, rest, found := strings.Cut(body, `data-finish-url>`)
	require.True(t, found, "page shows the callback address")
	shown, _, found := strings.Cut(rest, "</code>")
	require.True(t, found)

	callback, err := url.Parse(html.UnescapeString(shown))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:51734", callback.Host)
	require.Equal(t, "/callback", callback.Path)
	require.Equal(t, "client-state", callback.Query().Get("state"))
	require.NotEmpty(t, callback.Query().Get("code"))
	require.NotEmpty(t, callback.Query().Get("iss"))
}

// Unticked, a loopback client keeps the redirect it always had.
func TestHandleConsentPost_LoopbackRedirectsWhenNotFinishingElsewhere(t *testing.T) {
	t.Parallel()

	w := newFinishElsewhereFixture(t, "http://127.0.0.1:51734/callback").approve(t, false)
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:51734", loc.Host)
	require.NotEmpty(t, loc.Query().Get("code"))
}

// A browser can follow a web redirect itself, so the field is ignored there.
func TestHandleConsentPost_FinishElsewhereIgnoredForWebRedirect(t *testing.T) {
	t.Parallel()

	w := newFinishElsewhereFixture(t, "https://client.example.com/callback").approve(t, true)
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "client.example.com", loc.Host)
	require.NotEmpty(t, loc.Query().Get("code"))
}
