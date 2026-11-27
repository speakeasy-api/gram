package orgencryption_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

func newTranscriptWriterFixture(t *testing.T, provision, sealed bool) *transcriptWriterFixture {
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
	store, err := transcriptstore.New(ti.conn, ti.fixture.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { require.NoError(t, shutdown(context.Background())) })
	if sealed {
		writer.WithTranscripts(transcriptstore.NewTranscripts(store, p.ID))
	}
	return &transcriptWriterFixture{testInstance: ti, projectID: p.ID, chatID: chatID, writer: writer, store: store}
}

func (f *transcriptWriterFixture) message(content string) chat.MessageWrite {
	return chat.MessageWrite{Params: chatrepo.CreateChatMessageParams{ID: uuid.New(), ChatID: f.chatID, ProjectID: f.projectID, Role: "assistant", Content: content}}
}

func (f *transcriptWriterFixture) assertTranscript(t *testing.T, id uuid.UUID) {
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

// Message and transcript counts are project scoped. Outbox counts cover the entire
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
	f.assertTranscript(t, m.Params.ID)
	row, err := chatrepo.New(f.conn).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: m.Params.ID})
	require.NoError(t, err)
	require.Equal(t, m.Params.Content, row.Content, "transcript storage must retain plaintext")
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
		f.assertTranscript(t, m.Params.ID)
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
			m := f.message("EXAMPLE transactional content")
			errRollback := errors.New("roll back")
			err := f.writer.InTx(t.Context(), f.projectID, func(tx pgx.Tx) error {
				n, err := f.writer.WriteInTx(t.Context(), tx, []chat.MessageWrite{m})
				require.NoError(t, err)
				require.EqualValues(t, 1, n)
				require.Equal(t, before, f.counts(t), "caller transaction must not publish before commit")
				if commit {
					return nil
				}
				return errRollback
			})
			if commit {
				require.NoError(t, err)
				f.assertTranscript(t, m.Params.ID)
				after := f.counts(t)
				require.Equal(t, before[0]+1, after[0])
				require.Equal(t, before[1]+1, after[1])
				require.Greater(t, after[2]+after[3], before[2]+before[3])
			} else {
				require.ErrorIs(t, err, errRollback)
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
	f.assertTranscript(t, original.Params.ID)
	candidate := f.message("EXAMPLE candidate body must not replace stored body")
	candidate.Params.Source = conv.ToPGText("codex")
	n, err = f.writer.WriteCorrelated(t.Context(), f.projectID, candidate, "example-external-id")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.assertTranscript(t, original.Params.ID)
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
	f.assertTranscript(t, id)
	before := f.counts(t)
	m.Params.ID = uuid.New()
	m.Params.Content = "EXAMPLE retry does not replace original"
	n, err = f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{m}, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, before, f.counts(t))
	f.assertTranscript(t, id)
}

func TestTranscriptWriterAssistantTurn(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	first, second := f.message("EXAMPLE first assistant"), f.message("EXAMPLE second assistant")
	require.NoError(t, f.writer.WriteTurn(t.Context(), f.projectID, nil, []chat.MessageWrite{first, second}))
	f.assertTranscript(t, first.Params.ID)
	f.assertTranscript(t, second.Params.ID)
	counts := f.counts(t)
	require.EqualValues(t, 2, counts[0])
	require.EqualValues(t, 2, counts[1])
}

func TestTranscriptWriterOptOutWithoutStorageOrKeys(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, false, false)
	// Dropping only in this isolated database proves the opt-out path never consults transcript storage.
	_, err := f.conn.Exec(t.Context(), `DROP TABLE chat_message_transcripts CASCADE`) //nolint:glint // notestingrawsql: deliberately simulate a legacy schema without transcript storage; no fixture query can drop this table.
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
	// capture to this project or start consulting unavailable transcript storage.
	f.writer.WithTranscripts(transcriptstore.NewTranscripts(f.store, []uuid.UUID{}...))
	n, err = f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{f.message("EXAMPLE empty allowlist")})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.writer.WithTranscripts(transcriptstore.NewTranscripts(f.store, uuid.New()))
	n, err = f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{f.message("EXAMPLE excluded project")})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// InTx loads the key before every write transaction, so a key that will not
