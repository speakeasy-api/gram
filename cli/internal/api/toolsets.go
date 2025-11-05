package api

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/gen/toolsets"
	toolsets_client "github.com/speakeasy-api/gram/server/gen/http/toolsets/client"
	"github.com/speakeasy-api/gram/server/gen/types"
	goahttp "goa.design/goa/v3/http"
)

type ToolsetsClientOptions struct {
	Scheme string
	Host   string
}

type ToolsetsClient struct {
	client *toolsets.Client
}

func NewToolsetsClient(options *ToolsetsClientOptions) *ToolsetsClient {
	doer := goaSharedHTTPClient

	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := false

	h := toolsets_client.NewClient(options.Scheme, options.Host, doer, enc, dec, restoreBody)

	client := toolsets.NewClient(
		h.CreateToolset(),
		h.ListToolsets(),
		h.UpdateToolset(),
		h.DeleteToolset(),
		h.GetToolset(),
		h.CheckMCPSlugAvailability(),
		h.CloneToolset(),
		h.AddExternalOAuthServer(),
		h.RemoveOAuthServer(),
	)

	return &ToolsetsClient{client: client}
}

func (c *ToolsetsClient) GetToolset(
	ctx context.Context,
	sessionToken string,
	projectSlug string,
	toolsetSlug string,
) (*types.Toolset, error) {
	slug := types.Slug(toolsetSlug)
	payload := &toolsets.GetToolsetPayload{
		SessionToken:     &sessionToken,
		ProjectSlugInput: &projectSlug,
		Slug:             slug,
	}

	result, err := c.client.GetToolset(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to get toolset: %w", err)
	}

	return result, nil
}
