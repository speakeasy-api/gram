package access

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeBuildRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeMCPConnect, "tool_456")

	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "workos_user_member", "custom-builder"),
	}, nil).Once()

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.Equal(t, "build:read", result.Grants[0].Scope)
	require.Equal(t, []string{"project_123"}, result.Grants[0].Resources)
	require.Equal(t, "mcp:connect", result.Grants[1].Scope)
	require.Equal(t, []string{"tool_456"}, result.Grants[1].Resources)
}

func TestService_ListGrants_MultipleRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-mcp"), ScopeMCPConnect, "tool_456")

	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "workos_user_member", "custom-builder"),
		mockMember("org_workos_test", "membership_2", "workos_user_member", "custom-mcp"),
	}, nil).Once()

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.Equal(t, "build:read", result.Grants[0].Scope)
	require.Equal(t, []string{"project_123"}, result.Grants[0].Resources)
	require.Equal(t, "mcp:connect", result.Grants[1].Scope)
	require.Equal(t, []string{"tool_456"}, result.Grants[1].Resources)
}

func TestService_ListGrants_NotConnected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "current user has not joined this organization")
}

func TestService_ListGrants_WorkOSMembersFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")

	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list members from workos")
}
