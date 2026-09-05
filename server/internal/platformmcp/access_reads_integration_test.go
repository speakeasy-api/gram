package platformmcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAccessReadServiceSuppressesSmallMemberSearchAndReturnsMaskedLargeCohort(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_access_members")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	service := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "access-read-key")

	_, err = service.ListMembers(ctx, principal, ListAccessMembersInput{Query: "o"})
	require.ErrorIs(t, err, ErrAccessQueryRequired)

	seedAccessRole(t, ctx, conn, principal.OrganizationID, "operators", "Operators")
	for i := range 5 {
		userID := fmt.Sprintf("member-%d", i)
		seedAccessMember(t, ctx, conn, principal.OrganizationID, userID, fmt.Sprintf("operator%d@example.test", i))
		_, err = accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
			OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + userID, UserID: conv.ToPGText(userID),
			WorkosMembershipID: conv.ToPGText("membership-" + userID), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
			WorkosLastEventID: conv.ToPGTextEmpty(""), WorkosRoleSlug: "operators",
		})
		require.NoError(t, err)
	}

	small, err := service.ListMembers(ctx, principal, ListAccessMembersInput{Query: "operator0"})
	require.NoError(t, err)
	require.True(t, small.Suppressed)
	require.Empty(t, small.Members)
	require.Empty(t, small.ExpiresAt)

	large, err := service.ListMembers(ctx, principal, ListAccessMembersInput{Query: "operator", Limit: 2})
	require.NoError(t, err)
	require.False(t, large.Suppressed)
	require.Equal(t, NewSubjectCount(5), large.TotalMatches)
	require.True(t, large.Truncated)
	require.Len(t, large.Members, 2)
	for _, member := range large.Members {
		require.NotContains(t, member.MaskedIdentity, "example.test")
		require.NotEmpty(t, member.Reference)
		decoded, err := service.references.Decode(member.Reference, principal, subjectKindAccessMember, service.now())
		require.NoError(t, err)
		require.NotEmpty(t, decoded)
	}

	roles, err := service.ListRoles(ctx, principal)
	require.NoError(t, err)
	var roleReference string
	for _, candidate := range roles.Roles {
		if candidate.Name == "Operators" {
			roleReference = candidate.Reference
		}
	}
	require.NotEmpty(t, roleReference)
	byRole, err := service.ListMembers(ctx, principal, ListAccessMembersInput{RoleReference: roleReference})
	require.NoError(t, err)
	require.Equal(t, NewSubjectCount(5), byRole.TotalMatches)
	require.Len(t, byRole.Members, 5)
	for _, member := range byRole.Members {
		require.Equal(t, []string{"Operators"}, member.Roles)
	}

	byRoleName, err := service.ListMembers(ctx, principal, ListAccessMembersInput{Query: strings.ToUpper("Operators")})
	require.NoError(t, err)
	require.Equal(t, NewSubjectCount(5), byRoleName.TotalMatches)
}

func TestGetMCPAccessUsesFrontingServerIDAndStoredToolMetadata(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_access_target")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "access-read-key")

	// seedRegistrationLifecycle creates a remote cohort server; select it through
	// the same tenant-qualified inventory query the service uses.
	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{},
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, AfterMcpID: uuid.NullUUID{}, QueryText: "", ReadinessState: pgtype.Text{}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	target := rows[0]

	_, err = mcpserversrepo.New(conn).AddMCPServerToolMetadata(ctx, mcpserversrepo.AddMCPServerToolMetadataParams{
		ProjectID: target.ProjectID, McpServerID: target.McpServerID,
		Tools: []byte(`[{"tool_name":"list_tasks","read_only_hint":true}]`),
	})
	require.NoError(t, err)

	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "operators", "Operators")
	selector := authz.NewSelector(authz.ScopeMCPConnect, target.McpServerID.String())
	selector[authz.SelectorKeyTool] = "list_tasks"
	selectorBytes, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: role, Scope: string(authz.ScopeMCPConnect), Selectors: selectorBytes,
	})
	require.NoError(t, err)

	output, err := service.GetMCPAccess(ctx, principal, GetMCPAccessInput{ProjectID: project.ID.String(), MCPID: target.McpServerID.String()})
	require.NoError(t, err)
	require.Equal(t, "remote", output.MCP.Backend)
	require.Equal(t, "stored_metadata", output.MCP.ToolCatalog)
	require.Equal(t, []MCPAccessTool{{Name: "list_tasks", Disposition: "read_only"}}, output.MCP.Tools)
	require.Len(t, output.Roles, 1)
	require.Equal(t, "Operators", output.Roles[0].Name)
	require.True(t, output.Roles[0].CanEnterServer)
	require.Equal(t, "all", output.Roles[0].KnownToolAccess)
	require.Equal(t, []string{"list_tasks"}, output.Roles[0].AllowedKnownTools)
}

func seedAccessMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, userID, email string) {
	t.Helper()
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: userID, Email: email, DisplayName: userID, PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	workosUserID := "workos-" + userID
	require.NoError(t, usersrepo.New(conn).OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{WorkosID: conv.ToPGText(workosUserID), ID: userID}))
	require.NoError(t, orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(userID), WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty("membership-" + userID)),
	}))
}

func seedAccessRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug, name string) urn.Principal {
	t.Helper()
	now := time.Now().UTC()
	row, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID: organizationID, WorkosSlug: slug, WorkosName: name, WorkosDescription: pgtype.Text{},
		WorkosCreatedAt: conv.ToPGTimestamptz(now), WorkosUpdatedAt: conv.ToPGTimestamptz(now), WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(row.RoleUrn)
	require.NoError(t, err)
	return principal
}
