package transcriptstore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
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
		{name: "object", toolCalls: `{"id":"call_a"}`, count: 0, ids: []string{}},
		{name: "mixed shapes", toolCalls: `[{"id":"call_a"},"text",{"id":7},null,{"id":"call_b"}]`, count: 5, ids: []string{"call_a", "call_b"}},
		{name: "double encoded mixed shapes", toolCalls: `"[{\"id\":\"call_a\"},1]"`, count: 2, ids: []string{"call_a"}},
		{name: "json null", toolCalls: `null`, count: 0, ids: []string{}},
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
	require.NoError(t, none.Capture(t.Context(), f.db, f.org, f.project, []uuid.UUID{id}))

	other := NewTranscripts(f.store, uuid.New())
	require.False(t, other.Enabled(f.project))
	require.NoError(t, other.Capture(t.Context(), f.db, f.org, f.project, []uuid.UUID{id}))
	_, err := fixturerepo.New(f.db).ShadowMetadata(t.Context(), id)
	require.Error(t, err, "a project off the list captures nothing")

	enabled := NewTranscripts(f.store, f.project)
	require.True(t, enabled.Enabled(f.project))
	require.NoError(t, enabled.Capture(t.Context(), f.db, f.org, f.project, []uuid.UUID{id}))
	_, err = fixturerepo.New(f.db).ShadowMetadata(t.Context(), id)
	require.NoError(t, err)
}

type openRow struct {
	id      uuid.UUID
	content string
	notes   pgtype.Text
}

func openRowBody(r *openRow) Body {
	return Body{MessageID: r.id, Content: &r.content, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: &r.notes, ToolCallSummaries: nil, StorageError: nil}
}

func TestOpenRowsServesShadowsFallsBackAndFailsStale(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	shadowed, unshadowed := uuid.New(), uuid.New()
	f.add(t, shadowed)
	f.add(t, unshadowed)
	f.capture(t, shadowed)
	transcripts := NewTranscripts(f.store, f.project)

	rows := []openRow{
		{id: shadowed, content: "plaintext", notes: pgtype.Text{String: "", Valid: false}},
		{id: unshadowed, content: "plaintext", notes: pgtype.Text{String: "", Valid: false}},
	}
	require.NoError(t, OpenRows(t.Context(), transcripts, f.org, f.project, rows, openRowBody))
	require.Equal(t, "sensitive example", rows[0].content, "a shadowed message is served from its shadow")
	require.Equal(t, pgtype.Text{String: "example note", Valid: true}, rows[0].notes)
	require.Equal(t, "plaintext", rows[1].content, "a message with no shadow keeps its plaintext")

	off := []openRow{{id: shadowed, content: "plaintext", notes: pgtype.Text{String: "", Valid: false}}}
	require.NoError(t, OpenRows(t.Context(), NewTranscripts(f.store), f.org, f.project, off, openRowBody))
	require.Equal(t, "plaintext", off[0].content, "a project off the list reads plaintext")

	_, err := fixturerepo.New(f.db).ChangeContent(t.Context(), shadowed, project(f.project))
	require.NoError(t, err)
	stale := []openRow{{id: shadowed, content: "changed", notes: pgtype.Text{String: "", Valid: false}}}
	err = OpenRows(t.Context(), transcripts, f.org, f.project, stale, openRowBody)
	require.ErrorIs(t, err, ErrStale, "a stale shadow fails the read instead of serving plaintext")
}
