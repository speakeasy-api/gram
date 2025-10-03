package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func isSupportedSourceType(source Source) bool {
	return source.Type == SourceTypeOpenAPIV3
}

type UploadRequest struct {
	APIKey       secret.Secret
	ProjectSlug  string
	SourceReader *SourceReader
}

func (ur *UploadRequest) Read(ctx context.Context) (io.ReadCloser, int64, error) {
	reader, size, err := ur.SourceReader.Read(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source: %w", err)
	}
	return reader, size, nil
}

func Upload(
	ctx context.Context,
	logger *slog.Logger,
	assetsClient *api.AssetsClient,
	req *UploadRequest,
) (*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	rc, length, err := req.SourceReader.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read source: %w", err)
	}

	source := req.SourceReader.Source

	uploadRes, err := assetsClient.UploadOpenAPIv3(ctx, logger, &api.UploadOpenAPIv3Request{
		APIKey:        req.APIKey.Reveal(),
		ProjectSlug:   req.ProjectSlug,
		Reader:        rc,
		ContentType:   req.SourceReader.GetContentType(),
		ContentLength: length,
	})
	if err != nil {
		msg := "failed to upload asset in project '%s' for source %s: %w"
		return nil, fmt.Errorf(msg, req.ProjectSlug, source.Location, err)
	}

	return &deployments.AddOpenAPIv3DeploymentAssetForm{
		AssetID: uploadRes.Asset.ID,
		Name:    source.Name,
		Slug:    types.Slug(source.Slug),
	}, nil

}

// AddAssetsRequest lists the assets to add to a deployment.
type AddAssetsRequest struct {
	APIKey       secret.Secret
	ProjectSlug  string
	DeploymentID string
	Sources      []Source
}

// AddAssets uploads assets and adds them to an existing deployment.
func AddAssets(
	ctx context.Context,
	logger *slog.Logger,
	assetsClient *api.AssetsClient,
	deploymentsClient *api.DeploymentsClient,
	req AddAssetsRequest,
) (*deployments.EvolveResult, error) {
	newAssets := make(
		[]*deployments.AddOpenAPIv3DeploymentAssetForm,
		len(req.Sources),
	)
	for idx, source := range req.Sources {
		asset, err := Upload(ctx, logger, assetsClient, &UploadRequest{
			APIKey:       req.APIKey,
			ProjectSlug:  req.ProjectSlug,
			SourceReader: NewSourceReader(source),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to upload asset: %w", err)
		}

		newAssets[idx] = asset
	}

	result, err := deploymentsClient.Evolve(ctx, api.EvolveRequest{
		Assets:       newAssets,
		APIKey:       req.APIKey,
		ProjectSlug:  req.ProjectSlug,
		DeploymentID: req.DeploymentID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to evolve deployment: %w", err)
	}

	return result, nil
}

// createAssetsForDeployment creates remote assets out of each incoming source.
// The returned forms can be submitted to create a deployment.
func createAssetsForDeployment(
	ctx context.Context,
	logger *slog.Logger,
	assetsClient *api.AssetsClient,
	req *CreateDeploymentRequest,
) ([]*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	sources := req.Config.Sources
	project := req.ProjectSlug
	assets := make([]*deployments.AddOpenAPIv3DeploymentAssetForm, 0, len(sources))

	for _, source := range sources {
		if !isSupportedSourceType(source) {
			msg := "skipping unsupported source type"
			logger.WarnContext(ctx, msg, slog.String("type", string(source.Type)), slog.String("location", source.Location))
			continue
		}

		asset, err := Upload(ctx, logger, assetsClient, &UploadRequest{
			APIKey:       req.APIKey,
			ProjectSlug:  project,
			SourceReader: NewSourceReader(source),
		})
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
