package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/api"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/deplconfig"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/env"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

const (
	logKeyType     = "type"
	logKeyLocation = "location"
)

type DeploymentRequest struct {
	config  *deplconfig.DeploymentConfig
	assets  []*deployments.AddOpenAPIv3DeploymentAssetForm
	project string
}

func (dr *DeploymentRequest) GetApiKey() string {
	return env.APIKey()
}

func (dr *DeploymentRequest) GetProjectSlug() string {
	return dr.project
}

// GetIdempotencyKey returns a unique key to identify a deployment request.
func (dr *DeploymentRequest) GetIdempotencyKey() string {
	return uuid.New().String()
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
	assets, err := convertSourcesToAssets(ctx, req)
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

type createAssetRequest struct {
	sourceReader *deplconfig.SourceReader
	project      string
}

func (sac *createAssetRequest) GetApiKey() string {
	return env.APIKey()
}

func (sac *createAssetRequest) GetProjectSlug() string {
	return sac.project
}

func (sac *createAssetRequest) GetType() string {
	return sac.sourceReader.GetType()
}

func (sac *createAssetRequest) GetContentType() string {
	return sac.sourceReader.GetContentType()
}

func (sac *createAssetRequest) Read() (io.ReadCloser, int64, error) {
	reader, size, err := sac.sourceReader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source: %w", err)
	}
	return reader, size, nil
}

// convertSourcesToAssets creates remote assets out of each incoming source. The
// returned forms can be submitted to create a deployment.
func convertSourcesToAssets(
	ctx context.Context,
	req *CreateDeploymentRequest,
) ([]*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sources := req.Config.Sources
	project := req.Project

	assetsClient := api.NewAssetsClient()
	assets := make([]*deployments.AddOpenAPIv3DeploymentAssetForm, 0, len(sources))

	for _, source := range sources {
		if source.Type != deplconfig.SourceTypeOpenAPIV3 {
			logger.WarnContext(ctx, "skipping unsupported source type", slog.Any(logKeyType, source.Type), slog.Any(logKeyLocation, source.Location))
			continue
		}

		sourceReader := deplconfig.NewSourceReader(source)
		assetCreator := &createAssetRequest{
			sourceReader: sourceReader,
			project:      project,
		}

		uploadResult, err := assetsClient.CreateAsset(assetCreator)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to upload asset in project '%s' for source %s: %w",
				project, source.Location, err,
			)
		}

		asset := &deployments.AddOpenAPIv3DeploymentAssetForm{
			AssetID: uploadResult.Asset.ID,
			Name:    source.Name,
			Slug:    types.Slug(source.Slug),
		}

		assets = append(assets, asset)
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("no valid OpenAPI v3 sources found")
	}

	return assets, nil
}
