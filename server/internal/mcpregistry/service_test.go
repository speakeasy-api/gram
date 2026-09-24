package mcpregistry

import (
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRetainedUnpublishedGet(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	id := uuid.New()
	raw := `{"server":{"name":"app.example/test","version":"1"},"extension":9007199254740993}`
	err := repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: false})
	require.NoError(t, err)
	e, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.False(t, e.Published)
	require.Contains(t, string(e.Data), "9007199254740993")
	byName, err := s.GetByName(ctx, "app.example/test")
	require.NoError(t, err)
	require.Equal(t, e.ID, byName.ID)
	_, err = s.Get(ctx, uuid.New())
	require.ErrorIs(t, err, ErrNotFound)
}
func TestInvalidStoredRecordReadable(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	id := uuid.New()
	err := repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(`{"server":{"name":"invalid"}}`), Published: true})
	require.NoError(t, err)
	e, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, json.Valid(e.Data))
	require.NotEmpty(t, s.validator.Validate(e.Data))
	p, err := s.List(ctx, ListOptions{})
	require.NoError(t, err)
	require.Len(t, p.Entries, 1)
	require.NotEmpty(t, p.Entries[0].Issues)
}
func TestReadyMissingTable(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	require.NoError(t, s.Ready(ctx))
	_, err := db.Exec(ctx, "DROP TABLE mcp_registry_entries") //nolint:glint // notestingrawsql: drop only this test database table to exercise readiness failure; never expose destructive DDL through production SQLc methods.
	require.NoError(t, err)
	require.Error(t, s.Ready(ctx))
}
func TestListPagination(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, name := range []string{"app.example/B", "app.example/a", "app.example/a%_", "app.example/z"} {
		err := repo.New(db).InsertRegistryNamedEntryFixture(ctx, name)
		require.NoError(t, err)
	}
	p, err := s.List(ctx, ListOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, p.Entries, 2)
	require.Equal(t, "app.example/B", p.Entries[0].Name)
	require.NotEmpty(t, p.NextCursor)
	next, err := s.List(ctx, ListOptions{Limit: 2, Cursor: p.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Entries, 2)
	require.Empty(t, next.NextCursor)
	_, err = s.List(ctx, ListOptions{Query: "a", Cursor: p.NextCursor})
	require.Error(t, err)
	no := false
	_, err = s.List(ctx, ListOptions{Published: &no, Cursor: p.NextCursor})
	require.Error(t, err)
	for _, cursor := range []string{"!", strings.Repeat("a", 5000)} {
		_, err = s.List(ctx, ListOptions{Cursor: cursor})
		require.Error(t, err)
	}
	literal, err := s.List(ctx, ListOptions{Query: "%_"})
	require.NoError(t, err)
	require.Len(t, literal.Entries, 1)
}

func TestInvalidRecordsPagination(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, raw := range []string{`{"server":{"name":"app.example/number","description":123}}`, `{"server":{"name":"app.example/boolean","description":false}}`, `{"server":{"name":"app.example/null","description":null}}`, `{"server":{"name":"app.example/missing"}}`, `{"server":{"name":"app.example/valid"}}`} {
		id := uuid.New()
		err := repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: true})
		require.NoError(t, err)
		e, err := s.Get(ctx, id)
		require.NoError(t, err)
		require.JSONEq(t, raw, string(e.Data))
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for range 6 {
		p, err := s.List(ctx, ListOptions{Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		require.Len(t, p.Entries, 1)
		require.False(t, seen[p.Entries[0].ID], "duplicate cursor row")
		seen[p.Entries[0].ID] = true
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Empty(t, cursor)
	require.Len(t, seen, 5)
}
func TestListLimitsAndPublication(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	err := repo.New(db).InsertRegistryPublicationFixture(ctx)
	require.NoError(t, err)
	p, err := s.List(ctx, ListOptions{})
	require.NoError(t, err)
	require.Len(t, p.Entries, 25)
	require.NotEmpty(t, p.NextCursor)
	p, err = s.List(ctx, ListOptions{Limit: 50})
	require.NoError(t, err)
	require.Len(t, p.Entries, 50)
	require.NotEmpty(t, p.NextCursor)
	for _, limit := range []int32{-1, 51} {
		_, err = s.List(ctx, ListOptions{Limit: limit})
		require.Error(t, err)
	}
	for _, published := range []bool{true, false} {
		p, err := s.List(ctx, ListOptions{Published: &published, Limit: 50})
		require.NoError(t, err)
		require.Len(t, p.Entries, 30)
		require.Empty(t, p.NextCursor)
		for _, e := range p.Entries {
			require.Equal(t, published, e.Published)
		}
	}
}

func TestCaseVariantNamesPagination(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	records := []string{`{"server":{"name":"app.example/c"},"Server":{"Name":"zz"}}`, `{"server":{"name":"app.example/d","Name":"zz"}}`, `{"server":{"name":"app.example/e"},"SERVER":{"name":"zz"}}`, `{"server":{"name":"app.example/a"},"Server":{"Name":"zz"}}`, `{"server":{"name":"app.example/f","version":42}}`, `{"server":{"name":"app.example/g","description":false}}`, `{"server":{"name":"app.example/b"}}`}
	expected := map[uuid.UUID]bool{}
	for _, raw := range records {
		id := uuid.New()
		expected[id] = true
		err := repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: true})
		require.NoError(t, err)
		e, err := s.Get(ctx, id)
		require.NoError(t, err)
		require.JSONEq(t, raw, string(e.Data))
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for range len(records) + 1 {
		p, err := s.List(ctx, ListOptions{Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		require.Len(t, p.Entries, 1)
		e := p.Entries[0]
		require.False(t, seen[e.ID])
		seen[e.ID] = true
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Empty(t, cursor)
	require.Equal(t, expected, seen)
}
func TestInvalidListText(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	encode := func(c listCursor) string {
		raw, err := json.Marshal(c)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	for _, cursor := range []string{encode(listCursor{ID: uuid.New(), Name: "bad\x00name"}), "!", encode(listCursor{ID: uuid.New(), Query: "different"})} {
		_, err := s.List(ctx, ListOptions{Cursor: cursor})
		require.ErrorIs(t, err, ErrInvalidCursor)
	}
	_, err := s.List(ctx, ListOptions{Query: "bad\x00query"})
	require.ErrorIs(t, err, ErrInvalidListOptions)
}

func TestOversizeSearchText(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	_, err := s.List(ctx, ListOptions{Query: strings.Repeat("x", 1025)})
	require.ErrorIs(t, err, ErrInvalidListOptions)
}

func TestListUnavailableValidator(t *testing.T) {
	t.Parallel()
	for _, v := range []*Validator{nil, {}} {
		_, err := New(nil, v).List(t.Context(), ListOptions{})
		require.EqualError(t, err, "registry validator unavailable")
	}
}

func TestListByteBudget(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, name := range []string{"io.example/a", "io.example/b", "io.example/c"} {
		raw := []byte(`{"server":{"name":"` + name + `","description":"test","version":"1"},"extension":"` + strings.Repeat("x", 3<<20) + `"}`)
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: raw, Published: true}))
	}
	rows, err := repo.New(db).ListEntries(ctx, repo.ListEntriesParams{ByteBudget: 8 << 20, PageLimit: 51})
	require.NoError(t, err)
	materialized := 0
	for _, row := range rows {
		materialized += len(row.Data)
	}
	require.LessOrEqual(t, materialized, 8<<20)
	require.Len(t, rows, 3)
	require.Empty(t, rows[2].Data)

	first, err := s.List(ctx, ListOptions{Limit: 50})
	require.NoError(t, err)
	require.Len(t, first.Entries, 2)
	require.NotEmpty(t, first.NextCursor)
	second, err := s.List(ctx, ListOptions{Limit: 50, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Entries, 1)
	require.Equal(t, "io.example/c", second.Entries[0].Name)
	require.Empty(t, second.NextCursor)
}

func TestListOversizeStoredRecord(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	raw := []byte(`{"server":{"name":"io.example/a"},"extension":"` + strings.Repeat("x", 9<<20) + `"}`)
	id := uuid.New()
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: raw, Published: true}))
	require.NoError(t, repo.New(db).InsertRegistryNamedEntryFixture(ctx, "io.example/b"))
	rows, err := repo.New(db).ListEntries(ctx, repo.ListEntriesParams{ByteBudget: 8 << 20, PageLimit: 2})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Empty(t, rows[0].Data)

	first, err := s.List(ctx, ListOptions{Limit: 1})
	require.NoError(t, err)
	require.Len(t, first.Entries, 1)
	require.Equal(t, id, first.Entries[0].ID)
	require.Equal(t, "io.example/a", first.Entries[0].Name)
	require.Equal(t, []Issue{{Message: "record exceeds byte limit"}}, first.Entries[0].Issues)
	second, err := s.List(ctx, ListOptions{Limit: 1, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Entries, 1)
	require.Equal(t, "io.example/b", second.Entries[0].Name)
	retained, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Greater(t, len(retained.Data), 8<<20)
}
