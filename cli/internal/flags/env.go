package flags

import (
	"fmt"
	"strings"
)

const (
	envPrefix       = "SPEAKEASY_AI_"
	legacyEnvPrefix = "GRAM_"
)

// EnvSettings are the settings the CLI reads from the environment, without a
// prefix. Each is read from SPEAKEASY_AI_<name> first, then from the deprecated
// GRAM_<name>.
var EnvSettings = []string{
	"API_KEY",
	"API_URL",
	"SITE_URL",
	"ORG",
	"PROJECT",
	"PROFILE",
	"PROFILE_PATH",
	"LOG_LEVEL",
	"LOG_PRETTY",
	"FUNCTIONS_SDK_VERSION",
}

// EnvVars returns the environment variables that set name, in the order they
// are read: SPEAKEASY_AI_<name>, then the deprecated GRAM_<name>.
func EnvVars(name string) []string {
	return []string{envPrefix + name, legacyEnvPrefix + name}
}

// UnsetEmptyEnv unsets every SPEAKEASY_AI_* variable in EnvSettings that is
// set to "", so an empty new name does not hide its GRAM_* fallback. urfave/cli
// otherwise takes the first variable that is defined, even when it is empty.
func UnsetEmptyEnv(lookup func(string) (string, bool), unset func(string) error) error {
	for _, name := range EnvSettings {
		if v, ok := lookup(envPrefix + name); ok && v == "" {
			if err := unset(envPrefix + name); err != nil {
				return fmt.Errorf("unset %s: %w", envPrefix+name, err)
			}
		}
	}
	return nil
}

// LegacyEnvNotice returns a one-line deprecation notice that names every
// GRAM_* variable in EnvSettings that is set while its SPEAKEASY_AI_* replacement
// is not. It returns "" when there are none.
func LegacyEnvNotice(lookup func(string) (string, bool)) string {
	var renames []string
	for _, name := range EnvSettings {
		if v, ok := lookup(envPrefix + name); ok && v != "" {
			continue
		}
		if _, ok := lookup(legacyEnvPrefix + name); ok {
			renames = append(renames, legacyEnvPrefix+name+" to "+envPrefix+name)
		}
	}
	if len(renames) == 0 {
		return ""
	}
	return "Warning: GRAM_* environment variables are deprecated and still work for now. Rename " +
		strings.Join(renames, ", ") + "."
}
