package workloadpolicy_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	sessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func assignmentSession(t *testing.T, ctx context.Context, ti *testInstance, issuerID string, subject string) sessionsrepo.UserSession {
	t.Helper()
	q := sessionsrepo.New(ti.conn)
	issuer, err := q.CreateOrganizationUserSessionIssuer(ctx, sessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(ti.orgID), Slug: "assignment-" + uuid.NewString(), AuthnChallengeMode: "chain",
		SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	session, err := q.CreateUserSession(ctx, sessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID, SubjectUrn: urn.NewWorkloadSubject(uuid.MustParse(issuerID), subject),
		Jti: uuid.NewString(), RefreshTokenHash: conv.ToPGText(uuid.NewString()),
		ExpiresAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	})
	require.NoError(t, err)
	return session
}

func requireAssignmentSessionState(t *testing.T, ctx context.Context, ti *testInstance, session sessionsrepo.UserSession, revoked bool) {
	t.Helper()
	// This lookup only checks the persisted session tombstone, not the current
	// assignment or agent: losing access through resolution alone is insufficient.
	current, err := sessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, sessionsrepo.GetUserSessionByJTIParams{
		UserSessionIssuerID: session.UserSessionIssuerID, Jti: session.Jti,
	})
	if revoked {
		require.ErrorIs(t, err, pgx.ErrNoRows)
		return
	}
	require.NoError(t, err)
	require.Equal(t, session.ID, current.ID)
	require.False(t, current.Deleted)
}

func assignmentAdmission(t *testing.T, ctx context.Context, ti *testInstance, subject, matchKind string, agentID uuid.UUID) *types.WorkloadAdmission {
	t.Helper()
	policy, err := admit(t, ctx, ti, subject, matchKind, agentID)
	require.NoError(t, err)
	for _, admission := range policy.Admissions {
		if admission.Subject == subject && admission.MatchKind == matchKind {
			return admission
		}
	}
	require.FailNow(t, "new admission missing from policy")
	return nil
}

func TestAssignmentSessions_ReassigningBackDoesNotReviveOldSessions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registerAnthropic(t, ctx, ti, true)
	agentA := newAgent(t, ctx, ti, "agent-a")
	agentB := newAgent(t, ctx, ti, "agent-b")
	admission := assignmentAdmission(t, ctx, ti, channelOne, "exact", agentA)
	original := assignmentSession(t, ctx, ti, admission.WorkloadIssuerID, channelOne)
	requireAssignmentSessionState(t, ctx, ti, original, false)

	payload := updateSubjectPayload(admission.ID)
	payload.AgentID = new(agentB.String())
	_, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, agentB, resolvedAgent(t, ctx, ti, admission))
	requireAssignmentSessionState(t, ctx, ti, original, true)
	replacement := assignmentSession(t, ctx, ti, admission.WorkloadIssuerID, channelOne)

	payload.AgentID = new(agentA.String())
	_, err = ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, agentA, resolvedAgent(t, ctx, ti, admission))
	requireAssignmentSessionState(t, ctx, ti, original, true)
	requireAssignmentSessionState(t, ctx, ti, replacement, true)
	fresh := assignmentSession(t, ctx, ti, admission.WorkloadIssuerID, channelOne)
	requireAssignmentSessionState(t, ctx, ti, fresh, false)
}

func TestAssignmentSessions_MetadataOnlyUpdatePreservesSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registerAnthropic(t, ctx, ti, true)
	agent := newAgent(t, ctx, ti, "metadata-agent")
	admission := assignmentAdmission(t, ctx, ti, channelOne, "exact", agent)
	session := assignmentSession(t, ctx, ti, admission.WorkloadIssuerID, channelOne)
	payload := updateSubjectPayload(admission.ID)
	payload.Name = new("Renamed subject")
	payload.Tags = []string{"updated"}
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "Renamed subject", onlyAdmission(t, policy).Name)
	require.Equal(t, []string{"updated"}, onlyAdmission(t, policy).Tags)
	require.Equal(t, agent, resolvedAgent(t, ctx, ti, admission))
	requireAssignmentSessionState(t, ctx, ti, session, false)
}

func TestAssignmentSessions_WithdrawingExactRevokesDespiteOlderWildcard(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registerAnthropic(t, ctx, ti, true)
	broadAgent := newAgent(t, ctx, ti, "broad-agent")
	exactAgent := newAgent(t, ctx, ti, "exact-agent")
	broad := assignmentAdmission(t, ctx, ti, fleetRule, "wildcard", broadAgent)
	exact := assignmentAdmission(t, ctx, ti, channelOne, "exact", exactAgent)
	session := assignmentSession(t, ctx, ti, exact.WorkloadIssuerID, channelOne)
	broadSession := assignmentSession(t, ctx, ti, broad.WorkloadIssuerID, fleetStem+"other-agent")
	require.Equal(t, exactAgent, resolvedAgent(t, ctx, ti, exact))

	policy, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{ID: exact.ID})
	require.NoError(t, err)
	require.Equal(t, broad.ID, onlyAdmission(t, policy).ID)
	require.Equal(t, broadAgent, resolvedAgent(t, ctx, ti, exact), "the wildcard survives and now wins")
	requireAssignmentSessionState(t, ctx, ti, session, true)
	requireAssignmentSessionState(t, ctx, ti, broadSession, false)
	fresh := assignmentSession(t, ctx, ti, exact.WorkloadIssuerID, channelOne)
	requireAssignmentSessionState(t, ctx, ti, fresh, false)
}

func TestAssignmentSessions_BroadMutationPreservesExactException(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"reassign", "withdraw"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			registerAnthropic(t, ctx, ti, true)
			broadAgent := newAgent(t, ctx, ti, "broad-agent")
			exactAgent := newAgent(t, ctx, ti, "exact-agent")
			replacement := newAgent(t, ctx, ti, "replacement-agent")
			broad := assignmentAdmission(t, ctx, ti, fleetRule, "wildcard", broadAgent)
			exact := assignmentAdmission(t, ctx, ti, channelOne, "exact", exactAgent)
			exactSession := assignmentSession(t, ctx, ti, exact.WorkloadIssuerID, channelOne)
			broadSession := assignmentSession(t, ctx, ti, broad.WorkloadIssuerID, fleetStem+"other-agent")

			if operation == "reassign" {
				payload := updateSubjectPayload(broad.ID)
				payload.AgentID = new(replacement.String())
				_, err := ti.service.UpdateSubject(ctx, payload)
				require.NoError(t, err)
			} else {
				_, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{ID: broad.ID})
				require.NoError(t, err)
			}
			require.Equal(t, exactAgent, resolvedAgent(t, ctx, ti, exact))
			requireAssignmentSessionState(t, ctx, ti, exactSession, false)
			requireAssignmentSessionState(t, ctx, ti, broadSession, true)
		})
	}
}
