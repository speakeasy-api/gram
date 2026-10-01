package organizations_test

import (
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/stretchr/testify/require"
)

// distributionUseCase defines a use case whose default playbook needs no
// particular vendor, so the survey can assign it to a bare organization.
func distributionUseCase(t *testing.T, ti *testInstance) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, organizations.SyncOnboardingSteps(ctx, ti.conn))
	useCase, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, "identity", "Identity", "Know who is using AI.")
	require.NoError(t, err)
	id, err := uuid.Parse(useCase.ID)
	require.NoError(t, err)
	_, err = organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{
		UseCaseID: &id, OrganizationID: nil, Name: "Identity first", Description: "", IsDefault: true,
		StepSlugs: []string{"identity-provider", "enable-logging"},
	})
	require.NoError(t, err)
	return id
}

func TestService_SubmitOnboardingSurveyAssignsUseCaseDefaultPlaybook(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	distributionUseCase(t, ti)
	result, err := ti.service.SubmitOnboardingSurvey(ctx, &gen.SubmitOnboardingSurveyPayload{UseCase: "identity"})
	require.NoError(t, err)
	keys := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		keys = append(keys, task.Key)
	}
	require.Equal(t, []string{"identity-provider", "enable-logging"}, keys, "the playbook's cards, nothing else")

	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, result.Tasks, listed.Tasks)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assigned, err := organizations.LoadOrganizationOnboardingPlaybook(ctx, ti.conn, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.NotNil(t, assigned.Playbook)
	require.Equal(t, "Identity first", assigned.Playbook.Name)
	for _, step := range assigned.Applicability {
		require.True(t, step.Applies, step.Slug)
	}
}

func TestService_SubmitOnboardingSurveyRejectsUnknownUseCase(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.SubmitOnboardingSurvey(ctx, &gen.SubmitOnboardingSurveyPayload{UseCase: "unknown"})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_SubmitOnboardingSurveyNeedsADefaultPlaybook(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	_, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, "spend", "Spend Controls", "")
	require.NoError(t, err)
	result, err := ti.service.SubmitOnboardingSurvey(ctx, &gen.SubmitOnboardingSurveyPayload{UseCase: "spend"})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_SubmitOnboardingSurveyRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	result, err := ti.service.SubmitOnboardingSurvey(readOnlyCtx, &gen.SubmitOnboardingSurveyPayload{UseCase: "identity"})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
}
