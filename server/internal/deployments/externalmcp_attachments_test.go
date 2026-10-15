package deployments_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
)

// seedDeploymentWithExternalMCP inserts a completed deployment that carries an
// external MCP attachment, without running the processing workflow.
func seedDeploymentWithExternalMCP(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	q := repo.New(ti.conn)
	depID, err := q.InsertDeployment(ctx, repo.InsertDeploymentParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, q.CreateDeploymentStatus(ctx, repo.CreateDeploymentStatusParams{DeploymentID: depID, Status: "completed"}))

	_, err = externalmcprepo.New(ti.conn).CreateExternalMCPAttachment(ctx, externalmcprepo.CreateExternalMCPAttachmentParams{
		DeploymentID:            depID,
		RegistryID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:                    "Test",
		Slug:                    "test",
		RegistryServerSpecifier: "example/server",
	})
	require.NoError(t, err)

	return depID
}

func TestDeploymentsService_EvolveDropsExternalMCPAttachments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	seedDeploymentWithExternalMCP(t, ctx, ti)

	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		NonBlocking:         new(true),
		ExcludeExternalMcps: []string{"unrelated"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Deployment.ExternalMcps)
}

func TestDeploymentsService_RedeployDropsExternalMCPAttachments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	depID := seedDeploymentWithExternalMCP(t, ctx, ti)

	result, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
		DeploymentID:     depID.String(),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEqual(t, depID.String(), result.Deployment.ID)
	require.Empty(t, result.Deployment.ExternalMcps)
}
