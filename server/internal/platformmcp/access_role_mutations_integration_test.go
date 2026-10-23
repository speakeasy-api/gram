package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// countingRoleBackend counts the provider reconciliations a role write sends
// after its receipt commits.
type countingRoleBackend struct {
	*access.RoleManager
	identityReconciles int
	memberReconciles   int
	deliveryReconciles int
}

func (b *countingRoleBackend) ReconcileRoleIdentity(ctx context.Context, workosOrgID, slug, name, description string, create bool) {
	b.identityReconciles++
	b.RoleManager.ReconcileRoleIdentity(ctx, workosOrgID, slug, name, description, create)
}

func (b *countingRoleBackend) ReconcileRoleDelivery(ctx context.Context, reconciliation access.RoleReconciliation) {
	b.deliveryReconciles++
	b.RoleManager.ReconcileRoleDelivery(ctx, reconciliation)
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
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
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
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	backend := &countingRoleBackend{RoleManager: manager, identityReconciles: 0, memberReconciles: 0, deliveryReconciles: 0}
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
	restore := withSpentBudget(t, &service.budget)
	spent, err := service.Create(ctx, principal, createInput)
	require.NoError(t, err, "a replay must not be refused over a spent allowance")
	require.True(t, spent.Receipt.Replayed)
	require.Equal(t, 2, backend.identityReconciles, "a replay over a spent allowance must not reconcile again")
	require.Equal(t, "rate_limited", spent.Reconciliation, "the skipped reconciliation is reported")
	restore()

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
	updateInput := UpdateMCPAccessRoleInput{
		ProjectID: project.ID.String(), RoleReference: current.Reference, ExpectedVersion: current.Version,
		AddRules:       []MCPAccessRoleRule{{MCPID: targetID, Disposition: authz.DispositionReadOnly}},
		RemoveRules:    []MCPAccessRoleRule{{MCPID: targetID}},
		IdempotencyKey: "update-mcp-operators", Confirmed: true,
	}
	updated, err := service.Update(ctx, principal, updateInput)
	require.NoError(t, err)
	require.False(t, updated.Receipt.Replayed)
	require.Equal(t, "complete", updated.Reconciliation)
	require.NotEqual(t, current.Version, updated.Role.Version)
	require.Equal(t, 1, backend.deliveryReconciles, "a committed update republishes the role plugins it changed")

	updateReplayed, err := service.Update(ctx, principal, updateInput)
	require.NoError(t, err)
	require.True(t, updateReplayed.Receipt.Replayed)
	require.Equal(t, 1, backend.deliveryReconciles, "a replayed update must not republish again")

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

func TestAccessRoleMutationsBuiltinExactConnect(t *testing.T) {
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
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	id := builtinmcp.ID(project.ID, builtinmcp.Slack)
	input := CreateMCPAccessRoleInput{ProjectID: project.ID.String(), Name: "Builtin role", Rules: []MCPAccessRoleRule{{MCPID: id.String()}}, Confirmed: true, IdempotencyKey: "builtin-create"}
	created, err := service.Create(ctx, principal, input)
	require.NoError(t, err)
	read, err := reads.GetMCPAccess(ctx, principal, GetMCPAccessInput{ProjectID: project.ID.String(), MCPID: id.String()})
	require.NoError(t, err)
	var selected MCPRoleCoverage
	for _, role := range read.Roles {
		if role.Name == input.Name {
			selected = role
		}
	}
	require.True(t, selected.CanEnterServer)
	require.False(t, selected.AssignmentEligible)
	require.Empty(t, selected.MatchingPlugins)
	require.NotEmpty(t, selected.Version)
	roleID, err := reads.references.Decode(created.Role.Reference, principal, subjectKindAccessRole, reads.now())
	require.NoError(t, err)
	stored, err := manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	grants, _ := roleAuthzGrants(stored)
	require.True(t, authz.BuiltinConnectExplicitlyGranted(grants, id.String(), project.ID.String()))

	seedAccessMember(t, ctx, conn, principal.OrganizationID, principal.UserID, "builtin-owner@example.com")
	agent, err := agentsrepo.New(conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: principal.OrganizationID, OwnerUserID: principal.UserID, Name: "Builtin role fixture"})
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: principal.OrganizationID, RoleUrn: stored.PrincipalUrn, AgentID: agent.ID})
	require.NoError(t, err)
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	machineGrants, err := runtimepolicy.LoadAgentPolicy(ctx, conn, principal.OrganizationID, agentPrincipal)
	require.NoError(t, err)
	require.True(t, authz.BuiltinConnectExplicitlyGranted(machineGrants, id.String(), project.ID.String()))
	candidates, err := pluginsrepo.New(conn).ListRoleDeliveryServers(ctx, pluginsrepo.ListRoleDeliveryServersParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	for _, candidate := range candidates {
		require.NotEqual(t, id, candidate.ResourceID, "builtins must never enter plugin reconciliation candidates")
	}
	removed, err := service.Update(ctx, principal, UpdateMCPAccessRoleInput{ProjectID: project.ID.String(), RoleReference: selected.Reference, ExpectedVersion: selected.Version, RemoveRules: input.Rules, Confirmed: true, IdempotencyKey: "builtin-remove"})
	require.NoError(t, err)
	require.NotEmpty(t, removed.Role.Version)
	stored, err = manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	grants, _ = roleAuthzGrants(stored)
	require.False(t, authz.BuiltinConnectExplicitlyGranted(grants, id.String(), project.ID.String()))
	machineGrants, err = runtimepolicy.LoadAgentPolicy(ctx, conn, principal.OrganizationID, agentPrincipal)
	require.NoError(t, err)
	require.False(t, authz.BuiltinConnectExplicitlyGranted(machineGrants, id.String(), project.ID.String()))
	input.IdempotencyKey = "builtin-narrowed"
	input.Rules[0].Disposition = "read_only"
	_, err = service.Create(ctx, principal, input)
	require.Error(t, err)
	input.Rules = []MCPAccessRoleRule{{MCPID: builtinmcp.ID(uuid.New(), builtinmcp.Slack).String()}}
	_, err = service.Create(ctx, principal, input)
	require.Error(t, err)
	foreign := principal
	foreign.OrganizationID = "unrelated-org"
	_, err = reads.GetMCPAccess(ctx, foreign, GetMCPAccessInput{ProjectID: project.ID.String(), MCPID: id.String()})
	require.ErrorIs(t, err, ErrAccessMCPNotFound)
}
