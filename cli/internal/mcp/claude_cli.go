package mcp

import (
	"fmt"
	"os/exec"
)

// IsClaudeCLIAvailable checks if the claude CLI is available in PATH
func IsClaudeCLIAvailable() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// InstallViaClaudeCLI installs an MCP server using the native claude CLI
// Uses: claude mcp add --transport http "name" "url" --header "Header:${VAR}"
// Returns an error if the claude CLI is not available
func InstallViaClaudeCLI(info *ToolsetInfo, useEnvVar bool) error {
	var headerValue string

	if useEnvVar {
		// Use environment variable substitution
		headerValue = fmt.Sprintf("%s:${%s}", info.HeaderName, info.EnvVarName)
	} else {
		// Use API key directly
		headerValue = fmt.Sprintf("%s:%s", info.HeaderName, info.APIKey)
	}

	// Build command: claude mcp add --transport http "name" "url" --header "Header:value"
	args := []string{
		"mcp",
		"add",
		"--transport", "http",
		info.Name,
		info.URL,
		"--header", headerValue,
	}

	// #nosec G204 -- Executing claude CLI with user-provided args is intentional
	cmd := exec.Command("claude", args...)

	// Run the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude CLI command failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}
