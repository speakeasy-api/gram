package deploy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v4"
)

var urlSchemes = []string{"http", "https"}

var validSchemaVersions = []string{"1.0.0"}

const configTypeDeployment = "deployment"

type SourceType string

const (
	SourceTypeOpenAPIV3 SourceType = "openapiv3"
)

var AllowedTypes = []SourceType{SourceTypeOpenAPIV3}

type Source struct {
	Type SourceType `json:"type" yaml:"type" toml:"type"`

	// Location is the filepath or remote URL of the asset source.
	Location string `json:"location" yaml:"location" toml:"location"`

	// Name is the human readable name of the asset.
	Name string `json:"name" yaml:"name" toml:"name"`

	// Slug is the human readable public id of the asset.
	Slug string `json:"slug" yaml:"slug" toml:"slug"`
}

// Validate returns an error if the source is missing required fields.
func (s Source) Validate() error {
	if !isSupportedType(s) {
		return fmt.Errorf("source has unsupported type %q (allowed types: %s)", s.Type, SourceTypeOpenAPIV3)
	}

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

func isSupportedType(s Source) bool {
	return slices.Contains(AllowedTypes, s.Type)
}

type Config struct {
	// SchemaVersion defines the version of the configuration schema.
	SchemaVersion string `json:"schema_version" yaml:"schema_version" toml:"schema_version"`

	// Type must always be set to "deployment". See `ConfigTypeDeployment`.
	Type string `json:"type" yaml:"type" toml:"type"`

	// Sources defines the list of prospective assets to include in the
	// deployment.
	Sources []Source `json:"sources" yaml:"sources" toml:"sources"`
}

// NewConfig reads a deployment config.
func NewConfig(cfgRdr io.Reader, filename string) (*Config, error) {
	var cfg Config

	data, err := io.ReadAll(cfgRdr)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config JSON: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config YAML: %w", err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config TOML: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config file format: %s (supported: .json, .yaml, .yml, .toml)", ext)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workDir := filepath.Dir(filename)
	for i := range cfg.Sources {
		location := cfg.Sources[i].Location
		if isURL(location) || filepath.IsAbs(location) {
			continue
		}

		cfg.Sources[i].Location = filepath.Join(workDir, location)
	}

	return &cfg, nil
}

func NewConfigFromSources(sources ...Source) *Config {
	return &Config{
		SchemaVersion: validSchemaVersions[0],
		Type:          configTypeDeployment,
		Sources:       sources,
	}
}

// Validate returns an error if the schema version is invalid, if the config
// is missing sources, if sources have missing required fields, or if names/slugs are not unique.
func (dc Config) Validate() error {
	if !slices.Contains(validSchemaVersions, dc.SchemaVersion) {
		msg := "unexpected value for 'schema_version': '%s'. Expected one of %+v"
		return fmt.Errorf(msg, dc.SchemaVersion, validSchemaVersions)
	}

	if dc.Type != configTypeDeployment {
		msg := "unexpected value for 'type': '%s'. Expected '%s'"
		return fmt.Errorf(msg, dc.Type, configTypeDeployment)
	}

	if len(dc.Sources) < 1 {
		return fmt.Errorf("must specify at least one source")
	}

	for i, source := range dc.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("source at index %d: %w", i, err)
		}
	}

	if err := validateUniqueNames(dc.Sources); err != nil {
		return err
	}

	if err := validateUniqueSlugs(dc.Sources); err != nil {
		return err
	}

	return nil
}

// isURL checks if the given string is a URL (http or https).
func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && slices.Contains(urlSchemes, u.Scheme)
}
