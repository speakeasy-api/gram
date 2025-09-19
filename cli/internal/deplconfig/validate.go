package deplconfig

import (
	"fmt"
	"slices"
)

var ValidSchemaVersions = []string{"1.0.0"}

// SchemaValid returns true if the incoming schema version is valid.
func (dc DeploymentConfig) SchemaValid() bool {
	return slices.Contains(ValidSchemaVersions, dc.SchemaVersion)
}

const ConfigTypeDeployment = "deployment"

// SupportedType returns true if the source type is supported.
func (s Source) SupportedType() bool {
	return s.Type == SourceTypeOpenAPIV3
}

// TypeValid returns true if the incoming schema version is valid.
func (dc DeploymentConfig) TypeValid() bool {
	return dc.Type == ConfigTypeDeployment
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

// failDuplicates returns an error if any values in the counter map appear more than once.
func failDuplicates(counter map[string]int, errorPrefix string) error {
	var duplicates []string
	for key, count := range counter {
		if count > 1 {
			duplicates = append(duplicates, fmt.Sprintf("'%s' (%d times)", key, count))
		}
	}

	if len(duplicates) > 0 {
		return fmt.Errorf("%s: %v", errorPrefix, duplicates)
	}
	return nil
}

// ValidateUniqueNames returns an error if source names are not unique.
func ValidateUniqueNames(sources []Source) error {
	counter := make(map[string]int)
	for _, source := range sources {
		counter[source.Name]++
	}

	return failDuplicates(counter, "source names must be unique")
}

// ValidateUniqueSlugs returns an error if source slugs are not unique.
func ValidateUniqueSlugs(sources []Source) error {
	counter := make(map[string]int)
	for _, source := range sources {
		counter[source.Slug]++
	}

	return failDuplicates(counter, "source slugs must be unique")
}

// Validate returns an error if the schema version is invalid, if the config
// is missing sources, if sources have missing required fields, or if names/slugs are not unique.
func (dc DeploymentConfig) Validate() error {
	if !dc.SchemaValid() {
		msg := "unexpected value for 'schema_version': '%s'. Expected one of %+v"
		return fmt.Errorf(msg, dc.SchemaVersion, ValidSchemaVersions)
	}

	if !dc.TypeValid() {
		msg := "unexpected value for 'type': '%s'. Expected '%s'"
		return fmt.Errorf(msg, dc.Type, ConfigTypeDeployment)
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
