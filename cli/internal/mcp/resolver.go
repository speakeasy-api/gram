package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/profile"
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
	ToolsetSlug     string
	MCPURL          string
	ServerName      string
	APIKey          string
	HeaderName      string
	EnvVar          string
	Profile         *profile.Profile
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
		APIKey:     opts.APIKey,
	}

	// If toolset slug is provided, fetch from API
	if opts.ToolsetSlug != "" {
		if opts.Profile == nil || opts.Profile.Secret == "" {
			return nil, fmt.Errorf("profile not configured; run 'gram auth' first to use --toolset")
		}

		// Create toolsets client
		toolsetsClient := api.NewToolsetsClient(&api.ToolsetsClientOptions{
			Scheme: opts.APIURL.Scheme,
			Host:   opts.APIURL.Host,
		})

		// Fetch toolset
		opts.Logger.InfoContext(ctx, "fetching toolset information", slog.String("slug", opts.ToolsetSlug))
		toolset, err := toolsetsClient.GetToolset(ctx, secret.Secret(opts.Profile.Secret), opts.Profile.DefaultProjectSlug, opts.ToolsetSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch toolset: %w", err)
		}

		// Construct MCP URL from toolset
		info.URL = constructMCPURL(toolset, opts.APIURL.String())
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

	// If still no API key, try to get from other sources
	if info.APIKey == "" && info.EnvVarName == "" {
		// Use profile API key
		if opts.Profile != nil && opts.Profile.Secret != "" {
			info.APIKey = opts.Profile.Secret
			opts.Logger.InfoContext(ctx, "using API key from profile")
		}

		if info.APIKey == "" {
			return nil, fmt.Errorf("no API key provided and no profile configured (run 'gram auth' first or provide --api-key or --env-var)")
		}
	}

	return info, nil
}

func constructMCPURL(toolset *types.Toolset, baseURL string) string {
	// If toolset has a custom MCP slug, use it
	if toolset.McpSlug != nil && *toolset.McpSlug != "" {
		return fmt.Sprintf("%s/mcp/%s", baseURL, *toolset.McpSlug)
	}

	// Otherwise construct from org/project/environment
	return fmt.Sprintf("%s/mcp/%s/%s/%s",
		baseURL,
		toolset.OrganizationID,
		toolset.ProjectID,
		*toolset.DefaultEnvironmentSlug)
}

func deriveAuthConfig(toolset *types.Toolset) (headerName string, envVarName string) {
	// Default values
	headerName = "Gram-Apikey"
	envVarName = ""

	// Check if there are security variables
	if len(toolset.SecurityVariables) == 0 {
		return
	}

	// Use the first security variable to determine auth config
	secVar := toolset.SecurityVariables[0]

	// Derive header name from the security variable name
	if secVar.Name != "" {
		headerName = strings.ReplaceAll(secVar.Name, "_", "-")
		parts := strings.Split(headerName, "-")
		for i, part := range parts {
			if len(part) > 0 {
				parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
			}
		}
		headerName = strings.Join(parts, "-")
	}

	// Use the first environment variable name if available
	if len(secVar.EnvVariables) > 0 {
		envVarName = secVar.EnvVariables[0]
	}

	return headerName, envVarName
}

func deriveServerNameFromURL(mcpURL string) string {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return "gram-mcp"
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 0 {
		return pathParts[len(pathParts)-1]
	}

	return "gram-mcp"
}
