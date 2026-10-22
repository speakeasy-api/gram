package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// ToolsetInfo contains resolved MCP configuration information
type ToolsetInfo struct {
	Name       string
	URL        string
	HeaderName string
	EnvVarName string
	APIKey     string
}

// ResolverOptions contains options for resolving toolset information
type ResolverOptions struct {
	ProjectSlug     string
	ToolsetSlug     string
	MCPURL          string
	ServerName      string
	APIKey          secret.Secret
	HeaderName      string
	EnvVar          string
	APIURL          *url.URL
	Logger          *slog.Logger
	IsHeaderNameSet bool
	IsEnvVarSet     bool
}

// ResolveToolsetInfo resolves toolset information from either a slug (via API) or direct URL
func ResolveToolsetInfo(ctx context.Context, opts *ResolverOptions) (*ToolsetInfo, error) {
	info := &ToolsetInfo{
		Name:       "",
		URL:        "",
		HeaderName: opts.HeaderName,
		EnvVarName: opts.EnvVar,
		APIKey:     opts.APIKey.Reveal(),
	}

	// If toolset slug is provided, fetch from API
	if opts.ToolsetSlug != "" {
		// Create toolsets client
		toolsetsClient := api.NewToolsetsClient(&api.ToolsetsClientOptions{
			Scheme: opts.APIURL.Scheme,
			Host:   opts.APIURL.Host,
		})

		// Fetch toolset
		opts.Logger.InfoContext(ctx, "fetching toolset information", slog.String("slug", opts.ToolsetSlug))
		toolset, err := toolsetsClient.GetToolset(ctx, opts.APIKey, opts.ProjectSlug, opts.ToolsetSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch toolset: %w", err)
		}

		endpointsClient := api.NewMCPEndpointsClient(&api.MCPEndpointsClientOptions{
			Scheme: opts.APIURL.Scheme,
			Host:   opts.APIURL.Host,
		})
		info.URL, err = resolveHostedMCPURL(ctx, opts, endpointsClient, toolset)
		if err != nil {
			return nil, err
		}
		opts.Logger.InfoContext(ctx, "derived MCP URL from toolset", slog.String("url", info.URL))

		// Derive auth config from toolset if not explicitly provided
		if !opts.IsHeaderNameSet || !opts.IsEnvVarSet {
			derivedHeaderName, derivedEnvVar := deriveAuthConfig(toolset)
			if !opts.IsHeaderNameSet {
				info.HeaderName = derivedHeaderName
			}
			if !opts.IsEnvVarSet && derivedEnvVar != "" {
				info.EnvVarName = derivedEnvVar
				opts.Logger.InfoContext(ctx, "using environment variable from toolset",
					slog.String("var", info.EnvVarName))
			}
		}

		// Use toolset name if server name not provided
		if opts.ServerName == "" {
			info.Name = toolset.Name
			opts.Logger.InfoContext(ctx, "using toolset name as server name", slog.String("name", info.Name))
		} else {
			info.Name = opts.ServerName
		}
	} else {
		// Using direct MCP URL
		info.URL = opts.MCPURL

		// Derive server name from URL if not provided
		if opts.ServerName == "" {
			info.Name = deriveServerNameFromURL(info.URL)
			opts.Logger.InfoContext(ctx, "using derived server name", slog.String("name", info.Name))
		} else {
			info.Name = opts.ServerName
		}
	}

	// Validate the URL
	u, err := url.Parse(info.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid toolset URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("toolset URL must include scheme and host (e.g., https://mcp.getgram.ai/...)")
	}

	// Determine authentication
	if info.EnvVarName != "" {
		// Check if the environment variable is already set locally
		if envValue := os.Getenv(info.EnvVarName); envValue != "" {
			info.APIKey = envValue
			opts.Logger.InfoContext(ctx, "using API key from local environment variable",
				slog.String("var", info.EnvVarName))
			// Clear EnvVarName so we use the actual value, not substitution
			info.EnvVarName = ""
		} else {
			opts.Logger.InfoContext(ctx, "environment variable not set locally, will use substitution",
				slog.String("var", info.EnvVarName))
		}
	}

	return info, nil
}

