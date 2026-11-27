package transcriptstore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// The tests below change a shadowed message's plaintext or its shadow's
// metadata behind the store's back, so each derived value shows which of the
// two it was computed from.

func int4(v int32) pgtype.Int4 { return pgtype.Int4{Int32: v, Valid: true} }

var nullInt4 = pgtype.Int4{Int32: 0, Valid: false}

// setMetadata overrides one message's metadata, keeping the sealed value of
// any field passed as nil.
func (f *fixture) setMetadata(t *testing.T, id uuid.UUID, toolCallCount, contentBytes *pgtype.Int4) {
	t.Helper()
	fixtures := fixturerepo.New(f.db)
	meta, err := fixtures.StoredMetadata(t.Context(), id)
	require.NoError(t, err)
	if toolCallCount != nil {
		meta.ToolCallCount = *toolCallCount
	}
	if contentBytes != nil {
		meta.ContentBytes = *contentBytes
	}
	_, err = fixtures.SetShadowMetadata(t.Context(), meta.ToolCallCount, meta.ContentBytes, meta.ContentRawBytes, meta.ToolCallsBytes, id)
	require.NoError(t, err)
}

func TestEntryTotalsCountToolCallsFromShadowMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	shadowed, plain := uuid.New(), uuid.New()
	f.add(t, shadowed)
	f.add(t, plain)
	f.capture(t, shadowed)

	// The shadow records one tool call; the plaintext now says none.
	_, err := fixturerepo.New(f.db).SetToolCalls(t.Context(), nil, shadowed, project(f.project))
	require.NoError(t, err)

	totals := func(transcripts *Transcripts) GetChatEntryTotalsRow {
		t.Helper()
		row, err := transcripts.GetChatEntryTotals(t.Context(), f.db, GetChatEntryTotalsParams{ChatID: f.chat, ProjectID: f.project, Generation: 0, RiskMessageIds: []uuid.UUID{}, RiskContentPartIds: []uuid.UUID{}})
		require.NoError(t, err)
		return row
	}

	enabled := NewTranscripts(f.store, f.project)
	require.Equal(t, int64(2), totals(enabled).ToolCalls, "the shadowed message counts from its metadata, the other from plaintext")
	require.Equal(t, int64(2), totals(NewTranscripts(f.store)).ToolCalls, "the metadata lives on the message, so a project off the list reads it too")

	// A shadow captured before the count existed falls back to plaintext.
	f.setMetadata(t, shadowed, &nullInt4, nil)
	require.Equal(t, int64(1), totals(enabled).ToolCalls)

	// A count of zero is read as no tool calls.
	f.setMetadata(t, shadowed, new(int4(0)), nil)
	_, err = fixturerepo.New(f.db).SetToolCalls(t.Context(), []byte(`[{"id":"example"}]`), shadowed, project(f.project))
	require.NoError(t, err)
	require.Equal(t, int64(1), totals(enabled).ToolCalls)
}

