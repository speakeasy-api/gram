package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPServerConfig represents the configuration for a single MCP server
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"` // omitempty will exclude empty env maps
}

// MCPConfig represents the structure of .mcp.json or settings.local.json
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// ConfigLocation represents a location where Claude Code MCP configuration can be stored
type ConfigLocation struct {
	Path        string
	Description string
}

// GetConfigLocations returns the possible locations for Claude Code MCP configuration
// Priority order: project-local, user-local
func GetConfigLocations() ([]ConfigLocation, error) {
	locations := []ConfigLocation{}

	// Project-local configuration (.mcp.json in current directory)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}
	locations = append(locations, ConfigLocation{
		Path:        filepath.Join(cwd, ".mcp.json"),
		Description: "project-local (.mcp.json in current directory)",
	})

	// User-local configuration
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// ~/.claude/settings.local.json
	locations = append(locations, ConfigLocation{
		Path:        filepath.Join(homeDir, ".claude", "settings.local.json"),
		Description: "user-local (~/.claude/settings.local.json)",
	})

	return locations, nil
}

// ReadConfig reads the MCP configuration from the specified path
// If the file doesn't exist, returns an empty config
func ReadConfig(path string) (*MCPConfig, error) {
	// #nosec G304 -- Reading user-specified config file is intentional
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config if file doesn't exist
			return &MCPConfig{
				MCPServers: make(map[string]MCPServerConfig),
			}, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Ensure MCPServers map is initialized
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]MCPServerConfig)
	}

	return &config, nil
}

// WriteConfig writes the MCP configuration to the specified path
func WriteConfig(path string, config *MCPConfig) error {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// AddOrUpdateServer adds or updates an MCP server in the configuration
func (c *MCPConfig) AddOrUpdateServer(name string, serverConfig MCPServerConfig) {
	if c.MCPServers == nil {
		c.MCPServers = make(map[string]MCPServerConfig)
	}
	c.MCPServers[name] = serverConfig
}
