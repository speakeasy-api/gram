package app

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

func newInstallClaudeDesktopCommand() *cli.Command {
	return &cli.Command{
		Name:  "claude-desktop",
		Usage: "Install a Gram toolset as an MCP server in Claude Desktop",
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
				Usage: "The name to use for this MCP server in Claude Desktop (defaults to toolset name or derived from URL)",
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
				Name:  "output-dir",
				Usage: "Directory to save the .dxt file (defaults to Downloads folder)",
			},
		},
		Action: doInstallClaudeDesktop,
	}
}

func getDownloadsDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(homeDir, "Downloads")
}

func doInstallClaudeDesktop(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)

	toolsetSlug := c.String("toolset")
	mcpURL := c.String("mcp-url")
	outputDir := c.String("output-dir")

	// Validate that either toolset or mcp-url is provided
	if toolsetSlug == "" && mcpURL == "" {
		return fmt.Errorf("either --toolset or --mcp-url must be provided")
	}
	if toolsetSlug != "" && mcpURL != "" {
		return fmt.Errorf("cannot provide both --toolset and --mcp-url")
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
		logger.InfoContext(ctx, "using environment variable in user_config",
			slog.String("var", info.EnvVarName),
			slog.String("header", info.HeaderName))
	}

	// Generate .dxt manifest
	manifest, err := mcp.GenerateDXTManifest(info, useEnvVar)
	if err != nil {
		return fmt.Errorf("failed to generate DXT manifest: %w", err)
	}

	// Determine output directory
	if outputDir == "" {
		outputDir = getDownloadsDir()
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create filename from server name
	filename := fmt.Sprintf("%s.dxt", info.Name)
	outputPath := filepath.Join(outputDir, filename)

	// Write the .dxt file
	if err := os.WriteFile(outputPath, manifest, 0600); err != nil {
		return fmt.Errorf("failed to write DXT file: %w", err)
	}

	logger.InfoContext(ctx, "successfully created DXT file",
		slog.String("name", info.Name),
		slog.String("url", info.URL),
		slog.String("path", outputPath))

	fmt.Printf("\n✓ Successfully created MCP server manifest '%s.dxt'\n", info.Name)
	fmt.Printf("  URL: %s\n", info.URL)
	fmt.Printf("  File: %s\n", outputPath)

	if useEnvVar {
		fmt.Printf("\n⚠ You'll be prompted to set this value when installing:\n")
		fmt.Printf("  %s\n", info.EnvVarName)
	}

	fmt.Printf("\nDouble-click the .dxt file to install in Claude Desktop.\n")

	return nil
}
