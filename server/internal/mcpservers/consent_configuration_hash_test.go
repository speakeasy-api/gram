package mcpservers_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConsentConfigurationHashCompositionIsPinned guards every stored
// consent: approvals record this hash, so changing what it covers invalidates
// every existing consent on deploy. A change here needs a versioned
// transition that accepts the previous hash, not just a new golden value.
//
// The query builds every key but the upstream issuer once and merges the
// issuer in for each source. jsonb stores keys sorted, so the merged object
// renders exactly as building all the keys together did.
func TestConsentConfigurationHashCompositionIsPinned(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("queries.sql")
	require.NoError(t, err)
	query := string(source)
	start := strings.Index(query, "-- name: ListMCPServerConsentConfigurations")
	require.GreaterOrEqual(t, start, 0)
	query = query[start:]
	end := strings.Index(query, "FROM servers;")
	require.GreaterOrEqual(t, end, 0)
	query = query[:end]

	require.Contains(t, query, "(base || jsonb_build_object('issuer', remote_session_issuer_id))::text, 'UTF8')), 'hex')::text AS configuration_hash",
		"the stored hash takes the issuer from the column")
	require.Contains(t, query, "CASE WHEN cardinality(association_provider_ids) = 1 THEN association_provider_ids[1] END))::text, 'UTF8')), 'hex')::text AS association_configuration_hash",
		"the association hash differs only in the issuer")

	baseStart := strings.Index(query, "jsonb_build_object(\n")
	require.GreaterOrEqual(t, baseStart, 0)
	baseEnd := strings.Index(query, ") AS base,")
	require.Greater(t, baseEnd, baseStart)
	base := query[baseStart:baseEnd]
	keys := regexp.MustCompile(`'([a-z_]+)',`).FindAllStringSubmatch(base, -1)
	composition := make([]string, 0, len(keys))
	for _, key := range keys {
		composition = append(composition, key[1])
	}
	require.Equal(t, []string{
		"remote", "url", "transport", "tunnel", "resource", "toolset", "environment",
		"variation", "visibility", "network", "requires_auth", "headers",
	}, composition)
	require.Contains(t, base, "jsonb_build_array(h.name, h.value,\n                              h.value_from_request_header, h.is_required, h.is_secret)")
}