func TestFindAssistantToolCallUsesShadowIndex(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	fixtures := fixturerepo.New(f.db)
	shadowed, plain, legacy := uuid.New(), uuid.New(), uuid.New()
	f.add(t, shadowed)
	f.add(t, plain)
	f.add(t, legacy)
	f.capture(t, shadowed)
	_, err := fixtures.SetToolCalls(t.Context(), []byte(`[{"id":"other"}]`), shadowed, project(f.project))
	require.NoError(t, err)
	_, err = fixtures.SetToolCalls(t.Context(), []byte(`[{"id":"plain"}]`), plain, project(f.project))
	require.NoError(t, err)
	_, err = fixtures.SetToolCalls(t.Context(), []byte(`[{"id":"legacy"}]`), legacy, project(f.project))
	require.NoError(t, err)
	// A shadow captured before the index existed has no metadata and no IDs.
	_, err = fixtures.InsertTranscript(t.Context(), legacy, f.project, f.org)
	require.NoError(t, err)

	find := func(transcripts *Transcripts, toolCallID string) (uuid.UUID, error) {
		t.Helper()
		return transcripts.FindAssistantToolCallMessageID(t.Context(), f.db, f.project, f.chat, toolCallID)
	}

	enabled := NewTranscripts(f.store, f.project)
	id, err := find(enabled, "example")
	require.NoError(t, err)
	require.Equal(t, shadowed, id, "found by the shadow's indexed ID, not the plaintext")
	_, err = find(enabled, "other")
	require.ErrorIs(t, err, pgx.ErrNoRows, "an indexed message's plaintext is not scanned")
	id, err = find(enabled, "plain")
	require.NoError(t, err)
	require.Equal(t, plain, id, "a message with no shadow falls back to plaintext")
	id, err = find(enabled, "legacy")
	require.NoError(t, err)
	require.Equal(t, legacy, id, "a shadow without an index falls back to plaintext")

	// An element of another shape does not hide the IDs of its siblings.
	mixed := uuid.New()
	f.add(t, mixed)
	_, err = fixtures.SetToolCalls(t.Context(), []byte(`[{"id":"mixed"},"text",{"id":7}]`), mixed, project(f.project))
	require.NoError(t, err)
	f.capture(t, mixed)
	id, err = find(enabled, "mixed")
	require.NoError(t, err)
	require.Equal(t, mixed, id, "found by the shadow's index")

	off := NewTranscripts(f.store)
	_, err = find(off, "example")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	id, err = find(off, "other")
	require.NoError(t, err)
	require.Equal(t, shadowed, id, "a project off the list scans plaintext")
}

func TestPlatformCapsFollowShadowMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	f.setMetadata(t, id, nil, new(int4(200000)))

	read := func(transcripts *Transcripts) ReadPlatformChatMessagesRow {
		t.Helper()
		rows, err := transcripts.ReadPlatformChatMessages(t.Context(), f.db, ReadPlatformChatMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageID: uuid.NullUUID{UUID: id, Valid: true}, ChatID: f.chat, ContextSize: 5, FollowingLimit: 6})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0]
	}
	search := func(transcripts *Transcripts) SearchPlatformChatMessagesRow {
		t.Helper()
		now := time.Now()
		rows, err := transcripts.SearchPlatformChatMessages(t.Context(), f.db, SearchPlatformChatMessagesParams{
			OpenShadows: false, ProjectID: f.project, OrganizationID: f.org,
			FromTime: conv.ToPGTimestamptz(now.Add(-time.Hour)), ToTime: conv.ToPGTimestamptz(now.Add(time.Hour)),
			ChatIds: []uuid.UUID{}, OwnUserID: "", OwnedAgentIds: []uuid.UUID{}, OwnerUserID: "", Source: "", Pattern: "",
			ParticipantID: "", ParticipantProvider: "", HasCursor: false, OldestFirst: false,
			CursorAt: conv.ToPGTimestamptz(now), CursorID: uuid.Nil, RowLimit: 10,
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0]
	}

	enabled := NewTranscripts(f.store, f.project)
	r := read(enabled)
	require.True(t, r.Oversized, "the size comes from the shadow's metadata")
	require.True(t, r.ContentOversized)
	require.Empty(t, r.Content, "the opened shadow is blanked by the same flag")
	require.NotEmpty(t, r.ToolCalls)
	s := search(enabled)
	require.True(t, s.Oversized)
	require.Empty(t, s.Content)

	off := NewTranscripts(f.store)
	r = read(off)
	require.True(t, r.Oversized, "the metadata lives on the message, so a project off the list reads it too")
	require.Empty(t, r.Content)
	s = search(off)
	require.True(t, s.Oversized)
	require.Empty(t, s.Content)

	// A shadow captured before the sizes existed measures plaintext.
	f.setMetadata(t, id, nil, &nullInt4)
	r = read(enabled)
	require.False(t, r.Oversized)
	require.Equal(t, "sensitive example", r.Content)
}

