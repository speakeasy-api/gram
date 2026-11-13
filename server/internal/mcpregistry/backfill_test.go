package mcpregistry

import (
	_ "embed"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

//go:embed backfill.sql
var registryBackfillSQL string

func TestVersionBackfillPreservesLegacyRecords(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for i, published := range []bool{true, false} {
		id := uuid.New()
		raw := []byte(`{"server":{"name":"example.test/` + id.String() + `","version":"legacy"}}`)
		require.NoError(t, repo.New(db).InsertRegistryUnlinkedEntryFixture(ctx, repo.InsertRegistryUnlinkedEntryFixtureParams{ID: id, Data: raw, Published: published}))
		before, err := s.Get(ctx, id)
		require.NoError(t, err)
		_, err = db.Exec(ctx, registryBackfillSQL) //nolint:glint // notestingrawsql: execute the embedded operator backfill in a disposable test database.
		require.NoError(t, err)
		first, err := s.Get(ctx, id)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, first.ServerID)
		_, err = db.Exec(ctx, registryBackfillSQL) //nolint:glint // notestingrawsql: verify operator backfill idempotence in a disposable test database.
		require.NoError(t, err)
		second, err := s.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, first, second)
		before.ServerID = second.ServerID
		require.Equal(t, before, second)
		count, err := repo.New(db).CountRegistryServers(ctx)
		require.NoError(t, err)
		require.EqualValues(t, i+1, count)
		require.JSONEq(t, string(raw), string(second.Data))
		parent, err := s.GetServer(ctx, second.ServerID)
		require.NoError(t, err)
		require.True(t, parent.Visible)
		if published {
			require.Equal(t, &id, parent.DefaultEntryID)
		} else {
			require.Nil(t, parent.DefaultEntryID)
		}
		require.NoError(t, repo.New(db).HideRegistryServerFixture(ctx, parent.ID))
		_, err = db.Exec(ctx, registryBackfillSQL) //nolint:glint // notestingrawsql: verify later operator decisions survive rerunning the embedded backfill.
		require.NoError(t, err)
		parent, err = s.GetServer(ctx, parent.ID)
		require.NoError(t, err)
		require.False(t, parent.Visible)
		require.Nil(t, parent.DefaultEntryID)
	}
}

func TestCreateServerIdentityAtomic(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	created, err := s.Create(ctx, []byte(basicRecord))
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ServerID)
	parent, err := s.GetServer(ctx, created.ServerID)
	require.NoError(t, err)
	require.Equal(t, "example.test/demo", parent.Name)
	require.True(t, parent.Visible)
	require.Equal(t, &created.ID, parent.DefaultEntryID)
	_, err = s.Create(ctx, []byte(basicRecord))
	require.ErrorIs(t, err, ErrConflict)
	count, err := repo.New(db).CountRegistryServers(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
