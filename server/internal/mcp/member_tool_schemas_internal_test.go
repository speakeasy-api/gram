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

var annotatedToolSchema = json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)

func newMemberToolSchemaService(t *testing.T, backing cache.Cache) *Service {
	t.Helper()
	return &Service{
		memberToolSchemaCache: cache.NewTypedObjectCache[memberToolSchemas](testenv.NewLogger(t), backing, cache.SuffixNone),
		memberToolSchemaLocal: expirable.NewLRU[string, memberToolSchemas](memberToolSchemasLocalSize, nil, maxMemberToolSchemasTTL),
	}
}

func newRedisSchemaCache(t *testing.T) (cache.Cache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return cache.NewRedisCacheAdapter(client), mr
}

func listedCatalog(ttl time.Duration, private bool, tools map[string]json.RawMessage) *memberCatalog {
	byName := map[string]*toolListEntry{}
	for name, schema := range tools {
		byName[name] = &toolListEntry{Name: name, InputSchema: schema}
	}
	return &memberCatalog{entries: nil, byName: byName, incomplete: false, cacheTTL: ttl, cachePrivate: private}
}

// Without the member's own record, a cached-catalog connection's approved
// schema stands in; a live connection knows nothing.
func TestKnownMemberToolSchemaFallsBackToConsentedCatalog(t *testing.T) {
	t.Parallel()

	s := newMemberToolSchemaService(t, cache.NoopCache)
	member := metaMember{slug: "repo"}
	catalog := &connectionCatalog{Tools: []*toolListEntry{{Name: "repo--get_file_contents", InputSchema: annotatedToolSchema}}}
	now := time.Now()

	got, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{catalog: catalog, catalogRef: &toolfilter.CatalogReference{Mode: toolfilter.CatalogModeCached}}, member, "caller", "get_file_contents", now)
	require.True(t, known)
	require.JSONEq(t, string(annotatedToolSchema), string(got))

	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{catalog: catalog, catalogRef: &toolfilter.CatalogReference{Mode: toolfilter.CatalogModeLive}}, member, "caller", "get_file_contents", now)
	require.False(t, known)

	_, known = s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "caller", "get_file_contents", now)
	require.False(t, known)
}

