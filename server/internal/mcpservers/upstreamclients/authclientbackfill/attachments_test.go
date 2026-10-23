package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// member adds a user to the fixture's organization.
func (f *fixture) member() string {
	f.t.Helper()
	id := "user_" + uuid.NewString()
	_, err := usersrepo.New(f.pool).UpsertUser(f.t.Context(), usersrepo.UpsertUserParams{ID: id, Email: id + "@example.com", DisplayName: id, PhotoUrl: pgtype.Text{String: "", Valid: false}, Admin: false})
	require.NoError(f.t, err)
	_, err = orgrepo.New(f.pool).UpsertOrganizationUserRelationship(f.t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: f.orgID, UserID: pgtype.Text{String: id, Valid: true}})
	require.NoError(f.t, err)
	return id
}

func (f *fixture) agent(ownerUserID string) uuid.UUID {
	f.t.Helper()
	agent, err := agentsrepo.New(f.pool).CreateAgent(f.t.Context(), agentsrepo.CreateAgentParams{OrganizationID: f.orgID, OwnerUserID: ownerUserID, Name: "a-" + uuid.NewString()[:8]})
	require.NoError(f.t, err)
	return agent.ID
}

// attach binds agentID to connectionOwner's new connection through
// registration, as the session subject attachedBy made it. A nonzero
// generationOffset leaves the attachment pinned to an earlier authorization.
func (f *fixture) attach(agentID, registration uuid.UUID, attachedBy, connectionOwner string, generationOffset int64) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedAgentAttachmentFixture(f.t.Context(), SeedAgentAttachmentFixtureParams{
		ProjectID: f.projectID, OrganizationID: f.orgID, AgentID: agentID, UserSessionIssuerID: f.userSessionIssuer(),
		RemoteSessionClientID: registration, GenerationOffset: generationOffset,
		AttachedBy: attachedBy, ConnectionSubject: "user:" + connectionOwner,
	})
	require.NoError(f.t, err)
	return id
}

// requireNoConnectionGrant fails if the agent holds any grant beyond the
// personal default every agent gets.
func (f *fixture) requireNoConnectionGrant(agentID uuid.UUID) {
	f.t.Helper()
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String())
	for _, grant := range f.grantsOf(principal) {
		require.Equal(f.t, authz.ScopeMCPAuth, grant.Scope, "no grant without a servable attachment")
		require.Equal(f.t, authz.PersonalAuthClientSelector(), grant.Selector, "no grant without a servable attachment")
	}
}

func TestRun_AttachmentsBecomeGrantsOnTheOwnersConnection(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	registration, reauthorized := f.client("subject"), f.client("subject")
	server := f.server()
	f.associate(server, registration)
	f.associate(server, reauthorized)
	unrecorded := f.client("subject")
	f.run(true)

	owner, previous, stranger := f.member(), f.member(), f.member()
	kept := f.agent(owner)
	f.attach(kept, registration, "user:"+owner, owner, 0)
	transferred := f.agent(owner)
	transferredBinding := f.attach(transferred, registration, "user:"+previous, previous, 0)
	foreign := f.agent(owner)
	foreignBinding := f.attach(foreign, registration, "user:"+owner, stranger, 0)
	byAgent := f.agent(owner)
	byAgentBinding := f.attach(byAgent, registration, urn.NewAgentSubject(byAgent).String(), owner, 0)
	stale := f.agent(owner)
	staleBinding := f.attach(stale, reauthorized, "user:"+owner, owner, -1)
	unmapped := f.agent(owner)
	unmappedBinding := f.attach(unmapped, unrecorded, "user:"+owner, owner, 0)

	dry := f.run(false)
	require.Equal(t, 1, dry.PendingGrantsByKind[GrantAttachment])
	require.ElementsMatch(t, []AttachmentRemediation{
		{BindingID: transferredBinding.String(), AgentID: transferred.String(), Reason: AttachmentOwnerChanged},
		{BindingID: foreignBinding.String(), AgentID: foreign.String(), Reason: AttachmentForeignConnection},
		{BindingID: byAgentBinding.String(), AgentID: byAgent.String(), Reason: AttachmentNotByUser},
		{BindingID: staleBinding.String(), AgentID: stale.String(), Reason: AttachmentConnectionGone},
		{BindingID: unmappedBinding.String(), AgentID: unmapped.String(), Reason: AttachmentClientUnrecorded},
	}, dry.AttachmentRemediations, "an attachment the runtime would not serve is never carried over to anyone")

	f.run(true)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, kept.String())
	require.Contains(t, f.grantsOf(agent), authz.Grant{
		PrincipalUrn: agent.String(), Scope: authz.ScopeMCPAuth,
		Selector: authz.ConnectionAuthClientSelector(registration.String(), owner), Inferred: true,
	}, "the agent keeps its owner's connection, and only that one")
	for _, other := range []uuid.UUID{transferred, foreign, byAgent, stale, unmapped} {
		f.requireNoConnectionGrant(other)
	}
	require.Zero(t, f.run(false).PendingGrantsByKind[GrantAttachment])
}

