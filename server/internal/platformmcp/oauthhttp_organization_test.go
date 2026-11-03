package platformmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

// organizationIdentity reports the organization a user picked, or signed in
// with SSO for, at the identity provider.
type organizationIdentity struct {
	testIdentity
	organizationID string
	method         string
}

func (i organizationIdentity) ExchangeCodeForTokens(ctx context.Context, code string) (*identity.IDPUserInfo, error) {
	user, err := i.testIdentity.ExchangeCodeForTokens(ctx, code)
	if err != nil {
		return nil, err
	}
	user.OrganizationID = i.organizationID
	user.AuthenticationMethod = i.method
	return user, nil
}

type failingOrganizationSelector struct{}

func (failingOrganizationSelector) EligibleOrganizations(context.Context, string) ([]OrganizationOption, error) {
	return nil, errors.New("organization store unavailable")
}

// signInToPlatformMCP authorizes a registered client and completes the
// identity provider callback, returning where the callback sent the browser.
func signInToPlatformMCP(t *testing.T, service *OAuthHTTP) *url.URL {
	t.Helper()
	require.NoError(t, testStore(t, service).RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test client", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256&state=client-state", nil))
	require.Equal(t, http.StatusFound, authorize.Code)
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/platform-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil))
	require.Equal(t, http.StatusFound, callback.Code)
	next, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	return next
}

func getConsent(service *OAuthHTTP, state string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/connect?state="+url.QueryEscape(state), nil))
	return response
}

func postConsent(service *OAuthHTTP, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, request)
	return response
}

func postOrganizationSelection(service *OAuthHTTP, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/select-organization", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.OrganizationSelectionHandler().ServeHTTP(response, request)
	return response
}

// approveConsent approves the consent page the state names, posting back the
// organization the page shows.
func approveConsent(t *testing.T, service *OAuthHTTP, state string) *httptest.ResponseRecorder {
	t.Helper()
	consent := getConsent(service, state)
	require.Equal(t, http.StatusOK, consent.Code)
	body := consent.Body.String()
	return postConsent(service, url.Values{"state": {state}, "csrf_token": {formValue(t, body, "csrf_token")}, "organization_id": {formValue(t, body, "organization_id")}, "action": {"approve"}})
}

func TestOAuthHTTPConsentUsesIdentityProviderOrganization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.identity = organizationIdentity{organizationID: "org_workos_2"}
	service.organizations = testOrganizationSelector{organizations: twoTestOrganizations}
	telemetry := &testOAuthTelemetry{}
	service.telemetry = telemetry

	next := signInToPlatformMCP(t, service)
	require.Equal(t, "/platform-mcp/connect", next.Path, "the organization picked at the identity provider is not asked for again")
	state := next.Query().Get("state")

	consent := getConsent(service, state)
	require.Equal(t, http.StatusOK, consent.Code)
	body := consent.Body.String()
	require.Contains(t, body, "Organization two")
	require.Equal(t, "org-2", formValue(t, body, "organization_id"))
	require.Contains(t, body, `href="select-organization?state=`+url.QueryEscape(state)+`"`)
	require.Contains(t, body, "Change organization")

	approved := approveConsent(t, service, state)
	require.Equal(t, http.StatusSeeOther, approved.Code)
	redirect, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, redirect.Query().Get("code"))
	require.Equal(t, []OAuthEvent{{Operation: "interactive_authorization", Outcome: "succeeded", OrganizationSelection: organizationSelectionIdentityProvider}}, telemetry.events)
}

func TestOAuthHTTPConsentUsesSSOOrganization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.identity = organizationIdentity{organizationID: "org_workos_1", method: signins.MethodSSO}
	service.organizations = testOrganizationSelector{organizations: twoTestOrganizations}

	next := signInToPlatformMCP(t, service)
	require.Equal(t, "/platform-mcp/connect", next.Path)
	challenge, err := service.cache.Get(t.Context(), platformOAuthChallengePrefix+next.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, "org-1", challenge.OrganizationID)
	require.Equal(t, "org_workos_1", challenge.SSOOrganizationID)
	require.Equal(t, organizationSelectionIdentityProvider, challenge.OrganizationSelection)
}

