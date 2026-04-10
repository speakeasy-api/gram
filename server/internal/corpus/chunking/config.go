package chunking

import "errors"

type DocsMcpConfig struct {
	Version   string            `json:"version"`
	Strategy  *Strategy         `json:"strategy,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Overrides []Override        `json:"overrides,omitempty"`
}

type Override struct {
	Pattern  string            `json:"pattern"`
	Strategy *Strategy         `json:"strategy,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func ParseDocsMcpConfig(data []byte) (*DocsMcpConfig, error) {
	return nil, errors.New("not implemented")
}

// ResolveConfig finds the nearest .docs-mcp.json config for a file path,
// checking parent directories up to root. configs maps dir path -> config.
func ResolveConfig(filePath string, configs map[string]*DocsMcpConfig) *DocsMcpConfig {
	return nil
}

// ResolveStrategy returns the effective strategy for a file, applying overrides.
func ResolveStrategy(filePath string, config *DocsMcpConfig) Strategy {
	return Strategy{}
}
