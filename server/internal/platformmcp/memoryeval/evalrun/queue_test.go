package evalrun_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed"
	demoseedrepo "github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
)

// seedCorpusProject creates the local seed tenant's organization and main
// eval project, which is all CorpusOrganization looks for.
func seedCorpusProject(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	spec := demoseed.LocalSpec()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{ID: spec.OrgID, Name: spec.OrgName, Slug: spec.OrgSlug})
	require.NoError(t, err)
	id, ok := demoseed.MemoryEvalProjectID(spec, memoryeval.ProjectMain)
	require.True(t, ok)
	require.NoError(t, demoseedrepo.New(db).MemoryEvalInsertProject(t.Context(), demoseedrepo.MemoryEvalInsertProjectParams{ProjectID: id, Name: "Team memory eval", Slug: "team-memory-eval", OrgID: spec.OrgID}))
	return spec.OrgID
}

func TestQueueNeedsTheSeededCorpus(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrNoCorpus)
}

// Runs share the eval projects' memory, so only one may be active, and the
// daily limit counts failed runs too.
func TestQueueAllowsOneActiveRunAndADailyLimit(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	orgID := seedCorpusProject(t, db)
	q := repo.New(db)

	first, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.NoError(t, err)
	require.Equal(t, orgID, first.OrganizationID)
	require.Equal(t, "queued", first.Status)

	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrRunActive)

	fail := func(run repo.MemoryEvalRun) {
		_, err := q.FailRun(t.Context(), repo.FailRunParams{Error: pgtype.Text{String: "test", Valid: true}, CostUsd: pgtype.Numeric{}, ID: run.ID})
		require.NoError(t, err)
	}
	fail(first)
	for range evalrun.DailyRunLimit - 1 {
		run, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
		require.NoError(t, err)
		fail(run)
	}
	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrDailyLimit)
}

func TestQueueRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	seedCorpusProject(t, db)
	cfg := evalrun.DefaultConfig()
	cfg.Runs = evalrun.MaxRuns + 1
	_, err = evalrun.Queue(t.Context(), db, cfg, "staff@example.com")
	require.Error(t, err)
}
