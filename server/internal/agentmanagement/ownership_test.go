package agentmanagement

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestTransferAtomicallyReplacesOwnerAndPreservesDirectPolicy(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "replacement")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	created, err := service.Create(validatedHumanContext(t, "org-a", "owner"), &gen.CreatePayload{Name: "Transfer agent"})
	require.NoError(t, err)

	grant, err := service.CreatePolicyGrant(validatedHumanContext(t, "org-a", "owner"), &gen.CreatePolicyGrantPayload{
		AgentID: created.ID, Scope: string(authz.ScopeProjectRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"},
	})
	require.NoError(t, err)

	transferred, err := service.Transfer(validatedHumanContext(t, "org-a", "owner"), &gen.TransferPayload{
		AgentID: created.ID, OwnerUserID: "replacement",
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, transferred.ID)
	require.Equal(t, "replacement", transferred.OwnerUserID)
	require.Nil(t, transferred.OwnerReassignmentRequiredAt)
	require.Nil(t, transferred.OwnerReassignmentReason)

	grants, err := service.ListPolicyGrants(validatedHumanContext(t, "org-a", "replacement"), &gen.ListPolicyGrantsPayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Equal(t, []*gen.AgentPolicyGrant{grant}, grants)

	_, err = service.ListPolicyGrants(validatedHumanContext(t, "org-a", "owner"), &gen.ListPolicyGrantsPayload{AgentID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = service.Get(validatedHumanContext(t, "org-a", "owner"), &gen.GetPayload{ID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = service.Get(validatedHumanContext(t, "org-a", "replacement"), &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)

	var action, actorID, beforeOwner, afterOwner string
	//nolint:glint // notestingrawsql: verifies transactional ownership audit fields
	err = conn.QueryRow(t.Context(), `
		SELECT action, actor_id, before_snapshot->>'owner_user_id', after_snapshot->>'owner_user_id'
		FROM audit_logs WHERE organization_id = $1 AND subject_id = $2 AND action = 'agent:transfer'`,
		"org-a", created.ID).Scan(&action, &actorID, &beforeOwner, &afterOwner)
	require.NoError(t, err)
	require.Equal(t, "agent:transfer", action)
	require.Equal(t, "owner", actorID)
	require.Equal(t, "owner", beforeOwner)
	require.Equal(t, "replacement", afterOwner)
	require.Equal(t, []string{"agent:create", "agent:transfer"}, m1OutboxActions(t, conn, "org-a"))
}

func TestExplicitReassignmentIsTheOnlyOwnershipOperationThatClearsLatch(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "former-owner")
	seedOrganizationUser(t, conn, "org-a", "admin")
	seedOrganizationUser(t, conn, "org-a", "replacement")
	agent := createAgent(t, conn, "org-a", "former-owner", "Reassignment agent")
	require.NoError(t, agentownership.LatchOwnerLossByMembership(t.Context(), conn, "org-a", "former-owner", agentownership.OwnerReassignmentReasonOwnerInactive, agentownership.SystemActor, nil))

	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	allow(engine, authz.ScopeAgentTransfer, agent.ID)
	service := newTestService(conn, engine)
	adminCtx := validatedHumanContext(t, "org-a", "admin")

	_, err := service.Transfer(adminCtx, &gen.TransferPayload{AgentID: agent.ID.String(), OwnerUserID: "replacement"})
	requireOopsCode(t, err, oops.CodeConflict)
	latched, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	require.True(t, latched.OwnerReassignmentRequiredAt.Valid)

	reassigned, err := service.Reassign(adminCtx, &gen.ReassignPayload{AgentID: agent.ID.String(), OwnerUserID: "replacement"})
	require.NoError(t, err)
	require.Equal(t, "replacement", reassigned.OwnerUserID)
	require.Nil(t, reassigned.OwnerReassignmentRequiredAt)
	require.Nil(t, reassigned.OwnerReassignmentReason)

	_, err = service.Reassign(adminCtx, &gen.ReassignPayload{AgentID: agent.ID.String(), OwnerUserID: "former-owner"})
	requireOopsCode(t, err, oops.CodeConflict)

	var actions []string
	rows, err := conn.Query(t.Context(), `SELECT action FROM audit_logs WHERE subject_id = $1 ORDER BY seq`, agent.ID) //nolint:glint // notestingrawsql: verifies owner-loss and reassignment event order
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var action string
		require.NoError(t, rows.Scan(&action))
		actions = append(actions, action)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"agent:owner_loss", "agent:reassign"}, actions)
	require.Equal(t, actions, m1OutboxActions(t, conn, "org-a"))
}

func TestOwnerChangeRejectsIneligibleAndCrossTenantTargets(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganization(t, conn, "org-b")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "inactive")
	seedOrganizationUser(t, conn, "org-b", "other-tenant")
	agent := createAgent(t, conn, "org-a", "owner", "Target validation agent")
	_, err := conn.Exec(t.Context(), `UPDATE users SET deleted_at = clock_timestamp() WHERE id = 'inactive'`) //nolint:glint // notestingrawsql: creates an ineligible target
	require.NoError(t, err)

	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	for _, target := range []string{"missing", "inactive", "other-tenant"} {
		_, err := service.Transfer(ctx, &gen.TransferPayload{AgentID: agent.ID.String(), OwnerUserID: target})
		requireOopsCode(t, err, oops.CodeForbidden)
	}

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	require.Equal(t, "owner", stored.OwnerUserID)
}

func TestConcurrentOwnerLossAndTransferLeaveOneSafeOwnerState(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "replacement")
	agent := createAgent(t, conn, "org-a", "owner", "Concurrent ownership agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})

	ctx := t.Context()
	ownerCtx := validatedHumanContext(t, "org-a", "owner")
	start := make(chan struct{})
	transferResult := make(chan error, 1)
	lossResult := make(chan error, 1)
	go func() {
		<-start
		_, err := service.Transfer(ownerCtx, &gen.TransferPayload{AgentID: agent.ID.String(), OwnerUserID: "replacement"})
		transferResult <- err
	}()
	go func() {
		<-start
		lossResult <- pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if err := orgrepo.New(tx).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
				OrganizationID: "org-a", UserID: conv.ToPGText("owner"),
			}); err != nil {
				return fmt.Errorf("delete owner membership: %w", err)
			}
			return agentownership.LatchOwnerLossByMembership(ctx, tx, "org-a", "owner", agentownership.OwnerReassignmentReasonMembershipLost, agentownership.SystemActor, nil)
		})
	}()
	close(start)

	require.NoError(t, <-lossResult)
	transferErr := <-transferResult
	if transferErr != nil {
		var shareable *oops.ShareableError
		require.ErrorAs(t, transferErr, &shareable)
		require.Contains(t, []oops.Code{oops.CodeForbidden, oops.CodeConflict}, shareable.Code)
	}

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	switch stored.OwnerUserID {
	case "owner":
		require.True(t, stored.OwnerReassignmentRequiredAt.Valid)
	case "replacement":
		require.False(t, stored.OwnerReassignmentRequiredAt.Valid)
	default:
		require.Fail(t, "unexpected owner", stored.OwnerUserID)
	}
}

