package chatanalysis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin_chat_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestTriggerAnalysisRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.TriggerAnalysis(ctx, &gen.TriggerAnalysisPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.signaler.Signaled())
}

func TestTriggerAnalysisSignalsEveryOrganizationProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	second, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           "chat-analysis-trigger-second",
		Slug:           "chat-analysis-trigger-second",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	result, err := ti.service.TriggerAnalysis(adminCtx, &gen.TriggerAnalysisPayload{SessionToken: nil})
	require.NoError(t, err)

	signaled := ti.signaler.Signaled()
	require.Len(t, signaled, result.ProjectsSignaled)
	require.Contains(t, signaled, *authCtx.ProjectID)
	require.Contains(t, signaled, second.ID)
}
