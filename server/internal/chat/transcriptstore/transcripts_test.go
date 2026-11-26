package transcriptstore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func TestTranscriptsBackfillGatesByProjectAndFillsMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	fixtures := fixturerepo.New(f.db)

	var none *Transcripts
	_, err := none.Backfill(t.Context(), f.org, f.project, uuid.Nil, 10)
	require.ErrorIs(t, err, ErrNotEnabled)
	_, err = none.Progress(t.Context(), f.org, f.project)
	require.ErrorIs(t, err, ErrNotEnabled)

	other := NewTranscripts(f.store, uuid.New())
	_, err = other.Backfill(t.Context(), f.org, f.project, uuid.Nil, 10)
	require.ErrorIs(t, err, ErrNotEnabled)
	_, err = fixtures.ShadowMetadata(t.Context(), id)
	require.Error(t, err, "a project off the list backfills nothing")

	enabled := NewTranscripts(f.store, f.project)
	page, err := enabled.Backfill(t.Context(), f.org, f.project, uuid.Nil, 10)
	require.NoError(t, err)
	require.Equal(t, Page{After: id, Count: 1, More: false}, page)
	meta, err := fixtures.ShadowMetadata(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, pgtype.Int4{Int32: 1, Valid: true}, meta.ToolCallCount, "backfill fills metadata like a live capture")
	ids, err := fixtures.ShadowToolCallIDs(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []string{"example"}, ids)

	progress, err := enabled.Progress(t.Context(), f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Current: 1, Waiting: 0, Unattributed: 0}, progress)
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

// Every body update deletes the transcript sealed from the old body, so no
// stale transcript survives even when the caller does not reseal, and a kit
// patch that changes content or tool calls also drops the metadata and
// tool-call IDs measured from it. Callers reseal in the same transaction.
func TestBodyUpdatesDeleteTheTranscript(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	id := uuid.New()
	f.add(t, id)
	fixtures := fixturerepo.New(f.db)
	messages := chatrepo.New(f.db)
	progress := func() Progress {
		t.Helper()
		p, err := f.store.Progress(ctx, f.org, f.project)
		require.NoError(t, err)
		return p
	}
	sealed := Progress{Current: 1, Waiting: 0, Unattributed: 0}
	deleted := Progress{Current: 0, Waiting: 1, Unattributed: 0}
	text := func(v string) pgtype.Text { return pgtype.Text{String: v, Valid: true} }

	f.capture(t, id)
	require.Equal(t, sealed, progress())

	require.NoError(t, messages.UpdateToolCallOutcome(ctx, chatrepo.UpdateToolCallOutcomeParams{ToolOutcome: text("success"), ToolOutcomeNotes: text("example note"), ID: id, ProjectID: f.project}))
	require.Equal(t, sealed, progress(), "unchanged notes keep the transcript")
	require.NoError(t, messages.UpdateToolCallOutcome(ctx, chatrepo.UpdateToolCallOutcomeParams{ToolOutcome: text("success"), ToolOutcomeNotes: text("changed note"), ID: id, ProjectID: f.project}))
	require.Equal(t, deleted, progress(), "changed notes delete the transcript")

	f.capture(t, id)
	_, err := messages.StoreToolCallSummary(ctx, chatrepo.StoreToolCallSummaryParams{ToolCallID: "example", Summary: []byte(`"done"`), MessageID: id, ChatID: uuid.New(), ProjectID: f.project})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, sealed, progress(), "a summary for another chat leaves the transcript")
	_, err = messages.StoreToolCallSummary(ctx, chatrepo.StoreToolCallSummaryParams{ToolCallID: "example", Summary: []byte(`"done"`), MessageID: id, ChatID: f.chat, ProjectID: f.project})
	require.NoError(t, err)
	require.Equal(t, deleted, progress(), "a stored summary deletes the transcript")

	f.capture(t, id)
	_, err = fixtures.SetSource(ctx, pgtype.Text{String: "kit", Valid: true}, id)
	require.NoError(t, err)
	_, err = fixtures.ShadowMetadata(ctx, id)
	require.NoError(t, err, "sealing measured the message")
	n, err := messages.PatchKitProjectionMessage(ctx, chatrepo.PatchKitProjectionMessageParams{
		Content: "patched", ContentRaw: nil, ToolCalls: []byte(`[]`), ContentHash: nil, FinishReason: text("stop"),
		ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, ChatID: f.chat, ID: id,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	_, err = fixtures.ShadowMetadata(ctx, id)
	require.Error(t, err, "a body change drops the metadata measured from the old body")
	ids, err := fixtures.ShadowToolCallIDs(ctx, id)
	require.NoError(t, err)
	require.Empty(t, ids, "and its tool-call IDs")
	require.Equal(t, deleted, progress(), "and the transcript")
}
