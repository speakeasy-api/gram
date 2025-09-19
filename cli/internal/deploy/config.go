package deploy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"slices"

	"github.com/speakeasy-api/gram/cli/internal/env"
)

var urlSchemes = []string{"http", "https"}

var validSchemaVersions = []string{"1.0.0"}

const configTypeDeployment = "deployment"

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

// MissingRequiredFields returns an error if the source is missing required fields.
func (s Source) MissingRequiredFields() error {
	if s.Location == "" {
		return fmt.Errorf("source is missing required field 'Location'")
	}
	if s.Name == "" {
		return fmt.Errorf("source is missing required field 'name'")
	}
	if s.Slug == "" {
		return fmt.Errorf("source is missing required field 'slug'")
	}
	return nil
}

type Config struct {
	// SchemaVersion defines the version of the configuration schema.
	SchemaVersion string `json:"schema_version"`

	// Type must always be set to "deployment". See `ConfigTypeDeployment`.
	Type string `json:"type"`

	// Sources defines the list of prospective assets to include in the
	// deployment.
	Sources []Source `json:"sources"`
}

// NewConfig reads a deployment config.
func NewConfig(cfgRdr io.Reader, workDir string) (*Config, error) {
	var cfg Config

	if err := json.NewDecoder(cfgRdr).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	for i := range cfg.Sources {
		location := cfg.Sources[i].Location
		if isURL(location) || filepath.IsAbs(location) {
			continue
		}

		cfg.Sources[i].Location = filepath.Join(workDir, location)
	}

	return &cfg, nil
}

// SchemaValid returns true if the incoming schema version is valid.
func (dc Config) SchemaValid() bool {
	return slices.Contains(validSchemaVersions, dc.SchemaVersion)
}

// SupportedType returns true if the source type is supported.
func (s Source) SupportedType() bool {
	return s.Type == SourceTypeOpenAPIV3
}

// TypeValid returns true if the incoming schema version is valid.
func (dc Config) TypeValid() bool {
	return dc.Type == configTypeDeployment
}

// GetProducerToken returns an API key with a `producer` scope.
func (dc Config) GetProducerToken() string {
	return env.APIKey()
}

// Validate returns an error if the schema version is invalid, if the config
// is missing sources, if sources have missing required fields, or if names/slugs are not unique.
func (dc Config) Validate() error {
	if !dc.SchemaValid() {
		msg := "unexpected value for 'schema_version': '%s'. Expected one of %+v"
		return fmt.Errorf(msg, dc.SchemaVersion, validSchemaVersions)
	}

	if !dc.TypeValid() {
		msg := "unexpected value for 'type': '%s'. Expected '%s'"
		return fmt.Errorf(msg, dc.Type, configTypeDeployment)
	}

	if len(dc.Sources) < 1 {
		return fmt.Errorf("must specify at least one source")
	}

	for i, source := range dc.Sources {
		if !source.SupportedType() {
			return fmt.Errorf("source at index %d has unsupported type '%s'. Only '%s' is supported", i, source.Type, SourceTypeOpenAPIV3)
		}
		if err := source.MissingRequiredFields(); err != nil {
			return fmt.Errorf("source at index %d: %w", i, err)
		}
	}

	if err := ValidateUniqueNames(dc.Sources); err != nil {
		return err
	}

	if err := ValidateUniqueSlugs(dc.Sources); err != nil {
		return err
	}

	return nil
}

// isURL checks if the given string is a URL (http or https).
func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && slices.Contains(urlSchemes, u.Scheme)
}
