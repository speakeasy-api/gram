package killswitchapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/killswitches"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func agentKill(agentID string, serverIDs ...string) *gen.CreatePayload {
	scope := &gen.KillswitchScope{Type: "all_servers"}
	if len(serverIDs) > 0 {
		scope = &gen.KillswitchScope{Type: "selected_servers", ServerIds: serverIDs}
	}
	return &gen.CreatePayload{OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, AgentID: &agentID, Scope: scope, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}, ExternalNote: "MCP tool calls are blocked.", InternalNote: "Investigating agent activity."}
}

func TestAgentKillswitchIsolationOverlapAndRelease(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, servers := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	repo := agentsrepo.New(db)
	agent, err := repo.CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: orgID, OwnerUserID: userID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: "Example agent"})
	require.NoError(t, err)
	sibling, err := repo.CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: orgID, OwnerUserID: userID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: "Sibling agent"})
	require.NoError(t, err)
	userKill := agentKill(agent.ID.String())
	userKill.AgentID = nil
	userKill.UserID = &userID
	_, err = service.Create(ctx, userKill)
	require.NoError(t, err)
	badges, err := service.BatchAgentBadges(ctx, &gen.BatchAgentBadgesPayload{AgentIds: []string{agent.ID.String(), sibling.ID.String()}})
	require.NoError(t, err)
	require.Len(t, badges.Badges, 2)
	for _, badge := range badges.Badges {
		require.False(t, badge.Affected)
	}
	payload := agentKill(agent.ID.String(), servers[0].String())
	created, err := service.Create(ctx, payload)
	require.NoError(t, err)
	replay, err := service.Create(ctx, payload)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, created.ID, replay.ID)
	conflicting := *payload
	siblingID := sibling.ID.String()
	conflicting.AgentID = &siblingID
	_, err = service.Create(ctx, &conflicting)
	requireServiceError(t, err, "operation_conflict")
	detail, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	userKind := gen.KillswitchPrincipalKind("user")
	_, err = service.List(ctx, &gen.ListPayload{PrincipalKind: &userKind, AgentID: payload.AgentID})
	requireOops(t, err, oops.CodeBadRequest)
	require.Nil(t, detail.UserID)
	require.Equal(t, agent.ID.String(), *detail.AgentID)
	require.Equal(t, gen.KillswitchPrincipalKind("agent"), detail.PrincipalKind)
	log, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionKillswitchActivate)
	require.NoError(t, err)
	require.Equal(t, created.ID, log.SubjectID)
	require.Equal(t, userID, log.ActorID)
	preview, err := service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{CapabilityKey: CapabilityMCPToolCalls, AgentID: payload.AgentID, Scope: payload.Scope, Schedule: payload.Schedule})
	require.NoError(t, err)
	require.Len(t, preview.Overlaps, 1)
	require.Equal(t, created.ID, preview.Overlaps[0].ID)
	other, err := service.Create(ctx, agentKill(agent.ID.String()))
	require.NoError(t, err)
	kind := gen.KillswitchPrincipalKind("agent")
	list, err := service.List(ctx, &gen.ListPayload{PrincipalKind: &kind, Limit: new(int32(1))})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.NotNil(t, list.NextCursor)
	_, err = service.List(ctx, &gen.ListPayload{Cursor: list.NextCursor})
	requireOops(t, err, oops.CodeBadRequest)
	users, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, users.Items, 1)
	require.Equal(t, userID, *users.Items[0].UserID)
	_, err = service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version + 1})
	requireServiceError(t, err, "version_conflict")
	lifted, err := service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
	require.NoError(t, err)
	require.Len(t, lifted.RemainingOverlaps, 1)
	require.Equal(t, other.ID, lifted.RemainingOverlaps[0].ID)
	badges, err = service.BatchAgentBadges(ctx, &gen.BatchAgentBadgesPayload{AgentIds: []string{agent.ID.String(), sibling.ID.String()}})
	require.NoError(t, err)
	for _, badge := range badges.Badges {
		require.Equal(t, badge.AgentID == agent.ID.String(), badge.AffectedNow)
	}
	// Releasing an agent restriction leaves its owner's user restriction intact.
	_, err = service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: other.ID, ExpectedVersion: other.Version})
	require.NoError(t, err)
	userBadges, err := service.BatchUserBadges(ctx, &gen.BatchUserBadgesPayload{UserIds: []string{userID}})
	require.NoError(t, err)
	require.True(t, userBadges.Badges[0].AffectedNow)
}

