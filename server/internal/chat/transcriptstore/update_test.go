package transcriptstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var errInjected = errors.New("injected failure")

// failAt fails the named generated statement without running it, the way a
// network or encryption error would, so the transaction itself stays usable.
type failAt struct {
	repo.DBTX
	name string
}

func (d *failAt) matches(sql string) bool { return strings.Contains(sql, "-- name: "+d.name+" ") }

func (d *failAt) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if d.matches(sql) {
		return pgconn.CommandTag{}, errInjected
	}
	tag, err := d.DBTX.Exec(ctx, sql, args...) //nolint:glint // notestingrawsql: transparently intercept generated statements to inject a failure.
	return tag, safe(err)
}

func (d *failAt) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if d.matches(sql) {
		return failedRow{}
	}
	return d.DBTX.QueryRow(ctx, sql, args...) //nolint:glint // notestingrawsql: transparently intercept generated statements to inject a failure.
}

type failedRow struct{}

func (failedRow) Scan(...any) error { return errInjected }

func summaryChange(summary string) func(*chatrepo.ChatMessage) error {
	return func(m *chatrepo.ChatMessage) error {
		m.ToolCallSummaries = []byte(`{"example":"` + summary + `"}`)
		return nil
	}
}

func (f *fixture) summaries(t *testing.T, id uuid.UUID) string {
	t.Helper()
	opened, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err, "the transcript opens and matches the plaintext row")
	return string(opened.ToolCallSummaries)
}

// UpdateChatMessage writes the plaintext row and the transcript in the caller's
// transaction, so a failure at any step, rolled back, leaves both copies with
// the old body and nothing stale.
func TestFailedUpdateMessageRollsBack(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"WriteChatMessage", "Capture", "UpdateMessageMetadata", "DeleteToolCallIDs", "InsertToolCallIDs"} {
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
			_, err = NewTranscripts(f.store, f.project).UpdateChatMessage(t.Context(), &failAt{DBTX: tx, name: step}, f.org, f.project, f.chat, id, summaryChange("new summary"))
			require.ErrorIs(t, err, errInjected)
			require.NoError(t, tx.Rollback(t.Context()))

			require.Equal(t, before, f.summaries(t, id), "both copies keep the old body")
			progress, err := f.store.Progress(t.Context(), f.org, f.project)
			require.NoError(t, err)
			require.Equal(t, Progress{Current: 1}, progress)
		})
	}
}

// Trying to break it: a stale transcript needs a body change that commits
// without its transcript, and the read then fails instead of serving it.
func TestBodyChangeCommittedWithoutItsTranscript(t *testing.T) {
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
		_, err = NewTranscripts(f.store, f.project).UpdateChatMessage(t.Context(), &failAt{DBTX: tx, name: "Capture"}, f.org, f.project, f.chat, id, summaryChange("new summary"))
		require.ErrorIs(t, err, errInjected)
		// A Go-side failure does not abort the Postgres transaction, so a
		// caller that drops the error can still commit the plaintext write.
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

		off := NewTranscripts(f.store)     // the project was removed from the allowlist
		tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(t.Context()) }()
		_, err = off.UpdateChatMessage(t.Context(), tx, f.org, f.project, f.chat, id, summaryChange("new summary"))
		require.NoError(t, err, "plaintext mode writes only the plaintext row")
		require.NoError(t, tx.Commit(t.Context()))

		_, err = f.store.Load(t.Context(), f.org, f.project, id)
		require.ErrorIs(t, err, ErrStale, "re-enabling the project needs a backfill first")
		page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 10)
		require.NoError(t, err)
		require.Equal(t, 1, page.Count)
		require.JSONEq(t, `{"example":"new summary"}`, f.summaries(t, id))
	})
}

// staleCiphertext replaces the ciphertext the Capture upsert writes with an
// older valid one for the same message, so the stored transcript no longer
// matches the plaintext row the update just wrote.
type staleCiphertext struct {
	repo.DBTX
	ciphertext string
}

func (d *staleCiphertext) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "-- name: Capture ") {
		args[3] = d.ciphertext
	}
	tag, err := d.DBTX.Exec(ctx, sql, args...) //nolint:glint // notestingrawsql: transparently intercept the generated upsert to store a stale transcript.
	return tag, safe(err)
}

// Every shadow-mode update counts its consistency checks: success when both
// copies agreed before and after, otherwise the check that failed. These are
// the numbers that show how well shadow mode keeps the copies in step.
func TestUpdateMessageCountsConsistencyChecks(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	store, err := New(f.db, f.keys.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)
	f.store = store
	transcripts := NewTranscripts(store, f.project)
	update := func(db repo.DBTX, id uuid.UUID, notes string) {
		t.Helper()
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		if db == nil {
			db = tx
		}
		if wrapped, ok := db.(*staleCiphertext); ok {
			wrapped.DBTX = tx
		}
		_, err = transcripts.UpdateChatMessage(ctx, db, f.org, f.project, f.chat, id, setNotes(notes))
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
	}

	current := uuid.New()
	f.add(t, current)
	f.capture(t, current)
	update(nil, current, "success")

	missing := uuid.New()
	f.add(t, missing)
	update(nil, missing, "missing")

	disagreeing := uuid.New()
	f.add(t, disagreeing)
	f.capture(t, disagreeing)
	_, err = fixturerepo.New(f.db).ChangeContent(ctx, disagreeing, project(f.project))
	require.NoError(t, err)
	update(nil, disagreeing, "disagreeing")

	insane := uuid.New()
	f.add(t, insane)
	f.capture(t, insane)
	old, err := fixturerepo.New(f.db).Ciphertext(ctx, insane, f.project, f.org)
	require.NoError(t, err)
	update(&staleCiphertext{DBTX: nil, ciphertext: old}, insane, "insane")

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &collected))
	require.Equal(t, map[string]int64{
		"success":            1,
		"missing_transcript": 1,
		"hydrate_mismatch":   1,
		"sanity_mismatch":    1,
	}, countsByOutcome(t, collected, meterUpdateChecks))
	require.Equal(t, map[string]int64{"success": 4}, countsByOutcome(t, collected, meterUpdateMessages))
}
