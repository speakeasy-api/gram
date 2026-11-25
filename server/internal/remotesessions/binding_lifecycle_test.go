package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// Dashboard first-party OAuth creates a personal upstream grant without an
// inbound human runtime session. An agent attachment alone keeps it eligible,
// but only while its exact grant, owner, tenant and configuration remain live.
func TestRemoteSessionSweeps_AttachmentReachParity(t *testing.T) {
	t.Parallel()
	states := []string{"detach", "session-revoked", "agent-suspended", "agent-revoked", "owner-reassignment", "owner-departed", "owner-deleted", "owner-transfer", "config-deleted", "source-deleted", "client-deleted", "registration-retired", "issuer-deleted", "project-deleted", "opt-out", "policy-disabled"}
	for _, state := range states {
		t.Run("refresh/"+state, func(t *testing.T) {
			t.Parallel()
			checkAttachmentReachLifecycle(t, state, false)
		})
		// Refresh policy is intentionally not part of recheck reach.
		if state != "opt-out" && state != "policy-disabled" {
			t.Run("recheck/"+state, func(t *testing.T) {
				t.Parallel()
				checkAttachmentReachLifecycle(t, state, true)
			})
		}
	}
}

func checkAttachmentReachLifecycle(t *testing.T, state string, checkRecheck bool) {
	t.Helper()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Keepalive agent"})
	require.NoError(t, err)
	issuer := createRemoteIssuer(t, ctx, ti, "attachment-provider", "")
	config := createUserSessionIssuer(t, ctx, ti.conn, "attachment-runtime")
	source := createUserSessionIssuer(t, ctx, ti.conn, "attachment-source")
	client := createRemoteClient(t, ctx, ti, issuer, config.String(), "attachment-client")
	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), source.String(), client)
	binding, err := q.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionID: session.ID, SubjectUrn: session.SubjectUrn.String()})
	require.NoError(t, err)
	fixtures := testrepo.New(ti.conn)
	org := auth.ActiveOrganizationID
	project := *auth.ProjectID
	if !checkRecheck {
		_, err = fixtures.EnableAttachmentSourceRefreshFixture(ctx, testrepo.EnableAttachmentSourceRefreshFixtureParams{ID: session.ID, UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour))})
		require.NoError(t, err)
		enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	} else {
		require.NoError(t, fixtures.SetRemoteSessionValidationTrackingFixture(ctx, testrepo.SetRemoteSessionValidationTrackingFixtureParams{
			ID: session.ID, ProjectID: conv.ToNullUUID(project), CreatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
		}))
		serverID := seedRemoteMcpServerForLogin(t, ctx, ti.conn, project, config, "attachment-probe", "https://attachment-probe.example.test/mcp")
		require.NoError(t, fixtures.InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
			ProjectID: project, OrganizationID: org, McpServerID: serverID, RemoteSessionClientID: session.RemoteSessionClientID,
		}))
		_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID: project, McpServerID: conv.ToNullUUID(serverID), Slug: "attachment-probe",
		})
		require.NoError(t, err)
	}
	var humanSessions int64
	humanSessions, err = testrepo.New(ti.conn).CountAttachmentHumanSessionsFixture(ctx, session.SubjectUrn)
	require.NoError(t, err)
	require.Zero(t, humanSessions)
	window := newSweepWindow()
	bulkLookup := repo.ListEligiblePrincipalRemoteSessionBindingsParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config}
	eligible, err := q.ListEligiblePrincipalRemoteSessionBindings(ctx, bulkLookup)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	require.Equal(t, session.ID, eligible[0].ID)
	lookup := repo.GetPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionClientID: session.RemoteSessionClientID}
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.NoError(t, err)
	recheck := newRecheckWindow()
	var priorLease repo.ClearRemoteSessionRecheckLeaseParams
	if !checkRecheck {
		requireAttachmentRefreshReach(t, ctx, q, window, session.ID, org, true)
		// Remove cooldown so an empty subsequent claim proves ineligibility.
		_, err = fixtures.ClearAttachmentRefreshClaimFixture(ctx, session.ID)
		require.NoError(t, err)
	} else {
		priorLease = requireAttachmentRecheckReach(t, ctx, q, recheck, session, org, true)
		foreignLease := priorLease
		foreignLease.OrganizationIds = []string{"org_unrelated"}
		cleared, err := q.ClearRemoteSessionRecheckLease(ctx, foreignLease)
		require.NoError(t, err)
		require.Zero(t, cleared, "a valid attachment cannot clear a lease from a different tenant scope")
		cleared, err = q.ClearRemoteSessionRecheckLease(ctx, priorLease)
		require.NoError(t, err)
		require.EqualValues(t, 1, cleared)
		// Acquire a second lease so the post-mutation clear tests reach,
		// rather than succeeding vacuously with no current lease.
		recheck = newRecheckWindow()
		priorLease = requireAttachmentRecheckReach(t, ctx, q, recheck, session, org, true)
	}
	switch state {
	case "detach":
		_, err = q.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, ID: binding.ID})
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
	case "config-deleted", "source-deleted":
		id := config
		if state == "source-deleted" {
			id = source
		}
		_, err = testrepo.New(ti.conn).SoftDeleteAttachmentConfigFixture(ctx, testrepo.SoftDeleteAttachmentConfigFixtureParams{ID: id, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
	case "client-deleted":
		_, err = testrepo.New(ti.conn).SoftDeleteAttachmentClientFixture(ctx, testrepo.SoftDeleteAttachmentClientFixtureParams{ID: session.RemoteSessionClientID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
	case "registration-retired":
		err = ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: client, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		_, err = fixtures.GetPrincipalRemoteSessionBindingSessionFixture(ctx, binding.ID)
		require.NoError(t, err, "registration deletion physically removed the attachment")
		active, err := fixtures.CountActivePrincipalRemoteSessionBindingsForSessionFixture(ctx, session.ID)
		require.NoError(t, err)
		require.Zero(t, active, "registration deletion did not retire the attachment")
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
	stillAttached := state == "owner-transfer"
	if !checkRecheck {
		requireAttachmentRefreshReach(t, ctx, q, window, session.ID, org, stillAttached)
	} else {
		cleared, err := q.ClearRemoteSessionRecheckLease(ctx, priorLease)
		require.NoError(t, err)
		if stillAttached {
			require.EqualValues(t, 1, cleared)
		} else {
			require.Zero(t, cleared)
			_, err = fixtures.ClearAttachmentRefreshClaimFixture(ctx, session.ID)
			require.NoError(t, err)
		}
		requireAttachmentRecheckReach(t, ctx, q, newRecheckWindow(), session, org, stillAttached)
	}
	if stillAttached {
		_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
		require.NoError(t, err, "a valid delegation survives agent ownership transfer")
		eligible, err = q.ListEligiblePrincipalRemoteSessionBindings(ctx, bulkLookup)
		require.NoError(t, err)
		require.Len(t, eligible, 1)
	} else if state != "opt-out" && state != "policy-disabled" {
		_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		eligible, err = q.ListEligiblePrincipalRemoteSessionBindings(ctx, bulkLookup)
		require.NoError(t, err)
		require.Empty(t, eligible)
	}
}

func requireAttachmentRefreshReach(t *testing.T, ctx context.Context, q *repo.Queries, window sweepWindow, id uuid.UUID, org string, want bool) {
	t.Helper()
	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(id, org))
	if !want {
		require.Empty(t, rows)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		return
	}
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.NoError(t, err)
	_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(id, "org_unrelated"))
	require.ErrorIs(t, err, pgx.ErrNoRows, "a live attachment cannot reauthorize a refresh claimed in another tenant")
	require.Equal(t, id, candidate.RemoteSession.ID)
}

func requireAttachmentRecheckReach(t *testing.T, ctx context.Context, q *repo.Queries, window recheckWindow, session repo.RemoteSession, org string, want bool) repo.ClearRemoteSessionRecheckLeaseParams {
	t.Helper()
	rows, err := q.ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	candidate, err := q.GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(session.ID, org))
	if want {
		require.NoError(t, err)
		require.Equal(t, session.ID, candidate.RemoteSession.ID)
	} else {
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	params := repo.GetRemoteSessionRecheckEndpointsParams{
		ID: session.ID, UpdatedAt: session.UpdatedAt, GrantGeneration: session.GrantGeneration,
		OrganizationIds: []string{org}, NowTs: conv.ToPGTimestamptz(window.now),
	}
	endpoints, err := q.GetRemoteSessionRecheckEndpoints(ctx, params)
	require.NoError(t, err)
	if !want {
		require.Empty(t, rows)
		require.Empty(t, endpoints)
		return repo.ClearRemoteSessionRecheckLeaseParams{}
	}
	require.Len(t, rows, 1)
	require.Equal(t, session.ID, rows[0].ID)
	require.Len(t, endpoints, 1)
	_, err = q.GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(session.ID, "org_unrelated"))
	require.ErrorIs(t, err, pgx.ErrNoRows, "an attachment cannot reauthorize a recheck claimed by another tenant")
	params.OrganizationIds = []string{"org_unrelated"}
	foreignEndpoints, err := q.GetRemoteSessionRecheckEndpoints(ctx, params)
	require.NoError(t, err)
	require.Empty(t, foreignEndpoints, "a valid attachment cannot produce a probe target for another tenant")
	require.Equal(t, "attachment-probe", endpoints[0].Slug)
	return repo.ClearRemoteSessionRecheckLeaseParams{ID: session.ID, ClaimedAt: rows[0].ClaimedAt, OrganizationIds: []string{org}, NowTs: conv.ToPGTimestamptz(window.now)}
}
