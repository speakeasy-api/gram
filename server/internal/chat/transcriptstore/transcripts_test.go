package transcriptstore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
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

func TestOpenRowsServesTranscriptsAndFallsBackToPlaintext(t *testing.T) {
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
	stale[0].content = "changed"
	require.NoError(t, OpenRows(t.Context(), transcripts, f.org, f.project, stale, openRowBody))
	require.Equal(t, "changed", stale[0].content, "a disagreeing transcript is logged and the plaintext is kept")
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

// updateBody runs one UpdateMessage in its own committed transaction.
func (f *fixture) updateBody(t *testing.T, transcripts *Transcripts, chatID, id uuid.UUID, apply func(*chatrepo.ChatMessage) error) (bool, error) {
	t.Helper()
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	changed, err := transcripts.UpdateMessage(t.Context(), tx, f.org, f.project, chatID, id, apply)
	if err != nil {
		return false, err
	}
	require.NoError(t, tx.Commit(t.Context()))
	return changed, nil
}

func setNotes(v string) func(*chatrepo.ChatMessage) error {
	return func(m *chatrepo.ChatMessage) error {
		m.ToolOutcomeNotes = pgtype.Text{String: v, Valid: true}
		return nil
	}
}

// In shadow mode one change, made once in Go, lands in the plaintext row, the
// transcript, the metadata and the tool-call IDs.
func TestUpdateMessageKeepsEveryCopyCurrent(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	transcripts := NewTranscripts(f.store, f.project)

	changed, err := f.updateBody(t, transcripts, f.chat, id, setNotes("changed note"))
	require.NoError(t, err)
	require.True(t, changed)
	opened, err := f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err, "plaintext and transcript agree")
	require.Equal(t, "changed note", *opened.ToolOutcomeNotes)

	changed, err = f.updateBody(t, transcripts, f.chat, id, func(m *chatrepo.ChatMessage) error {
		m.ToolCallSummaries = []byte(`{"example":"summary","other":"done"}`)
		return nil
	})
	require.NoError(t, err)
	require.True(t, changed)
	opened, err = f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err)
	require.JSONEq(t, `{"example":"summary","other":"done"}`, string(opened.ToolCallSummaries))

	changed, err = f.updateBody(t, transcripts, f.chat, id, setNotes("changed note"))
	require.NoError(t, err)
	require.False(t, changed, "the same value is not written again")

	changed, err = f.updateBody(t, transcripts, f.chat, id, func(m *chatrepo.ChatMessage) error {
		m.Content, m.ToolCalls = "patched", []byte(`[{"id":"call_new"}]`)
		return nil
	})
	require.NoError(t, err)
	require.True(t, changed)
	opened, err = f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "patched", opened.Content)
	ids, err := fixturerepo.New(f.db).ShadowToolCallIDs(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []string{"call_new"}, ids, "tool-call IDs are measured from the new body")

	_, err = f.updateBody(t, transcripts, uuid.New(), id, setNotes("other chat"))
	require.ErrorIs(t, err, ErrNotFound, "the message must belong to the named chat")
	progress, err := f.store.Progress(ctx, f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Current: 1}, progress)
}

// In plaintext mode UpdateMessage writes the plaintext row and the metadata, and
// no transcript.
func TestUpdateMessageInPlaintextMode(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	id := uuid.New()
	f.add(t, id)
	var none *Transcripts

	changed, err := f.updateBody(t, none, f.chat, id, func(m *chatrepo.ChatMessage) error {
		m.Content = "plain update"
		return nil
	})
	require.NoError(t, err)
	require.True(t, changed)
	snapshot, err := fixturerepo.New(f.db).MessageSnapshot(ctx, id, project(f.project))
	require.NoError(t, err)
	require.Contains(t, snapshot, `"content": "plain update"`)
	_, err = fixturerepo.New(f.db).ShadowMetadata(ctx, id)
	require.NoError(t, err, "metadata is written in every mode")
	progress, err := f.store.Progress(ctx, f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Waiting: 1}, progress, "no transcript")
}

// In shadow mode the update starts from the decrypted transcript. When there
// is none, only a legacy one, or one that disagrees with the plaintext row,
// it starts from the plaintext row, which stays the source of truth, and the
// write seals a current transcript.
func TestUpdateMessageStartsFromTheTranscriptOrThePlaintext(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for _, tc := range []struct {
		name    string
		prepare func(*fixture, uuid.UUID)
		content string
	}{
		{name: "no transcript", prepare: func(*fixture, uuid.UUID) {}, content: "sensitive example"},
		{name: "legacy transcript", prepare: func(f *fixture, id uuid.UUID) {
			f.capture(t, id)
			_, err := fixturerepo.New(f.db).MakeLegacyCopy(ctx, id, project(f.project))
			require.NoError(t, err)
			_, err = fixturerepo.New(f.db).ChangeContent(ctx, id, project(f.project))
			require.NoError(t, err)
		}, content: "changed"},
		{name: "transcript disagrees with the plaintext", prepare: func(f *fixture, id uuid.UUID) {
			f.capture(t, id)
			// A write that bypassed UpdateMessage left the transcript behind.
			_, err := fixturerepo.New(f.db).ChangeContent(ctx, id, project(f.project))
			require.NoError(t, err)
		}, content: "changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t, true)
			id := uuid.New()
			f.add(t, id)
			tc.prepare(f, id)
			changed, err := f.updateBody(t, NewTranscripts(f.store, f.project), f.chat, id, setNotes("note"))
			require.NoError(t, err)
			require.True(t, changed)
			opened, err := f.store.Load(ctx, f.org, f.project, id)
			require.NoError(t, err)
			require.Equal(t, tc.content, opened.Content, "the plaintext row is kept")
			require.Equal(t, "note", *opened.ToolOutcomeNotes)
		})
	}
}

