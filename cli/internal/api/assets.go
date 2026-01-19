package api

import (
	"context"
	"fmt"
	"io"

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
		h.FetchOpenAPIv3FromURL(),
		h.ServeOpenAPIv3(),
		h.ServeFunction(),
		h.ListAssets(),
		h.UploadChatAttachment(),
		h.ServeChatAttachment(),
		h.CreateSignedChatAttachmentURL(),
		h.ServeChatAttachmentSigned(),
	)

	return &AssetsClient{client: client}
}

type UploadAssetForm struct {
	APIKey        string
	ProjectSlug   string
	Reader        io.ReadCloser
	ContentType   string
	ContentLength int64
}

func (c *AssetsClient) UploadOpenAPIv3(
	ctx context.Context,
	req *UploadAssetForm,
) (*assets.Asset, error) {
	payload := &assets.UploadOpenAPIv3Form{
		ApikeyToken:      &req.APIKey,
		ProjectSlugInput: &req.ProjectSlug,
		SessionToken:     nil,
		ContentType:      req.ContentType,
		ContentLength:    req.ContentLength,
	}

	result, err := c.client.UploadOpenAPIv3(ctx, payload, req.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to upload OpenAPI source: %w", err)
	}

	return result.Asset, nil
}

func (c *AssetsClient) UploadFunctions(
	ctx context.Context,
	req *UploadAssetForm,
) (*assets.Asset, error) {
	payload := &assets.UploadFunctionsForm{
		ApikeyToken:      &req.APIKey,
		ProjectSlugInput: &req.ProjectSlug,
		SessionToken:     nil,
		ContentType:      req.ContentType,
		ContentLength:    req.ContentLength,
	}

	result, err := c.client.UploadFunctions(ctx, payload, req.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to upload Functions source: %w", err)
	}

	return result.Asset, nil
}