// A listing records the tools it saw; an oversized annotated schema and a
// tool no listing included stay unknown, so a call lists them.
func TestRememberMemberToolSchemas(t *testing.T) {
	t.Parallel()

	backing, _ := newRedisSchemaCache(t)
	s := newMemberToolSchemaService(t, backing)
	member := metaMember{slug: "repo"}
	oversized := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("a", maxMemberToolSchemaBytes) + `","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	now := time.Now()
	gate := &metaGateContext{}

	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, "a", listedCatalog(time.Hour, false, map[string]json.RawMessage{"annotated": annotatedToolSchema, "oversized": oversized}), now)
	// A second caller's filtered listing adds to the record instead of replacing it.
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, "b", listedCatalog(time.Hour, false, map[string]json.RawMessage{"plain": json.RawMessage(`{"type":"object"}`)}), now)

	schema, known := s.knownMemberToolSchema(t.Context(), gate, member, "c", "annotated", now)
	require.True(t, known)
	require.JSONEq(t, string(annotatedToolSchema), string(schema))
	schema, known = s.knownMemberToolSchema(t.Context(), gate, member, "c", "plain", now)
	require.True(t, known)
	require.Empty(t, schema)
	_, known = s.knownMemberToolSchema(t.Context(), gate, member, "c", "unlisted", now)
	require.False(t, known, "a filtered listing cannot prove a tool needs no headers")
	_, known = s.knownMemberToolSchema(t.Context(), gate, member, "c", "oversized", now)
	require.False(t, known)
}

// The listing's ttlMs bounds the record, capped at six hours; a listing with
// no positive ttlMs is stale at once and recorded nowhere.
func TestRememberMemberToolSchemasHonoursTTL(t *testing.T) {
	t.Parallel()

	backing, _ := newRedisSchemaCache(t)
	s := newMemberToolSchemaService(t, backing)
	gate := &metaGateContext{}
	now := time.Now()

	stale := metaMember{slug: "stale", serverID: uuid.New()}
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), stale, "", listedCatalog(0, false, map[string]json.RawMessage{"annotated": annotatedToolSchema}), now)
	_, known := s.knownMemberToolSchema(t.Context(), gate, stale, "", "annotated", now)
	require.False(t, known)

	short := metaMember{slug: "short", serverID: uuid.New()}
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), short, "", listedCatalog(time.Minute, false, map[string]json.RawMessage{"annotated": annotatedToolSchema}), now)
	_, known = s.knownMemberToolSchema(t.Context(), gate, short, "", "annotated", now.Add(30*time.Second))
	require.True(t, known)
	_, known = s.knownMemberToolSchema(t.Context(), gate, short, "", "annotated", now.Add(time.Minute))
	require.False(t, known)

	long := metaMember{slug: "long", serverID: uuid.New()}
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), long, "", listedCatalog(48*time.Hour, false, map[string]json.RawMessage{"annotated": annotatedToolSchema}), now)
	_, known = s.knownMemberToolSchema(t.Context(), gate, long, "", "annotated", now.Add(maxMemberToolSchemasTTL))
	require.False(t, known, "the record never outlives the cap")
}

// The total budget is spent in tool-name order; later schemas are marked oversized.
func TestRememberMemberToolSchemasTotalBudget(t *testing.T) {
	t.Parallel()

	backing, _ := newRedisSchemaCache(t)
	s := newMemberToolSchemaService(t, backing)
	member := metaMember{slug: "repo"}
	schema := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("a", maxMemberToolSchemaBytes-200) + `","properties":{"owner":{"type":"string","x-mcp-header":"owner"}}}`)
	tools := map[string]json.RawMessage{}
	count := maxMemberToolSchemasBytes/len(schema) + 2
	for i := range count {
		tools[fmt.Sprintf("tool_%02d", i)] = schema
	}
	now := time.Now()
	s.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), member, "", listedCatalog(time.Hour, false, tools), now)

	kept := maxMemberToolSchemasBytes / len(schema)
	for i := range count {
		_, known := s.knownMemberToolSchema(t.Context(), &metaGateContext{}, member, "", fmt.Sprintf("tool_%02d", i), now)
		require.Equal(t, i < kept, known, "tool_%02d", i)
	}
}

// A public record is shared through Redis across processes and callers; a
// private one only with the caller whose credential listed it. A real miss is
// not answered from this process's copy, and a record never answers for
// another destination.
func TestMemberToolSchemasSharedCacheScopes(t *testing.T) {
	t.Parallel()

	backing, mr := newRedisSchemaCache(t)
	writer := newMemberToolSchemaService(t, backing)
	reader := newMemberToolSchemaService(t, backing)
	gate := &metaGateContext{}
	now := time.Now()
	public := metaMember{slug: "public", serverID: uuid.New()}
	private := metaMember{slug: "private", serverID: uuid.New()}

	writer.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), public, "caller-a", listedCatalog(time.Hour, false, map[string]json.RawMessage{"annotated": annotatedToolSchema}), now)
	writer.rememberMemberToolSchemas(t.Context(), testenv.NewLogger(t), private, "caller-a", listedCatalog(time.Hour, true, map[string]json.RawMessage{"annotated": annotatedToolSchema}), now)

	schema, known := reader.knownMemberToolSchema(t.Context(), gate, public, "caller-b", "annotated", now)
	require.True(t, known, "a public listing serves every caller")
	require.JSONEq(t, string(annotatedToolSchema), string(schema))
	_, known = reader.knownMemberToolSchema(t.Context(), gate, private, "caller-a", "annotated", now)
	require.True(t, known, "a private listing serves its own caller")
	_, known = reader.knownMemberToolSchema(t.Context(), gate, private, "caller-b", "annotated", now)
	require.False(t, known, "a private listing is never shared across credentials")
	_, known = reader.knownMemberToolSchema(t.Context(), gate, metaMember{slug: "public", serverID: uuid.New()}, "caller-a", "annotated", now)
	require.False(t, known, "a record never answers for another destination")

	mr.FlushAll()
	_, known = writer.knownMemberToolSchema(t.Context(), gate, public, "caller-a", "annotated", now)
	require.False(t, known, "a shared-cache miss is not answered from this process's copy")
}
