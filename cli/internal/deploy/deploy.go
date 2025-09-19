package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/deplconfig"
	"github.com/speakeasy-api/gram/cli/internal/env"
	"github.com/speakeasy-api/gram/server/gen/deployments"
)

type DeploymentRequest struct {
	config         *deplconfig.DeploymentConfig
	assets         []*deployments.AddOpenAPIv3DeploymentAssetForm
	project        string
	idempotencyKey string
}

func (dr *DeploymentRequest) GetApiKey() string {
	return env.APIKey()
}

func (dr *DeploymentRequest) GetProjectSlug() string {
	return dr.project
}

// GetIdempotencyKey returns a unique key to identify a deployment request.
func (dr *DeploymentRequest) GetIdempotencyKey() string {
	return dr.idempotencyKey
}

func (dr *DeploymentRequest) GetOpenAPIv3Assets() []*deployments.AddOpenAPIv3DeploymentAssetForm {
	return dr.assets
}

type CreateDeploymentRequest struct {
	Config         *deplconfig.DeploymentConfig
	Project        string
	IdempotencyKey string
}

// CreateDeployment creates a remote deployment from the incoming sources.
func CreateDeployment(
	ctx context.Context,
	logger *slog.Logger,
	req *CreateDeploymentRequest,
) (*deployments.CreateDeploymentResult, error) {
	assets, err := createAssetsForDeployment(ctx, logger, req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert sources to assets: %w", err)
	}

	client := api.NewDeploymentsClient()
	deploymentRequest := &DeploymentRequest{
		assets:         assets,
		config:         req.Config,
		project:        req.Project,
		idempotencyKey: req.IdempotencyKey,
	}

	result, err := client.CreateDeployment(ctx, deploymentRequest)
	if err != nil {
		return nil, fmt.Errorf("deployment creation failed: %w", err)
	}

	return result, nil
}

type CreateDeploymentFromFileRequest struct {
	FilePath       string
	Project        string
	IdempotencyKey string
}

func CreateDeploymentFromFile(
	ctx context.Context,
	logger *slog.Logger,
	req CreateDeploymentFromFileRequest,
) (*deployments.CreateDeploymentResult, error) {
	config, err := deplconfig.ReadDeploymentConfig(req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read deployment config: %w", err)
	}

	createReq := &CreateDeploymentRequest{
		Config:         config,
		Project:        req.Project,
		IdempotencyKey: req.IdempotencyKey,
	}

	return CreateDeployment(ctx, logger, createReq)
}

type uploadRequest struct {
	sourceReader *deplconfig.SourceReader
	project      string
}

func (ur *uploadRequest) GetApiKey() string {
	return env.APIKey()
}

func (ur *uploadRequest) GetProjectSlug() string {
	return ur.project
}

func (ur *uploadRequest) GetType() string {
	return ur.sourceReader.GetType()
}

func (ur *uploadRequest) GetContentType() string {
	return ur.sourceReader.GetContentType()
}

func (ur *uploadRequest) Read(ctx context.Context) (io.ReadCloser, int64, error) {
	reader, size, err := ur.sourceReader.Read(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source: %w", err)
	}
	return reader, size, nil
}