// load fails each write before anything is stored, even one that would have
// changed nothing.
func TestTranscriptWriterFailsClosedWithoutKey(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, true)
	m := chat.ExternalMessageWrite{Params: chatrepo.CreateExternalChatMessageParams{ID: uuid.New(), ChatID: f.chatID, ProjectID: f.projectID, Role: "assistant", Content: "EXAMPLE imported content", ExternalMessageID: conv.ToPGText("example-import-id")}}
	_, err := f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{m}, nil)
	require.NoError(t, err)
	require.NoError(t, f.fixture.Provisioner.Suspend(t.Context(), f.orgID, "EXAMPLE suspended test key", orgencryption.SystemActor()))
	before := f.counts(t)

	m.Params.ID = uuid.New()
	_, err = f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{m}, nil)
	require.ErrorIs(t, err, orgencryption.ErrSuspended, "a retry fails too")

	projected := f.message("EXAMPLE projected body")
	projected.Params.Source = conv.ToPGText("kit")
	guard := func(context.Context, pgx.Tx) error { return nil }
	skip := func(*chatrepo.ChatMessage) (chat.MessageWrite, bool, error) { return projected, false, nil }
	_, err = f.writer.ProjectMessage(t.Context(), f.projectID, projected.Params.ID, guard, skip)
	require.ErrorIs(t, err, orgencryption.ErrSuspended, "a skipped projection fails too")

	fresh := m
	fresh.Params.ID = uuid.New()
	fresh.Params.ExternalMessageID = conv.ToPGText("example-new-import-id")
	_, err = f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{fresh}, nil)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	apply := func(*chatrepo.ChatMessage) (chat.MessageWrite, bool, error) { return projected, true, nil }
	_, err = f.writer.ProjectMessage(t.Context(), f.projectID, projected.Params.ID, guard, apply)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.Equal(t, before, f.counts(t))
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
			f.assertTranscript(t, m.Params.ID)
			m.Params.Content = "EXAMPLE updated projection"
			changed, err = f.writer.ProjectMessage(t.Context(), f.projectID, m.Params.ID, guard, build)
			require.NoError(t, err)
			require.True(t, changed)
			f.assertTranscript(t, m.Params.ID)
		})
	}
}

// In encrypted mode the plaintext columns are empty, so an unchanged kit
// re-patch must compare against the decrypted row: it reports no change and
// keeps the completed risk analysis.
func TestTranscriptWriterEncryptedRepatchKeepsRiskAnalysis(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, false)
	f.writer.WithTranscripts(transcriptstore.NewTranscripts(f.store).WithEncryptedProjects(f.projectID))
	m := f.message("EXAMPLE encrypted projection")
	m.Params.Source = conv.ToPGText("kit")
	m.Params.ToolCalls = []byte(`[{"id":"example-call","type":"function","function":{"name":"example","arguments":"{}"}}]`)
	guard := func(context.Context, pgx.Tx) error { return nil }
	build := func(*chatrepo.ChatMessage) (chat.MessageWrite, bool, error) { return m, true, nil }
	changed, err := f.writer.ProjectMessage(t.Context(), f.projectID, m.Params.ID, guard, build)
	require.NoError(t, err)
	require.True(t, changed)

	err = f.writer.InTx(t.Context(), f.projectID, func(tx pgx.Tx) error {
		_, err := f.writer.UpdateChatMessage(t.Context(), tx, f.orgID, f.projectID, f.chatID, m.Params.ID, func(row *chatrepo.ChatMessage) error {
			row.RiskAnalyzedAt = pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}
			return nil
		})
		if err != nil {
			return fmt.Errorf("mark risk analyzed: %w", err)
		}
		return nil
	})
	require.NoError(t, err)

	changed, err = f.writer.ProjectMessage(t.Context(), f.projectID, m.Params.ID, guard, build)
	require.NoError(t, err)
	require.False(t, changed, "an unchanged re-patch changes nothing")
	row, err := chatrepo.New(f.conn).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: m.Params.ID})
	require.NoError(t, err)
	require.Empty(t, row.Content, "encrypted mode keeps the plaintext empty")
	require.True(t, row.RiskAnalyzedAt.Valid, "an unchanged re-patch keeps the completed risk analysis")

	m.Params.Content = "EXAMPLE changed projection"
	changed, err = f.writer.ProjectMessage(t.Context(), f.projectID, m.Params.ID, guard, build)
	require.NoError(t, err)
	require.True(t, changed)
	row, err = chatrepo.New(f.conn).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: m.Params.ID})
	require.NoError(t, err)
	require.False(t, row.RiskAnalyzedAt.Valid, "changed content is analyzed again")
	encrypted := transcriptstore.NewTranscripts(f.store).WithEncryptedProjects(f.projectID)
	content := row.Content
	require.NoError(t, encrypted.Open(t.Context(), f.orgID, f.projectID, []transcriptstore.ProtectedFieldRefs{{MessageID: m.Params.ID, Content: &content, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}}))
	require.Equal(t, "EXAMPLE changed projection", content)
}

