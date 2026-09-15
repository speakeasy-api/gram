package auth_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestService_StartSupportSession(t *testing.T) {
	t.Parallel()

	userInfo := adminMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	target := MockOrganizationEntry{
		ID:                 "support-session-target",
		Name:               "Support Session Target",
		Slug:               "support-session-target",
		WorkosID:           nil,
		UserWorkspaceSlugs: nil,
	}
	require.NoError(t, instance.createTestOrganization(ctx, target, ""))
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:             nil,
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  userInfo.Organizations[0].ID,
		AccountType:           "test",
		Email:                 &userInfo.Email,
		IsAdmin:               true,
		ProjectID:             nil,
		SupportOrganizationID: "",
	})

	result, err := instance.service.StartSupportSession(ctx, target.Slug)
	require.NoError(t, err)

	location, err := url.Parse(result)
	require.NoError(t, err)
	require.Equal(t, "/rpc/auth.login", location.Path)
	require.Equal(t, "/"+target.Slug, location.Query().Get("redirect"))
	token := location.Query().Get("support_handoff")
	require.NotEmpty(t, token)

	login, err := instance.service.Login(ctx, &gen.LoginPayload{SupportHandoff: &token})
	require.NoError(t, err)
	require.NotEmpty(t, login.Location)
}

func TestService_StartSupportSessionRequiresCurrentPlatformAdmin(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:             nil,
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  userInfo.Organizations[0].ID,
		AccountType:           "test",
		Email:                 &userInfo.Email,
		IsAdmin:               false,
		ProjectID:             nil,
		SupportOrganizationID: "",
	})

	result, err := instance.service.StartSupportSession(ctx, userInfo.Organizations[0].Slug)
	require.Error(t, err)
	require.Empty(t, result)
}
