package oktaresourceconnections_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newObserver(t *testing.T, si *instance) *oktaresourceconnections.Observer {
	t.Helper()
	return oktaresourceconnections.NewObserver(testenv.NewLogger(t), si.conn, audit.NewLogger())
}

// confirmedUpstream is a capable server confirmed with an audience matching
// its issuer, and an attempt for its upstream starting after confirmation.
func confirmedUpstream(t *testing.T, ctx context.Context, si *instance, name string) (fixture, identitychaining.Observation) {
	t.Helper()
	f := capableServer(t, ctx, si, name)
	_, err := confirm(t, ctx, si, audience, nil, f.serverID)
	require.NoError(t, err)
	row := rowFor(t, list(t, ctx, si, true), f.serverID)
	return f, identitychaining.Observation{
		OrganizationID:  si.orgID,
		TrustedIssuerID: si.oktaIssuerID,
		RemoteIssuerID:  f.issuerID,
		RemoteIssuer:    audience,
		Resource:        row.ResourceIndicator + "/",
		Outcome:         identitychaining.Outcome{Stage: identitychaining.StageComplete, Reason: identitychaining.ReasonSuccess, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false},
		GrantValidated:  true,
		StartedAt:       time.Now().Add(time.Second),
	}
}

func failure(stage identitychaining.Stage, reason identitychaining.Reason, retryable bool) identitychaining.Outcome {
	return identitychaining.Outcome{Stage: stage, Reason: reason, Confidence: identitychaining.ConfidenceInferred, Retryable: retryable, Cached: false}
}

func observedRow(t *testing.T, ctx context.Context, si *instance, serverID uuid.UUID) *gen.OktaResourceConnectionServer {
	t.Helper()
	return rowFor(t, list(t, ctx, si, true), serverID)
}

func TestObserveAttempt_Mapping(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	observer := newObserver(t, si)

	tests := []struct {
		name      string
		outcome   identitychaining.Outcome
		validated bool
		want      string
		state     string
	}{
		{"success", identitychaining.Outcome{Stage: identitychaining.StageComplete, Reason: identitychaining.ReasonSuccess, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false}, true, "verified", "verified"},
		{"redemption rejected after a valid grant", failure(identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant, false), true, "downstream_rejected", "broken"},
		{"exchange invalid_target", failure(identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false), false, "connection_missing", "needs_connection"},
		{"exchange scope denied", failure(identitychaining.StageExchange, identitychaining.ReasonScopePolicyDenied, false), false, "scope_not_allowed", "broken"},
		{"exchange invalid_client", failure(identitychaining.StageExchange, identitychaining.ReasonInvalidClient, false), false, "client_auth_failed", "broken"},
		{"redemption before any grant", failure(identitychaining.StageRedemption, identitychaining.ReasonConfigurationRequired, false), false, "", "connected"},
		{"malformed redemption response", failure(identitychaining.StageRedemption, identitychaining.ReasonMalformedResponse, false), true, "", "connected"},
		{"transient redemption", failure(identitychaining.StageRedemption, identitychaining.ReasonTransientFailure, true), true, "", "connected"},
		{"ambiguous invalid_grant", failure(identitychaining.StageExchange, identitychaining.ReasonInvalidGrant, false), false, "", "connected"},
		{"per-user access_denied", failure(identitychaining.StageExchange, identitychaining.ReasonAccessDenied, false), false, "", "connected"},
		{"transient exchange", failure(identitychaining.StageExchange, identitychaining.ReasonTransientFailure, true), false, "", "connected"},
		{"invalid grant", failure(identitychaining.StageValidation, identitychaining.ReasonMalformedAssertion, false), false, "", "connected"},
		{"delegation", failure(identitychaining.StageDelegation, identitychaining.ReasonReauthenticationRequired, false), false, "", "connected"},
		{"persistence after success", failure(identitychaining.StagePersistence, identitychaining.ReasonStaleConfiguration, true), true, "", "connected"},
	}
	for _, tt := range tests {
		f, obs := confirmedUpstream(t, ctx, si, "Map"+uuid.NewString()[:6])
		obs.Outcome = tt.outcome
		obs.GrantValidated = tt.validated
		require.NoError(t, observer.ObserveAttempt(ctx, obs), tt.name)
		row := observedRow(t, ctx, si, f.serverID)
		require.Equal(t, tt.want, conv.PtrValOr(row.ObservedResult, ""), tt.name)
		require.Equal(t, tt.state, row.State, tt.name)
		require.Equal(t, tt.want != "", row.ObservedAt != nil, tt.name)
	}
}