func TestAgentKillswitchSuspensionOwnerLossAndHistoricalRelease(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	repo := agentsrepo.New(db)
	agent, err := repo.CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: orgID, OwnerUserID: userID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: "Suspended agent"})
	require.NoError(t, err)
	_, err = repo.SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: orgID, ID: agent.ID})
	require.NoError(t, err)
	_, err = repo.LatchAgentsForOwnerLossByMembership(ctx, agentsrepo.LatchAgentsForOwnerLossByMembershipParams{OrganizationID: orgID, OwnerUserID: userID, OwnerReassignmentReason: pgtype.Text{String: "owner_left", Valid: true}})
	require.NoError(t, err)
	payload := agentKill(agent.ID.String())
	_, err = service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{CapabilityKey: CapabilityMCPToolCalls, AgentID: payload.AgentID, Scope: payload.Scope, Schedule: payload.Schedule})
	require.NoError(t, err)
	created, err := service.Create(ctx, payload)
	require.NoError(t, err)
	ownerBadges, err := service.BatchUserBadges(ctx, &gen.BatchUserBadgesPayload{UserIds: []string{userID}})
	require.NoError(t, err)
	require.False(t, ownerBadges.Badges[0].Affected)
	_, err = repo.ResumeAgent(ctx, agentsrepo.ResumeAgentParams{OrganizationID: orgID, ID: agent.ID})
	require.NoError(t, err)
	agentBadges, err := service.BatchAgentBadges(ctx, &gen.BatchAgentBadgesPayload{AgentIds: []string{agent.ID.String()}})
	require.NoError(t, err)
	require.True(t, agentBadges.Badges[0].AffectedNow)
	detail, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, gen.KillswitchStatus("active"), detail.Status)
	_, err = repo.RevokeAgent(ctx, agentsrepo.RevokeAgentParams{OrganizationID: orgID, ID: agent.ID})
	require.NoError(t, err)
	_, err = service.Create(ctx, agentKill(agent.ID.String()))
	requireOops(t, err, oops.CodeBadRequest)
	_, err = service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{CapabilityKey: CapabilityMCPToolCalls, AgentID: payload.AgentID, Scope: payload.Scope, Schedule: payload.Schedule})
	requireOops(t, err, oops.CodeBadRequest)
	listed, err := service.List(ctx, &gen.ListPayload{AgentID: payload.AgentID})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, created.ID, listed.Items[0].ID)
	agentKind := gen.KillswitchPrincipalKind("agent")
	orgList, err := service.List(ctx, &gen.ListPayload{PrincipalKind: &agentKind})
	require.NoError(t, err)
	require.Equal(t, created.ID, orgList.Items[0].ID)
	_, err = repo.DeleteAgent(ctx, agentsrepo.DeleteAgentParams{OrganizationID: orgID, ID: agent.ID})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{AgentID: payload.AgentID})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	orgList, err = service.List(ctx, &gen.ListPayload{PrincipalKind: &agentKind})
	require.NoError(t, err)
	require.Equal(t, created.ID, orgList.Items[0].ID)
	_, err = service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
	require.NoError(t, err)
}

func TestAgentKillswitchRejectsTenantAndAuthorizationMismatch(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	agent, err := agentsrepo.New(db).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: orgID, OwnerUserID: userID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: "Example agent"})
	require.NoError(t, err)
	// The foreign service has an independently authorized administrator and tenant.
	foreign, _, foreignOrg, foreignUser, _ := newIntegrationService(t)
	foreignCtx := customerContext(t, foreignOrg, foreignUser)
	payload := agentKill(agent.ID.String())
	_, err = foreign.Create(foreignCtx, payload)
	requireOops(t, err, oops.CodeBadRequest)
	_, err = foreign.PreviewOverlaps(foreignCtx, &gen.PreviewOverlapsPayload{CapabilityKey: CapabilityMCPToolCalls, AgentID: payload.AgentID, Scope: payload.Scope, Schedule: payload.Schedule})
	requireOops(t, err, oops.CodeBadRequest)
	foreignList, err := foreign.List(foreignCtx, &gen.ListPayload{AgentID: payload.AgentID})
	require.NoError(t, err)
	require.Empty(t, foreignList.Items)
	created, err := service.Create(ctx, payload)
	require.NoError(t, err)
	_, err = foreign.Get(foreignCtx, &gen.GetPayload{ID: created.ID})
	requireOops(t, err, oops.CodeNotFound)
	_, err = foreign.Lift(foreignCtx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
	requireOops(t, err, oops.CodeNotFound)
	denied, _, deniedOrg, deniedUser, _, _ := newIntegrationServiceWithAdmin(t, false)
	_, err = denied.Create(customerContext(t, deniedOrg, deniedUser), payload)
	requireOops(t, err, oops.CodeForbidden)
	payload.UserID = &userID
	_, err = service.Create(ctx, payload)
	requireOops(t, err, oops.CodeBadRequest)
	payload.UserID, payload.AgentID = nil, nil
	_, err = service.Create(ctx, payload)
	requireOops(t, err, oops.CodeBadRequest)
}
