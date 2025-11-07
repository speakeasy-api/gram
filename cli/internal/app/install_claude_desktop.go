package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/urfave/cli/v2"
)

func newInstallClaudeDesktopCommand() *cli.Command {
	return &cli.Command{
		Name:  "claude-desktop",
		Usage: "Install a Gram toolset as an MCP server in Claude Desktop",
		Flags: append(installFlags, &cli.StringFlag{
			Name:  "output-dir",
			Usage: "Directory to save the .dxt file (defaults to Downloads folder)",
		}),
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
	info, err := resolveToolsetInfo(c)
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
	outputDir := c.String("output-dir")
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
