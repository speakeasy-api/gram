package workloadpolicy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// A second platform, registered for one project alone, so a test can show an
// organization-wide session neither sees nor touches the project tier.
const (
	githubIssuer = "https://token.actions.githubusercontent.com"
	githubJWKS   = "https://token.actions.githubusercontent.com/.well-known/jwks"
)

// registerProjectGitHub registers GitHub Actions at the project tier, through
// the project-selecting ctx an API key arrives with.
func registerProjectGitHub(t *testing.T, ctx context.Context, ti *testInstance) *types.WorkloadIssuer {
	t.Helper()

	policy, err := ti.service.RegisterIssuer(asAPIKey(t, ctx), &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "GitHub Actions",
		Issuer:                 githubIssuer,
		JwksURI:                githubJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          true,
	})
	require.NoError(t, err)

	for _, issuer := range policy.Issuers {
		if issuer.Issuer == githubIssuer {
			require.Equal(t, ti.projectID.String(), issuer.ProjectID)
			return issuer
		}
	}
	require.FailNow(t, "project-tier issuer missing from the policy")
	return nil
}

func TestList_WithoutAProjectReadsTheOrganizationTierAlone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	registerProjectGitHub(t, ctx, ti)

	policy, err := ti.service.List(withoutProject(t, ctx), &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	issuer := onlyIssuer(t, policy)
	require.Equal(t, anthropicIssuer, issuer.Issuer)
	require.Empty(t, issuer.ProjectID)
}

func TestRegisterIssuer_WithoutAProjectWritesTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	issuer := onlyIssuer(t, registerAnthropic(t, orgCtx, ti, true))
	require.Empty(t, issuer.ProjectID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadIssuerCreate)
	require.NoError(t, err)
	require.False(t, record.ProjectID.Valid)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "organization", snapshot["tier"])

	// Organization-tier rows are visible to every project, so a caller that does
	// select one sees the same issuer.
	fromProject, err := ti.service.List(asAPIKey(t, ctx), &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, issuer.ID, onlyIssuer(t, fromProject).ID)
}

func TestRegisterIssuer_WithoutAProjectRefusesProjectScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RegisterIssuer(withoutProject(t, ctx), &gen.RegisterIssuerPayload{
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
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "project_scoped requires a caller that names a project")
}

func TestUpdateIssuer_WithoutAProjectEditsTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	issuer := onlyIssuer(t, registerAnthropic(t, orgCtx, ti, true))

	payload := updatePayload(issuer.ID)
	payload.Name = new("Claude in Slack")
	policy, err := ti.service.UpdateIssuer(orgCtx, payload)
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.Equal(t, "Claude in Slack", updated.Name)
	require.Empty(t, updated.ProjectID)
}

func TestUpdateIssuer_WithoutAProjectCannotReachTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := registerProjectGitHub(t, ctx, ti)

	payload := updatePayload(issuer.ID)
	payload.Name = new("Renamed")
	_, err := ti.service.UpdateIssuer(withoutProject(t, ctx), payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWithdrawIssuer_WithoutAProjectWithdrawsTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	issuer := onlyIssuer(t, registerAnthropic(t, orgCtx, ti, true))

	policy, err := ti.service.WithdrawIssuer(orgCtx, &gen.WithdrawIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               issuer.ID,
	})
	require.NoError(t, err)
	require.Empty(t, policy.Issuers)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadIssuerDelete)
	require.NoError(t, err)
	require.False(t, record.ProjectID.Valid)
}

func TestAdmitSubject_WithoutAProjectWritesTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	registerAnthropic(t, orgCtx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	policy, err := admit(t, orgCtx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)

	admission := onlyAdmission(t, policy)
	require.Empty(t, admission.ProjectID)
	require.Equal(t, agentID.String(), admission.AgentID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.False(t, record.ProjectID.Valid)
}

func TestAdmitSubject_WithoutAProjectRefusesProjectScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	registerAnthropic(t, orgCtx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")

	_, err := ti.service.AdmitSubject(orgCtx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             nil,
		Tags:             nil,
		AgentID:          agentID.String(),
		ProjectScoped:    true,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "project_scoped requires a caller that names a project")
}

func TestUpdateSubject_WithoutAProjectEditsTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	registerAnthropic(t, orgCtx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	admission := admitLabelled(t, orgCtx, ti, agentID)

	replacement := newAgent(t, ctx, ti, "claude-tag-replacement")
	payload := updateSubjectPayload(admission.ID)
	payload.Name = new("Release channel")
	payload.AgentID = new(replacement.String())
	policy, err := ti.service.UpdateSubject(orgCtx, payload)
	require.NoError(t, err)

	updated := onlyAdmission(t, policy)
	require.Equal(t, "Release channel", updated.Name)
	require.Equal(t, replacement.String(), updated.AgentID)
	require.Empty(t, updated.ProjectID)
}

func TestWithdrawSubject_WithoutAProjectWithdrawsTheOrganizationTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	registerAnthropic(t, orgCtx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	policy, err := admit(t, orgCtx, ti, fleetRule, string(workloadidentity.MatchKindWildcard), agentID)
	require.NoError(t, err)

	after, err := ti.service.WithdrawSubject(orgCtx, &gen.WithdrawSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               onlyAdmission(t, policy).ID,
	})
	require.NoError(t, err)
	require.Empty(t, after.Admissions)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.False(t, record.ProjectID.Valid)
}

func TestList_SessionCarryingAProjectReadsTheOrganizationTierAlone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID, "the session context must carry a project for this test to mean anything")
	require.True(t, contextvalues.HasValidatedGramSession(ctx))

	registerAnthropic(t, ctx, ti, true)
	registerProjectGitHub(t, ctx, ti)

	policy, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	issuer := onlyIssuer(t, policy)
	require.Equal(t, anthropicIssuer, issuer.Issuer)
	require.Empty(t, issuer.ProjectID)
}

func TestRegisterIssuer_SessionCarryingAProjectCannotWriteTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "GitHub Actions",
		Issuer:                 githubIssuer,
		JwksURI:                githubJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          true,
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	issuer := onlyIssuer(t, registerAnthropic(t, ctx, ti, true))
	require.Empty(t, issuer.ProjectID)
}

func TestUpdateIssuer_SessionCarryingAProjectCannotReachTheProjectTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := registerProjectGitHub(t, ctx, ti)

	payload := updatePayload(issuer.ID)
	payload.Name = new("Renamed")
	_, err := ti.service.UpdateIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestList_APIKeyWithAProjectReadsBothTiers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	registerProjectGitHub(t, ctx, ti)

	policy, err := ti.service.List(asAPIKey(t, ctx), &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	tiers := make(map[string]string, len(policy.Issuers))
	for _, issuer := range policy.Issuers {
		tiers[issuer.Issuer] = issuer.ProjectID
	}
	require.Equal(t, map[string]string{
		anthropicIssuer: "",
		githubIssuer:    ti.projectID.String(),
	}, tiers)
}
