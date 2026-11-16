package gram

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gendeployments "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

type redeployCompositionManagement struct{}

func (*redeployCompositionManagement) GetDeployment(context.Context, *gendeployments.GetDeploymentPayload) (*gendeployments.GetDeploymentResult, error) {
	return nil, platformmcp.ErrUnavailable
}
func (*redeployCompositionManagement) PrepareRedeploy(context.Context, pgx.Tx, *gendeployments.RedeployPayload) (*types.Deployment, error) {
	return nil, platformmcp.ErrUnavailable
}
func (*redeployCompositionManagement) DispatchRedeploy(context.Context, string, uuid.UUID) (*gendeployments.RedeployResult, error) {
	return nil, platformmcp.ErrUnavailable
}

func TestPlatformMCPDeploymentRedeployRequiresTemporal(t *testing.T) {
	t.Parallel()
	// Construction performs no queries; the non-nil pool proves the Temporal
	// gate, rather than another missing dependency, is what disables the service.
	db := new(pgxpool.Pool)
	reader := platformmcp.NewPostgresReader(testenv.NewLogger(t), db)
	config := platformMCPConfig{DB: db, DeploymentRedeploy: &redeployCompositionManagement{}, MeterProvider: noop.NewMeterProvider(), TemporalEnv: new(tenv.Environment)}
	require.NotNil(t, newPlatformMCPDeploymentRedeploy(config, reader, nil))
	config.TemporalEnv = nil
	require.Nil(t, newPlatformMCPDeploymentRedeploy(config, reader, nil))
}
