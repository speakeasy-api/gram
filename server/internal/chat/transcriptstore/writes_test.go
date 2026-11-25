package transcriptstore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
)

func judged(f *fixture, id uuid.UUID, notes string) UpdateToolCallOutcomeParams {
	return UpdateToolCallOutcomeParams{
		ToolOutcome:      pgtype.Text{String: "success", Valid: true},
		ToolOutcomeNotes: pgtype.Text{String: notes, Valid: true},
		ID:               id,
		ProjectID:        f.project,
	}
}

func TestShadowedWritesRecaptureInTheSameTransaction(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	transcripts := NewTranscripts(f.store, f.project)

	// On a pool the write opens its own transaction.
	require.NoError(t, transcripts.UpdateToolCallOutcome(t.Context(), f.db, judged(f, id, "judged on the pool")))
	messages, err := transcripts.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err, "the recaptured shadow is current")
	require.Len(t, messages, 1)
	require.Equal(t, "judged on the pool", messages[0].ToolOutcomeNotes.String)

	stored, err := transcripts.StoreToolCallSummary(t.Context(), f.db, StoreToolCallSummaryParams{ToolCallID: "example", Summary: []byte(`{"ok":true}`), MessageID: id, ChatID: f.chat, ProjectID: f.project})
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, stored)
	messages, err = transcripts.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Contains(t, string(messages[0].ToolCallSummaries), `"ok"`)

	// Inside a caller's transaction the write and recapture commit with it.
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises the savepoint; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, transcripts.UpdateToolCallOutcome(t.Context(), tx, judged(f, id, "judged in the caller's transaction")))
	require.NoError(t, tx.Commit(t.Context()))
	messages, err = transcripts.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Equal(t, "judged in the caller's transaction", messages[0].ToolOutcomeNotes.String)
}

func TestFailedRecaptureRollsBackOnlyItsWrite(t *testing.T) {
	t.Parallel()
	// No key is provisioned, so the recapture fails after the write ran.
	f := setup(t, false)
	id := uuid.New()
	f.add(t, id)
	transcripts := NewTranscripts(f.store, f.project)

	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises the savepoint; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.Error(t, transcripts.UpdateToolCallOutcome(t.Context(), tx, judged(f, id, "never committed")))

	// The caller's transaction is still usable and commits its other work,
	// but not the write whose recapture failed.
	_, err = fixturerepo.New(tx).ChangeContent(t.Context(), id, project(f.project))
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))

	snapshot, err := fixturerepo.New(f.db).MessageSnapshot(t.Context(), id, project(f.project))
	require.NoError(t, err)
	require.Contains(t, snapshot, `"changed"`)
	require.Contains(t, snapshot, "example note")
	require.NotContains(t, snapshot, "never committed")
}

func TestUnlistedProjectWritesWithoutCapture(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	id := uuid.New()
	f.add(t, id)

	// A project off the list writes plaintext only and needs no key.
	require.NoError(t, NewTranscripts(f.store, uuid.New()).UpdateToolCallOutcome(t.Context(), f.db, judged(f, id, "plaintext only")))
	snapshot, err := fixturerepo.New(f.db).MessageSnapshot(t.Context(), id, project(f.project))
	require.NoError(t, err)
	require.Contains(t, snapshot, "plaintext only")
}
