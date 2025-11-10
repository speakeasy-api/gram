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
// Uses: gemini mcp add --transport http "name" "url" --header "Header:${VAR}"
func InstallViaGeminiCLI(info *ToolsetInfo, useEnvVar bool) error {
	var headerValue string

	if useEnvVar {
		// Use environment variable substitution
		headerValue = fmt.Sprintf("%s:${%s}", info.HeaderName, info.EnvVarName)
	} else {
		// Use API key directly
		headerValue = fmt.Sprintf("%s:%s", info.HeaderName, info.APIKey)
	}

	// Build command: gemini mcp add --transport http "name" "url" --header "Header:value"
	args := []string{
		"mcp",
		"add",
		"--transport", "http",
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
