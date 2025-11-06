package app

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/claudecode"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

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
		},
		Action: doInstallClaudeCode,
	}
}

func doInstallClaudeCode(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)

	toolsetSlug := c.String("toolset")
	toolsetURL := c.String("toolset-url")

	// Validate that either toolset or toolset-url is provided
	if toolsetSlug == "" && toolsetURL == "" {
		return fmt.Errorf("either --toolset or --toolset-url must be provided")
	}
	if toolsetSlug != "" && toolsetURL != "" {
		return fmt.Errorf("cannot provide both --toolset and --toolset-url")
	}

	// Get API URL if needed
	var apiURL *url.URL
	if toolsetSlug != "" {
		var err error
		apiURL, err = workflow.ResolveURL(c, prof)
		if err != nil {
			return fmt.Errorf("failed to resolve API URL: %w", err)
		}
	}

	// Resolve toolset information using shared logic
	info, err := mcp.ResolveToolsetInfo(ctx, &mcp.ResolverOptions{
		ToolsetSlug:     toolsetSlug,
		ToolsetURL:      toolsetURL,
		ServerName:      c.String("name"),
		APIKey:          c.String("api-key"),
		HeaderName:      c.String("header-name"),
		EnvVar:          c.String("env-var"),
		Profile:         prof,
		APIURL:          apiURL,
		Logger:          logger,
		IsHeaderNameSet: c.IsSet("header-name"),
		IsEnvVarSet:     c.IsSet("env-var"),
	})
	if err != nil {
		return fmt.Errorf("failed to resolve toolset info: %w", err)
	}

	useEnvVar := info.EnvVarName != ""
	if useEnvVar {
		logger.InfoContext(ctx, "using environment variable substitution",
			slog.String("var", info.EnvVarName),
			slog.String("header", info.HeaderName))
	}

	// Try to use native claude CLI with HTTP transport first
	if mcp.IsClaudeCLIAvailable() {
		logger.InfoContext(ctx, "using claude CLI with native HTTP transport")

		if err := mcp.InstallViaClaudeCLI(info, useEnvVar); err != nil {
			logger.WarnContext(ctx, "claude CLI installation failed, falling back to config file",
				slog.String("error", err.Error()))
		} else {
			// Success with claude CLI
			logger.InfoContext(ctx, "successfully installed via claude CLI",
				slog.String("name", info.Name),
				slog.String("url", info.URL))

			fmt.Printf("\n✓ Successfully installed MCP server '%s' via claude CLI\n", info.Name)
			fmt.Printf("  URL: %s\n", info.URL)
			fmt.Printf("  Transport: HTTP (native)\n")

			if useEnvVar {
				fmt.Printf("\n⚠ Remember to set the environment variable:\n")
				fmt.Printf("  export %s='your-api-key-value'\n", info.EnvVarName)
			}

			return nil
		}
	} else {
		logger.InfoContext(ctx, "claude CLI not available, using .mcp.json config file")
	}

	// Fallback: Write to .mcp.json config file
	locations, err := claudecode.GetConfigLocations()
	if err != nil {
		return fmt.Errorf("failed to get config locations: %w", err)
	}
	configPath := locations[0].Path
	logger.InfoContext(ctx, "using config location",
		slog.String("path", configPath),
		slog.String("type", locations[0].Description))

	config, err := claudecode.ReadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if _, exists := config.MCPServers[info.Name]; exists {
		logger.WarnContext(ctx, "server with this name already exists, will be overwritten",
			slog.String("name", info.Name))
	}

	mcpConfig := mcp.BuildMCPConfig(info, useEnvVar)
	serverConfig := claudecode.MCPServerConfig{
		Command: mcpConfig.Command,
		Args:    mcpConfig.Args,
		Env:     mcpConfig.Env,
	}

	config.AddOrUpdateServer(info.Name, serverConfig)

	if err := claudecode.WriteConfig(configPath, config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	logger.InfoContext(ctx, "successfully wrote MCP config",
		slog.String("name", info.Name),
		slog.String("url", info.URL),
		slog.String("config", configPath))

	fmt.Printf("\n✓ Successfully installed MCP server '%s'\n", info.Name)
	fmt.Printf("  URL: %s\n", info.URL)
	fmt.Printf("  Config: %s\n", configPath)
	fmt.Printf("  Method: Config file (claude CLI not detected)\n")

	if useEnvVar {
		fmt.Printf("\n⚠ Remember to set the environment variable:\n")
		fmt.Printf("  export %s='your-api-key-value'\n", info.EnvVarName)
	}

	fmt.Printf("\nRestart Claude Code to load the new MCP server.\n")

	return nil
}
