package mcp

import (
	"encoding/json"
	"fmt"
)

// DXTManifest represents the Claude Desktop .dxt manifest structure
type DXTManifest struct {
	ManifestVersion string            `json:"manifest_version"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Description     string            `json:"description"`
	Author          DXTAuthor         `json:"author"`
	Server          DXTServer         `json:"server"`
	UserConfig      map[string]DXTVar `json:"user_config,omitempty"`
}

type DXTAuthor struct {
	Name string `json:"name"`
}

type DXTServer struct {
	Type       string         `json:"type"`
	EntryPoint string         `json:"entry_point"`
	MCPConfig  MCPServerConfig `json:"mcp_config"`
}

type DXTVar struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
	Required    bool   `json:"required"`
}

// GenerateDXTManifest creates a .dxt manifest for Claude Desktop
func GenerateDXTManifest(info *ToolsetInfo, useEnvVar bool) ([]byte, error) {
	var headerValue string
	var userConfig map[string]DXTVar

	if useEnvVar {
		// Use environment variable substitution with user_config
		headerValue = fmt.Sprintf("%s:${user_config.%s}", info.HeaderName, info.EnvVarName)
		userConfig = map[string]DXTVar{
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

	manifest := DXTManifest{
		ManifestVersion: "0.1",
		Name:            info.Name,
		Version:         "1.0.0",
		Description:     fmt.Sprintf("%s MCP server", info.Name),
		Author: DXTAuthor{
			Name: "Gram",
		},
		Server: DXTServer{
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
				Env:     map[string]string{},
				Type:    "",
				URL:     "",
				Headers: nil,
			},
		},
		UserConfig: userConfig,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DXT manifest: %w", err)
	}

	return data, nil
}
