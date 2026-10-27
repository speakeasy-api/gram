package platformmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

// workspaceTestState backs both the organization selector and the workspace
// provisioner, so an organization created mid-flow is eligible afterwards,
// as it is against the live database.
type workspaceTestState struct {
	mu            sync.Mutex
	organizations []OrganizationOption
	provisioned   []string
	invitations   []WorkspaceInvitation
	joined        []string
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
	// Mirrors the auth service, which refuses while an invitation is pending.
	if len(s.invitations) > 0 {
		return OrganizationOption{}, auth.ErrUserHasPendingInvitation
	}
	organization := OrganizationOption{ID: "org-new", Name: name}
	s.organizations = append(s.organizations, organization)
	s.provisioned = append(s.provisioned, userID+":"+name)
	return organization, nil
}

// testSSOOrganizationID is the WorkOS organization whose SSO covers the
// test invitations marked ssoOrganization.
const testSSOOrganizationID = "org_sso"

// PendingInvitations reports an invitation in ssoInvitations as requiring SSO
// unless the sign-in went through testSSOOrganizationID, as the organizations
// service does.
func (s *workspaceTestState) PendingInvitations(_ context.Context, verifiedEmail, ssoOrganizationID string) ([]WorkspaceInvitation, error) {
	if verifiedEmail != testVerifiedEmail {
		return nil, errors.New("unexpected verified email")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	invitations := append([]WorkspaceInvitation(nil), s.invitations...)
	for i := range invitations {
		invitations[i].RequiresSSO = invitations[i].RequiresSSO && ssoOrganizationID != testSSOOrganizationID
	}
	return invitations, nil
}

func (s *workspaceTestState) JoinInvitedWorkspace(_ context.Context, userID, verifiedEmail, ssoOrganizationID, invitationID string) (OrganizationOption, error) {
	if verifiedEmail != testVerifiedEmail {
		return OrganizationOption{}, errors.New("unexpected verified email")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, invitation := range s.invitations {
		if invitation.ID == invitationID {
			if invitation.RequiresSSO && ssoOrganizationID != testSSOOrganizationID {
				return OrganizationOption{}, ErrInvitationRequiresSSO
			}
			organization := OrganizationOption{ID: "org-invited", Name: invitation.OrganizationName}
			s.organizations = append(s.organizations, organization)
			s.invitations = append(s.invitations[:i], s.invitations[i+1:]...)
			s.joined = append(s.joined, userID+":"+invitationID)
			return organization, nil
		}
	}
	return OrganizationOption{}, errors.New("invitation is not pending")
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
	return continueWorkspaceFlow(t, service, "client-state")
}

// continueWorkspaceFlow runs another authorization for the client
// startWorkspaceFlow registered. Each needs its own client state.
func continueWorkspaceFlow(t *testing.T, service *OAuthHTTP, clientState string) string {
	t.Helper()
	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256&state="+url.QueryEscape(clientState), nil))
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

func postJoinWorkspace(service *OAuthHTTP, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/join-workspace", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.JoinWorkspaceHandler().ServeHTTP(response, request)
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
	require.Contains(t, body, "Name your organization")
	require.Contains(t, body, `action="create-workspace"`)
	require.Equal(t, "Acme", formValue(t, body, "workspace_name"))
	csrf := formValue(t, body, "csrf_token")

	// A refused name re-renders the form and keeps the challenge usable.
	refused := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"!"}})
	require.Equal(t, http.StatusOK, refused.Code)
	require.Contains(t, refused.Body.String(), `role="alert"`)
	require.Zero(t, workspaces.provisionedCount())

	empty := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"  "}})
	require.Equal(t, http.StatusOK, empty.Code)
	require.Contains(t, empty.Body.String(), "Enter an organization name.")
	require.Zero(t, workspaces.provisionedCount())

	created := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"Acme Inc"}})
	require.Equal(t, http.StatusSeeOther, created.Code)
	require.Equal(t, []string{"user-1:Acme Inc"}, workspaces.provisioned)
	connectURL, err := url.Parse(created.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/platform-mcp/connect", connectURL.Path)
	connectState := connectURL.Query().Get("state")
	require.NotEqual(t, state, connectState, "the consent step continues under a fresh state")
	// A concurrent submit that read the challenge before this one consumed
	// it goes on to consume the original state. Nothing may be stored there,
	// or it would take the challenge the consent page continues with.
	_, err = service.cache.Get(t.Context(), platformOAuthChallengePrefix+state)
	require.Error(t, err)

	connect := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(connect, httptest.NewRequest(http.MethodGet, connectURL.String(), nil))
	require.Equal(t, http.StatusOK, connect.Code)
	require.Contains(t, connect.Body.String(), "Acme Inc")

	approve := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(url.Values{"state": {connectState}, "csrf_token": {csrf}, "action": {"approve"}}.Encode()))
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

