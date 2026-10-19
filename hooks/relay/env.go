package relay

import (
	"os"
	"strings"
)

// Env returns the hooks setting name, read from SPEAKEASY_AI_<name> and,
// when that is unset or empty, from the deprecated GRAM_<name>. For example
// Env("HOOKS_API_KEY") reads SPEAKEASY_AI_HOOKS_API_KEY and falls back to
// the deprecated Gram name. The value is trimmed.
func Env(name string) string {
	if v := strings.TrimSpace(os.Getenv("SPEAKEASY_AI_" + name)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("GRAM_" + name))
}
