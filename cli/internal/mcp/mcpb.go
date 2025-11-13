package mcp

import (
	"encoding/json"
	"fmt"
)

// MCPBManifest represents the Claude Desktop .mcpb manifest structure
type MCPBManifest struct {
	ManifestVersion string            `json:"manifest_version"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Description     string            `json:"description"`
	Author          MCPBAuthor         `json:"author"`
	Server          MCPBServer         `json:"server"`
	UserConfig      map[string]MCPBVar `json:"user_config,omitempty"`
}

type MCPBAuthor struct {
	Name string `json:"name"`
}

type MCPBServer struct {
	Type       string         `json:"type"`
	EntryPoint string         `json:"entry_point"`
	MCPConfig  MCPServerConfig `json:"mcp_config"`
}

type MCPBVar struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
	Required    bool   `json:"required"`
}

// GenerateMCPBManifest creates a .mcpb manifest for Claude Desktop
func GenerateMCPBManifest(info *ToolsetInfo, useEnvVar bool) ([]byte, error) {
	var headerValue string
	var userConfig map[string]MCPBVar

	if useEnvVar {
		// Use environment variable substitution with user_config
		headerValue = fmt.Sprintf("%s:${user_config.%s}", info.HeaderName, info.EnvVarName)
		userConfig = map[string]MCPBVar{
			info.EnvVarName: {
				Type:        "string",
				Title:       info.EnvVarName,
				Description: "API key for authentication",
				Sensitive:   true,
				Required:    true,
			},
		}
	} else {
		// Hardcode the API key
		headerValue = fmt.Sprintf("%s:%s", info.HeaderName, info.APIKey)
		userConfig = nil
	}

	manifest := MCPBManifest{
		ManifestVersion: "0.1",
		Name:            info.Name,
		Version:         "1.0.0",
		Description:     fmt.Sprintf("Gram MCP server for %s", info.Name),
		Author: MCPBAuthor{
			Name: "Gram",
		},
		Server: MCPBServer{
			Type:       "node",
			EntryPoint: "npx",
			MCPConfig: MCPServerConfig{
				Command: "npx",
				Args: []string{
					"mcp-remote",
					info.URL,
					"--header",
					headerValue,
				},
				// Not used for command-based transport (will be omitted in JSON due to omitempty tags)
				Env:     nil,
				Type:    "",
				URL:     "",
				Headers: nil,
			},
		},
		UserConfig: userConfig,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCPB manifest: %w", err)
	}

	return data, nil
}
