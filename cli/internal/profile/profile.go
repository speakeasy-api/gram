// Package profile provides profile-based configuration management for the Gram
// CLI.
package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/gen/keys"
)

// Config represents the profile configuration file structure.
type Config struct {
	Current  string              `json:"current"`
	Profiles map[string]*Profile `json:"profiles"`
}

// Org represents an organization in the profile.
type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Project represents a project in the profile.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Profile represents a single profile with authentication and project settings.
type Profile struct {
	Name               string                        `json:"-"`
	Secret             string                        `json:"secret"`
	DefaultProjectSlug string                        `json:"defaultProjectSlug"`
	APIUrl             string                        `json:"apiUrl"`
	Org                *keys.ValidateKeyOrganization `json:"org"`
	Projects           []*keys.ValidateKeyProject    `json:"projects"`
}

func EmptyConfig() *Config {
	return &Config{
		Current:  "",
		Profiles: make(map[string]*Profile),
	}
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

	profile.Name = config.Current
	return profile, nil
}

func findProfileByName(
	profiles map[string]*Profile,
	profileName string,
) *Profile {
	targetName := profileName
	if targetName == "" {
		return nil
	}

	if prof, ok := profiles[targetName]; ok {
		prof.Name = targetName
		return prof
	}

	return nil
}

// LoadByName reads the profile configuration and returns the profile matching
// the specified name. If profileName is empty, falls back to current.
//
// Returns (nil, nil) if the profile file doesn't exist or no matching profile found.
// Returns an error if the file is malformed.
func LoadByName(path string, profileName string) (*Profile, error) {
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

	if profileName == "" {
		return findProfileByName(config.Profiles, config.Current), nil
	}

	return findProfileByName(config.Profiles, profileName), nil
}

type contextKey string

const profileContextKey contextKey = "profile"

// FromContext retrieves the loaded profile from the context. Returns nil if no
// profile was loaded.
func FromContext(ctx context.Context) *Profile {
	if prof, ok := ctx.Value(profileContextKey).(*Profile); ok {
		return prof
	}
	return nil
}

// WithProfile adds the incoming profile to ctx. No-ops if prof is nil.
func WithProfile(ctx context.Context, prof *Profile) context.Context {
	if prof != nil {
		return context.WithValue(ctx, profileContextKey, prof)
	}
	return ctx
}

// LintProfile checks a profile for potential issues and returns warning messages.
func LintProfile(p *Profile) []string {
	var warnings []string

	if p == nil {
		return warnings
	}

	if p.DefaultProjectSlug != "" {
		found := false
		for _, proj := range p.Projects {
			if proj != nil && proj.Slug == p.DefaultProjectSlug {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings,
				fmt.Sprintf("default project '%s' not found in available projects",
					p.DefaultProjectSlug))
		}
	}

	return warnings
}
