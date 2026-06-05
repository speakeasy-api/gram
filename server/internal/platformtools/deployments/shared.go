package deployments

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/deployments"
)

// DeploymentsService is the subset of the deployments management service that
// the managed assistant's deployment tools call. The concrete deployments
// service satisfies it; tools pass nil auth tokens because the assistant
// runtime supplies auth context out of band.
type DeploymentsService interface {
	GetDeploymentLogs(ctx context.Context, payload *deployments.GetDeploymentLogsPayload) (*deployments.GetDeploymentLogsResult, error)
}
