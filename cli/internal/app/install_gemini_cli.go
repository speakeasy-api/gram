package app

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

func newInstallGeminiCLICommand() *cli.Command {
	return &cli.Command{
		Name:  "gemini-cli",
		Usage: "Install a Gram toolset as an MCP server in Gemini CLI",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "toolset",
				Usage: "The slug of the Gram toolset to install (e.g., speakeasy-admin). Will automatically look up MCP configuration.",
			},
			&cli.StringFlag{
				Name:  "mcp-url",
				Usage: "The MCP server URL (e.g., https://mcp.getgram.ai/org/project/environment). Use this for manual configuration.",
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "The name to use for this MCP server in Gemini CLI (defaults to toolset name or derived from URL)",
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
		Action: doInstallGeminiCLI,
	}
}

func doInstallGeminiCLI(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)

	toolsetSlug := c.String("toolset")
	mcpURL := c.String("mcp-url")

	// Validate that either toolset or mcp-url is provided
	if toolsetSlug == "" && mcpURL == "" {
		return fmt.Errorf("either --toolset or --mcp-url must be provided")
	}
	if toolsetSlug != "" && mcpURL != "" {
		return fmt.Errorf("cannot provide both --toolset and --mcp-url")
	}

	// Check if gemini CLI is available
	if !mcp.IsGeminiCLIAvailable() {
		return fmt.Errorf("gemini CLI not found in PATH; please install Gemini CLI first")
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

	// Resolve toolset information
	info, err := mcp.ResolveToolsetInfo(ctx, &mcp.ResolverOptions{
		ToolsetSlug:     toolsetSlug,
		MCPURL:          mcpURL,
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

	// Execute: gemini mcp add --transport http "name" "url" --header "Header:${VAR}"
	logger.InfoContext(ctx, "installing via gemini CLI with native HTTP transport")

	if err := mcp.InstallViaGeminiCLI(info, useEnvVar); err != nil {
		return fmt.Errorf("failed to install via gemini CLI: %w", err)
	}

	logger.InfoContext(ctx, "successfully installed via gemini CLI",
		slog.String("name", info.Name),
		slog.String("url", info.URL))

	fmt.Printf("\n✓ Successfully installed MCP server '%s' via gemini CLI\n", info.Name)
	fmt.Printf("  URL: %s\n", info.URL)
	fmt.Printf("  Transport: HTTP (native)\n")

	if useEnvVar {
		fmt.Printf("\n⚠ Remember to set the environment variable before using:\n")
		fmt.Printf("  export %s='your-api-key-value'\n", info.EnvVarName)
	}

	fmt.Printf("\nRestart Gemini CLI to load the new MCP server.\n")

	return nil
}
