package mcp

import (
	"encoding/json"
	"fmt"
)

// MCPServerConfig represents the standard MCP server configuration
// Supports both command-based (legacy) and HTTP transport (native) configurations
type MCPServerConfig struct {
	// Command-based configuration (legacy, for compatibility)
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// HTTP transport configuration (native)
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// BuildMCPConfig constructs the native HTTP transport MCP server configuration
func BuildMCPConfig(info *ToolsetInfo, useEnvVar bool) MCPServerConfig {
	headers := make(map[string]string)

	if useEnvVar {
		// Use environment variable substitution
		headers[info.HeaderName] = fmt.Sprintf("${%s}", info.EnvVarName)
	} else {
		// Use API key directly
		headers[info.HeaderName] = info.APIKey
	}

	return MCPServerConfig{
		Command: "",
		Args:    nil,
		Env:     nil,
		Type:    "http",
		URL:     info.URL,
		Headers: headers,
	}
}

// MarshalConfigJSON marshals the MCP config to JSON string
func MarshalConfigJSON(serverName string, config MCPServerConfig) (string, error) {
	wrapper := map[string]map[string]MCPServerConfig{
		"mcpServers": {
			serverName: config,
		},
	}

	data, err := json.Marshal(wrapper)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	return string(data), nil
}
