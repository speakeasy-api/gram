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
// Uses: claude mcp add --transport http --scope <scope> "name" "url" --header "Header:${VAR}"
// scope: "project" (maps to claude CLI's "local") or "user"
// Returns an error if the claude CLI is not available
func InstallViaClaudeCLI(info *ToolsetInfo, useEnvVar bool, scope string) error {
	var headerValue string

	if useEnvVar {
		// Use environment variable substitution
		headerValue = fmt.Sprintf("%s:${%s}", info.HeaderName, info.EnvVarName)
	} else {
		// Use API key directly
		headerValue = fmt.Sprintf("%s:%s", info.HeaderName, info.APIKey)
	}

	// Map our scope terminology to claude CLI's scope terminology
	// Our "project" -> Claude CLI's "local" (.mcp.json in current directory)
	// Our "user" -> Claude CLI's "user" (~/.claude/settings.local.json)
	claudeScope := scope
	if scope == "project" {
		claudeScope = "local"
	}

	// Build command: claude mcp add --transport http --scope <scope> "name" "url" --header "Header:value"
	args := []string{
		"mcp",
		"add",
		"--transport", "http",
		"--scope", claudeScope,
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
