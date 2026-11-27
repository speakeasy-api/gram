package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// shadowChatSearch seeds twelve messages, oldest first, whose second, ninth
// and eleventh carry the needle, captures their encrypted shadows and turns
// on shadow reads for the project.
func shadowChatSearch(t *testing.T) (chatSearchFixture, []uuid.UUID) {
	t.Helper()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	chatID := f.chat(t, ctx, "user_fixture_owner", "encrypted search")
	ids := make([]uuid.UUID, 12)
	for i := range ids {
		content := "ordinary message"
		if i == 1 || i == 8 || i == 10 {
			content = "scan_needle in message"
		}
		ids[i] = seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: content, at: f.base.Add(time.Duration(i) * time.Second)})
	}

	keys := orgencryptiontest.New(t, f.db)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), f.principal.OrganizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(f.db, keys.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.NoError(t, store.Prepare(t.Context(), f.principal.OrganizationID), "writers load the key before the transaction")
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: ResealChatMessages runs inside the caller's message transaction; message rows come from generated seed queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, store.ResealChatMessages(t.Context(), tx, f.principal.OrganizationID, f.project.ID, ids))
	require.NoError(t, tx.Commit(t.Context()))
	f.service.WithTranscripts(transcriptstore.NewTranscripts(store, f.project.ID))
	return f, ids
}

func TestChatSearchScansEncryptedTranscriptsInBoundedCalls(t *testing.T) {
	t.Parallel()
	f, ids := shadowChatSearch(t)
	ctx := f.context(t.Context())
	f.service.scanBudget = 5
	input := f.toolSearch("SCAN_NEEDLE")
	input.SortOrder = chatSearchSortOldestFirst

	// Each call scans five messages and says so when it stops early, so the
	// agent can choose to continue.
	var found []string
	for _, want := range []struct {
		hits       []uuid.UUID
		incomplete bool
	}{
		{hits: []uuid.UUID{ids[1]}, incomplete: true},
		{hits: []uuid.UUID{ids[8]}, incomplete: true},
		{hits: []uuid.UUID{ids[10]}, incomplete: false},
	} {
		page, err := f.service.Search(ctx, f.principal, input)
		require.NoError(t, err)
		require.Len(t, page.Hits, len(want.hits))
		require.Equal(t, want.hits[0].String(), page.Hits[0].MessageID)
		require.Equal(t, "scan_needle in message", page.Hits[0].Text)
		require.Equal(t, want.incomplete, page.ScanIncomplete)
		if want.incomplete {
			require.NotEmpty(t, page.NextCursor)
			require.Contains(t, page.Note, "next_cursor")
		}
		if !want.incomplete {
			require.Empty(t, page.NextCursor)
			require.Empty(t, page.Note)
		}
		found = append(found, hitIDs(page.Hits)...)
		input.Cursor = page.NextCursor
	}

	// With room to scan everything, a page of one hit continues after that
	// hit, and the same hits arrive in the same order as the plaintext search.
	f.service.scanBudget = chatSearchScanBudget
	input = f.toolSearch("scan_needle")
	input.SortOrder = chatSearchSortOldestFirst
	input.Limit = 1
	all, pages := f.searchAll(t, ctx, input)
	require.Equal(t, found, all)
	require.Equal(t, 3, pages)
	f.service.WithTranscripts(nil)
	plain, _ := f.searchAll(t, ctx, input)
	require.Equal(t, plain, all)
}

func TestChatSearchServesPlaintextForAStaleShadowHit(t *testing.T) {
	t.Parallel()
	f, ids := shadowChatSearch(t)
	ctx := f.context(t.Context())
	_, err := fixturerepo.New(f.db).SetContent(t.Context(), "scan_needle edited behind the shadow", ids[1], uuid.NullUUID{UUID: f.project.ID, Valid: true})
	require.NoError(t, err)

	// The match is decided on the transcript. The hit disagrees with its
	// plaintext row, so it is logged and served from plaintext, the source of
	// truth in shadow mode.
	stale := f.toolSearch("scan_needle")
	stale.SortOrder = chatSearchSortOldestFirst
	page, err := f.service.Search(ctx, f.principal, stale)
	require.NoError(t, err)
	require.Equal(t, []string{ids[1].String(), ids[8].String(), ids[10].String()}, hitIDs(page.Hits))
	require.Equal(t, "scan_needle edited behind the shadow", page.Hits[0].Text)
	// A message the edit does not touch is still found.
	input := f.toolSearch("scan_needle")
	input.SortOrder = chatSearchSortOldestFirst
	input.From = f.base.Add(5 * time.Second).Format(time.RFC3339)
	page, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Equal(t, []string{ids[8].String(), ids[10].String()}, hitIDs(page.Hits))
}