// Listed projects without a store would silently skip encryption, so building
// one fails fast. No projects and no store is a valid way to turn it off.
func TestNewTranscriptsNeedsAStoreForListedProjects(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { NewTranscripts(nil, uuid.New()) })
	require.NotPanics(t, func() { require.False(t, NewTranscripts(nil).Enabled(uuid.New())) })
}

// In encrypted mode the transcript is the only copy of the body: updates start
// from it and write the plaintext columns empty, Seal empties them in the same
// transaction, reads serve the transcript without comparison, and a message
// without a transcript fails instead of falling back to the empty plaintext.
func TestEncryptedMode(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	encrypted := NewTranscripts(f.store).WithEncryptedProjects(f.project)
	require.Equal(t, ModeEncrypted, encrypted.Mode(f.project))
	require.True(t, encrypted.Enabled(f.project))
	fixtures := fixturerepo.New(f.db)
	open := func(id uuid.UUID) (openRow, error) {
		t.Helper()
		rows := []openRow{plaintextRow(id)}
		err := OpenRows(ctx, encrypted, f.org, f.project, rows, openRowBody)
		return rows[0], err
	}
	emptied := func(id uuid.UUID) {
		t.Helper()
		snapshot, err := fixtures.MessageSnapshot(ctx, id, project(f.project))
		require.NoError(t, err)
		for _, want := range []string{`"content": ""`, `"content_raw": null`, `"tool_calls": null`, `"tool_outcome_notes": null`, `"tool_call_summaries": null`, `"storage_error": null`} {
			require.Contains(t, snapshot, want, "the plaintext body columns are empty")
		}
	}

	// A message sealed in shadow mode, then updated in encrypted mode.
	updated := uuid.New()
	f.add(t, updated)
	f.capture(t, updated)
	changed, err := f.updateBody(t, encrypted, f.chat, updated, setNotes("encrypted note"))
	require.NoError(t, err)
	require.True(t, changed)
	emptied(updated)
	row, err := open(updated)
	require.NoError(t, err)
	require.Equal(t, "sensitive example", row.content, "the body comes from the transcript")
	require.True(t, sameJSON([]byte(`{"large":9007199254740993,"n":1e2}`), row.contentRaw), "content_raw: %s", row.contentRaw)
	require.JSONEq(t, `[{"id":"example"}]`, string(row.toolCalls))
	require.Equal(t, pgtype.Text{String: "encrypted note", Valid: true}, row.notes)
	require.JSONEq(t, `{"example":"summary"}`, string(row.summaries))
	require.Equal(t, pgtype.Text{}, row.storageError, "no storage error, served as NULL rather than the placeholder")

	// A backfill must not reseal it from the empty plaintext row.
	_, err = f.store.Backfill(ctx, f.org, f.project, uuid.Nil, 10)
	require.NoError(t, err)
	row, err = open(updated)
	require.NoError(t, err)
	require.Equal(t, "sensitive example", row.content, "the transcript survives a backfill")

	// A row sealed in shadow mode is emptied by an update that changes nothing.
	populated := uuid.New()
	f.add(t, populated)
	f.capture(t, populated)
	_, err = f.updateBody(t, encrypted, f.chat, populated, func(*chatrepo.ChatMessage) error { return nil })
	require.NoError(t, err)
	emptied(populated)
	meta, err := fixtures.ShadowMetadata(ctx, updated)
	require.NoError(t, err)
	require.Equal(t, pgtype.Int4{Int32: int32(len("sensitive example")), Valid: true}, meta.ContentBytes, "metadata describes the body, not the empty columns")

	// A new message: Seal empties the plaintext it was sealed from.
	inserted := uuid.New()
	f.add(t, inserted)
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, encrypted.Seal(ctx, tx, f.org, f.project, []uuid.UUID{inserted}))
	require.NoError(t, tx.Commit(ctx))
	emptied(inserted)
	row, err = open(inserted)
	require.NoError(t, err)
	require.Equal(t, "sensitive example", row.content)

	// No transcript: nothing to serve or change.
	missing := uuid.New()
	f.add(t, missing)
	_, err = f.updateBody(t, encrypted, f.chat, missing, setNotes("lost"))
	require.ErrorIs(t, err, ErrMissingTranscript)
	_, err = open(missing)
	require.ErrorIs(t, err, ErrMissingTranscript)
}

// Until a project is put in it, encrypted mode is unreachable from config.
func TestModes(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	shadow, other := uuid.New(), uuid.New()
	transcripts := NewTranscripts(f.store, shadow)
	require.Equal(t, ModeShadow, transcripts.Mode(shadow))
	require.Equal(t, ModePlaintext, transcripts.Mode(other))
	var none *Transcripts
	require.Equal(t, ModePlaintext, none.Mode(shadow))
	require.Panics(t, func() { NewTranscripts(nil).WithEncryptedProjects(shadow) })
}
