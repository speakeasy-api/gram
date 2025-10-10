package profile

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/gen/keys"
)

const defaultProfileName = "default"

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

// UpdateOrCreate updates or creates a profile with the given API key and
// metadata.
func UpdateOrCreate(
	apiKey string,
	apiURL string,
	org *keys.ValidateKeyOrganization,
	projects []*keys.ValidateKeyProject,
	path string,
) error {
	config, err := loadConfig(path)
	if err != nil {
		return err
	}

	if config == nil {
		config = &Config{
			Current:  defaultProfileName,
			Profiles: make(map[string]*Profile),
		}
	}

	if config.Profiles == nil {
		config.Profiles = make(map[string]*Profile)
	}

	// Find or create profile for this API URL
	profileName := findProfileByURL(config, apiURL)
	if profileName == "" {
		// No existing profile for this URL, create new one
		profileName = generateProfileName(config, apiURL)
	}

	// Set as current profile
	config.Current = profileName

	// Preserve existing default project if it's still in the projects list
	var defaultProjectSlug string
	existingProfile := config.Profiles[profileName]
	if existingProfile != nil && existingProfile.DefaultProjectSlug != "" {
		// Check if existing default is still valid
		for _, p := range projects {
			if p.Slug == existingProfile.DefaultProjectSlug {
				defaultProjectSlug = existingProfile.DefaultProjectSlug
				break
			}
		}
	}
	// Fall back to first project if no valid default
	if defaultProjectSlug == "" && len(projects) > 0 {
		defaultProjectSlug = projects[0].Slug
	}

	var orgData any
	if org != nil {
		orgData = map[string]string{
			"id":   org.ID,
			"name": org.Name,
			"slug": org.Slug,
		}
	}

	var projectsData []any
	for _, p := range projects {
		projectsData = append(projectsData, map[string]string{
			"id":   p.ID,
			"name": p.Name,
			"slug": p.Slug,
		})
	}

	config.Profiles[profileName] = &Profile{
		Secret:             apiKey,
		DefaultProjectSlug: defaultProjectSlug,
		APIUrl:             apiURL,
		Org:                orgData,
		Projects:           projectsData,
	}

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

// findProfileByURL searches for an existing profile with matching API URL.
func findProfileByURL(config *Config, apiURL string) string {
	for name, prof := range config.Profiles {
		if prof.APIUrl == apiURL {
			return name
		}
	}
	return ""
}

// generateProfileName creates a unique profile name based on API URL.
func generateProfileName(config *Config, apiURL string) string {
	// Try default first
	if _, exists := config.Profiles[defaultProfileName]; !exists {
		return defaultProfileName
	}

	// Extract hostname for profile name
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return defaultProfileName
	}

	baseName := parsed.Host
	if baseName == "" {
		baseName = defaultProfileName
	}

	// If base name is available, use it
	if _, exists := config.Profiles[baseName]; !exists {
		return baseName
	}

	// Append counter to make unique
	counter := 2
	for {
		name := fmt.Sprintf("%s-%d", baseName, counter)
		if _, exists := config.Profiles[name]; !exists {
			return name
		}
		counter++
	}
}
