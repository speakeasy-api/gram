package deploy

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/deplconfig"
	"github.com/speakeasy-api/gram/cli/internal/env"
	"github.com/speakeasy-api/gram/server/gen/deployments"
)

const (
	logKeyType     = "type"
	logKeyLocation = "location"
)

type DeploymentRequest struct {
	config          *deplconfig.DeploymentConfig
	assets          []*deployments.AddOpenAPIv3DeploymentAssetForm
	project         string
	idempotencyKey  string
	idempotencyOnce sync.Once
}

func (dr *DeploymentRequest) GetApiKey() string {
	return env.APIKey()
}

func (dr *DeploymentRequest) GetProjectSlug() string {
	return dr.project
}

// GetIdempotencyKey returns a unique key to identify a deployment request.
func (dr *DeploymentRequest) GetIdempotencyKey() string {
	dr.idempotencyOnce.Do(func() {
		dr.idempotencyKey = uuid.New().String()
	})
	return dr.idempotencyKey
}

func (dr *DeploymentRequest) GetOpenAPIv3Assets() []*deployments.AddOpenAPIv3DeploymentAssetForm {
	return dr.assets
}

type CreateDeploymentRequest struct {
	Config  *deplconfig.DeploymentConfig
	Project string
}

// CreateDeployment creates a remote deployment from the incoming sources.
func CreateDeployment(
	req *CreateDeploymentRequest,
) (*deployments.CreateDeploymentResult, error) {
	ctx := context.Background()
	assets, err := createAssetsForDeployment(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert sources to assets: %w", err)
	}

	client := api.NewDeploymentsClient()
	deploymentRequest := &DeploymentRequest{
		assets:  assets,
		config:  req.Config,
		project: req.Project,
	}

	result, err := client.CreateDeployment(deploymentRequest)
	if err != nil {
		return nil, fmt.Errorf("deployment creation failed: %w", err)
	}

	return result, nil
}

type CreateDeploymentFromFileRequest struct {
	FilePath string
	Project  string
}

func CreateDeploymentFromFile(
	req CreateDeploymentFromFileRequest,
) (*deployments.CreateDeploymentResult, error) {
	config, err := deplconfig.ReadDeploymentConfig(req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read deployment config: %w", err)
	}

	createReq := &CreateDeploymentRequest{
		Config:  config,
		Project: req.Project,
	}

	return CreateDeployment(createReq)
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

func (ur *uploadRequest) Read() (io.ReadCloser, int64, error) {
	reader, size, err := ur.sourceReader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source: %w", err)
	}
	return reader, size, nil
}