func TestEvaluationHistoryBudgetFollowsShadowMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)

	history := func(transcripts *Transcripts) LoadEvaluationHistoryRow {
		t.Helper()
		rows, err := transcripts.LoadEvaluationHistory(t.Context(), f.db, LoadEvaluationHistoryParams{
			OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, ChatID: f.chat, Generation: 0,
			TargetID: uuid.New(), TargetCreatedAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), TargetSeq: 1 << 62,
			HistoryLimit: 10, ContentBudget: 1000,
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0]
	}

	enabled := NewTranscripts(f.store, f.project)
	require.False(t, history(enabled).ContentTooLarge)

	f.setMetadata(t, id, nil, new(int4(5000)))
	r := history(enabled)
	require.True(t, r.ContentTooLarge, "the budget counts the shadow's metadata")
	require.Empty(t, r.Content, "the opened shadow is blanked by the same flag")

	r = history(NewTranscripts(f.store))
	require.True(t, r.ContentTooLarge, "the metadata lives on the message, so a project off the list reads it too")
	require.Empty(t, r.Content)
}

func TestPromptParentsSkipEmptyByShadowMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	_, err := fixturerepo.New(f.db).SetRoleAndSource(t.Context(), "user", pgtype.Text{String: "claude-code", Valid: true}, id, project(f.project))
	require.NoError(t, err)
	f.capture(t, id)

	list := func(transcripts *Transcripts) []ListClaudeUserMessagesForPromptAttachmentParentRow {
		t.Helper()
		rows, err := transcripts.ListClaudeUserMessagesForPromptAttachmentParent(t.Context(), f.db, f.project, f.chat)
		require.NoError(t, err)
		return rows
	}

	enabled := NewTranscripts(f.store, f.project)
	require.Len(t, list(enabled), 1)

	f.setMetadata(t, id, nil, new(int4(0)))
	require.Empty(t, list(enabled), "an empty prompt is recognized by its metadata")
	require.Empty(t, list(NewTranscripts(f.store)), "the metadata lives on the message, so a project off the list reads it too")

	f.setMetadata(t, id, nil, &nullInt4)
	require.Len(t, list(enabled), 1, "a shadow without sizes falls back to plaintext")
}

func TestUpsertCorrelatedReturnsShadowBody(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	fixtures := fixturerepo.New(f.db)
	existing := uuid.New()
	f.add(t, existing)
	_, err := fixtures.SetExternalMessageID(t.Context(), pgtype.Text{String: "turn-1", Valid: true}, pgtype.Text{String: "litellm", Valid: true}, existing, project(f.project))
	require.NoError(t, err)
	f.capture(t, existing)
	transcripts := NewTranscripts(f.store, f.project)

	upsert := func(externalID string, content string) UpsertCorrelatedChatMessageRow {
		t.Helper()
		tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: the upsert and its capture share one caller-owned transaction.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(t.Context()) }()
		row, err := transcripts.UpsertCorrelatedChatMessage(t.Context(), tx, UpsertCorrelatedChatMessageParams{
			ID: uuid.New(), ChatID: f.chat, Role: "assistant", ProjectID: f.project, Content: content,
			ExternalMessageID: pgtype.Text{String: externalID, Valid: true}, ToolCalls: []byte(`[{"id":"call_new"}]`),
			Source: pgtype.Text{String: "codex", Valid: true}, CreatedAt: conv.ToPGTimestamptz(time.Now()),
		})
		require.NoError(t, err)
		require.NoError(t, tx.Commit(t.Context()))
		return row
	}

	// Promotion keeps the stored body and returns it from the shadow.
	promoted := upsert("turn-1", "incoming body")
	require.False(t, promoted.Inserted)
	require.Equal(t, existing, promoted.ID)
	require.Equal(t, "sensitive example", promoted.Content)
	require.JSONEq(t, `[{"id":"example"}]`, string(promoted.ToolCalls))

	// An insert returns no body from the plaintext columns: the caller holds
	// what it wrote, and the chat message writer seals it.
	inserted := upsert("turn-2", "new turn")
	require.True(t, inserted.Inserted)
	require.Empty(t, inserted.Content)
	require.Empty(t, inserted.ToolCalls)
}
