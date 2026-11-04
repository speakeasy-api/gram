package metamcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	builtinrepo "github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func builtinAdmin(t *testing.T, ctx context.Context, ti *testInstance) *contextvalues.AuthContext {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NoError(t, authz.NewProvisioner(ti.conn).ProvisionOrganizationAdmin(ctx, ac.ActiveOrganizationID, authz.InitialOrganizationAdmin{UserID: ac.UserID, WorkOSUserID: "builtin-member-admin", WorkOSMembershipID: "builtin-member-membership"}))
	require.NoError(t, builtinmcp.RequireAdmin(ctx, ti.conn, ac.ActiveOrganizationID))
	return ac
}

func TestBuiltinMembershipLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ac := builtinAdmin(t, ctx, ti)
	gateway := seedMetaMcpServer(t, ctx, ti, "built-in gateway")
	env, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Slack", Slug: "slack"})
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerAddMember)
	require.NoError(t, err)
	member, err := ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack", EnvironmentID: conv.PtrEmpty(env.ID.String()), SortOrder: conv.PtrEmpty(3)})
	require.NoError(t, err)
	require.Equal(t, builtinmcp.ID(*ac.ProjectID, "slack").String(), member.McpServerID)
	require.Equal(t, "builtin_slack", *member.McpServerSlug)
	require.Equal(t, "slack", *member.BuiltinKey)
	require.Equal(t, env.ID.String(), *member.EnvironmentID)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerAddMember)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	gateways, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{})
	require.NoError(t, err)
	require.Len(t, gateways.MetaMcpServers, 1)
	require.Equal(t, 1, *gateways.MetaMcpServers[0].MemberCount)
	// The synthetic identity must not materialize an ordinary MCP server row.
	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(member.McpServerID), ProjectID: *ac.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// Visual hiding never changes an existing gateway membership.
	_, err = builtinrepo.New(ti.conn).InsertSettings(ctx, builtinrepo.InsertSettingsParams{ProjectID: conv.ToNullUUID(*ac.ProjectID), BuiltinKey: "slack", Hidden: true})
	require.NoError(t, err)
	listed, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{MetaMcpServerID: gateway.ID})
	require.NoError(t, err)
	require.Len(t, listed.Members, 1)
	reordered, err := ti.service.UpdateMetaMcpMember(ctx, &gen.UpdateMetaMcpMemberPayload{ID: member.ID, SortOrder: 5})
	require.NoError(t, err)
	require.Equal(t, 5, reordered.SortOrder)
	require.Equal(t, env.ID.String(), *reordered.EnvironmentID)
	updated, err := ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack", SortOrder: conv.PtrEmpty(8)})
	require.NoError(t, err)
	require.Equal(t, member.ID, updated.ID)
	require.Nil(t, updated.EnvironmentID)
	require.Equal(t, 8, updated.SortOrder)
	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{ID: member.ID}))
	listed, err = ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{MetaMcpServerID: gateway.ID})
	require.NoError(t, err)
	require.Empty(t, listed.Members)
}

func TestBuiltinMembershipRejectsPreparedAdminOverrides(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	// The fixture has broad prepared Admin grants but no durable admin assignment.
	gateway := seedMetaMcpServer(t, ctx, ti, "denied built-in gateway")
	_, err := ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack"})
	requireOopsCode(t, err, oops.CodeForbidden)
	ac := builtinAdmin(t, ctx, ti)
	member, err := ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack"})
	require.NoError(t, err)
	// A canonical machine actor cannot use its human owner's admin assignment.
	machineCtx := contextvalues.WithAuthenticatedActor(ctx, ac, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	_, err = ti.service.SetBuiltinMetaMcpMember(machineCtx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack"})
	requireOopsCode(t, err, oops.CodeForbidden)
	listed, err := ti.service.ListMetaMcpMembers(machineCtx, &gen.ListMetaMcpMembersPayload{MetaMcpServerID: gateway.ID})
	require.NoError(t, err)
	require.Empty(t, listed.Members)
	err = ti.service.RemoveMetaMcpMember(machineCtx, &gen.RemoveMetaMcpMemberPayload{ID: member.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
	nonAdminCtx := contextvalues.WithAuthenticatedActor(ctx, ac, urn.NewPrincipal(urn.PrincipalTypeUser, "ordinary-user"))
	listed, err = ti.service.ListMetaMcpMembers(nonAdminCtx, &gen.ListMetaMcpMembersPayload{MetaMcpServerID: gateway.ID})
	require.NoError(t, err)
	require.Empty(t, listed.Members)
	gateways, err := ti.service.ListMetaMcpServers(nonAdminCtx, &gen.ListMetaMcpServersPayload{})
	require.NoError(t, err)
	require.Len(t, gateways.MetaMcpServers, 1)
	require.Equal(t, 0, *gateways.MetaMcpServers[0].MemberCount)
	// Failed removal leaves the synthetic membership intact.
	_, err = metamcprepo.New(ti.conn).GetBuiltinMetaMCPMember(ctx, metamcprepo.GetBuiltinMetaMCPMemberParams{ID: uuid.MustParse(member.ID), ProjectID: conv.ToNullUUID(*ac.ProjectID)})
	require.NoError(t, err)
}

func TestBuiltinMembershipValidatesTargetAndEnvironment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ac := builtinAdmin(t, ctx, ti)
	gateway := seedMetaMcpServer(t, ctx, ti, "validated built-in gateway")
	_, err := ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "msteams"})
	requireOopsCode(t, err, oops.CodeInvalid)
	_, err = ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack", EnvironmentID: conv.PtrEmpty(uuid.NewString())})
	requireOopsCode(t, err, oops.CodeInvalid)
	otherProject := seedOtherProject(t, ctx, ti.conn, ac.ActiveOrganizationID)
	otherEnvironment, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: otherProject, Name: "Other Slack", Slug: "other-slack"})
	require.NoError(t, err)
	_, err = ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack", EnvironmentID: conv.PtrEmpty(otherEnvironment.ID.String())})
	requireOopsCode(t, err, oops.CodeInvalid)
	_, err = ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: uuid.NewString(), BuiltinKey: "slack"})
	requireOopsCode(t, err, oops.CodeNotFound)
	listed, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{MetaMcpServerID: gateway.ID})
	require.NoError(t, err)
	require.Empty(t, listed.Members)
}

func TestBuiltinMembershipDeletedWithGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ac := builtinAdmin(t, ctx, ti)
	gateway := seedMetaMcpServer(t, ctx, ti, "removed built-in gateway")
	member, err := ti.service.SetBuiltinMetaMcpMember(ctx, &gen.SetBuiltinMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, BuiltinKey: "slack"})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{ID: gateway.ID}))
	_, err = metamcprepo.New(ti.conn).GetBuiltinMetaMCPMember(ctx, metamcprepo.GetBuiltinMetaMCPMemberParams{ID: uuid.MustParse(member.ID), ProjectID: conv.ToNullUUID(*ac.ProjectID)})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
