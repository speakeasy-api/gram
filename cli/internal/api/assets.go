package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/speakeasy-api/gram/server/gen/assets"
	assets_client "github.com/speakeasy-api/gram/server/gen/http/assets/client"
	goahttp "goa.design/goa/v3/http"
)

type AssetsClientOptions struct {
	Scheme string
	Host   string
}

type AssetsClient struct {
	client *assets.Client
}

func NewAssetsClient(options *AssetsClientOptions) *AssetsClient {
	doer := goaSharedHTTPClient

	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := false

	h := assets_client.NewClient(options.Scheme, options.Host, doer, enc, dec, restoreBody)

	client := assets.NewClient(
		h.ServeImage(),
		h.UploadImage(),
		h.UploadFunctions(),
		h.UploadOpenAPIv3(),
		h.ServeOpenAPIv3(),
		h.ListAssets(),
	)

	return &AssetsClient{client: client}
}

// SourceReader defines the interface for reading source content.
type SourceReader interface {
	// GetType returns the type of the source (e.g., "openapiv3").
	GetType() string
	// GetContentType returns the MIME type of the content (e.g.,
	// "application/json", "application/yaml").
	GetContentType() string
	// Read returns a reader for the asset content and its size.
	Read(context.Context) (io.ReadCloser, int64, error)
}

// AssetCreator represents a source for creating an asset, composed of
// credential access and source reading.
type AssetCreator interface {
	SourceReader
}

type UploadOpenAPIv3Request struct {
	APIKey        string
	ProjectSlug   string
	Reader        io.ReadCloser
	ContentType   string
	ContentLength int64
}

func (c *AssetsClient) UploadOpenAPIv3(
	ctx context.Context,
	logger *slog.Logger,
	req *UploadOpenAPIv3Request,
) (*assets.UploadOpenAPIv3Result, error) {
	payload := &assets.UploadOpenAPIv3Form{
		ApikeyToken:      &req.APIKey,
		ProjectSlugInput: &req.ProjectSlug,
		SessionToken:     nil,
		ContentType:      req.ContentType,
		ContentLength:    req.ContentLength,
	}

	result, err := c.client.UploadOpenAPIv3(ctx, payload, req.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to upload OpenAPI asset: %w", err)
	}

	return result, nil
}
