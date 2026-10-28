package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestOrganizationPreviousGrantSnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	config := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "snapshot-config")
	issuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "snapshot-provider")
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, issuer, "snapshot-client", config)
	q := repo.New(ti.conn)
	// Expired grants still supply the previous permissions shown during renewal.
	deadline := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	source, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(auth.UserID), UserSessionIssuerID: config, RemoteSessionClientID: client, AccessTokenEncrypted: "ciphertext", AccessExpiresAt: pgtype.Timestamptz{Time: deadline, Valid: true}, Scopes: []string{"profile", "tools:read"}})
	require.NoError(t, err)
	lookup := repo.GetOrganizationUserRemoteSessionForClientParams{OrganizationID: auth.ActiveOrganizationID, SubjectUrn: source.SubjectUrn, RemoteSessionClientID: client}
	previous, err := q.GetOrganizationUserRemoteSessionForClient(ctx, lookup)
	require.NoError(t, err)
	require.Equal(t, source.ID, previous.ID)
	require.Equal(t, source.GrantGeneration, previous.GrantGeneration)
	require.Equal(t, source.Scopes, previous.Scopes)
	require.True(t, previous.AccessExpiresAt.Time.Equal(deadline))
	otherOrg := createOrganization(t, ctx, ti.conn, "snapshot-other-org")
	_, err = testrepo.New(ti.conn).CreateAttachmentMembershipFixture(ctx, testrepo.CreateAttachmentMembershipFixtureParams{OrganizationID: otherOrg, UserID: pgtype.Text{String: auth.UserID, Valid: true}})
	require.NoError(t, err)
	lookup.OrganizationID = otherOrg
	_, err = q.GetOrganizationUserRemoteSessionForClient(ctx, lookup)
	require.ErrorIs(t, err, pgx.ErrNoRows, "membership in both organizations must not expose another organization's grant")
	lookup.OrganizationID = auth.ActiveOrganizationID
	lookup.SubjectUrn = urn.NewUserSubject("different-user")
	_, err = q.GetOrganizationUserRemoteSessionForClient(ctx, lookup)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestListAgentConnectionRenewalCandidates(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"superseded", "deleted source", "other project", "other organization", "other subject", "other issuer", "other client", "detached", "suspended", "revoked", "owner reassignment", "owner departed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			q := repo.New(ti.conn)
			agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, Name: "Renewal example"})
			require.NoError(t, err)
			issuer := createRemoteIssuer(t, ctx, ti, "renewal-provider", "")
			config := createUserSessionIssuer(t, ctx, ti.conn, "renewal-config")
			client := createRemoteClient(t, ctx, ti, issuer, config.String(), "renewal-client")
			source := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), config.String(), client)
			binding, err := q.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionID: source.ID, SubjectUrn: source.SubjectUrn.String()})
			require.NoError(t, err)
			lookup := repo.ListAgentConnectionRenewalsParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: config, RemoteSessionClientID: source.RemoteSessionClientID, SubjectUrn: source.SubjectUrn}
			initial, err := q.ListAgentConnectionRenewals(ctx, lookup)
			require.NoError(t, err)
			require.Len(t, initial, 1)
			switch scenario {
			case "superseded":
				newer := insertRemoteSession(t, ctx, ti.conn, source.SubjectUrn, config.String(), client)
				require.Greater(t, newer.GrantGeneration, binding.GrantGeneration)
			case "deleted source":
				_, err = q.SoftDeleteRemoteSessionsByClientID(ctx, source.RemoteSessionClientID)
			case "other project":
				lookup.ProjectID = uuid.New()
			case "other organization":
				lookup.OrganizationID = "another-organization"
			case "other subject":
				lookup.SubjectUrn = urn.NewUserSubject("another-user")
			case "other issuer":
				lookup.UserSessionIssuerID = uuid.New()
			case "other client":
				lookup.RemoteSessionClientID = uuid.New()
			case "detached":
				_, err = q.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, ID: binding.ID})
			case "suspended":
				_, err = testrepo.New(ti.conn).SuspendAttachmentAgentFixture(ctx, testrepo.SuspendAttachmentAgentFixtureParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			case "revoked":
				_, err = testrepo.New(ti.conn).RevokeAttachmentAgentFixture(ctx, testrepo.RevokeAttachmentAgentFixtureParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			case "owner reassignment":
				_, err = testrepo.New(ti.conn).LatchAttachmentOwnerFixture(ctx, testrepo.LatchAttachmentOwnerFixtureParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			case "owner departed":
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentMembershipFixture(ctx, testrepo.SoftDeleteAttachmentMembershipFixtureParams{UserID: pgtype.Text{String: auth.UserID, Valid: true}, OrganizationID: auth.ActiveOrganizationID})
			}
			require.NoError(t, err)
			rows, err := q.ListAgentConnectionRenewals(ctx, lookup)
			require.NoError(t, err)
			if scenario == "superseded" || scenario == "deleted source" {
				require.Len(t, rows, 1, "old own assignments remain available for explicit renewal")
				require.Equal(t, binding.ID, rows[0].PrincipalRemoteSessionBinding.ID)
				require.Equal(t, agent.Name, rows[0].AgentName)
			} else {
				require.Empty(t, rows)
			}
		})
	}
}
