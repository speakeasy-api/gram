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

func buildProfile(
	name string,
	apiKey string,
	apiURL string,
	defaultProjectSlug string,
	org *keys.ValidateKeyOrganization,
	projects []*keys.ValidateKeyProject,
) *Profile {
	if defaultProjectSlug == "" && len(projects) > 0 {
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
