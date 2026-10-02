package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agentownership"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// seedLifecycleWorkloadSession creates a real assigned workload subject, rather
// than an agent subject, so lifecycle tests exercise assignment-based revocation.
func seedLifecycleWorkloadSession(t *testing.T, db *pgxpool.Pool, agentID uuid.UUID) uuid.UUID {
	t.Helper()
	issuer, err := workloadrepo.New(db).CreateWorkloadIssuer(t.Context(), workloadrepo.CreateWorkloadIssuerParams{
		OrganizationID: "org-a", Name: "Lifecycle issuer " + uuid.NewString(), Issuer: "https://issuer.example.com/" + uuid.NewString(),
		JwksUri: "https://issuer.example.com/jwks", Tags: []string{},
	})
	require.NoError(t, err)
	_, err = workloadrepo.New(db).UpsertWorkloadAgentAssignment(t.Context(), workloadrepo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID: "org-a", WorkloadIssuerID: issuer.ID, Subject: "build", MatchKind: "exact", AgentID: agentID,
	})
	require.NoError(t, err)
	session, _ := seedManagedSession(t, db, "org-a", "workload:"+issuer.ID.String()+":build")
	_, err = liveUserSession(t.Context(), db, "org-a", session)
	require.NoError(t, err, "the workload session must start live")
	return session
}

func requireLifecycleWorkloadRevoked(t *testing.T, db *pgxpool.Pool, sessionID uuid.UUID) {
	t.Helper()
	_, err := liveUserSession(t.Context(), db, "org-a", sessionID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the existing workload session must remain revoked")
}

func TestWorkloadLifecycleTransferRoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	seedOrganizationUser(t, db, "org-a", "replacement")
	agent := createAgent(t, db, "org-a", "owner", "Transfer workload")
	session := seedLifecycleWorkloadSession(t, db, agent.ID)
	other := createAgent(t, db, "org-a", "owner", "Unchanged workload")
	unaffected := seedLifecycleWorkloadSession(t, db, other.ID)
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	moved, err := service.Transfer(validatedHumanContext(t, "org-a", "owner"), &gen.TransferPayload{AgentID: agent.ID.String(), OwnerUserID: "replacement"})
	require.NoError(t, err)
	require.Equal(t, "replacement", moved.OwnerUserID)
	requireLifecycleWorkloadRevoked(t, db, session)
	replacementSession := seedLifecycleWorkloadSession(t, db, agent.ID)
	restored, err := service.Transfer(validatedHumanContext(t, "org-a", "replacement"), &gen.TransferPayload{AgentID: agent.ID.String(), OwnerUserID: "owner"})
	require.NoError(t, err)
	require.Equal(t, "owner", restored.OwnerUserID)
	requireLifecycleWorkloadRevoked(t, db, session)
	requireLifecycleWorkloadRevoked(t, db, replacementSession)
	_, err = liveUserSession(t.Context(), db, "org-a", unaffected)
	require.NoError(t, err, "another agent's workload session must survive")
}

func TestWorkloadLifecycleOwnerLossReassignment(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	seedOrganizationUser(t, db, "org-a", "admin")
	agent := createAgent(t, db, "org-a", "owner", "Reassigned workload")
	session := seedLifecycleWorkloadSession(t, db, agent.ID)
	require.NoError(t, orgrepo.New(db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: "org-a", UserID: conv.ToPGText("owner")}))
	require.NoError(t, agentownership.LatchOwnerLossByMembership(t.Context(), db, "org-a", "owner", agentownership.OwnerReassignmentReasonMembershipLost, agentownership.SystemActor, nil))
	requireLifecycleWorkloadRevoked(t, db, session)
	seedOrganizationUser(t, db, "org-a", "owner")
	requireLifecycleWorkloadRevoked(t, db, session)
	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	allow(engine, authz.ScopeAgentTransfer, agent.ID)
	service := newTestService(db, engine)
	restored, err := service.Reassign(validatedHumanContext(t, "org-a", "admin"), &gen.ReassignPayload{AgentID: agent.ID.String(), OwnerUserID: "owner"})
	require.NoError(t, err)
	require.Equal(t, "owner", restored.OwnerUserID)
	require.Nil(t, restored.OwnerReassignmentRequiredAt)
	requireLifecycleWorkloadRevoked(t, db, session)
}