func TestOAuthHTTPConsentUsesOnlyOrganization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	telemetry := &testOAuthTelemetry{}
	service.telemetry = telemetry

	next := signInToPlatformMCP(t, service)
	require.Equal(t, "/platform-mcp/connect", next.Path, "a user with one organization is not asked to pick it")
	state := next.Query().Get("state")

	consent := getConsent(service, state)
	require.Equal(t, http.StatusOK, consent.Code)
	require.Contains(t, consent.Body.String(), "Organization one")
	require.NotContains(t, consent.Body.String(), "Change organization", "there is no other organization to change to")

	approved := approveConsent(t, service, state)
	require.Equal(t, http.StatusSeeOther, approved.Code)
	require.Equal(t, []OAuthEvent{{Operation: "interactive_authorization", Outcome: "succeeded", OrganizationSelection: organizationSelectionOnlyOrganization}}, telemetry.events)
}

func TestOAuthHTTPOrganizationPickerWhenNothingIsPreselected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		organizationID string
		organizations  OrganizationSelector
		gate           Gate
	}{
		{name: "several organizations and none reported", organizations: testOrganizationSelector{organizations: twoTestOrganizations}, gate: allowGate{}},
		{name: "reported organization is not eligible", organizationID: "org_workos_unknown", organizations: testOrganizationSelector{organizations: twoTestOrganizations}, gate: allowGate{}},
		{name: "reported organization is not eligible and one is", organizationID: "org_workos_unknown", organizations: testOrganizationSelector{organizations: twoTestOrganizations[:1]}, gate: allowGate{}},
		{name: "platform mcp is disabled for the reported organization", organizationID: "org_workos_2", organizations: testOrganizationSelector{organizations: twoTestOrganizations}, gate: oauthTestGate{enabled: false}},
		{name: "platform mcp is disabled for the only organization", organizations: testOrganizationSelector{organizations: twoTestOrganizations[:1]}, gate: oauthTestGate{enabled: false}},
		{name: "feature gate is unavailable", organizationID: "org_workos_2", organizations: testOrganizationSelector{organizations: twoTestOrganizations}, gate: oauthTestGate{err: errors.New("feature provider unavailable")}},
		{name: "eligible organizations are unavailable", organizationID: "org_workos_2", organizations: failingOrganizationSelector{}, gate: allowGate{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := newTestOAuthHTTP(t)
			service.identity = organizationIdentity{organizationID: tc.organizationID}
			service.organizations = tc.organizations
			service.gate = tc.gate

			next := signInToPlatformMCP(t, service)
			require.Equal(t, "/platform-mcp/select-organization", next.Path)
			challenge, err := service.cache.Get(t.Context(), platformOAuthChallengePrefix+next.Query().Get("state"))
			require.NoError(t, err)
			require.Empty(t, challenge.OrganizationID)
			require.Empty(t, challenge.OrganizationSelection)
		})
	}
}

