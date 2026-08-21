package chatanalysis_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin_chat_analysis"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestTriggerAnalysisRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.TriggerAnalysis(ctx, &gen.TriggerAnalysisPayload{OrganizationID: "target-org"})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.signaler.Signaled())
}

func TestTriggerAnalysisReportsFailedProjectAndContinues(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	targetOrganizationID := createTargetOrganization(t, ctx, ti)

	first, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name: "chat-analysis-failing-first", Slug: "chat-analysis-failing-first", OrganizationID: targetOrganizationID,
	})
	require.NoError(t, err)
	second, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name: "chat-analysis-successful-second", Slug: "chat-analysis-successful-second", OrganizationID: targetOrganizationID,
	})
	require.NoError(t, err)
	ti.signaler.errors = map[uuid.UUID]error{first.ID: errors.New("signal failed")}

	_, err = chatanalysis.TriggerOrganization(ctx, ti.conn, ti.signaler, targetOrganizationID)
	require.Error(t, err)
	require.ErrorContains(t, err, first.ID.String())
	require.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, ti.signaler.Signaled())
}

func TestTriggerAnalysisSignalsOnlyRequestedOrganizationProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	targetOrganizationID := createTargetOrganization(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	first, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name: "chat-analysis-target-first", Slug: "chat-analysis-target-first", OrganizationID: targetOrganizationID,
	})
	require.NoError(t, err)
	second, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name: "chat-analysis-target-second", Slug: "chat-analysis-target-second", OrganizationID: targetOrganizationID,
	})
	require.NoError(t, err)

	result, err := ti.service.TriggerAnalysis(adminCtx, &gen.TriggerAnalysisPayload{OrganizationID: targetOrganizationID})
	require.NoError(t, err)
	require.Equal(t, 2, result.ProjectsSignaled)

	signaled := ti.signaler.Signaled()
	require.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, signaled)
	require.NotContains(t, signaled, *authCtx.ProjectID)
}
