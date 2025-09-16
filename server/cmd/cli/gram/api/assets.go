package api

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/deplconfig"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/env"

	"github.com/speakeasy-api/gram/server/gen/assets"
	assets_client "github.com/speakeasy-api/gram/server/gen/http/assets/client"
	goahttp "goa.design/goa/v3/http"
)

type AssetsClient struct {
	client *assets.Client
}

func NewAssetsClient() *AssetsClient {
	return &AssetsClient{
		client: newAssetsClient(),
	}
}

func (c *AssetsClient) ListAssets(
	apiKey string, projectSlug string,
) *assets.ListAssetsResult {
	ctx := context.Background()
	payload := &assets.ListAssetsPayload{
		ApikeyToken:      &apiKey,
		ProjectSlugInput: &projectSlug,
		SessionToken:     nil,
	}

	result, err := c.client.ListAssets(ctx, payload)
	if err != nil {
		log.Fatalf("Error calling ListAssets: %v", err)
	}

	return result
}

// SourceReader defines the interface for reading source content.
type SourceReader interface {
	// GetType returns the type of the source (e.g., "openapiv3").
	GetType() string
	// GetContentType returns the MIME type of the content (e.g.,
	// "application/json", "application/yaml").
	GetContentType() string
	// Read returns a reader for the asset content and its size.
	Read() (io.ReadCloser, int64, error)
}

// AssetCreator represents a source for creating an asset, composed of
// credential access and source reading.
type AssetCreator interface {
	CredentialGetter
	SourceReader
}

func (c *AssetsClient) CreateAsset(
	ac AssetCreator,
) (*assets.UploadOpenAPIv3Result, error) {
	ctx := context.Background()

	// TODO(cj): This will support other types later.
	if !isOpenAPIV3(ac) {
		return nil, fmt.Errorf(
			"unsupported source type: '%s', expected '%s'",
			ac.GetType(), deplconfig.SourceTypeOpenAPIV3,
		)
	}

	reader, contentLength, err := ac.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read source: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			log.Printf("Error closing reader: %v", closeErr)
		}
	}()

	apiKey := ac.GetApiKey()
	projectSlug := ac.GetProjectSlug()

	payload := &assets.UploadOpenAPIv3Form{
		ApikeyToken:      &apiKey,
		ProjectSlugInput: &projectSlug,
		SessionToken:     nil,
		ContentType:      ac.GetContentType(),
		ContentLength:    contentLength,
	}

	result, err := c.client.UploadOpenAPIv3(ctx, payload, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to upload OpenAPI asset: %w", err)
	}

	return result, nil
}

func isOpenAPIV3(a AssetCreator) bool {
	return a.GetType() == string(deplconfig.SourceTypeOpenAPIV3)
}

func newAssetsClient() *assets.Client {
	h := assetsService()
	return assets.NewClient(
		h.ServeImage(),
		h.UploadImage(),
		h.UploadFunctions(),
		h.UploadOpenAPIv3(),
		h.ServeOpenAPIv3(),
		h.ListAssets(),
	)
}

func assetsService() *assets_client.Client {
	doer := goaSharedHTTPClient

	scheme := env.APIScheme()
	host := env.APIHost()
	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := false

	return assets_client.NewClient(scheme, host, doer, enc, dec, restoreBody)
}
