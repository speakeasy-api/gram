package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// Dashboard first-party OAuth creates a personal upstream grant without an
// inbound human runtime session. An agent attachment alone keeps it eligible,
// but only while its exact grant, owner, tenant and configuration remain live.
func TestRefreshSweep_AttachmentOnlyLifecycle(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"detach", "session-revoked", "agent-suspended", "agent-revoked", "owner-reassignment", "owner-departed", "owner-deleted", "owner-transfer", "config-unlinked", "config-deleted", "source-deleted", "client-deleted", "issuer-deleted", "project-deleted", "opt-out", "policy-disabled"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			q := repo.New(ti.conn)
			agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Keepalive agent"})
			require.NoError(t, err)
			issuer := createRemoteIssuer(t, ctx, ti, "attachment-provider", "")
			config := createUserSessionIssuer(t, ctx, ti.conn, "attachment-runtime")
			source := config
			client := createRemoteClient(t, ctx, ti, issuer, config.String(), "attachment-client")
			session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), source.String(), client)
			binding, err := q.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, SubjectUrn: session.SubjectUrn.String(), RemoteSessionID: session.ID})
			require.NoError(t, err)
			_, err = testrepo.New(ti.conn).EnableAttachmentSourceRefreshFixture(ctx, testrepo.EnableAttachmentSourceRefreshFixtureParams{ID: session.ID, UpdatedAt: pgtype.Timestamptz{Time: time.Now().Add(-25 * time.Hour), Valid: true}})
			require.NoError(t, err)
			enableOrgAutoRefreshFeature(t, ctx, ti, auth.ActiveOrganizationID, productfeatures.FeatureRemoteSessionAutoRefresh)
			var humanSessions int64
			humanSessions, err = testrepo.New(ti.conn).CountAttachmentHumanSessionsFixture(ctx, session.SubjectUrn)
			require.NoError(t, err)
			require.Zero(t, humanSessions)
			window := newSweepWindow()
			lookup := repo.GetPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionClientID: session.RemoteSessionClientID}
			_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
			require.NoError(t, err)
			rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, session.ID, rows[0].ID)
			_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(session.ID, auth.ActiveOrganizationID))
			require.NoError(t, err)
			// Remove claim cooldown so a subsequent empty claim proves ineligibility.
			_, err = testrepo.New(ti.conn).ClearAttachmentRefreshClaimFixture(ctx, session.ID)
			require.NoError(t, err)
			switch state {
			case "detach":
				_, err = q.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, ID: binding.ID, SubjectUrn: session.SubjectUrn.String()})
			case "session-revoked":
				_, err = q.RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{ID: session.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
			case "agent-suspended":
				_, err = testrepo.New(ti.conn).SuspendAttachmentAgentFixture(ctx, testrepo.SuspendAttachmentAgentFixtureParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			case "agent-revoked":
				_, err = testrepo.New(ti.conn).RevokeAttachmentAgentFixture(ctx, testrepo.RevokeAttachmentAgentFixtureParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			case "owner-reassignment":
				_, err = testrepo.New(ti.conn).LatchAttachmentOwnerFixture(ctx, testrepo.LatchAttachmentOwnerFixtureParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			case "owner-departed":
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentMembershipFixture(ctx, testrepo.SoftDeleteAttachmentMembershipFixtureParams{UserID: pgtype.Text{String: auth.UserID, Valid: true}, OrganizationID: auth.ActiveOrganizationID})
			case "owner-deleted":
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentOwnerFixture(ctx, auth.UserID)
			case "owner-transfer":
				seedUser(t, ctx, ti.conn, "replacement-owner", "replacement@example.com", "Replacement owner")
				_, err = testrepo.New(ti.conn).AddAttachmentReplacementOwnerMembershipFixture(ctx, auth.ActiveOrganizationID)
				require.NoError(t, err)
				_, err = agentrepo.New(ti.conn).TransferAgent(ctx, agentrepo.TransferAgentParams{ID: agent.ID, OrganizationID: auth.ActiveOrganizationID, OwnerUserID: "replacement-owner"})
			case "config-unlinked":
				_, err = testrepo.New(ti.conn).UnlinkAttachmentClientFixture(ctx, testrepo.UnlinkAttachmentClientFixtureParams{RemoteSessionClientID: session.RemoteSessionClientID, UserSessionIssuerID: config})
			case "config-deleted", "source-deleted":
				id := config
				if state == "source-deleted" {
					id = source
				}
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentConfigFixture(ctx, testrepo.SoftDeleteAttachmentConfigFixtureParams{ID: id, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
			case "client-deleted":
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentClientFixture(ctx, testrepo.SoftDeleteAttachmentClientFixtureParams{ID: session.RemoteSessionClientID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
			case "issuer-deleted":
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentIssuerFixture(ctx, testrepo.SoftDeleteAttachmentIssuerFixtureParams{ID: uuid.MustParse(issuer), ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
			case "project-deleted":
				_, err = testrepo.New(ti.conn).SoftDeleteAttachmentProjectFixture(ctx, *auth.ProjectID)
			case "opt-out":
				_, err = testrepo.New(ti.conn).DisableAttachmentSourceRefreshFixture(ctx, session.ID)
			case "policy-disabled":
				_, err = testrepo.New(ti.conn).DisableAttachmentRefreshFeatureFixture(ctx, auth.ActiveOrganizationID)
			}
			require.NoError(t, err)
			_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(session.ID, auth.ActiveOrganizationID))
			require.ErrorIs(t, err, pgx.ErrNoRows)
			rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
			require.NoError(t, err)
			require.Empty(t, rows)
			if state != "opt-out" && state != "policy-disabled" {
				_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
		})
	}
}
