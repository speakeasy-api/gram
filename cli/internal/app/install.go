package app

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/claudecode"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/urfave/cli/v2"
)

func newInstallCommand() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "Install Gram toolsets as MCP servers in various clients",
		Subcommands: []*cli.Command{
			newInstallClaudeCodeCommand(),
		},
	}
}

func newInstallClaudeCodeCommand() *cli.Command {
	return &cli.Command{
		Name:  "claude-code",
		Usage: "Install a Gram toolset as an MCP server in Claude Code",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "toolset",
				Usage: "The slug of the Gram toolset to install (e.g., speakeasy-admin). Will automatically look up MCP configuration.",
			},
			&cli.StringFlag{
				Name:  "toolset-url",
				Usage: "The full MCP URL of the toolset (e.g., https://mcp.getgram.ai/org/project/environment). Use this for manual configuration.",
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "The name to use for this MCP server in Claude Code (defaults to toolset name or derived from URL)",
			},
			&cli.StringFlag{
				Name:  "api-key",
				Usage: "The API key to use for authentication (falls back to profile API key if not provided)",
			},
			&cli.StringFlag{
				Name:  "header-name",
				Usage: "The HTTP header name for the API key (defaults to Gram-Apikey)",
				Value: "Gram-Apikey",
			},
			&cli.StringFlag{
				Name:  "env-var",
				Usage: "Environment variable name to use for API key substitution (e.g., MCP_API_KEY). If provided, uses ${VAR} syntax instead of hardcoding the key",
			},
			&cli.StringFlag{
				Name:  "config-path",
				Usage: "Path to the Claude Code config file (defaults to project-local .mcp.json)",
			},
		},
		Action: doInstallClaudeCode,
	}
}

func deriveServerName(mcpURL string) string {
	// Parse the URL to extract meaningful parts
	u, err := url.Parse(mcpURL)
	if err != nil {
		return "gram-mcp"
	}

	// Extract the last meaningful part of the path
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 0 {
		// Use the last part of the path as the name
		return pathParts[len(pathParts)-1]
	}

	return "gram-mcp"
}

// constructMCPURL builds the MCP URL from a toolset
func constructMCPURL(toolset *types.Toolset, baseURL string) string {
	// If toolset has a custom MCP slug, use it
	if toolset.McpSlug != nil && *toolset.McpSlug != "" {
		return fmt.Sprintf("%s/mcp/%s", baseURL, *toolset.McpSlug)
	}

	// Otherwise construct from org/project/environment
	// The baseURL should be from the profile or API endpoint
	// Format: {base}/mcp/{org-slug}/{project-slug}/{environment-slug}
	// But we don't have org slug readily available, so we'll use the standard path format
	return fmt.Sprintf("%s/mcp/%s/%s/%s",
		baseURL,
		toolset.OrganizationID,
		toolset.ProjectID,
		*toolset.DefaultEnvironmentSlug)
}