// In encrypted mode every insert path keeps the protected fields out of
// chat_messages and seals them in the same transaction.
func TestTranscriptWriterEncryptedInsertsStoreNoPlaintext(t *testing.T) {
	t.Parallel()
	f := newTranscriptWriterFixture(t, true, false)
	encrypted := transcriptstore.NewTranscripts(f.store).WithEncryptedProjects(f.projectID)
	f.writer.WithTranscripts(encrypted)

	plain := f.message("EXAMPLE plain write")
	plain.Params.ToolCalls = []byte(`[{"id":"example-call","type":"function","function":{"name":"example","arguments":"{}"}}]`)
	n, err := f.writer.Write(t.Context(), f.projectID, []chat.MessageWrite{plain})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	correlated := f.message("EXAMPLE correlated prompt")
	correlated.Params.Role = "user"
	n, err = f.writer.WriteCorrelated(t.Context(), f.projectID, correlated, "agent-prompt:v1:example")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	external := chat.ExternalMessageWrite{Params: chatrepo.CreateExternalChatMessageParams{ID: uuid.New(), ChatID: f.chatID, ProjectID: f.projectID, Role: "assistant", Content: "EXAMPLE imported content", ExternalMessageID: conv.ToPGText("example-encrypted-import")}}
	n, err = f.writer.WriteExternalWithContentParts(t.Context(), f.projectID, []chat.ExternalMessageWrite{external}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	wants := map[uuid.UUID]struct{ content, toolCalls string }{
		plain.Params.ID:      {content: "EXAMPLE plain write", toolCalls: string(plain.Params.ToolCalls)},
		correlated.Params.ID: {content: "EXAMPLE correlated prompt", toolCalls: ""},
		external.Params.ID:   {content: "EXAMPLE imported content", toolCalls: ""},
	}
	for id, want := range wants {
		row, err := chatrepo.New(f.conn).GetKitProjectionMessage(t.Context(), chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: id})
		require.NoError(t, err)
		require.Empty(t, row.Content, "chat_messages never holds the plaintext")
		require.Empty(t, row.ToolCalls)
		content, toolCalls := row.Content, row.ToolCalls
		require.NoError(t, encrypted.Open(t.Context(), f.orgID, f.projectID, []transcriptstore.ProtectedFieldRefs{{MessageID: id, Content: &content, ContentRaw: nil, ToolCalls: &toolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}}))
		require.Equal(t, want.content, content, "the transcript holds what was written")
		if want.toolCalls == "" {
			require.Empty(t, toolCalls)
			continue
		}
		require.JSONEq(t, want.toolCalls, string(toolCalls), "the transcript holds the tool calls")
	}
}
