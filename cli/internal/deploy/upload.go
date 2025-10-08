package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

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
	assetsClient *api.AssetsClient,
	req *UploadRequest,
) (*deployments.AddOpenAPIv3DeploymentAssetForm, error) {
	rc, length, err := req.SourceReader.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read source: %w", err)
	}

	source := req.SourceReader.Source

	uploadRes, err := assetsClient.UploadOpenAPIv3(ctx, &api.UploadOpenAPIv3Request{
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
