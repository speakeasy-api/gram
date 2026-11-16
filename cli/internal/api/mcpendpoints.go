package api

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/cli/internal/secret"
	mcp_endpoints_client "github.com/speakeasy-api/gram/server/gen/http/mcp_endpoints/client"
	mcpendpoints "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	goahttp "goa.design/goa/v3/http"
)

type MCPEndpointsClientOptions struct {
	Scheme string
	Host   string
}

type MCPEndpointsClient struct {
	client *mcpendpoints.Client
}

func NewMCPEndpointsClient(options *MCPEndpointsClientOptions) *MCPEndpointsClient {
	h := mcp_endpoints_client.NewClient(options.Scheme, options.Host, goaSharedHTTPClient, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)

	client := mcpendpoints.NewClient(
		h.CreateMcpEndpoint(),
		h.GetMcpEndpoint(),
		h.ListMcpEndpoints(),
		h.UpdateMcpEndpoint(),
		h.CheckMcpEndpointSlugAvailability(),
		h.DeleteMcpEndpoint(),
	)

	return &MCPEndpointsClient{client: client}
}

// ListMcpEndpoints lists the endpoints addressing an MCP server.
func (c *MCPEndpointsClient) ListMcpEndpoints(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
	mcpServerID string,
) ([]*types.McpEndpoint, error) {
	key := apiKey.Reveal()
	result, err := c.client.ListMcpEndpoints(ctx, &mcpendpoints.ListMcpEndpointsPayload{
		McpServerID:      &mcpServerID,
		MetaMcpServerID:  nil,
		SessionToken:     nil,
		ApikeyToken:      &key,
		ProjectSlugInput: &projectSlug,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list mcp endpoints: %w", err)
	}

	return result.McpEndpoints, nil
}
