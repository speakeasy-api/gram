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
		corpus := MemoryEvalCorpus(spec, MemoryEvalAnchor(time.Now()))
		for _, key := range []memoryeval.ProjectKey{memoryeval.ProjectMain, memoryeval.ProjectOther} {
			project, ok := MemoryEvalProjectID(spec, key)
			require.True(t, ok)

			var wantIDs []uuid.UUID
			wantMessages := 0
			for _, chat := range corpus.Chats {
				if chat.Project != key {
					continue
				}
				require.Equal(t, detUUID(spec.NameSeed+"memoryeval-chat-"+chat.Key), chat.ID, "chat IDs are tenant-derived")
				wantIDs = append(wantIDs, chat.ID)
				wantMessages += len(chat.Messages)
			}
			require.NotEmpty(t, wantIDs, "%s %s", spec.OrgSlug, key)

			gotIDs, err := q.MemoryEvalListChatIDs(ctx, repo.MemoryEvalListChatIDsParams{OrgID: spec.OrgID, ProjectID: project})
			require.NoError(t, err)
			require.ElementsMatch(t, wantIDs, gotIDs, "%s %s", spec.OrgSlug, key)

			messages, err := q.MemoryEvalCountChatMessages(ctx, project)
			require.NoError(t, err)
			require.Equal(t, int64(wantMessages), messages, "%s %s", spec.OrgSlug, key)
		}
	}
	require.NotEqual(t, MemoryEvalCorpus(specs[0], time.Now()).Chats[0].ID, MemoryEvalCorpus(specs[1], time.Now()).Chats[0].ID)
}