func TestOAuthHTTPInvitedUserJoinsInsteadOfCreatingWorkspace(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{invitations: []WorkspaceInvitation{{ID: "invitation-1", OrganizationName: "Team Org"}}}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)

	selection := getSelection(service, state)
	require.Equal(t, http.StatusOK, selection.Code)
	body := selection.Body.String()
	require.Contains(t, body, "Join your team")
	require.Contains(t, body, "Join Team Org")
	require.Contains(t, body, `action="join-workspace"`)
	require.NotContains(t, body, `action="create-workspace"`)
	require.Equal(t, "invitation-1", formValue(t, body, "invitation_id"))
	csrf := formValue(t, body, "csrf_token")

	// A crafted create submit is shown the invitation and creates nothing.
	// The challenge stays usable for the join.
	created := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"Acme Inc"}})
	require.Equal(t, http.StatusOK, created.Code)
	require.Contains(t, created.Body.String(), "Join Team Org")
	require.Zero(t, workspaces.provisionedCount())

	// A forged CSRF token joins nothing.
	forged := postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {"forged"}, "invitation_id": {"invitation-1"}})
	require.Equal(t, http.StatusUnauthorized, forged.Code)
	require.Empty(t, workspaces.joined)

	joined := postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "invitation_id": {"invitation-1"}})
	require.Equal(t, http.StatusSeeOther, joined.Code)
	require.Equal(t, []string{"user-1:invitation-1"}, workspaces.joined)
	connectURL, err := url.Parse(joined.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/platform-mcp/connect", connectURL.Path)
	require.NotEqual(t, state, connectURL.Query().Get("state"), "the consent step continues under a fresh state")

	connect := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(connect, httptest.NewRequest(http.MethodGet, connectURL.String(), nil))
	require.Equal(t, http.StatusOK, connect.Code)
	require.Contains(t, connect.Body.String(), "Team Org")

	// The challenge is consumed, so a replayed join does nothing.
	replayed := postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "invitation_id": {"invitation-1"}})
	require.Equal(t, http.StatusUnauthorized, replayed.Code)
	require.Len(t, workspaces.joined, 1)
}

func TestOAuthHTTPUnverifiedEmailIsNotOfferedInvitations(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.identity = unverifiedTestIdentity{}
	workspaces := &workspaceTestState{invitations: []WorkspaceInvitation{{ID: "invitation-1", OrganizationName: "Team Org"}}}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)

	// Without a verified email the invitation cannot be claimed here, so the
	// page does not offer it. Creating is refused server-side instead.
	selection := getSelection(service, state)
	require.Equal(t, http.StatusOK, selection.Code)
	require.Contains(t, selection.Body.String(), "Name your organization")
	require.NotContains(t, selection.Body.String(), "Join Team Org")
	csrf := formValue(t, selection.Body.String(), "csrf_token")

	created := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "workspace_name": {"Acme Inc"}})
	require.Equal(t, http.StatusFound, created.Code)
	redirect, err := url.Parse(created.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "access_denied", redirect.Query().Get("error"))
	require.Contains(t, redirect.Query().Get("error_description"), "pending invitation")
	require.Zero(t, workspaces.provisionedCount())

	state = continueWorkspaceFlow(t, service, "client-state-2")
	csrf = formValue(t, getSelection(service, state).Body.String(), "csrf_token")
	joined := postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "invitation_id": {"invitation-1"}})
	require.Equal(t, http.StatusFound, joined.Code)
	require.Empty(t, workspaces.joined)
}

