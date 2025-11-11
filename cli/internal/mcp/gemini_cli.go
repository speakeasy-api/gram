package mcp

import (
	"fmt"
	"os/exec"
)

// IsGeminiCLIAvailable checks if the gemini CLI is available in PATH
func IsGeminiCLIAvailable() bool {
	_, err := exec.LookPath("gemini")
	return err == nil
}

// InstallViaGeminiCLI installs an MCP server using the native gemini CLI
// Uses: gemini mcp add --transport http --scope <scope> "name" "url" --header "Header:${VAR}"
// scope: "project" or "user" (will be passed as-is if gemini CLI supports it)
func InstallViaGeminiCLI(info *ToolsetInfo, useEnvVar bool, scope string) error {
	var headerValue string

	if useEnvVar {
		// Use environment variable substitution
		headerValue = fmt.Sprintf("%s:${%s}", info.HeaderName, info.EnvVarName)
	} else {
		// Use API key directly
		headerValue = fmt.Sprintf("%s:%s", info.HeaderName, info.APIKey)
	}

	// Build command: gemini mcp add --transport http --scope <scope> "name" "url" --header "Header:value"
	// Note: scope support depends on gemini CLI version
	args := []string{
		"mcp",
		"add",
		"--transport", "http",
		"--scope", scope,
		info.Name,
		info.URL,
		"--header", headerValue,
	}

	// #nosec G204 -- Executing gemini CLI with user-provided args is intentional
	cmd := exec.Command("gemini", args...)

	// Run the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gemini CLI command failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}
