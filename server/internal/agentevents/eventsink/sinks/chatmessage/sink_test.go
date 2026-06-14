package chatmessage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	chatmessagesink "github.com/speakeasy-api/gram/server/internal/agentevents/eventsink/sinks/chatmessage"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type testPayload struct{}

type recordingWriter struct {
	errors []error
	writes [][]chatRepo.CreateChatMessageParams
}

func (w *recordingWriter) Write(_ context.Context, _ uuid.UUID, params []chatRepo.CreateChatMessageParams) (int64, error) {
	w.writes = append(w.writes, params)
	if len(w.errors) > 0 {
		err := w.errors[0]
		w.errors = w.errors[1:]
		return 0, err
	}
	return int64(len(params)), nil
}

type recordingFeatures struct {
	enabled bool
	err     error
	calls   int
}

func (f *recordingFeatures) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	f.calls++
	return f.enabled, f.err
}

type recordingDBTX struct {
	err   error
	calls []hooksRepo.UpsertClaudeCodeSessionParams
}

func (db *recordingDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (db *recordingDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (db *recordingDBTX) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	call := hooksRepo.UpsertClaudeCodeSessionParams{
		ID:             args[0].(uuid.UUID),
		ProjectID:      args[1].(uuid.UUID),
		OrganizationID: args[2].(string),
		UserID:         args[3].(pgtype.Text),
		ExternalUserID: args[4].(pgtype.Text),
		Title:          args[5].(pgtype.Text),
	}
	db.calls = append(db.calls, call)
	return recordingRow{id: call.ID, err: db.err}
}

type recordingRow struct {
	id  uuid.UUID
	err error
}

func (r recordingRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*uuid.UUID) = r.id
	return nil
}

type recordingTitleGenerator struct {
	calls []titleCall
}

type titleCall struct {
	chatID    string
	orgID     string
	projectID string
}

func (g *recordingTitleGenerator) ScheduleChatTitleGeneration(_ context.Context, chatID, orgID, projectID string) error {
	g.calls = append(g.calls, titleCall{chatID: chatID, orgID: orgID, projectID: projectID})
	return nil
}

func TestChatMessageSinkSkipsMissingConversationID(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	sink := newChatMessageSink(writer, &recordingFeatures{enabled: true}, nil, nil)
	ev := newChatMessageEvent(t, types.UserPromptSubmit, types.FieldPrompt, "hello")
	ev.Context.ConversationID = ""

	require.NoError(t, sink.Write(t.Context(), ev))
	assert.Empty(t, writer.writes)
}

func TestChatMessageSinkSkipsWhenFeatureDisabled(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	features := &recordingFeatures{enabled: false}
	sink := newChatMessageSink(writer, features, nil, nil)
	ev := newChatMessageEvent(t, types.UserPromptSubmit, types.FieldPrompt, "hello")

	require.NoError(t, sink.Write(t.Context(), ev))
	assert.Equal(t, 1, features.calls)
	assert.Empty(t, writer.writes)
}

func TestChatMessageSinkWritesBuiltMessages(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	sink := newChatMessageSink(writer, &recordingFeatures{enabled: true}, nil, nil)
	ev := newChatMessageEvent(t, types.UserPromptSubmit, types.FieldPrompt, "hello")

	require.NoError(t, sink.Write(t.Context(), ev))
	require.Len(t, writer.writes, 1)
	require.Len(t, writer.writes[0], 1)
	assert.Equal(t, "hello", writer.writes[0][0].Content)
	assert.Equal(t, "cursor", writer.writes[0][0].Source.String)
}

func TestChatMessageSinkSchedulesTitleWhenRequested(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	titleGenerator := &recordingTitleGenerator{}
	sink := newChatMessageSink(writer, &recordingFeatures{enabled: true}, nil, titleGenerator)
	ev := newChatMessageEvent(t, types.AssistantResponseComplete, types.FieldAssistantText, "assistant")

	require.NoError(t, sink.Write(t.Context(), ev))
	require.Len(t, writer.writes, 1)
	require.Len(t, titleGenerator.calls, 1)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", titleGenerator.calls[0].chatID)
	assert.Equal(t, "org", titleGenerator.calls[0].orgID)
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", titleGenerator.calls[0].projectID)
}

func TestChatMessageSinkRetriesAfterForeignKeyViolation(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{errors: []error{foreignKeyViolation()}}
	db := &recordingDBTX{}
	sink := newChatMessageSink(writer, &recordingFeatures{enabled: true}, db, nil)
	ev := newChatMessageEvent(t, types.UserPromptSubmit, types.FieldPrompt, "hello")

	require.NoError(t, sink.Write(t.Context(), ev))
	require.Len(t, writer.writes, 2)
	require.Len(t, db.calls, 1)
	assert.Equal(t, "org", db.calls[0].OrganizationID)
	assert.Equal(t, activities.DefaultCursorChatTitle, db.calls[0].Title.String)
}

func TestChatMessageSinkReturnsNonForeignKeyWriteError(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{errors: []error{errors.New("boom")}}
	db := &recordingDBTX{}
	sink := newChatMessageSink(writer, &recordingFeatures{enabled: true}, db, nil)
	ev := newChatMessageEvent(t, types.UserPromptSubmit, types.FieldPrompt, "hello")

	err := sink.Write(t.Context(), ev)
	require.Error(t, err)
	assert.ErrorContains(t, err, "insert chat message")
	assert.Empty(t, db.calls)
}

func newChatMessageSink(writer chatmessagesink.Writer, features chatmessagesink.ProductFeaturesClient, db hooksRepo.DBTX, titleGenerator chatmessagesink.TitleGenerator) *chatmessagesink.Sink[*testPayload] {
	return chatmessagesink.New[*testPayload](chatmessagesink.Config{
		Writer:          writer,
		ProductFeatures: features,
		DB:              db,
		TitleGenerator:  titleGenerator,
	})
}

func newChatMessageEvent(t *testing.T, eventType types.EventType, field types.Field, value string) agentevents.Event[*testPayload] {
	t.Helper()

	agent, err := agentevents.NewAgent[*testPayload]("cursor")
	require.NoError(t, err)
	require.NoError(t, agent.Register(
		agentevents.Resolve[*testPayload, types.EventType](types.FieldEventType, func(agentevents.Event[*testPayload]) (types.EventType, bool, error) {
			return eventType, true, nil
		}),
		agentevents.Resolve[*testPayload, string](field, func(agentevents.Event[*testPayload]) (string, bool, error) {
			return value, true, nil
		}),
	))
	return agent.NewEvent(agentevents.EventContext{
		OrgID:          "org",
		ProjectID:      "22222222-2222-2222-2222-222222222222",
		UserID:         "user",
		UserEmail:      "dev@example.com",
		ConversationID: "11111111-1111-1111-1111-111111111111",
		Timestamp:      time.Unix(123, 0),
	}, &testPayload{})
}

func foreignKeyViolation() error {
	return &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation}
}
