package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/gen/keys"
)

// Save writes the profile configuration to disk.
func Save(config *Config, path string) error {
	// #nosec G301 - directory permissions are appropriate for config directory
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create profile directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write profile file: %w", err)
	}

	return nil
}

func loadOrCreateConfig(path string) (*Config, error) {
	config, err := loadConfig(path)
	if err != nil {
		return nil, err
	}

	if config == nil {
		config = EmptyConfig()
	}

	if config.Profiles == nil {
		config.Profiles = make(map[string]*Profile)
	}

	return config, nil
}

func preserveDefaultProjectSlug(existingProfile *Profile) string {
	if existingProfile != nil {
		return existingProfile.DefaultProjectSlug
	}
	return ""
}

func validateProjectSlug(projectSlug string, projects []*keys.ValidateKeyProject) bool {
	if projectSlug == "" {
		return true
	}
	for _, proj := range projects {
		if proj != nil && proj.Slug == projectSlug {
			return true
		}
	}
	return false
}

func buildProfile(
	name string,
	apiKey string,
	apiURL string,
	defaultProjectSlug string,
	org *keys.ValidateKeyOrganization,
	projects []*keys.ValidateKeyProject,
	providedProjectSlug string,
) *Profile {
	// Use provided project slug if it's valid
	if providedProjectSlug != "" && validateProjectSlug(providedProjectSlug, projects) {
		defaultProjectSlug = providedProjectSlug
	} else if defaultProjectSlug == "" && len(projects) > 0 {
		// Fall back to first project if no default and no valid provided
		defaultProjectSlug = projects[0].Slug
	}

	return &Profile{
		Name:               name,
		Secret:             apiKey,
		DefaultProjectSlug: defaultProjectSlug,
		APIUrl:             apiURL,
		Org:                org,
		Projects:           projects,
	}
}

// UpdateOrCreate updates or creates a profile with the given name, API key,
// and metadata. The saved profile gets set as "current".
func UpdateOrCreate(
	apiKey string,
	apiURL string,
	org *keys.ValidateKeyOrganization,
	projects []*keys.ValidateKeyProject,
	path string,
	profileName string,
	projectSlug string,
) error {
	config, err := loadOrCreateConfig(path)
	if err != nil {
		return err
	}

	prof := buildProfile(
		profileName,
		apiKey,
		apiURL,
		preserveDefaultProjectSlug(config.Profiles[profileName]),
		org,
		projects,
		projectSlug,
	)

	config.Current = profileName
	config.Profiles[profileName] = prof

	return Save(config, path)
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read profile file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse profile file: %w", err)
	}

	return &config, nil
}

// UpdateProjectSlug updates the default project slug for the current profile.
func UpdateProjectSlug(path string, projectSlug string) error {
	config, err := loadConfig(path)
	if err != nil {
		return fmt.Errorf("failed to load profile: %w", err)
	}

	if config == nil {
		return fmt.Errorf("no profile configuration found")
	}

	if config.Current == "" {
		return fmt.Errorf("no current profile set")
	}

	profile, ok := config.Profiles[config.Current]
	if !ok {
		return fmt.Errorf("current profile '%s' not found", config.Current)
	}

	// Validate that the project slug exists in the profile's projects
	if !validateProjectSlug(projectSlug, profile.Projects) {
		return fmt.Errorf("project '%s' not found in available projects", projectSlug)
	}

	profile.DefaultProjectSlug = projectSlug
	return Save(config, path)
}
