package mcp_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConsentBackendFingerprintCompositionIsPinned guards saved consent reuse
// for single MCP servers: user_session_consent_resources.inventory_hash folds
// in this fingerprint, so changing what it covers makes every saved approval
// miss once and shows the full consent page again.
//
// It intentionally still differs from the gateway member hash
// (ListMCPServerConsentConfigurations): it hashes stored secret header
// ciphertext, visibility and network access, and omits transport,
// environment, the upstream auth requirement and the tunnel resource.
// Aligning the two needs a transition that also accepts the previous
// fingerprint during reuse.
func TestConsentBackendFingerprintCompositionIsPinned(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("authnchallenge_consent_reuse.go")
	require.NoError(t, err)
	body := string(source)
	start := strings.Index(body, "func (s *Service) consentBackendFingerprint(")
	require.GreaterOrEqual(t, start, 0)
	body = body[start:]
	end := strings.Index(body, "\n}\n")
	require.GreaterOrEqual(t, end, 0)
	body = body[:end]
	keys := regexp.MustCompile(`"([a-z_]+)":|evidence\["([a-z_]+)"\]`).FindAllStringSubmatch(body, -1)
	composition := make([]string, 0, len(keys))
	for _, key := range keys {
		composition = append(composition, key[1]+key[2])
	}
	require.Equal(t, []string{
		"resource", "toolset", "server", "remote", "tunnel", "toolset", "variation",
		"issuer", "visibility", "network", "url", "headers",
	}, composition)
	require.Contains(t, body, "ConfigurationFingerprint(ctx, remote.ID)")
}
