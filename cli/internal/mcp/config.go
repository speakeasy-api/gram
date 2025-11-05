package mcp

import (
	"encoding/json"
	"fmt"
)

// MCPServerConfig represents the standard MCP server configuration
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// BuildMCPConfig constructs the standard MCP server configuration
func BuildMCPConfig(info *ToolsetInfo, useEnvVar bool) MCPServerConfig {
	var headerValue string
	var envConfig map[string]string

	if useEnvVar {
		// Use environment variable substitution
		headerValue = fmt.Sprintf("%s:${%s}", info.HeaderName, info.EnvVarName)
		envConfig = map[string]string{
			info.EnvVarName: "<your-value-here>",
		}
	} else {
		// Use API key directly
		headerValue = fmt.Sprintf("%s:%s", info.HeaderName, info.APIKey)
		envConfig = map[string]string{}
	}

	return MCPServerConfig{
		Command: "npx",
		Args: []string{
			"-y",
			"mcp-remote",
			info.URL,
			"--header",
			headerValue,
		},
		Env: envConfig,
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
