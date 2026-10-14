package platformmcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

// workspaceTestState backs both the organization selector and the workspace
// provisioner, so an organization created mid-flow is eligible afterwards,
// as it is against the live database.
type workspaceTestState struct {
	mu            sync.Mutex
	organizations []OrganizationOption
	provisioned   []string
}

func (s *workspaceTestState) EligibleOrganizations(context.Context, string) ([]OrganizationOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]OrganizationOption(nil), s.organizations...), nil
}

func (s *workspaceTestState) SuggestedWorkspaceName(context.Context, string) (string, error) {
	return "Acme", nil
}

func (s *workspaceTestState) ProvisionWorkspace(_ context.Context, userID, name string) (OrganizationOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	organization := OrganizationOption{ID: "org-new", Name: name}
	s.organizations = append(s.organizations, organization)
	s.provisioned = append(s.provisioned, userID+":"+name)
	return organization, nil
}

func (s *workspaceTestState) provisionedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.provisioned)
}

// startWorkspaceFlow runs authorize and the identity-provider callback and
// returns the challenge state the selection page is served for.
func startWorkspaceFlow(t *testing.T, service *OAuthHTTP) string {
	t.Helper()
	require.NoError(t, testStore(t, service).RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test client", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256&state=client-state", nil))
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/platform-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil))
	selectionURL, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	return selectionURL.Query().Get("state")
}

func getSelection(service *OAuthHTTP, state string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	service.OrganizationSelectionHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/select-organization?state="+url.QueryEscape(state), nil))
	return response
}

func postWorkspace(service *OAuthHTTP, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/create-workspace", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.CreateWorkspaceHandler().ServeHTTP(response, request)
	return response
}

func formValue(t *testing.T, body, name string) string {
	t.Helper()
	marker := `name="` + name + `"`
	start := strings.Index(body, marker)
	require.GreaterOrEqual(t, start, 0, "missing %s", name)
	_, rest, found := strings.Cut(body[start:], `value="`)
	require.True(t, found, "missing value for %s", name)
	value, _, _ := strings.Cut(rest, `"`)
	return value
}

func TestOAuthHTTPUserWithoutOrganizationCreatesWorkspaceAndContinues(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)

	selection := getSelection(service, state)
	require.Equal(t, http.StatusOK, selection.Code)
	body := selection.Body.String()
	require.Contains(t, body, "Name your workspace")
	require.Contains(t, body, `action="create-workspace"`)
	require.Equal(t, "Acme", formValue(t, body, "workspace_name"))
	csrf := formValue(t, body, "csrf_token")

	// A refused name re-renders the form and keeps the challenge usable.
	refused := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"!"}})
	require.Equal(t, http.StatusOK, refused.Code)
	require.Contains(t, refused.Body.String(), `role="alert"`)
	require.Zero(t, workspaces.provisionedCount())

	created := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"Acme Inc"}})
	require.Equal(t, http.StatusSeeOther, created.Code)
	require.Equal(t, []string{"user-1:Acme Inc"}, workspaces.provisioned)
	connectURL, err := url.Parse(created.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/platform-mcp/connect", connectURL.Path)

	connect := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(connect, httptest.NewRequest(http.MethodGet, connectURL.String(), nil))
	require.Equal(t, http.StatusOK, connect.Code)
	require.Contains(t, connect.Body.String(), "Acme Inc")

	approve := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(url.Values{"state": {state}, "csrf_token": {csrf}, "action": {"approve"}}.Encode()))
	approve.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approved := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(approved, approve)
	require.Equal(t, http.StatusSeeOther, approved.Code)
	redirect, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, redirect.Query().Get("code"))
	require.Equal(t, "client-state", redirect.Query().Get("state"))

	// The challenge is consumed, so a replayed submit creates nothing.
	replayed := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"Acme"}})
	require.Equal(t, http.StatusUnauthorized, replayed.Code)
	require.Equal(t, 1, workspaces.provisionedCount())
}

func TestOAuthHTTPCreateWorkspaceRejectsInvalidState(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)
	csrf := formValue(t, getSelection(service, state).Body.String(), "csrf_token")

	for name, form := range map[string]url.Values{
		"missing state":      {"csrf_token": {csrf}, "workspace_name": {"Acme"}},
		"unknown state":      {"state": {"unknown"}, "csrf_token": {csrf}, "workspace_name": {"Acme"}},
		"missing csrf token": {"state": {state}, "workspace_name": {"Acme"}},
		"wrong csrf token":   {"state": {state}, "csrf_token": {"forged"}, "workspace_name": {"Acme"}},
	} {
		response := postWorkspace(service, form)
		require.Equal(t, http.StatusUnauthorized, response.Code, name)
		require.Contains(t, response.Body.String(), `"invalid_request"`, name)
	}

	// A challenge that has not been through the identity provider carries no
	// user, so it cannot create a workspace either.
	anonymous := oauthChallenge{ID: "challenge-anonymous", ClientID: "client-1", RedirectURI: "http://127.0.0.1:3000/callback", CSRFToken: "csrf", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), anonymous))
	response := postWorkspace(service, url.Values{"state": {anonymous.ID}, "csrf_token": {"csrf"}, "workspace_name": {"Acme"}})
	require.Equal(t, http.StatusUnauthorized, response.Code)

	require.Zero(t, workspaces.provisionedCount())
}

func TestOAuthHTTPUserWithOrganizationNeverSeesWorkspaceForm(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{organizations: []OrganizationOption{{ID: "org-1", Name: "Organization one"}}}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)

	selection := getSelection(service, state)
	require.Equal(t, http.StatusOK, selection.Code)
	require.Contains(t, selection.Body.String(), "Choose an organization")
	require.NotContains(t, selection.Body.String(), "Name your workspace")
}

func TestOAuthHTTPUserWithoutOrganizationIsRefusedWithoutProvisioner(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.organizations = testOrganizationSelector{}
	state := startWorkspaceFlow(t, service)

	selection := getSelection(service, state)
	require.Equal(t, http.StatusForbidden, selection.Code)
	require.Contains(t, selection.Body.String(), "no eligible organizations are available")

	response := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {"csrf"}, "workspace_name": {"Acme"}})
	require.Equal(t, http.StatusNotFound, response.Code)
}
