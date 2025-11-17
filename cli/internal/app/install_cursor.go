package app

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/urfave/cli/v2"
)

func newInstallCursorCommand() *cli.Command {
	return &cli.Command{
		Name:   "cursor",
		Usage:  "Install a Gram toolset as an MCP server in Cursor (opens browser-based installation)",
		Flags:  baseInstallFlags,
		Action: doInstallCursor,
	}
}

func doInstallCursor(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	info, err := resolveToolsetInfo(c)
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

	// Try to open the deep link, but don't fail if it doesn't work
	if err := mcp.OpenURL(deepLink); err != nil {
		logger.WarnContext(ctx, "failed to open browser automatically, you can copy the URL manually",
			slog.String("error", err.Error()))
		fmt.Printf("\n⚠ Could not open Cursor automatically\n")
		fmt.Printf("  Please copy and paste this URL into your browser:\n")
		fmt.Printf("  %s\n\n", deepLink)
	} else {
		fmt.Printf("\n✓ Opening Cursor to install MCP server '%s'\n", info.Name)
	}

	fmt.Printf("  MCP Server URL: %s\n", info.URL)

	if useEnvVar {
		fmt.Printf("\n⚠ Remember to set the environment variable before using:\n")
		fmt.Printf("  export %s='your-api-key-value'\n", info.EnvVarName)
	}

	if err == nil {
		fmt.Printf("\nCursor should open and prompt you to install the MCP server.\n")
	}

	return nil
}