func TestOAuthHTTPSSOInvitationNeedsSSOSignIn(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{invitations: []WorkspaceInvitation{{ID: "invitation-1", OrganizationName: "Team Org", RequiresSSO: true}}}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)

	// A user who signed in another way is told to use SSO. Neither the join
	// nor the create form is offered, so the backstop cannot trap them.
	selection := getSelection(service, state)
	require.Equal(t, http.StatusOK, selection.Code)
	body := selection.Body.String()
	require.Contains(t, body, "Sign in with SSO")
	require.Contains(t, body, "Team Org")
	require.NotContains(t, body, `action="join-workspace"`)
	require.NotContains(t, body, `action="create-workspace"`)

	challenge, err := service.cache.Get(t.Context(), platformOAuthChallengePrefix+state)
	require.NoError(t, err)
	created := postWorkspace(service, url.Values{"state": {state}, "csrf_token": {challenge.CSRFToken}, "workspace_name": {"Acme Inc"}})
	require.Equal(t, http.StatusOK, created.Code)
	require.Contains(t, created.Body.String(), "Sign in with SSO")
	require.Zero(t, workspaces.provisionedCount())

	joined := postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {challenge.CSRFToken}, "invitation_id": {"invitation-1"}})
	require.Equal(t, http.StatusFound, joined.Code)
	redirect, err := url.Parse(joined.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "access_denied", redirect.Query().Get("error"))
	require.Equal(t, ssoRequiredDescription, redirect.Query().Get("error_description"))
	require.Empty(t, workspaces.joined)

	// An SSO sign-in through another organization's connection does not count.
	service.identity = otherSSOTestIdentity{}
	state = continueWorkspaceFlow(t, service, "client-state-2")
	require.Contains(t, getSelection(service, state).Body.String(), "Sign in with SSO")

	// After an SSO sign-in through the inviting organization the same
	// invitation can be joined.
	service.identity = ssoTestIdentity{}
	state = continueWorkspaceFlow(t, service, "client-state-3")
	body = getSelection(service, state).Body.String()
	require.Contains(t, body, "Join Team Org")
	joined = postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {formValue(t, body, "csrf_token")}, "invitation_id": {"invitation-1"}})
	require.Equal(t, http.StatusSeeOther, joined.Code)
	require.Equal(t, []string{"user-1:invitation-1"}, workspaces.joined)
}

type ssoTestIdentity struct{ testIdentity }

func (ssoTestIdentity) ExchangeCodeForTokens(context.Context, string) (*identity.IDPUserInfo, error) {
	var user identity.IDPUserInfo
	user.Evidence.VerifiedEmail = testVerifiedEmail
	user.AuthenticationMethod = signins.MethodSSO
	user.OrganizationID = testSSOOrganizationID
	return &user, nil
}

type otherSSOTestIdentity struct{ testIdentity }

func (otherSSOTestIdentity) ExchangeCodeForTokens(context.Context, string) (*identity.IDPUserInfo, error) {
	var user identity.IDPUserInfo
	user.Evidence.VerifiedEmail = testVerifiedEmail
	user.AuthenticationMethod = signins.MethodSSO
	user.OrganizationID = "org_other"
	return &user, nil
}

type unverifiedTestIdentity struct{ testIdentity }

func (unverifiedTestIdentity) ExchangeCodeForTokens(context.Context, string) (*identity.IDPUserInfo, error) {
	return &identity.IDPUserInfo{}, nil
}

func TestOAuthHTTPMixedInvitationsNameTheSSOOne(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{invitations: []WorkspaceInvitation{
		{ID: "invitation-1", OrganizationName: "Open Org"},
		{ID: "invitation-2", OrganizationName: "SSO Org", RequiresSSO: true},
	}}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)

	body := getSelection(service, state).Body.String()
	require.Contains(t, body, "Join Open Org")
	require.NotContains(t, body, "Join SSO Org")
	require.Contains(t, body, `To join <span class="text-foreground">SSO Org</span>,`)
	require.Equal(t, "invitation-1", formValue(t, body, "invitation_id"))
}

func TestOAuthHTTPJoinWorkspaceRefusalReturnsToClient(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	workspaces := &workspaceTestState{invitations: []WorkspaceInvitation{{ID: "invitation-1", OrganizationName: "Team Org"}}}
	service.organizations = workspaces
	service.workspaces = workspaces
	state := startWorkspaceFlow(t, service)
	csrf := formValue(t, getSelection(service, state).Body.String(), "csrf_token")

	response := postJoinWorkspace(service, url.Values{"state": {state}, "csrf_token": {csrf}, "invitation_id": {"someone-elses-invitation"}})
	require.Equal(t, http.StatusFound, response.Code)
	redirect, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "access_denied", redirect.Query().Get("error"))
	require.Equal(t, "client-state", redirect.Query().Get("state"))
	require.Empty(t, workspaces.joined)
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
	require.NotContains(t, selection.Body.String(), "Name your organization")
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