func TestRun_AttachmentsTheRuntimeNoLongerServesAreRemediated(t *testing.T) {
	t.Parallel()
	pool, err := testInfra.CloneTestDatabase(t, "authclientbackfill")
	require.NoError(t, err)

	cases := []struct {
		name    string
		reason  string
		breakIt func(f *fixture, agentID, binding, registration uuid.UUID, owner string)
	}{
		{"suspended agent", AttachmentAgentInactive, func(f *fixture, agentID, _, _ uuid.UUID, _ string) {
			_, err := agentsrepo.New(f.pool).SuspendAgent(f.t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.orgID, ID: agentID})
			require.NoError(f.t, err)
		}},
		{"revoked agent", AttachmentAgentInactive, func(f *fixture, agentID, _, _ uuid.UUID, _ string) {
			_, err := agentsrepo.New(f.pool).RevokeAgent(f.t.Context(), agentsrepo.RevokeAgentParams{OrganizationID: f.orgID, ID: agentID})
			require.NoError(f.t, err)
		}},
		{"agent waiting for a new owner", AttachmentAgentInactive, func(f *fixture, _, _, _ uuid.UUID, owner string) {
			_, err := agentsrepo.New(f.pool).LatchAgentsForOwnerLossByMembership(f.t.Context(), agentsrepo.LatchAgentsForOwnerLossByMembershipParams{
				OrganizationID: f.orgID, OwnerUserID: owner, OwnerReassignmentReason: pgtype.Text{String: "membership_removed", Valid: true},
			})
			require.NoError(f.t, err)
		}},
		{"deleted sign-in", AttachmentConfigurationGone, func(f *fixture, _, binding, _ uuid.UUID, _ string) {
			require.NoError(f.t, f.q.SoftDeleteAttachmentUserSessionIssuerFixture(f.t.Context(), binding))
		}},
		{"deleted project", AttachmentConfigurationGone, func(f *fixture, _, _, _ uuid.UUID, _ string) {
			require.NoError(f.t, f.q.SoftDeleteProjectFixture(f.t.Context(), f.projectID))
		}},
		{"deleted upstream issuer", AttachmentConfigurationGone, func(f *fixture, _, _, registration uuid.UUID, _ string) {
			require.NoError(f.t, f.q.SoftDeleteIssuerOfClientFixture(f.t.Context(), registration))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixtureIn(t, pool)
			registration := f.client("subject")
			f.associate(f.server(), registration)
			f.run(true)
			owner := f.member()
			agentID := f.agent(owner)
			binding := f.attach(agentID, registration, "user:"+owner, owner, 0)
			require.Empty(t, f.run(false).AttachmentRemediations, "the attachment is servable before it breaks")

			tc.breakIt(f, agentID, binding, registration, owner)
			require.Equal(t, []AttachmentRemediation{{BindingID: binding.String(), AgentID: agentID.String(), Reason: tc.reason}}, f.run(true).AttachmentRemediations)
			f.requireNoConnectionGrant(agentID)
		})
	}
}
