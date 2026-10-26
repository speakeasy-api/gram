package mcpservers_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConsentConfigurationHashCompositionIsPinned guards every approved
// gateway connection: each records this hash per member, so changing what it
// covers asks every existing connection for consent again on deploy. A change
// here needs a transition that also accepts the previous hash, not just a new
// golden value. Secret header values stay out: only their name and flags count.
func TestConsentConfigurationHashCompositionIsPinned(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("queries.sql")
	require.NoError(t, err)
	query := string(source)
	start := strings.Index(query, "-- name: ListMCPServerConsentConfigurations")
	require.GreaterOrEqual(t, start, 0)
	query = query[start:]
	end := strings.Index(query, "AS configuration_hash")
	require.GreaterOrEqual(t, end, 0)
	query = query[:end]
	keys := regexp.MustCompile(`'([a-z_]+)',`).FindAllStringSubmatch(query, -1)
	composition := make([]string, 0, len(keys))
	for _, key := range keys {
		composition = append(composition, key[1])
	}
	require.Equal(t, []string{
		"remote", "url", "transport", "tunnel", "resource", "toolset", "environment",
		"variation", "issuer", "requires_auth", "headers",
	}, composition)
	require.Contains(t, query, "jsonb_build_array(h.name,\n                          CASE WHEN h.is_secret THEN NULL ELSE h.value END,\n                          h.value_from_request_header, h.is_required, h.is_secret)")
}
