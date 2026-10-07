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
// prefix. Each is set by SPEAKEASY_AI_<name>. ApplyLegacyEnv also accepts the
// deprecated GRAM_<name>.
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

// EnvVar returns SPEAKEASY_AI_<name>, the environment variable that sets the
// setting called name.
func EnvVar(name string) string {
	return envPrefix + name
}

// EnvVars returns the environment variables a flag reads for the setting
// called name. Only the new name is listed, so help text shows it alone;
// ApplyLegacyEnv maps the deprecated GRAM_<name> onto it before flags are
// parsed.
func EnvVars(name string) []string {
	return []string{EnvVar(name)}
}

// ApplyLegacyEnv copies every deprecated GRAM_<name> in EnvSettings into
// SPEAKEASY_AI_<name> when the new name is unset or empty, so the CLI keeps
// honouring the old names. It never reads the plain SPEAKEASY_* names, which
// belong to the Speakeasy SDK generator CLI.
//
// It returns a one-line deprecation notice naming the copied variables, or ""
// when there were none.
func ApplyLegacyEnv(lookup func(string) (string, bool), setenv func(string, string) error) (string, error) {
	var renames []string
	for _, name := range EnvSettings {
		if v, ok := lookup(envPrefix + name); ok && v != "" {
			continue
		}
		legacy, ok := lookup(legacyEnvPrefix + name)
		if !ok {
			continue
		}
		if err := setenv(envPrefix+name, legacy); err != nil {
			return "", fmt.Errorf("set %s: %w", envPrefix+name, err)
		}
		renames = append(renames, legacyEnvPrefix+name+" to "+envPrefix+name)
	}
	if len(renames) == 0 {
		return "", nil
	}
	return "Warning: GRAM_* environment variables are deprecated and still work for now. Rename " +
		strings.Join(renames, ", ") + ".", nil
}
