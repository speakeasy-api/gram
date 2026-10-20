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

	claudeScope := claudeCLIScope(scope)

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

// RemoveViaClaudeCLI removes the MCP server called name with the native claude
// CLI: claude mcp remove --scope <scope> "name". scope is "project" or "user",
// as for InstallViaClaudeCLI.
func RemoveViaClaudeCLI(name string, scope string) error {
	// #nosec G204 -- Executing claude CLI with user-provided args is intentional
	cmd := exec.Command("claude", "mcp", "remove", "--scope", claudeCLIScope(scope), name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude CLI command failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// claudeCLIScope maps our scope terminology to the claude CLI's. Our "project"
// is the claude CLI's "local" (.mcp.json in the current directory); "user" is
// the same in both (~/.claude/settings.local.json).
func claudeCLIScope(scope string) string {
	if scope == "project" {
		return "local"
	}
	return scope
}
