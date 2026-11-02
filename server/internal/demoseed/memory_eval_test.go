//go:build demoseed_safety

package demoseed

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// TestSeedWritesMemoryEvalCorpus checks that both tenants get the whole eval
// corpus in its own projects, with tenant-derived chat IDs, and that a reseed
// replaces it instead of adding to it.
func TestSeedWritesMemoryEvalCorpus(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	q := repo.New(db)

	specs := []Spec{DefaultSpec(), LocalSpec()}
	for _, spec := range specs {
		seedLocalPostgres(ctx, t, db, spec)
	}
	seedLocalPostgres(ctx, t, db, specs[0])

	for _, spec := range specs {
		main, ok := MemoryEvalProjectID(spec, memoryeval.ProjectMain)
		require.True(t, ok)
		other, ok := MemoryEvalProjectID(spec, memoryeval.ProjectOther)
		require.True(t, ok)

		corpus := MemoryEvalCorpus(spec, MemoryEvalAnchor(time.Now()))
		count, err := q.MemoryEvalCountChats(ctx, repo.MemoryEvalCountChatsParams{OrgID: spec.OrgID, ProjectIds: []uuid.UUID{main, other}})
		require.NoError(t, err)
		require.Equal(t, int64(len(corpus.Chats)), count, spec.OrgSlug)
		require.NotEqual(t, memoryeval.ChatID(corpus.Chats[0].Key), corpus.Chats[0].ID, "chat IDs are tenant-derived")
	}
	require.NotEqual(t, MemoryEvalCorpus(specs[0], time.Now()).Chats[0].ID, MemoryEvalCorpus(specs[1], time.Now()).Chats[0].ID)
}
