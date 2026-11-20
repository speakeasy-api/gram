package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// functions_available reports the deployment's Functions provider, the same
// for every organization, so the dashboard can hide Functions entry points.
func TestProductFeaturesService_GetProductFeatures_FunctionsAvailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)

	res, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.NotNil(t, res.FunctionsAvailable)
	require.True(t, *res.FunctionsAvailable)

	logger := testenv.NewLogger(t)
	disabled := productfeatures.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, ti.redisClient,
		authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		audit.NewLogger(),
		productfeatures.DeploymentCapabilities{AssistantsAvailable: true, FunctionsAvailable: false},
	)
	res, err = disabled.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.NotNil(t, res.FunctionsAvailable)
	require.False(t, *res.FunctionsAvailable)
}
