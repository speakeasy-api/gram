// Package evalruntest holds test helpers shared by the eval run package and
// the admin handlers that queue runs.
package evalruntest

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed"
	demoseedrepo "github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// SeedCorpus creates the local seed tenant's organization and both eval
// projects, which is all a run start looks for, and returns the
// organization's ID.
func SeedCorpus(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	return SeedCorpusProjects(t, db, memoryeval.ProjectMain, memoryeval.ProjectOther)
}

// SeedCorpusProjects creates the local seed tenant's organization and the
// given eval projects, and returns the organization's ID.
func SeedCorpusProjects(t *testing.T, db *pgxpool.Pool, keys ...memoryeval.ProjectKey) string {
	t.Helper()
	spec := demoseed.LocalSpec()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:             spec.OrgID,
		Name:           spec.OrgName,
		Slug:           spec.OrgSlug,
		WorkosID:       pgtype.Text{String: "", Valid: false},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	for _, key := range keys {
		id, ok := demoseed.MemoryEvalProjectID(spec, key)
		require.True(t, ok)
		require.NoError(t, demoseedrepo.New(db).MemoryEvalInsertProject(t.Context(), demoseedrepo.MemoryEvalInsertProjectParams{
			ProjectID: id,
			Name:      "Team memory eval " + string(key),
			Slug:      "team-memory-eval-" + string(key),
			OrgID:     spec.OrgID,
			CreatedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		}))
	}
	return spec.OrgID
}
