package platformmcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxSetupResourceBytes = 32 * 1024

// SetupResource is a reviewed, static setup guide. It is intentionally supplied
// only by explicit composition, never fetched from a provider or documentation
// service at request time.
type SetupResource struct {
	URI         string
	Name        string
	Title       string
	Description string
	Text        string
}

func registerSetupResources(reg *Registrar, resources []SetupResource) {
	server := reg.server
	for _, resource := range resources {
		if !validSetupResource(resource) {
			continue
		}
		resource := resource
		server.AddResource(&mcp.Resource{ //nolint:exhaustruct // MCP SDK metadata and annotations are intentionally omitted.
			URI:         resource.URI,
			Name:        resource.Name,
			Title:       resource.Title,
			Description: resource.Description,
			MIMEType:    "text/markdown",
			Size:        int64(len(resource.Text)),
		}, func(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			if request.Params.URI != resource.URI {
				return nil, mcp.ResourceNotFoundError(request.Params.URI)
			}
			return &mcp.ReadResourceResult{ //nolint:exhaustruct // MCP SDK metadata is intentionally omitted.
				Contents: []*mcp.ResourceContents{{
					URI:      resource.URI,
					MIMEType: "text/markdown",
					Text:     resource.Text,
				}}}, nil
		})
	}
}

func validSetupResource(resource SetupResource) bool {
	parsed, err := url.Parse(resource.URI)
	return err == nil && parsed.Scheme == "gram" && parsed.Host == "platform-mcp" && strings.HasPrefix(resource.URI, "gram://platform-mcp/setup/") && resource.Name != "" && resource.Title != "" && resource.Description != "" && resource.Text != "" && len(resource.Text) <= maxSetupResourceBytes && resource.URI == setupResourceURIFromURI(resource.URI)
}

func setupResourceURI(provider, intent string) string {
	return fmt.Sprintf("gram://platform-mcp/setup/%s/%s", provider, intent)
}

func setupResourceURIFromURI(uri string) string {
	parts := strings.Split(strings.TrimPrefix(uri, "gram://platform-mcp/setup/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return setupResourceURI(parts[0], parts[1])
}
