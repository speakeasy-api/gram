package transcriptstore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestMetadataOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		toolCalls string
		count     int32
		ids       []string
	}{
		{name: "none", toolCalls: "", count: 0, ids: []string{}},
		{name: "empty array", toolCalls: `[]`, count: 0, ids: []string{}},
		{name: "array", toolCalls: `[{"id":"call_a"},{"id":"call_b"}]`, count: 2, ids: []string{"call_a", "call_b"}},
		{name: "call without id", toolCalls: `[{"id":"call_a"},{"type":"function"}]`, count: 2, ids: []string{"call_a"}},
		{name: "double encoded", toolCalls: `"[{\"id\":\"call_a\"}]"`, count: 1, ids: []string{"call_a"}},
		{name: "double encoded empty", toolCalls: `"[]"`, count: 0, ids: []string{}},
		{name: "double encoded null", toolCalls: `"null"`, count: 0, ids: []string{}},
		{name: "unparseable string", toolCalls: `"not json"`, count: 1, ids: []string{}},
		{name: "double encoded tab-padded empty array", toolCalls: `"\t[]"`, count: 1, ids: []string{}},
		{name: "double encoded space-padded empty array", toolCalls: `" [] "`, count: 0, ids: []string{}},
		{name: "object", toolCalls: `{"id":"call_a"}`, count: 0, ids: []string{}},
		{name: "array of non-objects", toolCalls: `["call_a",1,{"id":"call_b"}]`, count: 3, ids: []string{"call_b"}},
		{name: "id key is exact", toolCalls: `[{"ID":"call_a"},{"Id":"call_b"},{"id":"call_c"}]`, count: 3, ids: []string{"call_c"}},
		{name: "non-string id", toolCalls: `[{"id":7},{"id":null},{"id":"call_a"}]`, count: 3, ids: []string{"call_a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var raw []byte
			if tc.toolCalls != "" {
				raw = []byte(tc.toolCalls)
			}
			meta := metadataOf("héllo", []byte(`{"a":1}`), raw)
			require.Equal(t, tc.count, meta.toolCallCount)
			require.Equal(t, tc.ids, meta.toolCallIDs)
			require.Equal(t, int32(6), meta.contentBytes, "bytes, not runes")
			require.Equal(t, int32(7), meta.contentRawBytes)
			require.Equal(t, conv.SafeInt32(len(tc.toolCalls)), meta.toolCallsBytes)
		})
	}
}

func TestCaptureStoresMetadataAndToolCallIDs(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)

	fixtures := fixturerepo.New(f.db)
	meta, err := fixtures.ShadowMetadata(t.Context(), id)
	require.NoError(t, err)
	var lengths struct {
		Content, ContentRaw, ToolCalls int32
	}
	//nolint:glint // notestingrawsql: compares stored lengths with PostgreSQL's own octet_length.
	row := f.db.QueryRow(t.Context(), `SELECT octet_length(content), octet_length(content_raw::text), octet_length(tool_calls::text) FROM chat_messages WHERE id = $1`, id)
	require.NoError(t, row.Scan(&lengths.Content, &lengths.ContentRaw, &lengths.ToolCalls))
	require.Equal(t, pgtype.Int4{Int32: 1, Valid: true}, meta.ToolCallCount)
	require.Equal(t, pgtype.Int4{Int32: lengths.Content, Valid: true}, meta.ContentBytes)
	require.Equal(t, pgtype.Int4{Int32: lengths.ContentRaw, Valid: true}, meta.ContentRawBytes, "matches octet_length(content_raw::text)")
	require.Equal(t, pgtype.Int4{Int32: lengths.ToolCalls, Valid: true}, meta.ToolCallsBytes, "matches octet_length(tool_calls::text)")
	ids, err := fixtures.ShadowToolCallIDs(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []string{"example"}, ids)

	// A recapture rewrites the tool-call IDs with the shadow.
	_, err = fixtures.SetToolCalls(t.Context(), []byte(`[{"id":"first"},{"id":"second"}]`), id, project(f.project))
	require.NoError(t, err)
	f.capture(t, id)
	ids, err = fixtures.ShadowToolCallIDs(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, ids)
	meta, err = fixtures.ShadowMetadata(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, pgtype.Int4{Int32: 2, Valid: true}, meta.ToolCallCount)

	// No tool calls leaves no rows.
	_, err = fixtures.SetToolCalls(t.Context(), nil, id, project(f.project))
	require.NoError(t, err)
	f.capture(t, id)
	ids, err = fixtures.ShadowToolCallIDs(t.Context(), id)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestTranscriptsGateByProject(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)

	var none *Transcripts
	require.False(t, none.Enabled(f.project))
	require.NoError(t, none.Seal(t.Context(), f.db, f.org, f.project, []uuid.UUID{id}))

	other := NewTranscripts(f.store, uuid.New())
	require.False(t, other.Enabled(f.project))
	require.NoError(t, other.Seal(t.Context(), f.db, f.org, f.project, []uuid.UUID{id}))
	_, err := fixturerepo.New(f.db).ShadowMetadata(t.Context(), id)
	require.Error(t, err, "a project off the list captures nothing")

	enabled := NewTranscripts(f.store, f.project)
	require.True(t, enabled.Enabled(f.project))
	require.NoError(t, enabled.Seal(t.Context(), f.db, f.org, f.project, []uuid.UUID{id}))
	_, err = fixturerepo.New(f.db).ShadowMetadata(t.Context(), id)
	require.NoError(t, err)
}

