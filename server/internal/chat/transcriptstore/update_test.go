package transcriptstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

var errInjected = errors.New("injected failure")

// failAt fails the named generated statement without running it, the way a
// network or encryption error would, so the transaction itself stays usable.
type failAt struct {
	repo.DBTX
	name string
}

func (d *failAt) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "-- name: "+d.name+" ") {
		return pgconn.CommandTag{}, errInjected
	}
	tag, err := d.DBTX.Exec(ctx, sql, args...) //nolint:glint // notestingrawsql: transparently intercept generated sealing statements to inject a failure.
	return tag, safe(err)
}

func (f *fixture) storeSummary(t *testing.T, db chatrepo.DBTX, id uuid.UUID, summary string) {
	t.Helper()
	_, err := chatrepo.New(db).StoreToolCallSummary(t.Context(), chatrepo.StoreToolCallSummaryParams{
		ToolCallID: "example", Summary: []byte(`"` + summary + `"`), MessageID: id, ChatID: f.chat, ProjectID: f.project,
	})
	require.NoError(t, err)
}

func (f *fixture) summaries(t *testing.T, id uuid.UUID) string {
	t.Helper()
	opened, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err, "the transcript opens and matches the plaintext row")
	return string(opened.ToolCallSummaries)
}

// summaryChange is StoreToolCallSummary's change, applied to the transcript.
func summaryChange(summary string) func(*transcriptcrypto.Message) error {
	return func(m *transcriptcrypto.Message) error {
		m.ToolCallSummaries = []byte(`{"example":"` + summary + `"}`)
		return nil
	}
}

// The body update and its transcript update share one transaction, so a
// transcript update that fails at any step and is rolled back takes the body
// update with it: the plaintext and the transcript both keep the old body, and
// nothing is stale.
func TestFailedTranscriptUpdateRollsBackTheBodyUpdate(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"Capture", "UpdateMessageMetadata", "DeleteToolCallIDs", "InsertToolCallIDs"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			f := setup(t, true)
			id := uuid.New()
			f.add(t, id)
			f.capture(t, id)
			before := f.summaries(t, id)

			tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(t.Context()) }()
			f.storeSummary(t, tx, id, "new summary")
			transcripts := NewTranscripts(f.store, f.project)
			err = transcripts.Update(t.Context(), &failAt{DBTX: tx, name: step}, f.org, f.project, id, summaryChange("new summary"))
			require.ErrorIs(t, err, errInjected)
			require.NoError(t, tx.Rollback(t.Context()))

			require.Equal(t, before, f.summaries(t, id), "the summary update rolled back with the failed transcript update")
			progress, err := f.store.Progress(t.Context(), f.org, f.project)
			require.NoError(t, err)
			require.Equal(t, Progress{Current: 1}, progress)
		})
	}
}

// Trying to break it: the transaction protects every caller that rolls back
// on error. A stale transcript needs a body update that commits without its
// transcript update, and then the read fails loudly instead of serving it.
func TestBodyUpdateCommittedWithoutItsTranscript(t *testing.T) {
	t.Parallel()

	t.Run("caller ignores the error and commits", func(t *testing.T) {
		t.Parallel()
		f := setup(t, true)
		id := uuid.New()
		f.add(t, id)
		f.capture(t, id)

		tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(t.Context()) }()
		f.storeSummary(t, tx, id, "new summary")
		transcripts := NewTranscripts(f.store, f.project)
		err = transcripts.Update(t.Context(), &failAt{DBTX: tx, name: "Capture"}, f.org, f.project, id, summaryChange("new summary"))
		require.ErrorIs(t, err, errInjected)
		// A Go-side failure does not abort the Postgres transaction, so a
		// caller that drops the error can still commit.
		require.NoError(t, tx.Commit(t.Context()))

		_, err = f.store.Load(t.Context(), f.org, f.project, id)
		require.ErrorIs(t, err, ErrStale, "the stale transcript is never served")
	})

	t.Run("sealing is off for the project", func(t *testing.T) {
		t.Parallel()
		f := setup(t, true)
		id := uuid.New()
		f.add(t, id)
		f.capture(t, id)

		tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(t.Context()) }()
		f.storeSummary(t, tx, id, "new summary")
		off := NewTranscripts(f.store) // the project was removed from the allowlist
		require.NoError(t, off.Update(t.Context(), tx, f.org, f.project, id, summaryChange("new summary")), "Update is a no-op")
		require.NoError(t, tx.Commit(t.Context()))

		_, err = f.store.Load(t.Context(), f.org, f.project, id)
		require.ErrorIs(t, err, ErrStale, "re-enabling the project needs a backfill first")
		page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 10)
		require.NoError(t, err)
		require.Equal(t, 1, page.Count)
		require.JSONEq(t, `{"example":"new summary"}`, f.summaries(t, id))
	})
}
