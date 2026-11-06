package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newMemberToolSchemaService(t *testing.T) *Service {
	t.Helper()
	return &Service{
		memberToolSchemaCache: cache.NewTypedObjectCache[memberToolSchemas](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone),
		memberToolSchemaLocal: expirable.NewLRU[string, memberToolSchemas](memberToolSchemasLocalSize, nil, memberToolSchemasTTL),
	}
}

// Without the member's own record, a cached-catalog connection's approved
// schema stands in; a live connection knows nothing.
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

// A listing records the tools it saw; an oversized annotated schema and a
// tool no listing included stay unknown, so a call lists them.
func TestRememberMemberToolSchemas(t *testing.T) {
	t.Parallel()

	s := newMemberToolSchemaService(t)
	member := metaMember{slug: "repo"}
	annotated := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	oversized := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("a", maxMemberToolSchemaBytes) + `","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	now := time.Now()

	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: map[string]*toolListEntry{
		"annotated": {Name: "annotated", InputSchema: annotated},
		"oversized": {Name: "oversized", InputSchema: oversized},
	}, incomplete: false}, now)
	// A second caller's filtered listing adds to the record instead of replacing it.
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: map[string]*toolListEntry{
		"plain": {Name: "plain", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}, incomplete: false}, now)

	schema, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "annotated")
	require.True(t, known)
	require.JSONEq(t, string(annotated), string(schema))
	schema, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "plain")
	require.True(t, known)
	require.Empty(t, schema)
	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "unlisted")
	require.False(t, known, "a filtered listing cannot prove a tool needs no headers")
	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "oversized")
	require.False(t, known)

	// Tools no listing included for a whole lifetime are dropped.
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: map[string]*toolListEntry{}, incomplete: false}, now.Add(memberToolSchemasTTL))
	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "annotated")
	require.False(t, known)
}

// A tool last seen a whole lifetime ago is unknown even before a write prunes it.
func TestKnownMemberToolSchemaIgnoresStaleTools(t *testing.T) {
	t.Parallel()

	s := newMemberToolSchemaService(t)
	member := metaMember{slug: "repo"}
	annotated := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: map[string]*toolListEntry{
		"annotated": {Name: "annotated", InputSchema: annotated},
	}, incomplete: false}, time.Now().Add(-memberToolSchemasTTL-time.Minute))

	_, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "annotated")
	require.False(t, known)
}

// The total budget is spent in tool-name order; later schemas are marked oversized.
func TestRememberMemberToolSchemasTotalBudget(t *testing.T) {
	t.Parallel()

	s := newMemberToolSchemaService(t)
	member := metaMember{slug: "repo"}
	schema := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("a", maxMemberToolSchemaBytes-200) + `","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	byName := map[string]*toolListEntry{}
	count := maxMemberToolSchemasBytes/len(schema) + 2
	for i := range count {
		name := fmt.Sprintf("tool_%02d", i)
		byName[name] = &toolListEntry{Name: name, InputSchema: schema}
	}
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: byName, incomplete: false}, time.Now())

	kept := maxMemberToolSchemasBytes / len(schema)
	for i := range count {
		_, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, fmt.Sprintf("tool_%02d", i))
		require.Equal(t, i < kept, known, "tool_%02d", i)
	}
}

// The shared Redis record is read back, a real miss reads nothing (not this
// process's copy), and a record never answers for another destination.
func TestMemberToolSchemasSharedCache(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	adapter := cache.NewRedisCacheAdapter(client)
	newService := func() *Service {
		return &Service{
			memberToolSchemaCache: cache.NewTypedObjectCache[memberToolSchemas](testenv.NewLogger(t), adapter, cache.SuffixNone),
			memberToolSchemaLocal: expirable.NewLRU[string, memberToolSchemas](memberToolSchemasLocalSize, nil, memberToolSchemasTTL),
		}
	}
	member := metaMember{slug: "repo", serverID: uuid.New()}
	other := metaMember{slug: "repo", serverID: uuid.New()}
	annotated := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)

	writer := newService()
	writer.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, &memberCatalog{byName: map[string]*toolListEntry{
		"annotated": {Name: "annotated", InputSchema: annotated},
	}, incomplete: false}, time.Now())

	reader := newService()
	schema, known := reader.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "annotated")
	require.True(t, known, "another process reads the shared record")
	require.JSONEq(t, string(annotated), string(schema))
	_, known = reader.knownMemberToolSchema(t.Context(), &metaGateContext{}, other, "annotated")
	require.False(t, known, "a record never answers for another destination")

	mr.FlushAll()
	_, known = writer.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "annotated")
	require.False(t, known, "a shared-cache miss is not answered from this process's copy")
}
