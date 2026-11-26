package orgencryption_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type transcriptWriterFixture struct {
	*testInstance
	projectID uuid.UUID
	chatID    uuid.UUID
	writer    *chat.ChatMessageWriter
	store     *transcriptstore.Store
}

func newTranscriptWriterFixture(t *testing.T, provision, shadow bool) *transcriptWriterFixture {
	t.Helper()
	ti := newTestInstance(t)
	p, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Transcript test", Slug: "transcript-test", OrganizationID: ti.orgID})
	require.NoError(t, err)
	chatID := uuid.New()
	_, err = chatrepo.New(ti.conn).UpsertChat(t.Context(), chatrepo.UpsertChatParams{ID: chatID, ProjectID: p.ID, OrganizationID: ti.orgID})
	require.NoError(t, err)
	if provision {
		require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	}
	store, err := transcriptstore.New(ti.conn, ti.fixture.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { require.NoError(t, shutdown(context.Background())) })
	if shadow {
		writer.WithTranscriptShadow(store, p.ID)
	}
	return &transcriptWriterFixture{testInstance: ti, projectID: p.ID, chatID: chatID, writer: writer, store: store}
}

func (f *transcriptWriterFixture) message(content string) chat.MessageWrite {
	return chat.MessageWrite{Params: chatrepo.CreateChatMessageParams{ID: uuid.New(), ChatID: f.chatID, ProjectID: f.projectID, Role: "assistant", Content: content}}
}

func (f *transcriptWriterFixture) assertShadow(t *testing.T, id uuid.UUID) {
	t.Helper()
	row, err := chatrepo.New(f.conn).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: id})
	require.NoError(t, err)
	opened, err := f.store.Load(t.Context(), f.orgID, f.projectID, id)
	require.NoError(t, err)
	require.Equal(t, row.Content, opened.Content)
	require.JSONEq(t, jsonOrNull(row.ContentRaw), jsonOrNull(opened.ContentRaw))
	require.JSONEq(t, jsonOrNull(row.ToolCalls), jsonOrNull(opened.ToolCalls))
	if row.ToolOutcomeNotes.Valid {
		require.NotNil(t, opened.ToolOutcomeNotes)
		require.Equal(t, row.ToolOutcomeNotes.String, *opened.ToolOutcomeNotes)
	} else {
		require.Nil(t, opened.ToolOutcomeNotes)
	}
	require.JSONEq(t, jsonOrNull(row.ToolCallSummaries), jsonOrNull(opened.ToolCallSummaries))
}

func jsonOrNull(b []byte) string {
	if len(b) == 0 {
		return "null"
	}
	return string(b)
}

// Message and shadow counts are project scoped. Outbox counts cover the entire
// isolated fixture database, including provisioning events captured in baselines.
func (f *transcriptWriterFixture) counts(t *testing.T) [4]int64 {
	t.Helper()
	messages, err := chatrepo.New(f.conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: f.chatID, ProjectID: f.projectID})
	require.NoError(t, err)
	progress, err := f.store.Progress(t.Context(), f.orgID, f.projectID)
	require.NoError(t, err)
	q := testrepo.New(f.conn)
	legacy, err := q.CountOutboxRows(t.Context())
	require.NoError(t, err)
	published, err := q.CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	return [4]int64{int64(len(messages)), progress.Current, legacy, published}
}

func TestTranscriptWriterNative(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	m := f.message("EXAMPLE native plaintext")
	m.Params.ContentRaw = []byte(`{"type":"text","text":"EXAMPLE rich content"}`)
	m.Params.ToolCalls = []byte(`[{"id":"example-call","type":"function","function":{"name":"example","arguments":"{}"}}]`)
	n, err := f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{m})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.assertShadow(t, m.Params.ID)
	row, err := chatrepo.New(f.conn).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: m.Params.ID})
	require.NoError(t, err)
	require.Equal(t, m.Params.Content, row.Content, "shadow storage must retain plaintext")
}

// Each write warms the transcript key before its transaction, so a process
// unwraps it once per cache period and no capture calls KMS inside a write.
func TestTranscriptWriterUnwrapsOncePerCachePeriod(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	before := f.fixture.KMS.Decrypts()
	for range 3 {
		m := f.message("EXAMPLE repeated write")
		n, err := f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{m})
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		f.assertShadow(t, m.Params.ID)
	}
	require.Equal(t, before+1, f.fixture.KMS.Decrypts())
}

func TestTranscriptWriterCallerTransaction(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{true, false} {
		t.Run(map[bool]string{true: "commit", false: "rollback"}[commit], func(t *testing.T) {
			t.Parallel()
			f := newTranscriptWriterFixture(t, true, true)
			before := f.counts(t)
			tx := testenv.BeginTx(t, t.Context(), f.conn)
			m := f.message("EXAMPLE transactional content")
			n, err := f.writer.WriteInTx(t.Context(), tx, []chat.MessageWrite{m})
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
			require.Equal(t, before, f.counts(t), "caller transaction must not publish before commit")
			if commit {
				require.NoError(t, tx.Commit(t.Context()))
				f.assertShadow(t, m.Params.ID)
				after := f.counts(t)
				require.Equal(t, before[0]+1, after[0])
				require.Equal(t, before[1]+1, after[1])
				require.Greater(t, after[2]+after[3], before[2]+before[3])
			} else {
				require.NoError(t, tx.Rollback(t.Context()))
				require.Equal(t, before, f.counts(t))
			}
		})
	}
}

