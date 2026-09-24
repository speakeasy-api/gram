package workloadpolicy_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func admit(t *testing.T, ctx context.Context, ti *testInstance, subject string, matchKind string, agentID uuid.UUID) (*gen.WorkloadIdentityPolicy, error) {
	t.Helper()

	policy, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          subject,
		MatchKind:        new(matchKind),
		Name:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    nil,
	})
	if err != nil {
		// Wrapped so wrapcheck is satisfied; every assertion on this error uses
		// errors.Is or errors.As, which both see through it.
		return nil, fmt.Errorf("admit subject: %w", err)
	}

	return policy, nil
}

func TestAdmitSubject_WildcardAdmitsTheFleetAndAssignsItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)

	// The onboarding this API exists for: the agent id is minted per Slack
	// channel and never shown, so an exact subject cannot be typed in advance.
	policy, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 1)
	admission := policy.Admissions[0]
	require.Equal(t, fleetRule, admission.Subject)
	require.Equal(t, string(workloadidentity.MatchKindWildcard), admission.MatchKind)
	require.Equal(t, anthropicIssuer, admission.Issuer)
	require.Equal(t, "Claude Tag", admission.IssuerName)
	// Admitted and assigned in one call: a subject with no agent is refused at
	// the token endpoint, so it must not be reachable from here.
	require.Equal(t, agentID.String(), admission.AgentID)
	require.Equal(t, "claude-tag-poc", admission.AgentName)
	require.True(t, admission.WildcardActive)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The snapshot has to carry the breadth of the grant, not just the subject:
	// a wildcard rule stands for every subject under its stem.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, fleetRule, snapshot["subject"])
	require.Equal(t, "wildcard", snapshot["match_kind"])
	require.Equal(t, agentID.String(), snapshot["assigned_agent_id"])
	require.Equal(t, "organization", snapshot["tier"])
}

func TestAdmitSubject_RefusesAWildcardTheIssuerDoesNotPermit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, false)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// Two gates, neither implied by the other. This is the early, legible
	// refusal; the lookup re-checks the same permission on every read.
	_, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorIs(t, err, workloadidentity.ErrWildcardNotPermitted)
}

func TestAdmitSubject_RefusesAStarInAnExactSubject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// An exact rule containing "*" stores a literal that matches nothing, and
	// then reads as correct in the list afterwards.
	_, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorIs(t, err, workloadidentity.ErrExactSubjectHasWildcard)
}

func TestAdmitSubject_RefusesAnUnregisteredIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// Resolve, do not validate: the lookup is tenancy-scoped, so an issuer the
	// caller cannot see is a not-found rather than a check after the fact.
	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdmitSubject_RefusesAnUnknownAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)

	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), uuid.New())
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdmitSubject_RefusesTheSameSubjectTwiceAtOneTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	require.NoError(t, err)

	_, err = admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAdmitSubject_RequiresWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	readOnly := withScopes(t, ctx, ti, authz.ScopeWorkloadRead)
	_, err := admit(t, readOnly, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAdmitSubject_RefusesTwoIssuersSharingAURLAtOneTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// The issuer column is not unique, and the per-tier name index lets two rows
	// in one project carry the same URL under different names. Choosing between
	// them would silently decide which jwks_uri verifies this subject and whether
	// wildcards are permitted for it, so it has to be a refusal — tier precedence
	// resolves across tiers, never within one.
	for _, name := range []string{"Claude Tag A", "Claude Tag B"} {
		_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
			SessionToken:           nil,
			ApikeyToken:            nil,
			ProjectSlugInput:       nil,
			Name:                   name,
			Issuer:                 anthropicIssuer,
			JwksURI:                anthropicJWKS,
			AllowWildcardAdmission: nil,
			ProjectScoped:          new(true),
		})
		require.NoError(t, err)
	}

	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          channelOne,
		MatchKind:        new(string(workloadidentity.MatchKindExact)),
		Name:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    new(true),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "same tier")
}
