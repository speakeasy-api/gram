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

	// The assignments are keyed independently of the admissions, so every
	// assertion above would still pass while the tuples kept resolving to an
	// agent. Mirrors what the single-subject withdrawal test checks.
	assignments, err := repo.New(ti.conn).SoftDeleteWorkloadAgentAssignmentsByIssuer(ctx, repo.SoftDeleteWorkloadAgentAssignmentsByIssuerParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: uuid.MustParse(policy.Issuers[0].ID),
	})
	require.NoError(t, err)
	require.Empty(t, assignments, "withdrawing the issuer should already have tombstoned every assignment under it")
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
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    true,
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

	// The before-snapshot has to say which agent the withdrawn admission ran
	// under, whether or not the shared assignment went with it. Reading the agent
	// only on the branch that deletes the assignment left exactly this case — the
	// other tier surviving — with no agent recorded.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, agentID.String(), snapshot["assigned_agent_id"],
		"the withdrawal's snapshot lost the agent because the other tier kept the assignment")
}

// A sibling project's issuer is invisible in the caller's list, so it must not be
// withdrawable by supplying its UUID. GetWorkloadIssuer and SoftDeleteWorkloadIssuer
// once scoped on organization alone, which made a row the caller could not observe
// a row it could destroy.
func TestWithdrawIssuer_RefusesASiblingProjectsIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Same organization, a different project than the one the caller has selected.
	siblingProject := uuid.New()
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: registering under another project is not reachable through the API
		ctx, `
		INSERT INTO projects (id, organization_id, name, slug)
		VALUES ($1::uuid, $2, 'sibling', 'sibling-' || left($1::uuid::text, 8))
	`, siblingProject, ti.orgID)
	require.NoError(t, err)

	var siblingIssuer uuid.UUID
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: see above
		ctx, `
		INSERT INTO workload_issuers
		  (organization_id, project_id, name, issuer, jwks_uri)
		VALUES ($1, $2, 'sibling-ci', 'https://sibling.example.com',
		        'https://sibling.example.com/.well-known/jwks.json')
		RETURNING id
	`, ti.orgID, siblingProject).Scan(&siblingIssuer)
	require.NoError(t, err)

	_, err = ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               siblingIssuer.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Still live, so the refusal was a refusal and not a silent no-op.
	var deleted bool
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: asserting the row the API must not have touched
		ctx, `SELECT deleted FROM workload_issuers WHERE id = $1`, siblingIssuer).Scan(&deleted)
	require.NoError(t, err)
	require.False(t, deleted, "a sibling project's issuer must survive a withdrawal it was never visible to")
}