func TestObserveAttempt_OnlyRecordsOnTheOrganizationsConfirmedOktaUpstream(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	observer := newObserver(t, si)
	f, obs := confirmedUpstream(t, ctx, si, "Guarded")
	unconfirmed := capableServer(t, ctx, si, "Unconfirmed")

	otherIdP := obs
	otherIdP.TrustedIssuerID = uuid.New()
	otherOrg := obs
	otherOrg.OrganizationID = createOrganization(t, ctx, si.conn)
	otherUpstream := obs
	otherUpstream.RemoteIssuerID = unconfirmed.issuerID
	beforeConfirmation := obs
	beforeConfirmation.StartedAt = time.Now().Add(-time.Hour)
	for name, o := range map[string]identitychaining.Observation{
		"another identity provider":              otherIdP,
		"another organization":                   otherOrg,
		"an unconfirmed upstream":                otherUpstream,
		"an attempt older than the confirmation": beforeConfirmation,
	} {
		require.NoError(t, observer.ObserveAttempt(ctx, o), name)
		require.Nil(t, observedRow(t, ctx, si, f.serverID).ObservedResult, name)
	}
	require.Nil(t, observedRow(t, ctx, si, unconfirmed.serverID).ObservedResult)

	require.NoError(t, observer.ObserveAttempt(ctx, obs))
	require.Equal(t, "verified", conv.PtrValOr(observedRow(t, ctx, si, f.serverID).ObservedResult, ""))

	// An attempt that started before the recorded one never overwrites it.
	older := obs
	older.StartedAt = obs.StartedAt.Add(-time.Millisecond)
	older.Outcome = failure(identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false)
	require.NoError(t, observer.ObserveAttempt(ctx, older))
	require.Equal(t, "verified", conv.PtrValOr(observedRow(t, ctx, si, f.serverID).ObservedResult, ""))
}

func TestObserveAttempt_AuditsTransitionsAsTheSystem(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	observer := newObserver(t, si)
	f, obs := confirmedUpstream(t, ctx, si, "Audited")

	// The proxied request's agent and OAuth client must not become the actor.
	agentCtx := contextvalues.WithAuthenticatedActor(ctx, si.authCtx, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	agentCtx = contextvalues.SetOAuthClientID(agentCtx, "agent-client")

	require.NoError(t, observer.ObserveAttempt(agentCtx, obs))
	count, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeSystem), record.ActorType)
	require.Equal(t, oktaresourceconnections.ObserverActor, record.ActorID)
	require.Nil(t, record.ActingClientID)
	require.Equal(t, string(audit.SurfaceSystem), conv.PtrValOr(record.ActingSurface, ""))
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "verified", after["observed_result"])
	require.Equal(t, "verified", after["state"])
	require.Equal(t, oktaresourceconnections.ObserverActorDisplayName, record.ActorDisplay)

	// The same result advances the timestamp without another audit entry.
	again := obs
	again.StartedAt = obs.StartedAt.Add(time.Minute)
	require.NoError(t, observer.ObserveAttempt(ctx, again))
	count, err = audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.WithinDuration(t, again.StartedAt, rows[0].OktaResourceConnection.ObservedAt.Time, time.Millisecond)

	// A later failure replaces verified and is audited.
	missing := obs
	missing.StartedAt = obs.StartedAt.Add(2 * time.Minute)
	missing.Outcome = failure(identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false)
	require.NoError(t, observer.ObserveAttempt(ctx, missing))
	count, err = audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	row := observedRow(t, ctx, si, f.serverID)
	require.Equal(t, "needs_connection", row.State)
	require.True(t, row.Pending)

	// Reconfirming clears the observation.
	_, err = confirm(t, ctx, si, audience, nil, f.serverID)
	require.NoError(t, err)
	row = observedRow(t, ctx, si, f.serverID)
	require.Equal(t, "connected", row.State)
	require.Nil(t, row.ObservedResult)
	require.Nil(t, row.ObservedAt)
}

