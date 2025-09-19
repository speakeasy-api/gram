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

type uploadRequest struct {
	apiKey       secret.Secret
	projectSlug  string
	sourceReader *SourceReader
}

func (ur *uploadRequest) Read(ctx context.Context) (io.ReadCloser, int64, error) {
	reader, size, err := ur.sourceReader.Read(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source: %w", err)
	}
	return reader, size, nil
}

func uploadFromSource(
	ctx context.Context,
	logger *slog.Logger,
	assetsClient *api.AssetsClient,
	req *uploadRequest,
) (*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	rc, length, err := req.sourceReader.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read source: %w", err)
	}

	source := req.sourceReader.source

	uploadRes, err := assetsClient.UploadOpenAPIv3(ctx, logger, &api.UploadOpenAPIv3Request{
		APIKey:        req.apiKey.Reveal(),
		ProjectSlug:   req.projectSlug,
		Reader:        rc,
		ContentType:   req.sourceReader.GetContentType(),
		ContentLength: length,
	})
	if err != nil {
		msg := "failed to upload asset in project '%s' for source %s: %w"
		return nil, fmt.Errorf(msg, req.projectSlug, source.Location, err)
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

		asset, err := uploadFromSource(ctx, logger, assetsClient, &uploadRequest{
			apiKey:       req.APIKey,
			projectSlug:  project,
			sourceReader: NewSourceReader(source),
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