type openRow struct {
	id           uuid.UUID
	content      string
	contentRaw   []byte
	toolCalls    []byte
	notes        pgtype.Text
	summaries    []byte
	storageError pgtype.Text
}

func openRowBody(r *openRow) Body {
	return Body{MessageID: r.id, Content: &r.content, ContentRaw: &r.contentRaw, ToolCalls: &r.toolCalls, ToolOutcomeNotes: &r.notes, ToolCallSummaries: &r.summaries, StorageError: &r.storageError}
}

// plaintextRow is a loaded row whose six body fields all hold placeholder
// plaintext, so a test can see which ones Open replaced.
func plaintextRow(id uuid.UUID) openRow {
	return openRow{
		id: id, content: "plaintext", contentRaw: []byte(`{"plaintext":true}`), toolCalls: []byte(`[]`),
		notes: pgtype.Text{String: "plaintext", Valid: true}, summaries: []byte(`{"plaintext":true}`),
		storageError: pgtype.Text{String: "plaintext", Valid: true},
	}
}

func TestOpenRowsServesShadowsFallsBackAndFailsStale(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	shadowed, unshadowed := uuid.New(), uuid.New()
	f.add(t, shadowed)
	f.add(t, unshadowed)
	_, err := fixturerepo.New(f.db).SetStorageError(t.Context(), pgtype.Text{String: "example error", Valid: true}, shadowed, project(f.project))
	require.NoError(t, err)
	f.capture(t, shadowed)
	transcripts := NewTranscripts(f.store, f.project)

	rows := []openRow{plaintextRow(shadowed), plaintextRow(unshadowed)}
	require.NoError(t, OpenRows(t.Context(), transcripts, f.org, f.project, rows, openRowBody))
	opened := rows[0]
	require.Equal(t, "sensitive example", opened.content, "a shadowed message is served from its shadow")
	require.True(t, sameJSON([]byte(`{"large":9007199254740993,"n":1e2}`), opened.contentRaw), "numbers survive exactly: %s", opened.contentRaw)
	require.JSONEq(t, `[{"id":"example"}]`, string(opened.toolCalls))
	require.Equal(t, pgtype.Text{String: "example note", Valid: true}, opened.notes)
	require.JSONEq(t, `{"example":"summary"}`, string(opened.summaries))
	require.Equal(t, pgtype.Text{String: "example error", Valid: true}, opened.storageError, "all six fields come from the copy")
	require.Equal(t, plaintextRow(unshadowed), rows[1], "a message with no shadow keeps its plaintext")

	off := []openRow{plaintextRow(shadowed)}
	require.NoError(t, OpenRows(t.Context(), NewTranscripts(f.store), f.org, f.project, off, openRowBody))
	require.Equal(t, plaintextRow(shadowed), off[0], "a project off the list reads plaintext")

	_, err = fixturerepo.New(f.db).ChangeContent(t.Context(), shadowed, project(f.project))
	require.NoError(t, err)
	stale := []openRow{plaintextRow(shadowed)}
	err = OpenRows(t.Context(), transcripts, f.org, f.project, stale, openRowBody)
	require.ErrorIs(t, err, ErrStale, "a stale shadow fails the read instead of serving plaintext")
}

