package deploy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func isSupportedSourceType(source Source) bool {
	return source.Type == SourceTypeOpenAPIV3
}

func NewUploadRequest(source Source, project string) *uploadRequest {
	return &uploadRequest{
		sourceReader: NewSourceReader(source),
		project:      project,
	}
}

func uploadFromSource(
	ctx context.Context,
	logger *slog.Logger,
	source Source,
	project string,
) (*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	uploadReq := NewUploadRequest(source, project)
	uploadRes, err := api.NewAssetsClient().UploadOpenAPIv3(ctx, logger, uploadReq)

	if err != nil {
		msg := "failed to upload asset in project '%s' for source %s: %w"
		return nil, fmt.Errorf(msg, project, source.Location, err)
	}

	return &deployments.AddOpenAPIv3DeploymentAssetForm{
		AssetID: uploadRes.Asset.ID,
		Name:    source.Name,
		Slug:    types.Slug(source.Slug),
	}, nil

}

// createAssetsForDeployment creates remote assets out of each incoming source.
// The returned forms can be submitted to create a deployment.
func createAssetsForDeployment(
	ctx context.Context,
	logger *slog.Logger,
	req *CreateDeploymentRequest,
) ([]*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	sources := req.Config.Sources
	project := req.Project
	assets := make([]*deployments.AddOpenAPIv3DeploymentAssetForm, 0, len(sources))

	for _, source := range sources {
		if !isSupportedSourceType(source) {
			msg := "skipping unsupported source type"
			logger.WarnContext(ctx, msg, slog.String("type", string(source.Type)), slog.String("location", source.Location))
			continue
		}

		asset, err := uploadFromSource(ctx, logger, source, project)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("no valid sources found")
	}

	return assets, nil
}
