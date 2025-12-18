package externalmcp

import (
	"net/url"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ExternalMCPOAuthConfig contains OAuth configuration extracted from an external MCP tool
// that requires OAuth authentication.
type ExternalMCPOAuthConfig struct {
	// RemoteURL is the parsed URL of the external MCP server
	RemoteURL *url.URL
	// RegistryID is the ID of the MCP registry the server belongs to
	RegistryID string
	// Slug is the tool prefix slug (e.g., "github")
	Slug string
	// Name is the reverse-DNS server name (e.g., "ai.exa/exa")
	Name string
}

// WellKnownOAuthServerURL returns the .well-known/oauth-authorization-server URL
func (c *ExternalMCPOAuthConfig) WellKnownOAuthServerURL() string {
	return c.RemoteURL.Scheme + "://" + c.RemoteURL.Host + "/.well-known/oauth-authorization-server"
}

// ResolveOAuthConfig returns the OAuth configuration from the first external MCP tool
// in the toolset that requires OAuth, or nil if none found.
func ResolveOAuthConfig(toolset *types.Toolset) *ExternalMCPOAuthConfig {
	for _, tool := range toolset.Tools {
		if !conv.IsProxyTool(tool) || !tool.ExternalMcpToolDefinition.RequiresOauth {
			continue
		}

		remoteURL, err := url.Parse(tool.ExternalMcpToolDefinition.RemoteURL)
		if err != nil {
			continue
		}

		return &ExternalMCPOAuthConfig{
			RemoteURL:  remoteURL,
			RegistryID: tool.ExternalMcpToolDefinition.RegistryID,
			Slug:       tool.ExternalMcpToolDefinition.Slug,
			Name:       tool.ExternalMcpToolDefinition.Name,
		}
	}

	return nil
}
