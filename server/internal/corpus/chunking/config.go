package chunking

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

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
	var config DocsMcpConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse .docs-mcp.json: %w", err)
	}
	return &config, nil
}

// ResolveConfig finds the nearest config for a file path by checking
// progressively shorter directory prefixes.
func ResolveConfig(filePath string, configs map[string]*DocsMcpConfig) *DocsMcpConfig {
	dir := filepath.Dir(filePath)
	if dir == "." {
		dir = ""
	}

	for {
		if config, ok := configs[dir]; ok {
			return config
		}
		if dir == "" {
			break
		}
		parent := filepath.Dir(dir)
		if parent == "." {
			parent = ""
		}
		if parent == dir {
			break
		}
		dir = parent
	}

	return configs[""]
}

func ResolveStrategy(filePath string, config *DocsMcpConfig) Strategy {
	base := Strategy{
		ChunkBy:      "h2",
		MaxChunkSize: 2000,
		MinChunkSize: 0,
	}
	if config.Strategy != nil {
		base = *config.Strategy
	}

	baseName := filepath.Base(filePath)
	for _, override := range config.Overrides {
		if matchGlob(override.Pattern, baseName, filePath) && override.Strategy != nil {
			return *override.Strategy
		}
	}

	return base
}

func matchGlob(pattern, baseName, fullPath string) bool {
	if !strings.Contains(pattern, "/") {
		matched, _ := filepath.Match(pattern, baseName)
		return matched
	}
	matched, _ := filepath.Match(pattern, fullPath)
	return matched
}