func TestOAuthHTTPChangeOrganizationFromConsent(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.identity = organizationIdentity{organizationID: "org_workos_2"}
	service.organizations = testOrganizationSelector{organizations: twoTestOrganizations}
	telemetry := &testOAuthTelemetry{}
	service.telemetry = telemetry

	next := signInToPlatformMCP(t, service)
	require.Equal(t, "/platform-mcp/connect", next.Path)
	preselectedState := next.Query().Get("state")
	preselectedConsent := getConsent(service, preselectedState).Body.String()
	csrf := formValue(t, preselectedConsent, "csrf_token")

	// The change link opens the picker with the current organization chosen.
	picker := getSelection(service, preselectedState)
	require.Equal(t, http.StatusOK, picker.Code)
	require.Contains(t, picker.Body.String(), `<option value="org-2" selected>Organization two</option>`)
	require.Contains(t, picker.Body.String(), `<option value="org-1">Organization one</option>`)

	changed := postOrganizationSelection(service, url.Values{"state": {preselectedState}, "csrf_token": {csrf}, "organization_id": {"org-1"}})
	require.Equal(t, http.StatusSeeOther, changed.Code)
	connectURL, err := url.Parse(changed.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/platform-mcp/connect", connectURL.Path)
	changedState := connectURL.Query().Get("state")
	require.NotEqual(t, preselectedState, changedState, "the consent page for the new organization continues under a fresh state")

	// The consent page still open for the previous organization cannot
	// approve the organization the user changed to.
	stale := postConsent(service, url.Values{"state": {preselectedState}, "csrf_token": {csrf}, "organization_id": {"org-2"}, "action": {"approve"}})
	require.Equal(t, http.StatusBadRequest, stale.Code)
	require.Empty(t, stale.Header().Get("Location"))

	consent := getConsent(service, changedState)
	require.Equal(t, http.StatusOK, consent.Code)
	require.Contains(t, consent.Body.String(), "Organization one")
	require.Equal(t, "org-1", formValue(t, consent.Body.String(), "organization_id"))

	approved := approveConsent(t, service, changedState)
	require.Equal(t, http.StatusSeeOther, approved.Code)
	redirect, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, redirect.Query().Get("code"))
	require.Equal(t, organizationSelectionChanged, telemetry.events[len(telemetry.events)-1].OrganizationSelection)
}

func TestOAuthHTTPChangeOrganizationWithoutRemainingMemberships(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{}
	service.workspaces = workspaces
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: "http://127.0.0.1:3000/callback", CSRFToken: "csrf", OrganizationID: "org-1", OrganizationSelection: organizationSelectionOnlyOrganization, Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))
	service.organizations = testOrganizationSelector{organizations: nil}

	picker := getSelection(service, challenge.ID)

	require.Equal(t, http.StatusForbidden, picker.Code)
	require.NotContains(t, picker.Body.String(), "create-workspace", "the workspace forms refuse a challenge that already has an organization")
}

// consentChallenge stores a challenge ready for consent to organization org-1.
func consentChallenge(t *testing.T, service *OAuthHTTP) oauthChallenge {
	t.Helper()
	require.NoError(t, testStore(t, service).RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: "http://127.0.0.1:3000/callback", State: "client-state", CSRFToken: "csrf", OrganizationID: "org-1", Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))
	return challenge
}

func TestOAuthHTTPConsentRejectsAnotherOrganization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	challenge := consentChallenge(t, service)

	response := postConsent(service, url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "organization_id": {"org-2"}, "action": {"approve"}})

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Empty(t, response.Header().Get("Location"), "no authorization code is issued")
}

func TestOAuthHTTPConsentAcceptsPageWithoutOrganization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	challenge := consentChallenge(t, service)

	response := postConsent(service, url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}})

	require.Equal(t, http.StatusSeeOther, response.Code)
	redirect, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, redirect.Query().Get("code"))
}

func TestOAuthHTTPChangeOrganizationToTheSameOrganization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.identity = organizationIdentity{organizationID: "org_workos_2"}
	service.organizations = testOrganizationSelector{organizations: twoTestOrganizations}
	telemetry := &testOAuthTelemetry{}
	service.telemetry = telemetry

	next := signInToPlatformMCP(t, service)
	require.Equal(t, "/platform-mcp/connect", next.Path)
	state := next.Query().Get("state")
	csrf := formValue(t, getConsent(service, state).Body.String(), "csrf_token")

	kept := postOrganizationSelection(service, url.Values{"state": {state}, "csrf_token": {csrf}, "organization_id": {"org-2"}})
	require.Equal(t, http.StatusSeeOther, kept.Code)
	connectURL, err := url.Parse(kept.Header().Get("Location"))
	require.NoError(t, err)

	approved := approveConsent(t, service, connectURL.Query().Get("state"))
	require.Equal(t, http.StatusSeeOther, approved.Code)
	require.Equal(t, []OAuthEvent{{Operation: "interactive_authorization", Outcome: "succeeded", OrganizationSelection: organizationSelectionIdentityProvider}}, telemetry.events)
}