func TestToolCallIDsStayWithTheirProject(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)

	// A caller naming another project neither deletes nor adds rows.
	q := repo.New(f.db)
	require.NoError(t, q.DeleteToolCallIDs(t.Context(), repo.DeleteToolCallIDsParams{MessageID: id, ProjectID: uuid.New()}))
	require.NoError(t, q.InsertToolCallIDs(t.Context(), repo.InsertToolCallIDsParams{MessageID: id, ProjectID: uuid.New(), ToolCallIds: []string{"forged"}}))
	ids, err := fixturerepo.New(f.db).ShadowToolCallIDs(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []string{"example"}, ids)
}

// Each body update changes the plaintext in SQL and the transcript in Go, in
// one transaction: the transcript stays current, and a kit patch's metadata
// and tool-call IDs are measured again from the new body.
func TestBodyUpdatesKeepTheTranscriptCurrent(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	transcripts := NewTranscripts(f.store, f.project)
	text := func(v string) pgtype.Text { return pgtype.Text{String: v, Valid: true} }
	inTx := func(update func(chatrepo.DBTX), apply func(*transcriptcrypto.Message) error) {
		t.Helper()
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		update(tx)
		require.NoError(t, transcripts.Update(ctx, tx, f.org, f.project, id, apply))
		require.NoError(t, tx.Commit(ctx))
	}

	inTx(func(db chatrepo.DBTX) {
		require.NoError(t, chatrepo.New(db).UpdateToolCallOutcome(ctx, chatrepo.UpdateToolCallOutcomeParams{ToolOutcome: text("success"), ToolOutcomeNotes: text("changed note"), ID: id, ProjectID: f.project}))
	}, func(m *transcriptcrypto.Message) error {
		notes := "changed note"
		m.ToolOutcomeNotes = &notes
		return nil
	})
	opened, err := f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "changed note", *opened.ToolOutcomeNotes)

	inTx(func(db chatrepo.DBTX) {
		_, err := chatrepo.New(db).StoreToolCallSummary(ctx, chatrepo.StoreToolCallSummaryParams{ToolCallID: "other", Summary: []byte(`"done"`), MessageID: id, ChatID: f.chat, ProjectID: f.project})
		require.NoError(t, err)
	}, func(m *transcriptcrypto.Message) error {
		m.ToolCallSummaries = []byte(`{"example":"summary","other":"done"}`)
		return nil
	})
	opened, err = f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err)
	require.JSONEq(t, `{"example":"summary","other":"done"}`, string(opened.ToolCallSummaries))

	_, err = fixturerepo.New(f.db).SetSource(ctx, pgtype.Text{String: "kit", Valid: true}, id)
	require.NoError(t, err)
	inTx(func(db chatrepo.DBTX) {
		_, err := chatrepo.New(db).PatchKitProjectionMessage(ctx, chatrepo.PatchKitProjectionMessageParams{
			Content: "patched", ContentRaw: nil, ToolCalls: []byte(`[{"id":"call_new"}]`), ContentHash: nil, FinishReason: text("stop"),
			ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, ChatID: f.chat, ID: id,
		})
		require.NoError(t, err)
	}, func(m *transcriptcrypto.Message) error {
		m.Content, m.ContentRaw, m.ToolCalls = "patched", nil, []byte(`[{"id":"call_new"}]`)
		return nil
	})
	opened, err = f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "patched", opened.Content)
	ids, err := fixturerepo.New(f.db).ShadowToolCallIDs(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []string{"call_new"}, ids, "the patch's tool-call IDs are measured from the new body")
	progress, err := f.store.Progress(ctx, f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Current: 1}, progress)
}

