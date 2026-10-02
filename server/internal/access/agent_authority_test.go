package access

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func seedAuthorityAgent(t *testing.T, ctx context.Context, ti *testInstance) agentsrepo.Agent {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: ac.ActiveOrganizationID, OwnerUserID: ac.UserID, Name: "Authority " + uuid.NewString()})
	require.NoError(t, err)
	return agent
}

func seedAuthoritySession(t *testing.T, ctx context.Context, ti *testInstance, agent agentsrepo.Agent) uuid.UUID {
	t.Helper()
	issuer, err := workloadrepo.New(ti.conn).CreateWorkloadIssuer(ctx, workloadrepo.CreateWorkloadIssuerParams{
		OrganizationID: agent.OrganizationID, Name: "Authority " + uuid.NewString(), Issuer: "https://issuer.example.test/" + uuid.NewString(), JwksUri: "https://issuer.example.test/jwks", Tags: []string{},
	})
	require.NoError(t, err)
	_, err = workloadrepo.New(ti.conn).UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{OrganizationID: agent.OrganizationID, WorkloadIssuerID: issuer.ID, Subject: "build", MatchKind: "exact", AgentID: agent.ID})
	require.NoError(t, err)
	return seedAuthoritySubjectSession(t, ctx, ti, agent.OrganizationID, urn.NewWorkloadSubject(issuer.ID, "build"))
}

func seedAuthoritySubjectSession(t *testing.T, ctx context.Context, ti *testInstance, org string, subject urn.SessionSubject) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "authority-" + uuid.NewString(), AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	session, err := usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID, SubjectUrn: subject, Jti: uuid.NewString(), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
	})
	require.NoError(t, err)
	return session.ID
}

func requireAuthorityState(t *testing.T, ctx context.Context, ti *testInstance, agent agentsrepo.Agent, session uuid.UUID, revoked bool) {
	t.Helper()
	stored, err := agentsrepo.New(ti.conn).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: agent.OrganizationID, ID: agent.ID})
	require.NoError(t, err)
	require.Equal(t, agent.ID, stored.ID)
	_, err = usersessionsrepo.New(ti.conn).GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{OrganizationID: agent.OrganizationID, ID: session})
	if revoked {
		require.ErrorIs(t, err, pgx.ErrNoRows)
	} else {
		require.NoError(t, err)
	}
}

func TestAgentAuthorityResourceAudienceRoundTrip(t *testing.T) {
	t.Parallel()
	for _, viaRole := range []bool{false, true} {
		name := "direct"
		if viaRole {
			name = "role"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			agent := seedAuthorityAgent(t, ctx, ti)
			other := seedAuthorityAgent(t, ctx, ti)
			session := seedAuthoritySession(t, ctx, ti, agent)
			otherSession := seedAuthoritySession(t, ctx, ti, other)
			serverID := seedMCPServer(t, ctx, ti.conn, agent.OrganizationID)
			principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
			if viaRole {
				principal = seedInternalRole(t, ctx, ti.conn, agent.OrganizationID, "authority-builders")
				_, err := accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: agent.OrganizationID, RoleUrn: principal.String(), AgentID: agent.ID})
				require.NoError(t, err)
			}
			otherPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, other.ID.String())
			seedGrant(t, ctx, ti.conn, agent.OrganizationID, principal, authz.ScopeMCPConnect, serverID)
			seedGrant(t, ctx, ti.conn, agent.OrganizationID, otherPrincipal, authz.ScopeMCPConnect, serverID)
			_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
				ResourceKind: "mcp", ResourceID: serverID, ExpectedVersion: currentAudienceVersion(t, ctx, ti, serverID),
				Entries: []*gen.SetResourceAudienceEntry{{PrincipalUrn: otherPrincipal.String(), Level: "use"}},
			})
			require.NoError(t, err)
			requireAuthorityState(t, ctx, ti, agent, session, true)
			requireAuthorityState(t, ctx, ti, other, otherSession, false)
			replacementSession := seedAuthoritySession(t, ctx, ti, agent)
			_, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
				ResourceKind: "mcp", ResourceID: serverID, ExpectedVersion: currentAudienceVersion(t, ctx, ti, serverID),
				Entries: []*gen.SetResourceAudienceEntry{{PrincipalUrn: principal.String(), Level: "use"}, {PrincipalUrn: otherPrincipal.String(), Level: "use"}},
			})
			require.NoError(t, err)
			requireAuthorityState(t, ctx, ti, agent, session, true)
			requireAuthorityState(t, ctx, ti, agent, replacementSession, true)
			requireAuthorityState(t, ctx, ti, other, otherSession, false)
		})
	}
}

func TestAgentAuthorityRoleGrantRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	agent := seedAuthorityAgent(t, ctx, ti)
	other := seedAuthorityAgent(t, ctx, ti)
	session := seedAuthoritySession(t, ctx, ti, agent)
	otherSession := seedAuthoritySession(t, ctx, ti, other)
	roleID := seedRole(t, ctx, ti.conn, agent.OrganizationID, mockRole("role_authority", "Authority", "authority", "Authority test"))
	principal := seededRolePrincipal(t, ctx, ti.conn, agent.OrganizationID, "authority")
	_, err := accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: agent.OrganizationID, RoleUrn: principal.String(), AgentID: agent.ID})
	require.NoError(t, err)
	seedGrant(t, ctx, ti.conn, agent.OrganizationID, principal, authz.ScopeProjectRead, "project-one")
	grant := &gen.RoleGrant{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-one"}}}
	actor := RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, agent.OwnerUserID)}
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, _, err = ti.service.roleMgr.UpdateRoleTx(ctx, tx, agent.OrganizationID, "", actor, &gen.UpdateRolePayload{ID: roleID, AddGrants: []*gen.RoleGrant{grant}})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	requireAuthorityState(t, ctx, ti, agent, session, false)
	for _, payload := range []*gen.UpdateRolePayload{{ID: roleID, RemoveGrants: []*gen.RoleGrant{grant}}, {ID: roleID, AddGrants: []*gen.RoleGrant{grant}}} {
		currentSession := seedAuthoritySession(t, ctx, ti, agent)
		tx := testenv.BeginTx(t, ctx, ti.conn)
		_, _, err := ti.service.roleMgr.UpdateRoleTx(ctx, tx, agent.OrganizationID, "", actor, payload)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		requireAuthorityState(t, ctx, ti, agent, session, true)
		requireAuthorityState(t, ctx, ti, agent, currentSession, true)
		requireAuthorityState(t, ctx, ti, other, otherSession, false)
	}
}

func TestAgentAuthorityRoleMembershipRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	agent := seedAuthorityAgent(t, ctx, ti)
	retained := seedAuthorityAgent(t, ctx, ti)
	session := seedAuthoritySession(t, ctx, ti, agent)
	retainedSession := seedAuthoritySession(t, ctx, ti, retained)
	roleID := seedRole(t, ctx, ti.conn, agent.OrganizationID, mockRole("role_membership", "Membership", "membership", "Membership test"))
	principal := seededRolePrincipal(t, ctx, ti.conn, agent.OrganizationID, "membership")
	for _, id := range []uuid.UUID{agent.ID, retained.ID} {
		_, err := accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: agent.OrganizationID, RoleUrn: principal.String(), AgentID: id})
		require.NoError(t, err)
	}
	actor := RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, agent.OwnerUserID)}
	for _, ids := range [][]string{{retained.ID.String()}, {retained.ID.String(), agent.ID.String()}} {
		currentSession := seedAuthoritySession(t, ctx, ti, agent)
		tx := testenv.BeginTx(t, ctx, ti.conn)
		_, _, err := ti.service.roleMgr.UpdateRoleTx(ctx, tx, agent.OrganizationID, "", actor, &gen.UpdateRolePayload{ID: roleID, AgentIds: ids, AddGrants: []*gen.RoleGrant{}, RemoveGrants: []*gen.RoleGrant{}})
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		requireAuthorityState(t, ctx, ti, agent, session, true)
		requireAuthorityState(t, ctx, ti, agent, currentSession, true)
		requireAuthorityState(t, ctx, ti, retained, retainedSession, false)
	}
}

func TestAgentAuthorityRoleAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	agent := seedAuthorityAgent(t, ctx, ti)
	session := seedAuthoritySession(t, ctx, ti, agent)
	roleID := seedRole(t, ctx, ti.conn, agent.OrganizationID, mockRole("role_rollback", "Rollback", "rollback", "Rollback test"))
	principal := seededRolePrincipal(t, ctx, ti.conn, agent.OrganizationID, "rollback")
	_, err := accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: agent.OrganizationID, RoleUrn: principal.String(), AgentID: agent.ID})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).RejectPublishOutboxWritesFixture(ctx))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, _, err = ti.service.roleMgr.UpdateRoleTx(ctx, tx, agent.OrganizationID, "", RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, agent.OwnerUserID)}, &gen.UpdateRolePayload{ID: roleID, AgentIds: []string{}})
	require.Error(t, err)
	require.NoError(t, tx.Rollback(ctx))
	requireAuthorityState(t, ctx, ti, agent, session, false)
	roles, err := accessrepo.New(ti.conn).ListAgentRolePrincipals(ctx, accessrepo.ListAgentRolePrincipalsParams{OrganizationID: agent.OrganizationID, AgentID: agent.ID})
	require.NoError(t, err)
	require.Contains(t, roles, principal.String())
}

func TestAgentAuthorityRoleDeletion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	agent := seedAuthorityAgent(t, ctx, ti)
	session := seedAuthoritySession(t, ctx, ti, agent)
	roleID := seedRole(t, ctx, ti.conn, agent.OrganizationID, mockRole("role_deleted", "Deleted", "deleted-authority", "Deleted test"))
	principal := seededRolePrincipal(t, ctx, ti.conn, agent.OrganizationID, "deleted-authority")
	_, err := accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: agent.OrganizationID, RoleUrn: principal.String(), AgentID: agent.ID})
	require.NoError(t, err)
	ti.roles.On("DeleteRole", mock.Anything, mock.Anything, "deleted-authority").Return(nil).Once()
	require.NoError(t, ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID}))
	requireAuthorityState(t, ctx, ti, agent, session, true)
}

func TestAgentAuthorityRoleCreation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	agent := seedAuthorityAgent(t, ctx, ti)
	other := seedAuthorityAgent(t, ctx, ti)
	session := seedAuthoritySession(t, ctx, ti, agent)
	otherSession := seedAuthoritySession(t, ctx, ti, other)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, _, err := ti.service.roleMgr.CreateRoleTx(ctx, tx, agent.OrganizationID, "", RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, agent.OwnerUserID)}, &gen.CreateRolePayload{
		Name: "Authority creation", AgentIds: []string{agent.ID.String()}, Grants: []*gen.RoleGrant{{Scope: string(authz.ScopeProjectRead)}},
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	requireAuthorityState(t, ctx, ti, agent, session, true)
	requireAuthorityState(t, ctx, ti, other, otherSession, false)
}

