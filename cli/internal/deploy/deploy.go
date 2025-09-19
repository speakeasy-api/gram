package deploy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/server/gen/deployments"
)

type CreateDeploymentRequest struct {
	Config         *Config
	APIKey         string
	ProjectSlug    string
	IdempotencyKey string
}

// CreateDeployment creates a remote deployment from the incoming sources.
func CreateDeployment(
	ctx context.Context,
	logger *slog.Logger,
	req CreateDeploymentRequest,
) (*deployments.CreateDeploymentResult, error) {
	assets, err := createAssetsForDeployment(ctx, logger, &CreateDeploymentRequest{
		Config:         req.Config,
		APIKey:         req.APIKey,
		ProjectSlug:    req.ProjectSlug,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert sources to assets: %w", err)
	}

	client := api.NewDeploymentsClient()

	result, err := client.CreateDeployment(ctx, api.CreateDeploymentRequest{
		APIKey:          req.APIKey,
		ProjectSlug:     req.ProjectSlug,
		IdempotencyKey:  req.IdempotencyKey,
		OpenAPIv3Assets: assets,
	})
	if err != nil {
		return nil, fmt.Errorf("deployment creation failed: %w", err)
	}

	return result, nil
}