// Update builds on the decrypted transcript, not on the plaintext. When the
// two disagree, the change cannot make them agree, and Update refuses to seal.
func TestUpdateBuildsOnTheTranscript(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	setNotes := func(v string) func(*transcriptcrypto.Message) error {
		return func(m *transcriptcrypto.Message) error { m.ToolOutcomeNotes = &v; return nil }
	}
	run := func(f *fixture, id uuid.UUID, notes string) error {
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		require.NoError(t, chatrepo.New(tx).UpdateToolCallOutcome(ctx, chatrepo.UpdateToolCallOutcomeParams{ToolOutcome: pgtype.Text{String: "success", Valid: true}, ToolOutcomeNotes: pgtype.Text{String: notes, Valid: true}, ID: id, ProjectID: f.project}))
		if err := NewTranscripts(f.store, f.project).Update(ctx, tx, f.org, f.project, id, setNotes(notes)); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	t.Run("no transcript starts from the plaintext", func(t *testing.T) {
		t.Parallel()
		f := setup(t, true)
		id := uuid.New()
		f.add(t, id)
		require.NoError(t, run(f, id, "first note"))
		opened, err := f.store.Load(ctx, f.org, f.project, id)
		require.NoError(t, err)
		require.Equal(t, "first note", *opened.ToolOutcomeNotes)
	})

	t.Run("a legacy transcript starts from the plaintext", func(t *testing.T) {
		t.Parallel()
		f := setup(t, true)
		id := uuid.New()
		f.add(t, id)
		f.capture(t, id)
		_, err := fixturerepo.New(f.db).MakeLegacyCopy(ctx, id, project(f.project))
		require.NoError(t, err)
		_, err = fixturerepo.New(f.db).ChangeContent(ctx, id, project(f.project))
		require.NoError(t, err)
		require.NoError(t, run(f, id, "note"))
		opened, err := f.store.Load(ctx, f.org, f.project, id)
		require.NoError(t, err)
		require.Equal(t, "changed", opened.Content, "the stale legacy body was not carried forward")
	})

	t.Run("a transcript that disagrees with the plaintext is refused", func(t *testing.T) {
		t.Parallel()
		f := setup(t, true)
		id := uuid.New()
		f.add(t, id)
		f.capture(t, id)
		// A write that bypassed the application left the transcript behind.
		_, err := fixturerepo.New(f.db).ChangeContent(ctx, id, project(f.project))
		require.NoError(t, err)
		require.ErrorIs(t, run(f, id, "note"), ErrStale)
	})

	t.Run("a change that differs from the SQL update is refused", func(t *testing.T) {
		t.Parallel()
		f := setup(t, true)
		id := uuid.New()
		f.add(t, id)
		f.capture(t, id)
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		require.NoError(t, chatrepo.New(tx).UpdateToolCallOutcome(ctx, chatrepo.UpdateToolCallOutcomeParams{ToolOutcome: pgtype.Text{String: "success", Valid: true}, ToolOutcomeNotes: pgtype.Text{String: "in SQL", Valid: true}, ID: id, ProjectID: f.project}))
		err = NewTranscripts(f.store, f.project).Update(ctx, tx, f.org, f.project, id, setNotes("in Go"))
		require.ErrorIs(t, err, ErrStale)
	})
}

// Listed projects without a store would silently skip encryption, so building
// one fails fast. No projects and no store is a valid way to turn it off.
func TestNewTranscriptsNeedsAStoreForListedProjects(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { NewTranscripts(nil, uuid.New()) })
	require.NotPanics(t, func() { require.False(t, NewTranscripts(nil).Enabled(uuid.New())) })
}
