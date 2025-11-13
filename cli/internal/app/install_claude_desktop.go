package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/urfave/cli/v2"
)

func newInstallClaudeDesktopCommand() *cli.Command {
	return &cli.Command{
		Name:  "claude-desktop",
		Usage: "Generate a .mcpb file for installing a Gram toolset in Claude Desktop (standalone desktop app)",
		Flags: append(baseInstallFlags, &cli.StringFlag{
			Name:  "output-dir",
			Usage: "Directory to save the .mcpb file (defaults to Downloads folder)",
		}),
		Action: doInstallClaudeDesktop,
	}
}

// sanitizeFilename removes or replaces characters that are problematic in filenames
func sanitizeFilename(name string) string {
	// Replace spaces with dashes
	name = strings.ReplaceAll(name, " ", "-")

	// Remove or replace invalid filename characters: / \ : * ? " < > |
	invalidChars := regexp.MustCompile(`[/\\:*?"<>|]`)
	name = invalidChars.ReplaceAllString(name, "")

	// Remove leading/trailing dots and spaces
	name = strings.Trim(name, ". ")

	// If name is empty after sanitization, use a default
	if name == "" {
		name = "gram-mcp-server"
	}

	return name
}

func getDownloadsDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	downloadsDir := filepath.Join(homeDir, "Downloads")

	// Check if Downloads directory exists
	if _, err := os.Stat(downloadsDir); err == nil {
		return downloadsDir, nil
	}

	// Fallback to current directory if Downloads doesn't exist
	return ".", nil
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

	// Generate .mcpb manifest
	manifest, err := mcp.GenerateMCPBManifest(info, useEnvVar)
	if err != nil {
		return fmt.Errorf("failed to generate MCPB manifest: %w", err)
	}

	// Determine output directory
	outputDir := c.String("output-dir")
	if outputDir == "" {
		outputDir, err = getDownloadsDir()
		if err != nil {
			return fmt.Errorf("failed to determine downloads directory: %w", err)
		}
	} else {
		// Only create directory if user explicitly specified it
		err := os.MkdirAll(outputDir, 0750)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Create filename from server name (sanitize to ensure it's filesystem-safe)
	safeName := sanitizeFilename(info.Name)
	filename := fmt.Sprintf("%s.mcpb", safeName)
	outputPath := filepath.Join(outputDir, filename)

	// Write the .mcpb file
	if err := os.WriteFile(outputPath, manifest, 0600); err != nil {
		return fmt.Errorf("failed to write MCPB file: %w", err)
	}

	logger.InfoContext(ctx, "successfully created MCPB file",
		slog.String("name", info.Name),
		slog.String("url", info.URL),
		slog.String("path", outputPath))

	fmt.Printf("\n✓ Successfully created MCP server manifest: %s\n", filename)
	fmt.Printf("  Server Name: %s\n", info.Name)
	fmt.Printf("  MCP URL: %s\n", info.URL)
	fmt.Printf("  File Location: %s\n", outputPath)

	if useEnvVar {
		fmt.Printf("\n📋 Note: You'll be prompted to set this value when installing:\n")
		fmt.Printf("  %s (your API key)\n", info.EnvVarName)
	}

	fmt.Printf("\n📦 Next Steps:\n")
	fmt.Printf("  1. Open your Downloads folder (or the output directory)\n")
	fmt.Printf("  2. Double-click the '%s' file\n", filename)
	fmt.Printf("  3. Claude Desktop will open and prompt you to install the MCP server\n")
	if useEnvVar {
		fmt.Printf("  4. Enter your API key when prompted\n")
	}
	fmt.Printf("\n💡 Tip: After installation, restart Claude Desktop to load the new MCP server.\n")

	return nil
}
