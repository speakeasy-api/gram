package hooks

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestOrgDefaultHostCacheExpires(t *testing.T) {
	t.Parallel()

	cache := newOrgDefaultHostCache()
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	stored := pgtype.Text{String: "https://ai.example.test", Valid: true}

	_, ok := cache.get("<ORG_ID>", now)
	require.False(t, ok)

	cache.put("<ORG_ID>", stored, now)
	got, ok := cache.get("<ORG_ID>", now.Add(orgDefaultHostCacheTTL-time.Second))
	require.True(t, ok)
	require.Equal(t, stored, got)

	_, ok = cache.get("<ORG_ID>", now.Add(orgDefaultHostCacheTTL+time.Second))
	require.False(t, ok)
	require.NotContains(t, cache.entries, "<ORG_ID>", "expired entries are evicted")

	// A NULL default host is cached too, so organizations without one skip the read.
	cache.put("<OTHER_ORG_ID>", pgtype.Text{String: "", Valid: false}, now)
	got, ok = cache.get("<OTHER_ORG_ID>", now)
	require.True(t, ok)
	require.False(t, got.Valid)
}
