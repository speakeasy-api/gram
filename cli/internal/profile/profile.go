// Package profile provides profile-based configuration management for the Gram
// CLI.
package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the profile configuration file structure.
type Config struct {
	Current  string              `json:"current"`
	Profiles map[string]*Profile `json:"profiles"`
}

// Profile represents a single profile with authentication and project settings.
type Profile struct {
	Secret             string `json:"secret"`
	DefaultProjectSlug string `json:"defaultProjectSlug"`
	APIUrl             string `json:"apiUrl"`
	Org                any    `json:"org"`
	Projects           []any  `json:"projects"`
}

// DefaultProfilePath returns the default path to the profile configuration file.
func DefaultProfilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".gram", "profile.json"), nil
}

// Load reads the profile configuration from the specified path, or from
// DefaultProfilePath() if path is empty, and returns the currently active
// profile based on the "current" field.
//
// Returns (nil, nil) if the profile file doesn't exist.
// Returns an error if the file is malformed or the current profile is invalid.
func Load(path string) (*Profile, error) {
	var profilePath string
	if path != "" {
		profilePath = path
	} else {
		defaultPath, err := DefaultProfilePath()
		if err != nil {
			return nil, err
		}
		profilePath = defaultPath
	}

	data, err := os.ReadFile(filepath.Clean(profilePath))
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

	if config.Current == "" {
		return nil, fmt.Errorf("profile file missing 'current' field")
	}

	profile, ok := config.Profiles[config.Current]
	if !ok {
		return nil, fmt.Errorf(
			"current profile '%s' not found in profiles",
			config.Current,
		)
	}

	return profile, nil
}