const endpointLookupTimeout = 15 * time.Second

type mcpEndpointLister interface {
	ListMcpEndpoints(ctx context.Context, apiKey secret.Secret, projectSlug string, mcpServerID string) ([]*types.McpEndpoint, error)
}

// resolveHostedMCPURL reads the canonical MCP server's (id == toolset id) platform endpoint.
func resolveHostedMCPURL(ctx context.Context, opts *ResolverOptions, endpoints mcpEndpointLister, toolset *types.Toolset) (string, error) {
	baseURL := opts.APIURL.String()
	toolsetSlug := ""
	if toolset.McpSlug != nil {
		toolsetSlug = string(*toolset.McpSlug)
	}

	listCtx, cancel := context.WithTimeout(ctx, endpointLookupTimeout)
	defer cancel()
	list, err := endpoints.ListMcpEndpoints(listCtx, opts.APIKey, opts.ProjectSlug, toolset.ID)
	if err != nil {
		opts.Logger.DebugContext(ctx, "could not list MCP endpoints, using toolset MCP URL", slog.String("error", err.Error()))
	} else if slug, n := platformEndpointSlug(list, toolsetSlug); n > 1 {
		return "", fmt.Errorf("toolset %q has %d MCP endpoints; pass --mcp-url to choose one", toolset.Slug, n)
	} else if slug != "" {
		return fmt.Sprintf("%s/mcp/%s", baseURL, slug), nil
	}

	if toolsetSlug == "" {
		return "", fmt.Errorf("toolset %q has no MCP URL; enable its MCP server or pass --mcp-url", toolset.Slug)
	}
	return fmt.Sprintf("%s/mcp/%s", baseURL, toolsetSlug), nil
}

// platformEndpointSlug keeps the toolset's slug if reported, else the sole platform endpoint, plus the candidate count.
func platformEndpointSlug(endpoints []*types.McpEndpoint, toolsetSlug string) (string, int) {
	var candidates []string
	for _, endpoint := range endpoints {
		if endpoint == nil || endpoint.Slug == "" || (endpoint.CustomDomainID != nil && *endpoint.CustomDomainID != "") {
			continue
		}
		if toolsetSlug != "" {
			if string(endpoint.Slug) == toolsetSlug {
				return toolsetSlug, 1
			}
			continue
		}
		candidates = append(candidates, string(endpoint.Slug))
	}
	if len(candidates) != 1 {
		return "", len(candidates)
	}
	return candidates[0], 1
}

func deriveAuthConfig(toolset *types.Toolset) (headerName string, envVarName string) {
	// Default to standard Authorization header
	headerName = "Authorization"
	envVarName = ""

	// Check if there are security variables
	if len(toolset.SecurityVariables) == 0 {
		return
	}

	// Use the first security variable's environment variable if available
	secVar := toolset.SecurityVariables[0]
	if len(secVar.EnvVariables) > 0 {
		envVarName = secVar.EnvVariables[0]
	}

	return headerName, envVarName
}

const (
	// DefaultServerName names an installed MCP server when the URL gives no
	// better name.
	DefaultServerName = "speakeasy-mcp"

	// legacyServerName is the DefaultServerName of CLI releases before the
	// speakeasy rename.
	legacyServerName = "gram-mcp"
)

// LegacyServerName returns the name earlier CLI releases used for name, so an
// entry they installed is recognised and replaced rather than duplicated.
// Only the two default names changed in the rename: DefaultServerName and the
// Claude Desktop default DefaultServerName + "-server". Names derived from the
// MCP URL were the same before and after, so they never map.
func LegacyServerName(name string) (string, bool) {
	switch name {
	case DefaultServerName:
		return legacyServerName, true
	case DefaultServerName + "-server":
		return legacyServerName + "-server", true
	default:
		return "", false
	}
}

func deriveServerNameFromURL(mcpURL string) string {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return DefaultServerName
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 0 {
		return pathParts[len(pathParts)-1]
	}

	return DefaultServerName
}
