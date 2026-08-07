package mcp

import (
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

// IsClaudeCLIAvailable checks if the claude CLI is available in PATH
func IsClaudeCLIAvailable() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
var validHeaderNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
var validEnvVarPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validateToolsetInfo(info *ToolsetInfo) error {
	if info == nil {
		return fmt.Errorf("toolset info is nil")
	}
	if !validNamePattern.MatchString(info.Name) {
		return fmt.Errorf("invalid name: must be alphanumeric with hyphens/underscores, starting with alphanumeric")
	}
	if info.URL == "" {
		return fmt.Errorf("URL is required")
	}
	u, err := url.Parse(info.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid URL: must be a valid HTTP or HTTPS URL")
	}
	if !validHeaderNamePattern.MatchString(info.HeaderName) {
		return fmt.Errorf("invalid header name: must be alphanumeric with hyphens/underscores, starting with alphanumeric")
	}
	if !validEnvVarPattern.MatchString(info.EnvVarName) {
		return fmt.Errorf("invalid env var name: must be a valid shell variable name")
	}
	return nil
}

// InstallViaClaudeCLI installs an MCP server using the native claude CLI
// Uses: claude mcp add --transport http --scope <scope> "name" "url" --header "Header:${VAR}"
// scope: "project" (maps to claude CLI's "local") or "user"
// Returns an error if the claude CLI is not available
func InstallViaClaudeCLI(info *ToolsetInfo, useEnvVar bool, scope string) error {
	if err := validateToolsetInfo(info); err != nil {
		return err
	}

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

	if claudeScope != "local" && claudeScope != "user" {
		return fmt.Errorf("invalid scope: must be 'project' or 'user'")
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