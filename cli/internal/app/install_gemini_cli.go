package app

import (
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/urfave/cli/v2"
)

func newInstallGeminiCLICommand() *cli.Command {
	return &cli.Command{
		Name:   "gemini-cli",
		Usage:  "Install a Gram toolset as an MCP server in Gemini CLI (requires gemini command)",
		Flags:  installFlags,
		Action: doInstallGeminiCLI,
	}
}

func doInstallGeminiCLI(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)

	info, err := resolveToolsetInfo(c)
	if err != nil {
		return fmt.Errorf("failed to resolve toolset info: %w", err)
	}

	useEnvVar := info.EnvVarName != ""
	scope := c.String("scope")

	if useEnvVar {
		logger.InfoContext(ctx, "using environment variable substitution",
			slog.String("var", info.EnvVarName),
			slog.String("header", info.HeaderName))
	}

	// Execute: gemini mcp add --transport http --scope <scope> "name" "url" --header "Header:${VAR}"
	logger.InfoContext(ctx, "installing via gemini CLI with native HTTP transport",
		slog.String("scope", scope))

	if err := mcp.InstallViaGeminiCLI(info, useEnvVar, scope); err != nil {
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