func TestReassignmentCanRestoreFormerOwnerOnlyThroughExplicitOperation(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "admin")
	agent := createAgent(t, conn, "org-a", "owner", "Restored owner agent")
	require.NoError(t, agentownership.LatchOwnerLossByMembership(t.Context(), conn, "org-a", "owner", agentownership.OwnerReassignmentReasonOwnerInactive, agentownership.SystemActor, nil))

	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	allow(engine, authz.ScopeAgentTransfer, agent.ID)
	service := newTestService(conn, engine)
	reassigned, err := service.Reassign(validatedHumanContext(t, "org-a", "admin"), &gen.ReassignPayload{AgentID: agent.ID.String(), OwnerUserID: "owner"})
	require.NoError(t, err)
	require.Equal(t, "owner", reassigned.OwnerUserID)
	require.Nil(t, reassigned.OwnerReassignmentRequiredAt)
}

func TestLatchedRestoredOwnerCannotManagePolicyButExactWriteAdministratorCan(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "admin")
	agent := createAgent(t, conn, "org-a", "owner", "Latched policy agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ownerCtx := validatedHumanContext(t, "org-a", "owner")
	createPayload := &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"},
	}
	grant, err := service.CreatePolicyGrant(ownerCtx, createPayload)
	require.NoError(t, err)

	require.NoError(t, orgrepo.New(conn).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: "org-a", UserID: conv.ToPGText("owner"),
	}))
	require.NoError(t, agentownership.LatchOwnerLossByMembership(t.Context(), conn, "org-a", "owner", agentownership.OwnerReassignmentReasonMembershipLost, agentownership.SystemActor, nil))
	seedOrganizationUser(t, conn, "org-a", "owner")
	latched, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	require.True(t, latched.OwnerReassignmentRequiredAt.Valid)

	listPayload := &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()}
	updatePayload := &gen.UpdatePolicyGrantPayload{
		AgentID: agent.ID.String(), GrantID: grant.ID, Scope: string(authz.ScopeProjectWrite), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"},
	}
	deletePayload := &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: grant.ID}
	_, err = service.CreatePolicyGrant(ownerCtx, createPayload)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = service.ListPolicyGrants(ownerCtx, listPayload)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = service.UpdatePolicyGrant(ownerCtx, updatePayload)
	requireOopsCode(t, err, oops.CodeForbidden)
	requireOopsCode(t, service.DeletePolicyGrant(ownerCtx, deletePayload), oops.CodeForbidden)

	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	allow(engine, authz.ScopeAgentWrite, agent.ID)
	adminService := newTestService(conn, engine)
	adminCtx := validatedHumanContext(t, "org-a", "admin")
	grants, err := adminService.ListPolicyGrants(adminCtx, listPayload)
	require.NoError(t, err)
	require.Equal(t, []*gen.AgentPolicyGrant{grant}, grants)
	updated, err := adminService.UpdatePolicyGrant(adminCtx, updatePayload)
	require.NoError(t, err)
	require.Equal(t, grant.ID, updated.ID)
	require.Equal(t, string(authz.ScopeProjectWrite), updated.Scope)
	require.NoError(t, adminService.DeletePolicyGrant(adminCtx, deletePayload))
	created, err := adminService.CreatePolicyGrant(adminCtx, createPayload)
	require.NoError(t, err)
	grants, err = adminService.ListPolicyGrants(adminCtx, listPayload)
	require.NoError(t, err)
	require.Equal(t, []*gen.AgentPolicyGrant{created}, grants)

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	require.Equal(t, latched.OwnerUserID, stored.OwnerUserID)
	require.Equal(t, latched.OwnerReassignmentRequiredAt, stored.OwnerReassignmentRequiredAt)
	require.Equal(t, latched.OwnerReassignmentReason, stored.OwnerReassignmentReason)
}
