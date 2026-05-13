package auth

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateLegibleOrgName_Format(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+ [a-z1-9]{4}$`)
	for range 100 {
		name := generateLegibleOrgName()
		require.True(t, pattern.MatchString(name), "name %q does not match %s", name, pattern)
	}
}

func TestGenerateLegibleOrgName_Distribution(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 200)
	for range 200 {
		seen[generateLegibleOrgName()] = struct{}{}
	}
	require.Greater(t, len(seen), 100, "expected diverse names, got %d unique of 200", len(seen))
}