func TestAgentAuthorityBatchPreservesWinningAssignmentSpecificity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	broad := seedAuthorityAgent(t, ctx, ti)
	narrow := seedAuthorityAgent(t, ctx, ti)
	exact := seedAuthorityAgent(t, ctx, ti)
	issuer, err := workloadrepo.New(ti.conn).CreateWorkloadIssuer(ctx, workloadrepo.CreateWorkloadIssuerParams{
		OrganizationID: broad.OrganizationID, Name: "Wildcard authority", Issuer: "https://issuer.example.test", JwksUri: "https://issuer.example.test/jwks", Tags: []string{}, AllowWildcardAdmission: true,
	})
	require.NoError(t, err)
	for _, assignment := range []struct {
		agent         uuid.UUID
		subject, kind string
	}{
		{broad.ID, "build/*", "wildcard"}, {narrow.ID, "build/team/*", "wildcard"}, {exact.ID, "build/team/exact", "exact"},
	} {
		_, err := workloadrepo.New(ti.conn).UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{
			OrganizationID: broad.OrganizationID, WorkloadIssuerID: issuer.ID, AgentID: assignment.agent, Subject: assignment.subject, MatchKind: assignment.kind,
		})
		require.NoError(t, err)
	}
	broadSession := seedAuthoritySubjectSession(t, ctx, ti, broad.OrganizationID, urn.NewWorkloadSubject(issuer.ID, "build/other"))
	narrowSession := seedAuthoritySubjectSession(t, ctx, ti, broad.OrganizationID, urn.NewWorkloadSubject(issuer.ID, "build/team/other"))
	exactSession := seedAuthoritySubjectSession(t, ctx, ti, broad.OrganizationID, urn.NewWorkloadSubject(issuer.ID, "build/team/exact"))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, invalidateAgentAuthorityTx(ctx, tx, broad.OrganizationID, nil, []uuid.UUID{narrow.ID, broad.ID, broad.ID}))
	require.NoError(t, tx.Commit(ctx))
	requireAuthorityState(t, ctx, ti, broad, broadSession, true)
	requireAuthorityState(t, ctx, ti, narrow, narrowSession, true)
	requireAuthorityState(t, ctx, ti, exact, exactSession, false)
}

func TestAgentAuthorityResourceAudienceNoop(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	agent := seedAuthorityAgent(t, ctx, ti)
	session := seedAuthoritySession(t, ctx, ti, agent)
	serverID := seedMCPServer(t, ctx, ti.conn, agent.OrganizationID)
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	seedGrant(t, ctx, ti.conn, agent.OrganizationID, principal, authz.ScopeMCPConnect, serverID)
	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp", ResourceID: serverID, ExpectedVersion: currentAudienceVersion(t, ctx, ti, serverID),
		Entries: []*gen.SetResourceAudienceEntry{{PrincipalUrn: principal.String(), Level: "use"}},
	})
	require.NoError(t, err)
	requireAuthorityState(t, ctx, ti, agent, session, false)
}

func TestAgentAuthorityRoleEmptyGrantArraysPreserveAuthority(t *testing.T) {
	t.Parallel()
	for _, edit := range []string{"rename", "description", "metadata noop", "retained membership"} {
		t.Run(edit, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			agent := seedAuthorityAgent(t, ctx, ti)
			session := seedAuthoritySession(t, ctx, ti, agent)
			roleID := seedRole(t, ctx, ti.conn, agent.OrganizationID, mockRole("role_metadata", "Metadata", "metadata", "Original description"))
			principal := seededRolePrincipal(t, ctx, ti.conn, agent.OrganizationID, "metadata")
			_, err := accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: agent.OrganizationID, RoleUrn: principal.String(), AgentID: agent.ID})
			require.NoError(t, err)
			seedGrant(t, ctx, ti.conn, agent.OrganizationID, principal, authz.ScopeProjectRead, "project-one")
			payload := &gen.UpdateRolePayload{ID: roleID, AddGrants: []*gen.RoleGrant{}, RemoveGrants: []*gen.RoleGrant{}}
			switch edit {
			case "rename":
				payload.Name = new("Renamed metadata role")
			case "description":
				payload.Description = new("Updated description")
			case "metadata noop":
				payload.Name = new("Metadata")
				payload.Description = new("Original description")
			case "retained membership":
				payload.AgentIds = []string{agent.ID.String()}
			}
			tx := testenv.BeginTx(t, ctx, ti.conn)
			updated, _, err := ti.service.roleMgr.UpdateRoleTx(ctx, tx, agent.OrganizationID, "", RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, agent.OwnerUserID)}, payload)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			if payload.Name != nil {
				require.Equal(t, *payload.Name, updated.After.Name)
			}
			if payload.Description != nil {
				require.Equal(t, *payload.Description, updated.After.Description)
			}
			require.Equal(t, updated.Before.Grants, updated.After.Grants)
			requireAuthorityState(t, ctx, ti, agent, session, false)
		})
	}
}

func TestChangedRoleAgentIDsCanonicalUUIDs(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	changed, err := changedRoleAgentIDs([]string{id}, []string{strings.ToUpper(id), id})
	require.NoError(t, err)
	require.Empty(t, changed)
}
