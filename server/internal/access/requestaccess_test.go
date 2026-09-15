package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestService_RequestAccess_NotifiesAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_admin", "ada@example.com", "Ada Admin", "user_admin", "membership_admin")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_admin", mockMember("", "membership_admin", "user_admin", "admin"))

	resourceID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	resourceName := "My MCP Server"
	projectID, err := ti.service.resourceProjectID(ctx, authCtx.ActiveOrganizationID, resourceID)
	require.NoError(t, err)
	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:        "mcp:connect",
		ResourceID:   &resourceID,
		ResourceName: &resourceName,
		Message:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.SentToCount)

	sent := ti.emailSender.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, "ada@example.com", sent[0].Email)

	link := sent[0].DataVariables["manage_access_link"]
	require.Contains(t, link, ti.siteURL.String())
	require.Contains(t, link, "/access/roles?")
	require.Contains(t, link, "grant_user="+authCtx.UserID)
	require.Contains(t, link, "scope=mcp%3Aconnect")
	require.Contains(t, link, "resource_id="+resourceID)
	require.Contains(t, link, "project_id="+projectID)
	require.NotEmpty(t, sent[0].DataVariables["requester_name"])
	require.NotEmpty(t, sent[0].DataVariables["organization_name"])
}

func TestService_RequestAccess_LegacyResourceStillNotifiesWithoutProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_admin", "ada@example.com", "Ada Admin", "user_admin", "membership_admin")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_admin", mockMember("", "membership_admin", "user_admin", "admin"))

	resourceID := "legacy-server"
	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:        "mcp:connect",
		ResourceID:   &resourceID,
		ResourceName: nil,
		Message:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.SentToCount)

	sent := ti.emailSender.Sent()
	require.Len(t, sent, 1)
	link := sent[0].DataVariables["manage_access_link"]
	require.Contains(t, link, "resource_id=legacy-server")
	require.NotContains(t, link, "project_id=")
}

func TestService_RequestAccess_NoAdminsToNotify(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:        "mcp:connect",
		ResourceID:   nil,
		ResourceName: nil,
		Message:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.SentToCount)
	require.Empty(t, ti.emailSender.Sent())
}

func TestService_RequestAccess_AllNotificationsFail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_admin", "ada@example.com", "Ada Admin", "user_admin", "membership_admin")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_admin", mockMember("", "membership_admin", "user_admin", "admin"))
	ti.emailSender.FailSends()

	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:        "mcp:connect",
		ResourceID:   nil,
		ResourceName: nil,
		Message:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.SentToCount)
	require.Len(t, ti.emailSender.Attempts(), 1)
	require.Empty(t, ti.emailSender.Sent())
}
