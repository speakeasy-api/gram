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
// registration, as attachedBy made it. A nonzero generationOffset leaves the
// attachment pinned to an earlier authorization.
func (f *fixture) attach(agentID, registration uuid.UUID, attachedBy, connectionOwner string, generationOffset int64) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedAgentAttachmentFixture(f.t.Context(), SeedAgentAttachmentFixtureParams{
		ProjectID: f.projectID, OrganizationID: f.orgID, AgentID: agentID, UserSessionIssuerID: f.userSessionIssuer(),
		RemoteSessionClientID: registration, GenerationOffset: generationOffset,
		AttachedBy: "user:" + attachedBy, ConnectionSubject: "user:" + connectionOwner,
	})
	require.NoError(f.t, err)
	return id
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

	owner, previous := f.member(), f.member()
	kept := f.agent(owner)
	f.attach(kept, registration, owner, owner, 0)
	transferred := f.agent(owner)
	transferredBinding := f.attach(transferred, registration, previous, previous, 0)
	stale := f.agent(owner)
	staleBinding := f.attach(stale, reauthorized, owner, owner, -1)
	unmapped := f.agent(owner)
	unmappedBinding := f.attach(unmapped, unrecorded, owner, owner, 0)

	dry := f.run(false)
	require.Equal(t, 1, dry.PendingGrantsByKind[GrantAttachment])
	require.ElementsMatch(t, []AttachmentRemediation{
		{BindingID: transferredBinding.String(), AgentID: transferred.String(), Reason: AttachmentOwnerChanged},
		{BindingID: staleBinding.String(), AgentID: stale.String(), Reason: AttachmentConnectionGone},
		{BindingID: unmappedBinding.String(), AgentID: unmapped.String(), Reason: AttachmentClientUnrecorded},
	}, dry.AttachmentRemediations, "an attachment the runtime would not serve is never carried over to anyone")

	f.run(true)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, kept.String())
	require.Contains(t, f.grantsOf(agent), authz.Grant{
		PrincipalUrn: agent.String(), Scope: authz.ScopeMCPAuth,
		Selector: authz.ConnectionAuthClientSelector(registration.String(), owner), Inferred: true,
	}, "the agent keeps its owner's connection, and only that one")
	for _, other := range []uuid.UUID{transferred, stale, unmapped} {
		for _, grant := range f.grantsOf(urn.NewPrincipal(urn.PrincipalTypeAgent, other.String())) {
			_, named := grant.Selector[authz.SelectorKeyUpstreamClient]
			require.False(t, named, "no connection grant without a servable attachment")
		}
	}
	require.Zero(t, f.run(false).PendingGrantsByKind[GrantAttachment])
}
