package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/gen/keys"
)

// Save writes the profile configuration to disk. It writes a temporary file
// and renames it over path, so a failed write never leaves a truncated file.
func Save(config *Config, path string) error {
	// Write through a symlinked profile, such as one managed by a dotfiles
	// tool, instead of replacing the link with a regular file.
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	dir := filepath.Dir(path)
	// #nosec G301 - directory permissions are appropriate for config directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create profile directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".profile-*.json")
	if err != nil {
		return fmt.Errorf("failed to write profile file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write profile file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write profile file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
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
	data, err := readProfileFile(path)
	if err != nil || data == nil {
		return nil, err
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

// Clear removes all profiles from the configuration file. When path is the
// default profile path, it also empties the legacy profile file if one
// exists, so cleared credentials are not read back from it.
func Clear(path string) error {
	if err := Save(EmptyConfig(), path); err != nil {
		return err
	}

	if legacyPath, ok := legacyPathFor(path); ok {
		if _, err := os.Stat(legacyPath); err == nil {
			return Save(EmptyConfig(), legacyPath)
		}
	}
	return nil
}
