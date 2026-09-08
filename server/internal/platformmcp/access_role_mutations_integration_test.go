package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAccessRoleMutationsCommitReplayAndPreserveOtherGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_access_role_mutations")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{},
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, AfterMcpID: uuid.NullUUID{}, QueryText: "", ReadinessState: pgtype.Text{}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	targetID := rows[0].McpServerID.String()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	createAuditsBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)
	createInput := CreateMCPAccessRoleInput{
		ProjectID: project.ID.String(), Name: "MCP Operators", Description: "Uses selected MCP servers",
		Rules: []MCPAccessRoleRule{{MCPID: targetID}}, IdempotencyKey: "create-mcp-operators", Confirmed: true,
	}
	created, err := service.Create(ctx, principal, createInput)
	require.NoError(t, err)
	require.Equal(t, "MCP Operators", created.Role.Name)
	require.NotEmpty(t, created.Role.Reference)
	require.NotEmpty(t, created.Role.Version)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "pending", created.Reconciliation)

	roleID, err := reads.references.Decode(created.Role.Reference, principal, subjectKindAccessRole, reads.now())
	require.NoError(t, err)
	stored, err := manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.False(t, stored.IsSystem)
	require.Len(t, stored.Grants, 1)
	require.Equal(t, string(authz.ScopeMCPConnect), stored.Grants[0].Scope)

	createAuditsAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)
	require.Equal(t, createAuditsBefore+1, createAuditsAfter)
	receipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, Operation: operationCreateMCPAccessRole,
		IdempotencyKey: createInput.IdempotencyKey, UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	require.Equal(t, created.Receipt.ID, receipt.ID.String())

	replayed, err := service.Create(ctx, principal, createInput)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, created.Receipt.ID, replayed.Receipt.ID)
	createAuditsReplayed, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)
	require.Equal(t, createAuditsAfter, createAuditsReplayed)

	projectSelector := authz.NewSelector(authz.ScopeProjectRead, project.ID.String())
	encodedProjectSelector, err := projectSelector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+roleID),
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      encodedProjectSelector,
	})
	require.NoError(t, err)

	roles, err := reads.ListRoles(ctx, principal)
	require.NoError(t, err)
	var current AccessRole
	for _, role := range roles.Roles {
		if role.Name == "MCP Operators" {
			current = role
			break
		}
	}
	require.NotEmpty(t, current.Reference)
	require.NotEmpty(t, current.Version)

	updateAuditsBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	updated, err := service.Update(ctx, principal, UpdateMCPAccessRoleInput{
		ProjectID: project.ID.String(), RoleReference: current.Reference, ExpectedVersion: current.Version,
		AddRules:       []MCPAccessRoleRule{{MCPID: targetID, Disposition: authz.DispositionReadOnly}},
		RemoveRules:    []MCPAccessRoleRule{{MCPID: targetID}},
		IdempotencyKey: "update-mcp-operators", Confirmed: true,
	})
	require.NoError(t, err)
	require.False(t, updated.Receipt.Replayed)
	require.Equal(t, "complete", updated.Reconciliation)
	require.NotEqual(t, current.Version, updated.Role.Version)

	stored, err = manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.Len(t, stored.Grants, 2)
	var preservedProjectRead, updatedMCPRule bool
	for _, grant := range stored.Grants {
		switch grant.Scope {
		case string(authz.ScopeProjectRead):
			preservedProjectRead = true
		case string(authz.ScopeMCPConnect):
			require.Len(t, grant.Selectors, 1)
			require.NotNil(t, grant.Selectors[0].Disposition)
			require.Equal(t, authz.DispositionReadOnly, *grant.Selectors[0].Disposition)
			updatedMCPRule = true
		}
	}
	require.True(t, preservedProjectRead)
	require.True(t, updatedMCPRule)
	updateAuditsAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, updateAuditsBefore+1, updateAuditsAfter)

	_, err = service.Update(ctx, principal, UpdateMCPAccessRoleInput{
		ProjectID: project.ID.String(), RoleReference: current.Reference, ExpectedVersion: current.Version,
		AddRules: []MCPAccessRoleRule{{MCPID: targetID}}, IdempotencyKey: "stale-mcp-operators", Confirmed: true,
	})
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	updateAuditsStale, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, updateAuditsAfter, updateAuditsStale)
}