// deriveAuthConfig determines the authentication configuration from a toolset's security variables
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
	// Convert to HTTP header format (e.g., "api_key" -> "Api-Key")
	if secVar.Name != "" {
		// Replace underscores with hyphens and title case
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

func doInstallClaudeCode(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)

	toolsetSlug := c.String("toolset")
	toolsetURL := c.String("toolset-url")
	serverName := c.String("name")
	apiKey := c.String("api-key")
	headerName := c.String("header-name")
	envVar := c.String("env-var")
	configPath := c.String("config-path")

	// Validate that either toolset or toolset-url is provided
	if toolsetSlug == "" && toolsetURL == "" {
		return fmt.Errorf("either --toolset or --toolset-url must be provided")
	}
	if toolsetSlug != "" && toolsetURL != "" {
		return fmt.Errorf("cannot provide both --toolset and --toolset-url")
	}

	// If toolset is provided, fetch toolset information from API
	if toolsetSlug != "" {
		if prof == nil || prof.Secret == "" {
			return fmt.Errorf("profile not configured; run 'gram auth' first to use --toolset")
		}

		// Get API URL from profile
		apiURL, err := workflow.ResolveURL(c, prof)
		if err != nil {
			return fmt.Errorf("failed to resolve API URL: %w", err)
		}

		// Create toolsets client
		toolsetsClient := api.NewToolsetsClient(&api.ToolsetsClientOptions{
			Scheme: apiURL.Scheme,
			Host:   apiURL.Host,
		})

		// Fetch toolset
		logger.InfoContext(ctx, "fetching toolset information", slog.String("slug", toolsetSlug))
		toolset, err := toolsetsClient.GetToolset(ctx, prof.Secret, prof.DefaultProjectSlug, toolsetSlug)
		if err != nil {
			return fmt.Errorf("failed to fetch toolset: %w", err)
		}

		// Construct MCP URL from toolset
		toolsetURL = constructMCPURL(toolset, apiURL.String())
		logger.InfoContext(ctx, "derived MCP URL from toolset", slog.String("url", toolsetURL))

		// Derive auth config from toolset if not explicitly provided
		if !c.IsSet("header-name") || !c.IsSet("env-var") {
			derivedHeaderName, derivedEnvVar := deriveAuthConfig(toolset)
			if !c.IsSet("header-name") {
				headerName = derivedHeaderName
			}
			if !c.IsSet("env-var") && derivedEnvVar != "" {
				envVar = derivedEnvVar
				logger.InfoContext(ctx, "using environment variable from toolset",
					slog.String("var", envVar))
			}
		}

		// Use toolset name if server name not provided
		if serverName == "" {
			serverName = toolset.Name
			logger.InfoContext(ctx, "using toolset name as server name", slog.String("name", serverName))
		}
	}

	// Validate the toolset URL
	u, err := url.Parse(toolsetURL)
	if err != nil {
		return fmt.Errorf("invalid toolset URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("toolset URL must include scheme and host (e.g., https://mcp.getgram.ai/...)")
	}

	// Derive server name from URL if not provided
	if serverName == "" {
		serverName = deriveServerName(toolsetURL)
		logger.InfoContext(ctx, "using derived server name", slog.String("name", serverName))
	}

	// Determine authentication method
	useEnvVar := envVar != ""
	if !useEnvVar {
		// Use profile API key if not provided
		if apiKey == "" && prof != nil && prof.Secret != "" {
			apiKey = prof.Secret
			logger.InfoContext(ctx, "using API key from profile")
		}

		if apiKey == "" {
			return fmt.Errorf("no API key provided and no profile configured (run 'gram auth' first or provide --api-key or --env-var)")
		}
	}

	// Determine config path
	if configPath == "" {
		locations, err := claudecode.GetConfigLocations()
		if err != nil {
			return fmt.Errorf("failed to get config locations: %w", err)
		}
		// Default to project-local .mcp.json
		configPath = locations[0].Path
		logger.InfoContext(ctx, "using config location",
			slog.String("path", configPath),
			slog.String("type", locations[0].Description))
	}

	// Read existing config
	config, err := claudecode.ReadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Check if server already exists
	if _, exists := config.MCPServers[serverName]; exists {
		logger.WarnContext(ctx, "server with this name already exists, will be overwritten",
			slog.String("name", serverName))
	}

	// Construct the MCP server configuration
	// The command uses npx to run mcp-remote which connects to Gram's HTTP MCP server
	var headerValue string
	var envConfig map[string]string

	if useEnvVar {
		// Use environment variable substitution
		headerValue = fmt.Sprintf("%s:${%s}", headerName, envVar)
		envConfig = map[string]string{
			envVar: "<your-value-here>",
		}
		logger.InfoContext(ctx, "using environment variable substitution",
			slog.String("var", envVar),
			slog.String("header", headerName))
	} else {
		// Use API key directly
		headerValue = fmt.Sprintf("%s:%s", headerName, apiKey)
		envConfig = map[string]string{}
	}

	serverConfig := claudecode.MCPServerConfig{
		Command: "npx",
		Args: []string{
			"-y",
			"mcp-remote",
			toolsetURL,
			"--header",
			headerValue,
		},
		Env: envConfig,
	}

	// Add or update the server
	config.AddOrUpdateServer(serverName, serverConfig)

	// Write the config back
	if err := claudecode.WriteConfig(configPath, config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	logger.InfoContext(ctx, "successfully installed Gram MCP server",
		slog.String("name", serverName),
		slog.String("url", toolsetURL),
		slog.String("config", configPath))

	fmt.Printf("\n✓ Successfully installed MCP server '%s'\n", serverName)
	fmt.Printf("  URL: %s\n", toolsetURL)
	fmt.Printf("  Config: %s\n", configPath)

	if useEnvVar {
		fmt.Printf("\n⚠ Remember to set the environment variable before using:\n")
		fmt.Printf("  export %s='your-api-key-value'\n", envVar)
	}

	fmt.Printf("\nRestart Claude Code to load the new MCP server.\n")

	return nil
}
