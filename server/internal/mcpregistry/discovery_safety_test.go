package mcpregistry

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryRejectsIncompleteCursorBindings(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	for _, raw := range []string{`{"name":"io.example/a","search":false,"version":"","include_deleted":false}`, `{"name":"io.example/a","search":"","version":"","include_deleted":false,"unknown":true}`, `{"name":"io.example/a","search":"","version":"","include_deleted":false} {}`} {
		_, err := s.Discover(ctx, DiscoveryOptions{Cursor: base64.RawURLEncoding.EncodeToString([]byte(raw))})
		require.ErrorIs(t, err, ErrInvalidCursor)
	}
	complete := map[string]any{"name": "io.example/a", "search": discoveryFilterBinding(""), "version": discoveryFilterBinding(""), "include_deleted": false}
	for _, field := range []string{"name", "search", "version", "include_deleted"} {
		for _, null := range []bool{false, true} {
			bad := map[string]any{}
			maps.Copy(bad, complete)
			if null {
				bad[field] = nil
			} else {
				delete(bad, field)
			}
			raw, err := json.Marshal(bad)
			require.NoError(t, err)
			_, err = s.Discover(ctx, DiscoveryOptions{Cursor: base64.RawURLEncoding.EncodeToString(raw)})
			require.ErrorIs(t, err, ErrInvalidCursor, string(raw))
		}
	}
}

func TestDiscoveryOmitsInvalidHistoricalRows(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	rows := []string{
		`{"server":{"name":"io.example/a","version":"1"}}`,
		`{"server":{"name":"io.example/b","version":"1","description":"valid"},"_meta":{"io.modelcontextprotocol.registry/official":{"status":false}}}`,
		`{"server":{"name":"io.example/c","version":"1","description":"valid"},"extra":"` + strings.Repeat("x", 8<<20) + `"}`,
		`{"server":{"name":"io.example/d","version":"1","description":"valid"}}`,
	}
	for _, raw := range rows {
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: []byte(raw), Published: true}))
	}
	for _, name := range []string{"io.example/a", "io.example/b", "io.example/c"} {
		_, err := s.LookupDiscoveryVersion(ctx, name, "latest", true)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = s.GetByName(ctx, name)
		require.NoError(t, err)
	}
	cursor := ""
	for i := range 4 {
		p, err := s.Discover(ctx, DiscoveryOptions{Limit: 1, Cursor: cursor, IncludeDeleted: true})
		require.NoError(t, err)
		if i < 3 {
			require.Empty(t, p.Records)
			require.NotEmpty(t, p.NextCursor)
			require.NotEqual(t, cursor, p.NextCursor)
		} else {
			require.Len(t, p.Records, 1)
			require.Empty(t, p.NextCursor)
		}
		cursor = p.NextCursor
	}
}

func TestDiscoveryMalformedNameScanProgress(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: []byte(`{"server":{"name":"42"}}`), Published: true}))
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: []byte(`{"server":{"name":"io.example/valid","description":"valid","version":"1"}}`), Published: true}))

	first, err := s.Discover(ctx, DiscoveryOptions{Limit: 1})
	require.NoError(t, err)
	require.Len(t, first.Records, 1)
	require.Empty(t, first.NextCursor)
}
