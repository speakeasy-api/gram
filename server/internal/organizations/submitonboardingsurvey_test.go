package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/stretchr/testify/require"
)

func TestService_SubmitOnboardingSurveyAppliesUseCaseDefaultPlaybook(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.SubmitOnboardingSurvey(ctx, &gen.SubmitOnboardingSurveyPayload{UseCase: "gateway"})
	require.NoError(t, err)
	keys := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		keys = append(keys, task.Key)
	}
	require.Equal(t, []string{"create-marketplace", "distribute-servers"}, keys)

	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, result.Tasks, listed.Tasks)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	saved, err := organizations.LoadOnboardingConfiguration(ctx, ti.conn, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "gateway", *saved.Preset)
}

func TestService_SubmitOnboardingSurveyRejectsUnknownUseCase(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.SubmitOnboardingSurvey(ctx, &gen.SubmitOnboardingSurveyPayload{UseCase: "unknown"})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_SubmitOnboardingSurveyRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	result, err := ti.service.SubmitOnboardingSurvey(readOnlyCtx, &gen.SubmitOnboardingSurveyPayload{UseCase: "gateway"})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
}
