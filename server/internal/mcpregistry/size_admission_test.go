package mcpregistry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

func TestSerializedSizeAdmission(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"spaces", "exponent", "input"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := newTestService(t)
			base := `{"server":{"name":"io.example/size","description":"test","version":"1"},"_meta":{"io.example/private":{"integer":9007199254740993,"decimal":1.234567890123456789}},"extension":`
			var raw string
			switch mode {
			case "spaces":
				raw = base + `"` + strings.Repeat("x", (8<<20)-len(base)-4) + `"}`
			case "exponent":
				raw = base + `[` + strings.TrimSuffix(strings.Repeat("1e100000,", 85), ",") + `]}`
			case "input":
				raw = base + `0}` + strings.Repeat(" ", 8<<20)
			}
			_, err := s.Create(ctx, json.RawMessage(raw))
			var invalid *InvalidError
			require.ErrorAs(t, err, &invalid)
			require.Empty(t, invalid.Issues[0].Path)
			require.Less(t, len(invalid.Issues[0].Message), 100)
			page, err := s.List(ctx, ListOptions{Limit: 10})
			require.NoError(t, err)
			require.Empty(t, page.Entries)
			good := json.RawMessage(base + `0}`)
			old, err := s.Create(ctx, good)
			require.NoError(t, err)
			old, err = s.SetPublished(ctx, old.ID, Token(old), false)
			require.NoError(t, err)
			_, err = s.Save(ctx, old.ID, Token(old), json.RawMessage(raw))
			require.ErrorAs(t, err, &invalid)
			got, err := s.Get(ctx, old.ID)
			require.NoError(t, err)
			require.Equal(t, old, got)
			saved, err := s.Save(ctx, old.ID, Token(old), good)
			require.NoError(t, err)
			got, err = s.Get(ctx, saved.ID)
			require.NoError(t, err)
			require.Contains(t, string(got.Data), "9007199254740993")
			require.Contains(t, string(got.Data), "1.234567890123456789")
			page, err = s.List(ctx, ListOptions{Limit: 10})
			require.NoError(t, err)
			require.Len(t, page.Entries, 1)
			require.Empty(t, page.Entries[0].Issues)
			_, err = s.SetPublished(ctx, got.ID, Token(got), true)
			require.NoError(t, err)
		})
	}
}

func TestOversizedStoredRecordRemainsRepairable(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	id := uuid.New()
	good := `{"server":{"name":"io.example/legacy","description":"test","version":"1"}}`
	raw := strings.TrimSuffix(good, "}") + `,"extension":"` + strings.Repeat("x", StoredRecordByteLimit) + `"}`
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: false}))
	old, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Greater(t, len(old.Data), StoredRecordByteLimit)
	page, err := s.List(ctx, ListOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	require.NotEmpty(t, page.Entries[0].Issues)
	_, err = s.SetPublished(ctx, id, Token(old), true)
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	unchanged, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, old, unchanged)
	repaired, err := s.Save(ctx, id, Token(old), []byte(good))
	require.NoError(t, err)
	_, err = s.SetPublished(ctx, id, Token(repaired), true)
	require.NoError(t, err)
}

func TestSQLStoredRecordLimitAdmission(t *testing.T) {
	t.Parallel()
	ctx, _, db := newTestService(t)
	q := repo.New(db)
	raw := []byte(basicRecord)
	_, err := q.CreateEntry(ctx, repo.CreateEntryParams{Data: raw, StoredRecordLimit: 1})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	count, err := q.CountRegistryEntries(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	original, err := q.CreateEntry(ctx, repo.CreateEntryParams{Data: raw, StoredRecordLimit: StoredRecordByteLimit})
	require.NoError(t, err)
	_, err = q.UpdateEntry(ctx, repo.UpdateEntryParams{ID: original.ID, Data: raw, StoredRecordLimit: 1})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	retained, err := q.GetEntry(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, original, retained)
}
