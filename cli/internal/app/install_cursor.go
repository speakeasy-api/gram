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

func newInstallCursorCommand() *cli.Command {
	return &cli.Command{
		Name:  "cursor",
		Usage: "Install a Gram toolset as an MCP server in Cursor",
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
				Usage: "The name to use for this MCP server in Cursor (defaults to toolset name or derived from URL)",
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
		Action: doInstallCursor,
	}
}

func doInstallCursor(c *cli.Context) error {
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

	// Resolve toolset information
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

	// Build MCP config
	useEnvVar := info.EnvVarName != ""
	if useEnvVar {
		logger.InfoContext(ctx, "using environment variable substitution",
			slog.String("var", info.EnvVarName),
			slog.String("header", info.HeaderName))
	}

	mcpConfig := mcp.BuildMCPConfig(info, useEnvVar)

	// Marshal config to JSON for URI encoding
	configJSON, err := mcp.MarshalConfigJSON(info.Name, mcpConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// URL encode the config
	configEncoded := url.QueryEscape(configJSON)

	// Construct Cursor deep link
	deepLink := fmt.Sprintf(
		"cursor://anysphere.cursor-deeplink/mcp/install?name=%s&config=%s",
		url.QueryEscape(info.Name),
		configEncoded,
	)

	logger.InfoContext(ctx, "opening Cursor deep link", slog.String("name", info.Name))

	// Open the deep link
	if err := mcp.OpenURL(deepLink); err != nil {
		return fmt.Errorf("failed to open Cursor deep link: %w", err)
	}

	fmt.Printf("\n✓ Opening Cursor to install MCP server '%s'\n", info.Name)
	fmt.Printf("  URL: %s\n", info.URL)

	if useEnvVar {
		fmt.Printf("\n⚠ Remember to set the environment variable before using:\n")
		fmt.Printf("  export %s='your-api-key-value'\n", info.EnvVarName)
	}

	fmt.Printf("\nCursor should open and prompt you to install the MCP server.\n")

	return nil
}
