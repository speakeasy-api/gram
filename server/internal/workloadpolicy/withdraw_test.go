package workloadpolicy_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func TestWithdrawSubject_StopsItAuthenticatingAndClearsItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	policy, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)

	after, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               policy.Admissions[0].ID,
	})
	require.NoError(t, err)
	require.Empty(t, after.Admissions)
	// The issuer survives its subject: withdrawing one machine is not a reason
	// to stop trusting the platform that vouched for it.
	require.Len(t, after.Issuers, 1)

	// The assignment is keyed independently of the admission, so leaving it
	// behind would keep the tuple resolving to an agent after withdrawal.
	assignments, err := repo.New(ti.conn).SoftDeleteWorkloadAgentAssignmentForSubject(ctx, repo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: uuid.MustParse(policy.Admissions[0].WorkloadIssuerID),
		MatchKind:        string(workloadidentity.MatchKindWildcard),
		Subject:          fleetRule,
	})
	require.NoError(t, err)
	require.Empty(t, assignments, "the withdrawal should already have tombstoned the assignment")

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.Equal(t, before+1, count)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, agentID.String(), snapshot["assigned_agent_id"])
}

func TestWithdrawIssuer_CascadesToItsAdmittedSubjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy := registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)
	_, err = admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	require.NoError(t, err)

	withdrawn, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)

	after, err := ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               policy.Issuers[0].ID,
	})
	require.NoError(t, err)
	require.Empty(t, after.Issuers)
	// ON DELETE CASCADE only fires on a hard delete, so without the explicit
	// cascade these rows would stay in the active set pointing at a tombstone:
	// invisible in the list, and still matching.
	require.Empty(t, after.Admissions)

	// One entry per affected row. A single +1 on the issuer's own action would
	// not catch the per-subject events silently stopping.
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.Equal(t, withdrawn+2, count)

	issuerEvents, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, int64(1), issuerEvents)
}

func TestWithdrawIssuer_RefusesAnIssuerOutsideTheCallersTenancy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWithdrawIssuer_RequiresWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy := registerAnthropic(t, ctx, ti, true)

	readOnly := withScopes(t, ctx, ti, authz.ScopeWorkloadRead)
	_, err := ti.service.WithdrawIssuer(readOnly, &gen.WithdrawIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               policy.Issuers[0].ID,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestList_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Reading the policy discloses which machines the organization recognises,
	// so it is withheld from members rather than riding on project:read.
	noScopes := withScopes(t, ctx, ti, authz.ScopeProjectRead)
	_, err := ti.service.List(noScopes, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestWithdrawSubject_LeavesTheOtherTiersAgentInPlace(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	// The same tuple admitted at both tiers. They share one agent assignment,
	// because the assignment is keyed on (issuer, match_kind, subject) while an
	// admission is tiered.
	orgTier, err := admit(t, ctx, ti, channelOne, string(workloadidentity.MatchKindExact), agentID)
	require.NoError(t, err)
	require.Len(t, orgTier.Admissions, 1)

	_, err = ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
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
	require.NoError(t, err)

	after, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               orgTier.Admissions[0].ID,
	})
	require.NoError(t, err)

	// The surviving admission must keep its agent. Without it the workload
	// authenticates and is then refused for having no agent, which reads as a
	// broken rule rather than a withdrawn one.
	require.Len(t, after.Admissions, 1)
	require.Equal(t, agentID.String(), after.Admissions[0].AgentID,
		"the remaining tier's admission lost its agent when the other tier was withdrawn")
	// And it must be the OTHER tier that survived. Both admissions share one
	// agent assignment, so the agent assertion alone cannot tell which row is
	// left: withdrawing the wrong tier would satisfy it too.
	require.Equal(t, ti.projectID.String(), after.Admissions[0].ProjectID,
		"the project-tier admission should have survived withdrawing the organization-tier one")
}
