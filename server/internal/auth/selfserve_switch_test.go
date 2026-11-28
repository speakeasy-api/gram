package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformswitches"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestRegister_RefusedWhileSelfServeDisabled(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = []MockOrganizationEntry{}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	_, err := platformswitches.Set(ctx, instance.conn, platformswitches.KeySelfServeDisabled, true, "operator@example.com")
	require.NoError(t, err)

	session := sessions.Session{
		SessionID:            t.Name(),
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: "",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		AccountType:          "test",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	})

	err = instance.service.Register(ctx, &gen.RegisterPayload{OrgName: "Blocked Organization", SessionToken: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.ErrorIs(t, err, auth.ErrSelfServeDisabled)

	stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
	require.NoError(t, err)
	require.Empty(t, stored.ActiveOrganizationID)
	require.Empty(t, instance.trialNotifier.trialStarted)
	orgs, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userInfo.UserID))
	require.NoError(t, err)
	require.Empty(t, orgs, "a refused registration must not persist an organization")

	_, err = platformswitches.Set(ctx, instance.conn, platformswitches.KeySelfServeDisabled, false, "operator@example.com")
	require.NoError(t, err)
	require.NoError(t, instance.service.Register(ctx, &gen.RegisterPayload{OrgName: "Allowed Organization", SessionToken: nil}))
}

func TestProvisionPlatformMCPWorkspace_RefusedWhileSelfServeDisabled(t *testing.T) {
	t.Parallel()

	userInfo := platformMCPWorkspaceUser()
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	_, err := platformswitches.Set(ctx, instance.conn, platformswitches.KeySelfServeDisabled, true, "operator@example.com")
	require.NoError(t, err)

	_, _, err = instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Blocked Workspace", "Claude Code")
	require.ErrorIs(t, err, auth.ErrSelfServeDisabled)
	require.Empty(t, instance.trialNotifier.trialStarted)
}

func TestCallback_LoginDoesNotArmTrialWhileSelfServeDisabled(t *testing.T) {
	t.Parallel()

	const (
		email          = "switch-off-signup@example.com"
		organizationID = "org-switch-off-signup"
		workosOrgID    = "org_01SWITCH_OFF_SIGNUP"
	)
	userInfo := &MockUserInfo{
		UserID:        "user_01SWITCH_OFF_SIGNUP",
		Email:         email,
		Organizations: nil,
	}
	ctx, instance := newTestAuthServiceWithWorkOSClient(t, userInfo, &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			userInfo.UserID: {{
				ID:             "om_switch_off_signup",
				UserID:         userInfo.UserID,
				OrganizationID: workosOrgID,
				Organization:   "Gated Org",
				RoleSlugs:      []string{"admin"},
			}},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: "Gated Org"},
		},
	})
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
		ID:                 organizationID,
		Name:               "Gated Org",
		Slug:               organizationID,
		WorkosID:           new(workosOrgID),
		UserWorkspaceSlugs: nil,
	}, userInfo.UserID))
	_, err := platformswitches.Set(ctx, instance.conn, platformswitches.KeySelfServeDisabled, true, "operator@example.com")
	require.NoError(t, err)

	result, err := instance.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")

	ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, organizationID, authCtx.ActiveOrganizationID)
	require.False(t, authCtx.Whitelisted)
	require.Empty(t, instance.trialNotifier.trialStarted)
}
