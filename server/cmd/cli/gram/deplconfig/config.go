package deplconfig

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/env"
)

type SourceType string

const (
	SourceTypeOpenAPIV3 SourceType = "openapiv3"
)

type Source struct {
	Type SourceType `json:"type"`

	// Location is the filepath or remote URL of the asset source.
	Location string `json:"location"`

	// Name is the human readable name of the asset.
	Name string `json:"name"`

	// Slug is the human readable public id of the asset.
	Slug string `json:"slug"`
}

type DeploymentConfig struct {
	// SchemaVersion defines the version of the configuration schema.
	SchemaVersion string `json:"schema_version"`

	// Type must always be set to "deployment". See `ConfigTypeDeployment`.
	Type string `json:"type"`

	// Sources defines the list of prospective assets to include in the
	// deployment.
	Sources []Source `json:"sources"`
}

// GetProducerToken returns an API key with a `producer` scope.
func (dc DeploymentConfig) GetProducerToken() string {
	return env.APIKey()
}

// ResolveLocations resolves relative source locations relative to the specified directory.
// URLs and absolute paths are left unchanged.
func (dc *DeploymentConfig) ResolveLocations(baseDir string) {
	for i := range dc.Sources {
		location := dc.Sources[i].Location
		if isURL(location) || filepath.IsAbs(location) {
			continue
		}

		dc.Sources[i].Location = filepath.Join(baseDir, location)
	}
}

var urlSchemes = []string{"http", "https"}

// isURL checks if the given string is a URL (http or https).
func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && slices.Contains(urlSchemes, u.Scheme)
}

// ReadDeploymentConfig reads a deployment config.
func ReadDeploymentConfig(filePath string) (*DeploymentConfig, error) {
	var cfg DeploymentConfig

	data, err := readFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Resolve relative paths in sources relative to the config file's directory
	configDir := filepath.Dir(filePath)
	cfg.ResolveLocations(configDir)

	return &cfg, nil
}

// readFile validates that a file exists at `filePath` and that its mode is
// regular.
func readFile(filePath string) ([]byte, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid file path: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}

	data, err := os.ReadFile(filePath) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return data, nil
}
