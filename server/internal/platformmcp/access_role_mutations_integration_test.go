package platformmcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type roleAdmissionOutsideTransactionFlags struct {
	feature.Provider
	t         *testing.T
	db        *pgxpool.Pool
	evaluated bool
}

func (f *roleAdmissionOutsideTransactionFlags) EvaluateFlag(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	f.t.Helper()
	f.evaluated = true
	require.Zero(f.t, f.db.Stat().AcquiredConns(), "role admission must resolve before opening the mutation transaction")
	evaluation, err := feature.EvaluateFlag(ctx, f.Provider, flag, distinctID, groups)
	if err != nil {
		return evaluation, fmt.Errorf("evaluate role admission flag: %w", err)
	}
	return evaluation, nil
}

// countingRoleBackend counts the provider reconciliations a role write sends
// after its receipt commits.
type countingRoleBackend struct {
	*access.RoleManager
	identityReconciles int
	memberReconciles   int
}

func (b *countingRoleBackend) ReconcileRoleIdentity(ctx context.Context, workosOrgID, slug, name, description string, create bool) {
	b.identityReconciles++
	b.RoleManager.ReconcileRoleIdentity(ctx, workosOrgID, slug, name, description, create)
}

func (b *countingRoleBackend) ReconcileMemberRoles(ctx context.Context, reconciliation access.MemberRoleReconciliation) {
	b.memberReconciles++
	b.RoleManager.ReconcileMemberRoles(ctx, reconciliation)
}

func TestAccessRoleMutationsCreateWithoutGrantsAndReplay(t *testing.T) {
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

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	admissionFlags := &roleAdmissionOutsideTransactionFlags{Provider: flags, t: t, db: conn, evaluated: false}
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard(admissionFlags, nil))
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	input := CreateMCPAccessRoleInput{
		ProjectID: project.ID.String(), Name: "Grantless role", Rules: []MCPAccessRoleRule{},
		IdempotencyKey: "create-grantless-role", Confirmed: true,
	}
	created, err := service.Create(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, "Grantless role", created.Role.Name)
	require.NotEmpty(t, created.Role.Reference)
	require.NotEmpty(t, created.Role.Version)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "pending", created.Reconciliation)

	roleID, err := reads.references.Decode(created.Role.Reference, principal, subjectKindAccessRole, reads.now())
	require.NoError(t, err)
	stored, err := manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.False(t, stored.IsSystem)
	require.Empty(t, stored.Grants)

	replayed, err := service.Create(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, created.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, created.Role.Version, replayed.Role.Version)
	require.Equal(t, created.Role.MCPAccess, replayed.Role.MCPAccess)
	replayedRoleID, err := reads.references.Decode(replayed.Role.Reference, principal, subjectKindAccessRole, reads.now())
	require.NoError(t, err)
	require.Equal(t, roleID, replayedRoleID)
}

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
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{}, SkipAuthorizationFilter: true,
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
	admissionFlags := &roleAdmissionOutsideTransactionFlags{Provider: flags, t: t, db: conn, evaluated: false}
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard(admissionFlags, nil))
	backend := &countingRoleBackend{RoleManager: manager, identityReconciles: 0, memberReconciles: 0}
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", backend)
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
	require.Equal(t, 2, backend.identityReconciles, "a replay within the allowance re-runs reconciliation, so a failed sync recovers")

	// The replay itself stays free once the allowance is spent, but its
	// provider reconciliation is charged, so a retry loop cannot send
	// unbounded provider work.
	budget := service.budget
	service.budget = OperationBudget{Connection: denyOperationLimiter{}, Organization: denyOperationLimiter{}}
	spent, err := service.Create(ctx, principal, createInput)
	require.NoError(t, err, "a replay must not be refused over a spent allowance")
	require.True(t, spent.Receipt.Replayed)
	require.Equal(t, 2, backend.identityReconciles, "a replay over a spent allowance must not reconcile again")
	service.budget = budget

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
	require.True(t, admissionFlags.evaluated, "role update must exercise admission preparation")
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
