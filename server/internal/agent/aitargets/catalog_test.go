package aitargets_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestLoadSeedsDefaultsOnFirstRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	catalog := aitargets.NewCatalog(testenv.NewLogger(t), conn, aitargets.DefaultCacheTTL)

	snapshot, err := catalog.Load(ctx)
	require.NoError(t, err)

	defaults := aitargets.Defaults()
	require.EqualValues(t, len(defaults), snapshot.ListVersion, "one seed revision per default target")
	require.Equal(t, aitargets.NewSnapshot(snapshot.ListVersion, defaults).Targets(), snapshot.Targets())

	revisions, err := repo.New(conn).ListAIScanCatalogRevisions(ctx, 100)
	require.NoError(t, err)
	require.Len(t, revisions, len(defaults))
	for _, revision := range revisions {
		require.Equal(t, aitargets.ActionSeed, revision.Action)
		require.False(t, revision.ActorUserID.Valid, "the seed has no actor")
		require.Nil(t, revision.TargetBefore)
		require.NotEmpty(t, revision.TargetAfter)
	}
}

func TestSeedDefaultsIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)

	require.NoError(t, aitargets.SeedDefaults(ctx, conn))
	require.NoError(t, aitargets.SeedDefaults(ctx, conn))

	version, err := repo.New(conn).GetAIScanCatalogListVersion(ctx)
	require.NoError(t, err)
	require.EqualValues(t, len(aitargets.Defaults()), version)
}

func TestSeedDefaultsDoesNotResurrectAnEmptiedCatalog(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	require.NoError(t, aitargets.SeedDefaults(ctx, conn))

	queries := repo.New(conn)
	for _, target := range aitargets.Defaults() {
		_, err := queries.SoftDeleteAIScanTarget(ctx, target.ID)
		require.NoError(t, err)
	}
	require.NoError(t, aitargets.SeedDefaults(ctx, conn))

	rows, err := queries.ListEnabledAIScanTargets(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "a catalog that has been written must never be reseeded")
}

func TestLoadCachesUntilInvalidated(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	catalog := aitargets.NewCatalog(testenv.NewLogger(t), conn, time.Hour)

	before, err := catalog.Load(ctx)
	require.NoError(t, err)

	queries := repo.New(conn)
	added := aitargets.Target{
		ID:          "chatgpt-classic",
		DisplayName: "ChatGPT Classic",
		Category:    aitargets.CategoryHarness,
		Signatures:  aitargets.Signatures{BundleIDs: []string{"com.openai.chat"}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{}},
		VersionHint: nil,
		Enabled:     true,
	}
	_, err = queries.UpsertAIScanTarget(ctx, aitargets.UpsertParams(added))
	require.NoError(t, err)
	newVersion, err := aitargets.RecordRevision(ctx, queries, aitargets.Revision{
		TargetID: added.ID,
		Action:   aitargets.ActionUpsert,
		Actor:    aitargets.Actor{UserID: "user", Email: "admin@example.com"},
		Reason:   "test",
		Before:   nil,
		After:    &added,
	})
	require.NoError(t, err)

	cached, err := catalog.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, before.ETag, cached.ETag, "within the TTL the snapshot must be served from cache")

	catalog.Invalidate()
	fresh, err := catalog.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, newVersion, fresh.ListVersion)
	require.NotEqual(t, before.ETag, fresh.ETag)
	_, ok := fresh.ByID("chatgpt-classic")
	require.True(t, ok)
}

func TestLoadServesDisabledTargetsToNobody(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	catalog := aitargets.NewCatalog(testenv.NewLogger(t), conn, 0)

	_, err := catalog.Load(ctx)
	require.NoError(t, err)
	_, err = repo.New(conn).SetAIScanTargetEnabled(ctx, repo.SetAIScanTargetEnabledParams{Enabled: false, ID: "aider"})
	require.NoError(t, err)

	snapshot, err := catalog.Load(ctx)
	require.NoError(t, err)
	_, ok := snapshot.ByID("aider")
	require.False(t, ok, "disabled targets are not served")
	require.Len(t, snapshot.Targets(), len(aitargets.Defaults())-1)

	records, err := catalog.ListRecords(ctx)
	require.NoError(t, err)
	require.Len(t, records, len(aitargets.Defaults()), "the management listing still shows disabled targets")
}

func TestLoadServesLastGoodSnapshotWhenRefreshFails(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	catalog := aitargets.NewCatalog(testenv.NewLogger(t), conn, 0)

	first, err := catalog.Load(ctx)
	require.NoError(t, err)

	conn.Close()
	again, err := catalog.Load(ctx)
	require.NoError(t, err, "a failed refresh must fall back to the last good snapshot")
	require.Equal(t, first.ETag, again.ETag)
}

func TestLoadFailsWhenNothingHasEverBeenRead(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	conn.Close()
	catalog := aitargets.NewCatalog(testenv.NewLogger(t), conn, aitargets.DefaultCacheTTL)

	_, err := catalog.Load(t.Context())
	require.Error(t, err)
}
