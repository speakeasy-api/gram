package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Without the member's own record, a cached-catalog connection's approved
// schema stands in; a live connection knows nothing.
func newMemberToolSchemaService(t *testing.T) *Service {
	t.Helper()
	return &Service{
		memberToolSchemaCache: cache.NewTypedObjectCache[memberToolSchemas](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone),
		memberToolSchemaLocal: expirable.NewLRU[string, memberToolSchemas](memberToolSchemasLocalSize, nil, memberToolSchemasTTL),
	}
}

func TestKnownMemberToolSchemaFallsBackToConsentedCatalog(t *testing.T) {
	t.Parallel()

	s := newMemberToolSchemaService(t)
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

// A complete listing makes every tool known; an oversized annotated schema
// stays unknown so a call lists it; an incomplete listing records nothing.
func TestRememberMemberToolSchemas(t *testing.T) {
	t.Parallel()

	s := newMemberToolSchemaService(t)
	member := metaMember{slug: "repo"}
	annotated := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	oversized := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("a", maxMemberToolSchemaBytes) + `","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	catalog := &memberCatalog{byName: map[string]*toolListEntry{
		"annotated": {Name: "annotated", InputSchema: annotated},
		"plain":     {Name: "plain", InputSchema: json.RawMessage(`{"type":"object"}`)},
		"oversized": {Name: "oversized", InputSchema: oversized},
	}}

	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: catalog.byName, incomplete: true})
	_, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "plain")
	require.False(t, known, "an incomplete listing records nothing")

	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, catalog)
	schema, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "annotated")
	require.True(t, known)
	require.JSONEq(t, string(annotated), string(schema))
	schema, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "plain")
	require.True(t, known)
	require.Empty(t, schema)
	schema, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "unlisted")
	require.True(t, known, "a complete listing that omits a tool needs no relisting")
	require.Empty(t, schema)
	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "oversized")
	require.False(t, known)
}