func TestReadiness_AudienceMismatchOutranksObservedMissingConnection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Mismatch")
	_, err := confirm(t, ctx, si, "https://auth.vendor.example", nil, f.serverID)
	require.NoError(t, err)
	obs := identitychaining.Observation{
		OrganizationID:  si.orgID,
		TrustedIssuerID: si.oktaIssuerID,
		RemoteIssuerID:  f.issuerID,
		RemoteIssuer:    audience,
		Resource:        observedRow(t, ctx, si, f.serverID).ResourceIndicator,
		Outcome:         failure(identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false),
		GrantValidated:  false,
		StartedAt:       time.Now().Add(time.Second),
	}
	require.NoError(t, newObserver(t, si).ObserveAttempt(ctx, obs))
	row := observedRow(t, ctx, si, f.serverID)
	require.Equal(t, "broken", row.State)
	require.Equal(t, "audience_mismatch", conv.PtrValOr(row.BrokenReason, ""))
	require.Equal(t, "connection_missing", conv.PtrValOr(row.ObservedResult, ""))
}

func TestObserveAttempt_OneUsersFailureDoesNotFlipVerified(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	observer := newObserver(t, si)
	f, obs := confirmedUpstream(t, ctx, si, "Flap")
	require.NoError(t, observer.ObserveAttempt(ctx, obs))

	rejected := obs
	rejected.StartedAt = obs.StartedAt.Add(time.Minute)
	rejected.Outcome = failure(identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant, false)
	require.NoError(t, observer.ObserveAttempt(ctx, rejected))
	require.Equal(t, "verified", observedRow(t, ctx, si, f.serverID).State)

	rejected.StartedAt = obs.StartedAt.Add(25 * time.Hour)
	require.NoError(t, observer.ObserveAttempt(ctx, rejected))
	row := observedRow(t, ctx, si, f.serverID)
	require.Equal(t, "broken", row.State)
	require.Equal(t, "downstream_rejected", conv.PtrValOr(row.BrokenReason, ""))
	count, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}

func TestObserveAttempt_RefreshesAnUnchangedResultWithoutAuditing(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	observer := newObserver(t, si)
	f, obs := confirmedUpstream(t, ctx, si, "Refresh")
	require.NoError(t, observer.ObserveAttempt(ctx, obs))

	later := obs
	later.StartedAt = obs.StartedAt.Add(2 * time.Minute)
	require.NoError(t, observer.ObserveAttempt(ctx, later))
	rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.WithinDuration(t, later.StartedAt, rows[0].OktaResourceConnection.ObservedAt.Time, time.Millisecond)
	count, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	// A contradictory attempt started between the successes but finished last.
	older := obs
	older.StartedAt = obs.StartedAt.Add(time.Minute)
	older.Outcome = failure(identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false)
	require.NoError(t, observer.ObserveAttempt(ctx, older))
	require.Equal(t, "verified", observedRow(t, ctx, si, f.serverID).State)
	rows, err = si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "verified", rows[0].OktaResourceConnection.ObservedResult.String)
	require.WithinDuration(t, later.StartedAt, rows[0].OktaResourceConnection.ObservedAt.Time, time.Millisecond)
	count, err = audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionObserve)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
