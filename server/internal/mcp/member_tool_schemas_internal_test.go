package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Without the member's own record, a cached-catalog connection's approved
// schema stands in; a live connection knows nothing.
func TestKnownMemberToolSchemaFallsBackToConsentedCatalog(t *testing.T) {
	t.Parallel()

	s := &Service{memberToolSchemaCache: cache.NewTypedObjectCache[memberToolSchemas](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone)}
	member := metaMember{slug: "repo"}
	schema := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	catalog := &connectionCatalog{Tools: []*toolListEntry{{Name: "repo--get_file_contents", InputSchema: schema}}}

	got, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{catalog: catalog, catalogRef: &toolfilter.CatalogReference{Mode: toolfilter.CatalogModeCached}}, member, "get_file_contents")
	require.True(t, known)
	require.JSONEq(t, string(schema), string(got))

	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{catalog: catalog, catalogRef: &toolfilter.CatalogReference{Mode: toolfilter.CatalogModeLive}}, member, "get_file_contents")
	require.False(t, known)

	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "get_file_contents")
	require.False(t, known)
}
