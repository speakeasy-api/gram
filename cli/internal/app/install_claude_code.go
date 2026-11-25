package app

import (
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/claudecode"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/urfave/cli/v2"
)

func newInstallClaudeCodeCommand() *cli.Command {
	return &cli.Command{
		Name:   "claude-code",
		Usage:  "Install a toolset as an MCP server in Claude Code (CLI editor with native HTTP support)",
		Flags:  installFlags,
		Action: doInstallClaudeCode,
	}
}

func doInstallClaudeCode(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)

	// Resolve toolset information using shared logic
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

	// Try to use native claude CLI with HTTP transport first
	if mcp.IsClaudeCLIAvailable() {
		logger.InfoContext(ctx, "using claude CLI with native HTTP transport",
			slog.String("scope", scope))

		if err := mcp.InstallViaClaudeCLI(info, useEnvVar, scope); err != nil {
			logger.WarnContext(ctx, "claude CLI installation failed, falling back to config file",
				slog.String("error", err.Error()))
		} else {
			// Success with claude CLI
			logger.InfoContext(ctx, "successfully installed via claude CLI",
				slog.String("name", info.Name),
				slog.String("url", info.URL))

			// Remove an entry an earlier release installed under the legacy
			// default name, as the config file path below does. It usually
			// doesn't exist, so a failure here is only logged.
			if legacy, ok := mcp.LegacyServerName(info.Name); ok {
				if err := mcp.RemoveViaClaudeCLI(legacy, scope); err != nil {
					logger.DebugContext(ctx, "no legacy server to remove via claude CLI",
						slog.String("legacy_name", legacy), slog.String("error", err.Error()))
				} else {
					logger.InfoContext(ctx, "removed server installed under its legacy name",
						slog.String("legacy_name", legacy), slog.String("name", info.Name))
				}
			}

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

	// Determine config path based on scope flag (already declared above)
	var configPath string
	var configDesc string

	switch scope {
	case "project":
		configPath = locations[0].Path
		configDesc = locations[0].Description
	case "user":
		configPath = locations[1].Path
		configDesc = locations[1].Description
	default:
		return fmt.Errorf("invalid scope '%s': must be 'project' or 'user'", scope)
	}

	logger.InfoContext(ctx, "using config location",
		slog.String("path", configPath),
		slog.String("type", configDesc),
		slog.String("scope", scope))

	config, err := claudecode.ReadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Replace an entry an earlier release installed under the legacy default
	// name instead of adding a duplicate next to it.
	if legacy, ok := mcp.LegacyServerName(info.Name); ok {
		if _, exists := config.MCPServers[legacy]; exists {
			logger.InfoContext(ctx, "replacing server installed under its legacy name",
				slog.String("legacy_name", legacy), slog.String("name", info.Name))
			delete(config.MCPServers, legacy)
		}
	}

	if _, exists := config.MCPServers[info.Name]; exists {
		logger.WarnContext(ctx, "server with this name already exists, will be overwritten",
			slog.String("name", info.Name))
	}

	mcpConfig := mcp.BuildMCPConfig(info, useEnvVar)
	serverConfig := claudecode.MCPServerConfig{
		Command: "",
		Args:    nil,
		Env:     nil,
		Type:    mcpConfig.Type,
		URL:     mcpConfig.URL,
		Headers: mcpConfig.Headers,
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
