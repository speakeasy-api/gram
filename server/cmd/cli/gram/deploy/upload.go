package deploy

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/api"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/deplconfig"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/log"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func isSupportedSourceType(source deplconfig.Source) bool {
	return source.Type == deplconfig.SourceTypeOpenAPIV3
}

func NewUploadRequest(source deplconfig.Source, project string) *uploadRequest {
	return &uploadRequest{
		sourceReader: deplconfig.NewSourceReader(source),
		project:      project,
	}
}

func uploadFromSource(
	source deplconfig.Source,
	project string,
) (*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	uploadReq := NewUploadRequest(source, project)
	uploadRes, err := api.NewAssetsClient().CreateAsset(uploadReq)

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
	req *CreateDeploymentRequest,
) ([]*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	sources := req.Config.Sources
	project := req.Project
	assets := make([]*deployments.AddOpenAPIv3DeploymentAssetForm, 0, len(sources))

	for _, source := range sources {
		if !isSupportedSourceType(source) {
			msg := "skipping unsupported source type"
			log.L.WarnContext(ctx, msg, logKeyType, source.Type, logKeyLocation, source.Location)
			continue
		}

		asset, err := uploadFromSource(source, project)
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