func TestTranscriptWriterKeyFailuresAreAtomic(t *testing.T) {
	t.Parallel()
	for _, suspended := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "suspended"}[suspended], func(t *testing.T) {
			t.Parallel()
			f := newTranscriptWriterFixture(t, suspended, true)
			if suspended {
				require.NoError(t, f.fixture.Provisioner.Suspend(t.Context(), f.orgID, "EXAMPLE suspended test key", orgencryption.SystemActor()))
			}
			before := f.counts(t)
			expected := orgencryption.ErrNotProvisioned
			if suspended {
				expected = orgencryption.ErrSuspended
			}
			_, err := f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{f.message("EXAMPLE rejected message")})
			require.ErrorIs(t, err, expected)
			require.Equal(t, before, f.counts(t))
			err = f.writer.WriteTurn(t.Context(), f.projectID, nil, []chat.MessageWrite{f.message("EXAMPLE rejected turn one"), f.message("EXAMPLE rejected turn two")})
			require.ErrorIs(t, err, expected)
			require.Equal(t, before, f.counts(t))
		})
	}
}

func TestTranscriptWriterCorrelatedPromotion(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	original := f.message("EXAMPLE stored body")
	original.Params.Source = conv.ToPGText("litellm")
	n, err := f.writer.WriteCorrelated(t.Context(), f.projectID, original, "example-external-id")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.assertShadow(t, original.Params.ID)
	candidate := f.message("EXAMPLE candidate body must not replace stored body")
	candidate.Params.Source = conv.ToPGText("codex")
	n, err = f.writer.WriteCorrelated(t.Context(), f.projectID, candidate, "example-external-id")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.assertShadow(t, original.Params.ID)
	opened, err := f.store.Load(t.Context(), f.orgID, f.projectID, original.Params.ID)
	require.NoError(t, err)
	require.Equal(t, original.Params.Content, opened.Content)
	_, err = f.store.Load(t.Context(), f.orgID, f.projectID, candidate.Params.ID)
	require.ErrorIs(t, err, transcriptstore.ErrNotFound)
	counts := f.counts(t)
	require.EqualValues(t, 1, counts[0])
	require.EqualValues(t, 1, counts[1])
}

func TestTranscriptWriterExternalIdempotence(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	id := uuid.New()
	m := chat.ExternalMessageWrite{Params: chatrepo.CreateExternalChatMessageParams{ID: id, ChatID: f.chatID, ProjectID: f.projectID, Role: "assistant", Content: "EXAMPLE imported content", ExternalMessageID: conv.ToPGText("example-import-id")}}
	n, err := f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{m}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.assertShadow(t, id)
	before := f.counts(t)
	m.Params.ID = uuid.New()
	m.Params.Content = "EXAMPLE retry does not replace original"
	n, err = f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{m}, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, before, f.counts(t))
	f.assertShadow(t, id)
}

func TestTranscriptWriterAssistantTurn(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	first, second := f.message("EXAMPLE first assistant"), f.message("EXAMPLE second assistant")
	require.NoError(t, f.writer.WriteTurn(t.Context(), f.projectID, nil, []chat.MessageWrite{first, second}))
	f.assertShadow(t, first.Params.ID)
	f.assertShadow(t, second.Params.ID)
	counts := f.counts(t)
	require.EqualValues(t, 2, counts[0])
	require.EqualValues(t, 2, counts[1])
}

func TestTranscriptWriterOptOutWithoutStorageOrKeys(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, false, false)
	// Dropping only in this isolated database proves the opt-out path never consults shadow storage.
	_, err := f.conn.Exec(t.Context(), `DROP TABLE chat_message_transcripts CASCADE`) //nolint:glint // notestingrawsql: deliberately simulate a legacy schema without shadow storage; no fixture query can drop this table.
	require.NoError(t, err)
	m := f.message("EXAMPLE opt-out plaintext")
	n, err := f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{m})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	rows, err := chatrepo.New(f.conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: f.chatID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, m.Params.Content, rows[0].Content)

	// Neither an accidentally empty allowlist nor another project may widen
	// capture to this project or start consulting unavailable shadow storage.
	f.writer.WithTranscriptShadow(f.store, []uuid.UUID{}...)
	n, err = f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{f.message("EXAMPLE empty allowlist")})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.writer.WithTranscriptShadow(f.store, uuid.New())
	n, err = f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{f.message("EXAMPLE excluded project")})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func TestTranscriptWriterProjection(t *testing.T) {
	t.Parallel()
	for _, provision := range []bool{true, false} {
		t.Run(map[bool]string{true: "capture-insert-and-update", false: "rollback-missing-key"}[provision], func(t *testing.T) {
			t.Parallel()
			f := newTranscriptWriterFixture(t, provision, true)
			m := f.message("EXAMPLE projected body")
			m.Params.Source = conv.ToPGText("kit")
			guard := func(context.Context, pgx.Tx) error { return nil }
			build := func(*chatrepo.ChatMessage) (chat.MessageWrite, bool, error) { return m, true, nil }
			before := f.counts(t)
			changed, err := f.writer.ProjectMessage(t.Context(), f.projectID, m.Params.ID, guard, build)
			if !provision {
				require.ErrorIs(t, err, orgencryption.ErrNotProvisioned)
				require.False(t, changed)
				require.Equal(t, before, f.counts(t))
				return
			}
			require.NoError(t, err)
			require.True(t, changed)
			f.assertShadow(t, m.Params.ID)
			m.Params.Content = "EXAMPLE updated projection"
			changed, err = f.writer.ProjectMessage(t.Context(), f.projectID, m.Params.ID, guard, build)
			require.NoError(t, err)
			require.True(t, changed)
			f.assertShadow(t, m.Params.ID)
		})
	}
}
