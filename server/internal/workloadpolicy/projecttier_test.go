package workloadpolicy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// registerProjectAnthropic registers Claude Tag at the project tier from a
// dashboard session, the way a project's Access Hub does.
func registerProjectAnthropic(t *testing.T, ctx context.Context, ti *testInstance) *types.WorkloadIssuer {
	t.Helper()

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          true,
	})
	require.NoError(t, err)

	return onlyIssuer(t, policy)
}

func admitProjectScoped(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID) *gen.WorkloadIdentityPolicy {
	t.Helper()

	policy, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          fleetRule,
		MatchKind:        string(workloadidentity.MatchKindWildcard),
		Name:             nil,
		Tags:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    true,
	})
	require.NoError(t, err)

	return policy
}

func listProjectScoped(t *testing.T, ctx context.Context, ti *testInstance) *gen.WorkloadIdentityPolicy {
	t.Helper()

	policy, err := ti.service.List(ctx, &gen.ListPayload{
		ProjectScoped:    true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	return policy
}

func TestList_ProjectScopedSessionReadsTheProjectTierAlone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	registerProjectGitHub(t, ctx, ti)

	issuer := onlyIssuer(t, listProjectScoped(t, ctx, ti))
	require.Equal(t, githubIssuer, issuer.Issuer)
	require.Equal(t, ti.projectID.String(), issuer.ProjectID)
}

func TestList_ProjectScopedWithoutAProjectIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.List(withoutProject(t, ctx), &gen.ListPayload{
		ProjectScoped:    true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestList_ProjectScopedLeavesOutOrganizationTierAdmissions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	_, err := admit(t, ctx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)

	policy := listProjectScoped(t, ctx, ti)
	require.Empty(t, policy.Issuers)
	require.Empty(t, policy.Admissions)
}

func TestRegisterIssuer_ProjectScopedSessionWritesTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := registerProjectAnthropic(t, ctx, ti)
	require.Equal(t, ti.projectID.String(), issuer.ProjectID)

	// The organization page, which does not set project_scoped, does not see it.
	orgPolicy, err := ti.service.List(ctx, &gen.ListPayload{
		ProjectScoped:    false,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, orgPolicy.Issuers)
}

func TestUpdateIssuer_ProjectScopedEditsTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := registerProjectAnthropic(t, ctx, ti)

	payload := updatePayload(issuer.ID)
	payload.ProjectScoped = true
	payload.Name = new("Renamed")
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "Renamed", onlyIssuer(t, policy).Name)
}

func TestUpdateIssuer_ProjectScopedCannotReachTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := onlyIssuer(t, registerAnthropic(t, ctx, ti, true))

	payload := updatePayload(issuer.ID)
	payload.ProjectScoped = true
	payload.Name = new("Renamed")
	_, err := ti.service.UpdateIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWithdrawIssuer_ProjectScopedCannotReachTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := onlyIssuer(t, registerAnthropic(t, ctx, ti, true))

	_, err := ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
		ID:               issuer.ID,
		ProjectScoped:    true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWithdrawIssuer_ProjectScopedWithdrawsTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := registerProjectAnthropic(t, ctx, ti)

	policy, err := ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
		ID:               issuer.ID,
		ProjectScoped:    true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, policy.Issuers)
}

func TestAdmitSubject_ProjectScopedSessionWritesTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerProjectAnthropic(t, ctx, ti)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	admission := onlyAdmission(t, admitProjectScoped(t, ctx, ti, agentID))
	require.Equal(t, ti.projectID.String(), admission.ProjectID)
	require.Equal(t, fleetRule, admission.Subject)
}

func TestUpdateSubject_ProjectScopedCannotReachTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	admission := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(admission.ID)
	payload.ProjectScoped = true
	payload.Name = new("Renamed")
	_, err := ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWithdrawSubject_ProjectScopedCannotReachTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	admission := admitLabelled(t, ctx, ti, agentID)

	_, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{
		ID:               admission.ID,
		ProjectScoped:    true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWithdrawSubject_ProjectScopedWithdrawsTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerProjectAnthropic(t, ctx, ti)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	admission := onlyAdmission(t, admitProjectScoped(t, ctx, ti, agentID))

	policy, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{
		ID:               admission.ID,
		ProjectScoped:    true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, policy.Admissions)
}

func TestListTokenEndpoints_ProjectScopedListsTheProjectAlone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	projectIssuer := newProjectIssuer(t, ctx, ti, ti.projectID, "project-issuer")
	setShared(t, ctx, ti, projectIssuer, "")
	orgIssuer := newOrganizationIssuer(t, ctx, ti, "org-issuer")
	setShared(t, ctx, ti, orgIssuer, "")

	payload := listTokenEndpointsPayload()
	payload.ProjectScoped = true
	result, err := ti.service.ListTokenEndpoints(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, []string{projectIssuer.ID.String()}, issuerIDs(result.Items))
}