func TestWorkloadLifecycleSuspendResume(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	agent := createAgent(t, db, "org-a", "owner", "Suspended workload")
	session := seedLifecycleWorkloadSession(t, db, agent.ID)
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	_, err := service.Suspend(ctx, &gen.SuspendPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	_, err = liveUserSession(t.Context(), db, "org-a", session)
	require.NoError(t, err, "temporary suspension does not retire the stored credential")
	restored, err := service.Resume(ctx, &gen.ResumePayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Equal(t, gen.AgentLifecycle("active"), restored.Lifecycle)
	_, err = liveUserSession(t.Context(), db, "org-a", session)
	require.NoError(t, err, "temporary suspension does not retire the stored credential")
}

func TestWorkloadLifecycleTerminalMutations(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"revoke", "delete"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-a")
			seedOrganizationUser(t, db, "org-a", "owner")
			agent := createAgent(t, db, "org-a", "owner", "Terminal workload")
			session := seedLifecycleWorkloadSession(t, db, agent.ID)
			service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
			ctx := validatedHumanContext(t, "org-a", "owner")
			if operation == "revoke" {
				_, err := service.Revoke(ctx, &gen.RevokePayload{AgentID: agent.ID.String()})
				require.NoError(t, err)
			} else {
				require.NoError(t, service.Delete(ctx, &gen.DeletePayload{AgentID: agent.ID.String()}))
				_, err := agentsrepo.New(db).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
			requireLifecycleWorkloadRevoked(t, db, session)
		})
	}
}

func TestWorkloadLifecyclePolicyMutations(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	agent := createAgent(t, db, "org-a", "owner", "Policy workload")
	session := seedLifecycleWorkloadSession(t, db, agent.ID)
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	selector := &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"}
	grant, err := service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: selector})
	require.NoError(t, err)
	requireLifecycleWorkloadRevoked(t, db, session)
	afterCreate := seedLifecycleWorkloadSession(t, db, agent.ID)
	_, err = service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: grant.ID, Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: selector})
	require.NoError(t, err)
	_, err = liveUserSession(t.Context(), db, "org-a", afterCreate)
	require.NoError(t, err, "a no-op policy update preserves the session")

	_, err = service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: grant.ID, Scope: string(authz.ScopeProjectWrite), Effect: "allow", Selector: selector})
	require.NoError(t, err)
	requireLifecycleWorkloadRevoked(t, db, session)
	requireLifecycleWorkloadRevoked(t, db, afterCreate)
	afterUpdate := seedLifecycleWorkloadSession(t, db, agent.ID)
	require.NoError(t, service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: grant.ID}))
	requireLifecycleWorkloadRevoked(t, db, session)
	requireLifecycleWorkloadRevoked(t, db, afterCreate)
	requireLifecycleWorkloadRevoked(t, db, afterUpdate)
}

func TestWorkloadLifecycleAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	agent := createAgent(t, db, "org-a", "owner", "Rollback workload")
	session := seedLifecycleWorkloadSession(t, db, agent.ID)
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	require.NoError(t, testrepo.New(db).RejectPublishOutboxWritesFixture(t.Context()))
	_, err := service.Suspend(validatedHumanContext(t, "org-a", "owner"), &gen.SuspendPayload{AgentID: agent.ID.String()})
	require.Error(t, err)
	stored, err := agentsrepo.New(db).GetAgentByID(t.Context(), agentsrepo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	require.False(t, stored.SuspendedAt.Valid)
	_, err = liveUserSession(t.Context(), db, "org-a", session)
	require.NoError(t, err, "session revocation must roll back with the failed lifecycle transaction")
}

func TestWorkloadLifecycleOwnerLossAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	agent := createAgent(t, db, "org-a", "owner", "Owner loss rollback workload")
	session := seedLifecycleWorkloadSession(t, db, agent.ID)
	require.NoError(t, testrepo.New(db).RejectPublishOutboxWritesFixture(t.Context()))
	err := agentownership.LatchOwnerLossByMembership(t.Context(), db, "org-a", "owner", agentownership.OwnerReassignmentReasonOwnerInactive, agentownership.SystemActor, nil)
	require.Error(t, err)
	stored, err := agentsrepo.New(db).GetAgentByID(t.Context(), agentsrepo.GetAgentByIDParams{OrganizationID: "org-a", ID: agent.ID})
	require.NoError(t, err)
	require.False(t, stored.OwnerReassignmentRequiredAt.Valid)
	_, err = liveUserSession(t.Context(), db, "org-a", session)
	require.NoError(t, err, "pool-based owner-loss must roll back session revocation with its audit")
}
