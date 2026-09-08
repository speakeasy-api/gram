package externalmcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
)

func TestMetadataScope(t *testing.T) {
	t.Parallel()
	const remoteURL = "https://mcp.example.test/tools?key=url-secret"
	opts := &ClientOptions{
		MetadataScope: "project-a:upstream-a",
		Authorization: "Bearer auth-secret",
		Headers:       map[string]string{"X-Api-Key": "header-secret", "X-Workspace": "workspace-a"},
	}
	scope := metadataScope(remoteURL, types.TransportTypeStreamableHTTP, opts)
	require.Len(t, scope, 64)
	for _, secret := range []string{"url-secret", "auth-secret", "header-secret", "project-a"} {
		require.NotContains(t, scope, secret)
	}
	cache := newToolMetadataCache(10, time.Minute)
	cache.put(scope, "search", json.RawMessage(`{}`))

	changed := *opts
	changed.Headers = map[string]string{"X-Workspace": "workspace-a", "X-Api-Key": "header-secret"}
	require.Equal(t, scope, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))

	changedScopes := []string{
		metadataScope(remoteURL+"2", types.TransportTypeStreamableHTTP, opts),
		metadataScope(remoteURL, types.TransportTypeSSE, opts),
	}
	changed.MetadataScope = "project-b:upstream-a"
	changedScopes = append(changedScopes, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))
	changed = *opts
	changed.MetadataScope = "project-a:upstream-b"
	changedScopes = append(changedScopes, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))
	changed = *opts
	changed.Authorization = "Bearer rotated-secret"
	changedScopes = append(changedScopes, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))
	changed = *opts
	changed.Headers = map[string]string{"X-Api-Key": "rotated-secret", "X-Workspace": "workspace-a"}
	changedScopes = append(changedScopes, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))
	changed.Headers = map[string]string{"X-Api-Key": "header-secret", "X-Workspace": "workspace-b"}
	changedScopes = append(changedScopes, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))
	for _, other := range changedScopes {
		require.NotEqual(t, scope, other)
		_, ok := cache.get(other, "search")
		require.False(t, ok, "metadata must not cross auth/config/project/upstream boundaries")
	}
	changed = *opts
	changed.MetadataScope = ""
	require.Empty(t, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, &changed))
	require.Empty(t, metadataScope(remoteURL, types.TransportTypeStreamableHTTP, nil))
}

func TestToolMetadataCacheCopiesAndRefreshesSchemas(t *testing.T) {
	t.Parallel()
	cache := newToolMetadataCache(2, time.Minute)
	schema := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Owner"}}}`)
	expected := string(schema)
	cache.put("scope", "search", schema)
	schema[0] = '['
	got, ok := cache.get("scope", "search")
	require.True(t, ok)
	require.Equal(t, expected, string(got))
	got[0] = '['
	again, ok := cache.get("scope", "search")
	require.True(t, ok)
	require.Equal(t, expected, string(again))

	cache.put("scope", "search", json.RawMessage(`{}`))
	refreshed, ok := cache.get("scope", "search")
	require.True(t, ok, "annotation-free metadata is a cache hit")
	require.JSONEq(t, `{}`, string(refreshed))
	_, ok = cache.get("scope", "other-tool")
	require.False(t, ok)
}

func TestToolMetadataCacheTTLAndEviction(t *testing.T) {
	t.Parallel()
	cache := newToolMetadataCache(2, time.Minute)
	now := time.Unix(0, 0)
	cache.now = func() time.Time { return now }
	cache.put("scope", "a", json.RawMessage(`{}`))
	cache.put("scope", "b", json.RawMessage(`{}`))
	now = now.Add(30 * time.Second)
	_, ok := cache.get("scope", "a")
	require.True(t, ok)
	cache.put("scope", "c", json.RawMessage(`{}`))
	_, ok = cache.get("scope", "b")
	require.False(t, ok, "least recently used entry must be evicted")
	require.Len(t, cache.entries, 2)
	now = now.Add(30 * time.Second)
	_, ok = cache.get("scope", "a")
	require.False(t, ok, "reads must not extend TTL, including annotation-free schemas")
	_, ok = cache.get("scope", "c")
	require.True(t, ok)
	cache.put("scope", "c", json.RawMessage(`{}`))
	now = now.Add(30 * time.Second)
	_, ok = cache.get("scope", "c")
	require.True(t, ok, "a refreshed entry gets a new TTL")
}

func TestToolMetadataCacheRejectsInvalidAndOversizedSchemas(t *testing.T) {
	t.Parallel()
	cache := newToolMetadataCache(2, time.Minute)
	for _, schema := range []string{"", "null", "[{}]", "false", "{broken", `"string"`, `{"description":"` + strings.Repeat("x", maxCachedToolSchemaBytes) + `"}`} {
		cache.put("scope", "search", json.RawMessage(`{}`))
		cache.put("scope", "search", json.RawMessage(schema))
		_, ok := cache.get("scope", "search")
		require.False(t, ok, "invalid replacement must not leave stale metadata")
	}
	cache.put("", "search", json.RawMessage(`{}`))
	_, ok := cache.get("", "search")
	require.False(t, ok)
	require.Empty(t, cache.entries)
}

func TestToolMetadataCacheConcurrentAccess(t *testing.T) {
	t.Parallel()
	cache := newToolMetadataCache(8, time.Minute)
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Go(func() {
			for range 100 {
				name := fmt.Sprintf("tool-%d", worker)
				cache.put("scope", name, json.RawMessage(`{}`))
				_, _ = cache.get("scope", name)
			}
		})
	}
	wg.Wait()
	require.LessOrEqual(t, len(cache.entries), 8)
	require.Equal(t, len(cache.entries), cache.lru.Len())
}
