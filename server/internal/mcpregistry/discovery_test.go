package mcpregistry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryVisibilityAndPagination(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, row := range []struct {
		name, status string
		published    bool
	}{
		{"io.example/a", "active", true}, {"io.example/b", "deleted", true}, {"io.example/c", "active", false}, {"io.example/d_under", "active", true},
	} {
		data, _ := json.Marshal(map[string]any{"server": map[string]any{"name": row.name, "version": "1", "description": "synthetic record"}, "_meta": map[string]any{"io.modelcontextprotocol.registry/official": map[string]any{"status": row.status}}, "extension": json.RawMessage(`9007199254740993`)})
		require.Empty(t, s.validator.Validate(data))
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: data, Published: row.published}))
	}
	p, err := s.Discover(ctx, DiscoveryOptions{Limit: 1})
	require.NoError(t, err)
	require.Len(t, p.Records, 1)
	require.NotEmpty(t, p.NextCursor)
	require.Contains(t, string(p.Records[0]), "9007199254740993")
	next, err := s.Discover(ctx, DiscoveryOptions{Limit: 1, Cursor: p.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Records, 1)
	require.Empty(t, next.NextCursor)
	_, err = s.Discover(ctx, DiscoveryOptions{Cursor: p.NextCursor, IncludeDeleted: true})
	require.ErrorIs(t, err, ErrInvalidCursor)
	all, err := s.Discover(ctx, DiscoveryOptions{IncludeDeleted: true})
	require.NoError(t, err)
	require.Len(t, all.Records, 3)
	literal, err := s.Discover(ctx, DiscoveryOptions{Search: "%_"})
	require.NoError(t, err)
	require.Empty(t, literal.Records)
	for _, name := range []string{"io.example/b", "io.example/c"} {
		_, err = s.LookupDiscoveryVersion(ctx, name, "latest", false)
		require.ErrorIs(t, err, ErrNotFound)
	}
	_, err = s.LookupDiscoveryVersion(ctx, "io.example/b", "latest", true)
	require.NoError(t, err)
	_, err = s.LookupDiscoveryVersion(ctx, "io.example/c", "latest", true)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = s.LookupDiscoveryVersion(ctx, "io.example/a", "old", true)
	require.ErrorIs(t, err, ErrNotFound)
	for _, value := range []string{"", "malformed", "2026-01-01T00:00:00Z"} {
		_, err = s.Discover(ctx, DiscoveryOptions{UpdatedSince: &value})
		require.ErrorIs(t, err, ErrUnsupportedUpdatedSince)
	}
}

func TestDiscoveryPageByteBudgetPreservesContinuation(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, name := range []string{"io.example/budget-a", "io.example/budget-b", "io.example/budget-c"} {
		data, err := json.Marshal(map[string]any{"server": map[string]any{"name": name, "version": "1", "description": "synthetic record"}, "extension": strings.Repeat("x", 6<<20)})
		require.NoError(t, err)
		require.Empty(t, s.validator.Validate(data))
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: data, Published: true}))
	}
	rows, err := repo.New(db).DiscoverEntries(ctx, repo.DiscoverEntriesParams{Search: "budget-", PageLimit: 101, ByteBudget: 16 << 20})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Empty(t, rows[2].Data, "over-budget body must not cross the database boundary")
	page, err := s.Discover(ctx, DiscoveryOptions{Search: "budget-", Limit: 100})
	require.NoError(t, err)
	require.Len(t, page.Records, 2)
	require.NotEmpty(t, page.NextCursor)
	next, err := s.Discover(ctx, DiscoveryOptions{Search: "budget-", Limit: 100, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Records, 1)
	require.Contains(t, string(next.Records[0]), "budget-c")
	require.Empty(t, next.NextCursor)
}

func TestDiscoveryMetadataPreservesUnescapedPayloadBudget(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, name := range []string{"example.test/budget-a", "example.test/budget-b", "example.test/budget-c"} {
		raw := `{"server":{"name":"` + name + `","description":"Budget","version":"1"},"_meta":{"example.test/raw":{"integer":9007199254740993,"decimal":1.234567890123456789,"payload":"` + strings.Repeat("<", 6<<20) + `"}}}`
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: []byte(raw), Published: true}))
	}
	page, err := s.Discover(ctx, DiscoveryOptions{Limit: 100})
	require.NoError(t, err)
	require.Len(t, page.Records, 2)
	for _, record := range page.Records {
		require.Contains(t, string(record), "9007199254740993")
		require.Contains(t, string(record), "1.234567890123456789")
		require.NotContains(t, string(record), `\u003c`)
	}
	require.LessOrEqual(t, len(page.Records[0])+len(page.Records[1]), 16<<20)
	require.NotEmpty(t, page.NextCursor)
	next, err := s.Discover(ctx, DiscoveryOptions{Limit: 100, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Records, 1)
	require.Equal(t, "example.test/budget-c", recordName(next.Records[0]))
	require.Empty(t, next.NextCursor)
}

func TestRegistryNameExistsWithoutDefaultOrEntries(t *testing.T) {
	t.Parallel()
	ctx, _, db := newTestService(t)
	tx := testenv.BeginTx(t, ctx, db)
	_, err := repo.New(tx).CreateServer(ctx, "example.test/demo")
	require.NoError(t, err)
	taken, err := NameTakenTx(ctx, tx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	require.True(t, taken)
	taken, err = NameTakenTx(ctx, tx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/other", 1)))
	require.NoError(t, err)
	require.False(t, taken)
}
