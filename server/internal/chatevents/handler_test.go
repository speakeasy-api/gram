package chatevents

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testOrganizationID = "org-test"

var (
	testProjectID = uuid.MustParse("01900000-0000-7000-8000-000000000101")
	testCreatedAt = time.Date(2026, 9, 28, 12, 0, 0, 123456789, time.UTC)
)

type stored struct {
	projectID uuid.UUID
	row       repo.ListMessagesForAgentEventsRow
}

// storedMessages stands in for the tenant-pinned query.
type storedMessages map[uuid.UUID]stored

func (s storedMessages) LoadMessages(_ context.Context, organizationID string, projectID uuid.UUID, ids []uuid.UUID) ([]repo.ListMessagesForAgentEventsRow, error) {
	var rows []repo.ListMessagesForAgentEventsRow
	for _, id := range ids {
		if m, ok := s[id]; ok && m.projectID == projectID && m.row.OrganizationID == organizationID {
			rows = append(rows, m.row)
		}
	}
	return rows, nil
}

type failingMessages struct{ err error }

func (f failingMessages) LoadMessages(context.Context, string, uuid.UUID, []uuid.UUID) ([]repo.ListMessagesForAgentEventsRow, error) {
	return nil, f.err
}

func logsEnabled(context.Context, string) (bool, error) { return true, nil }

func event(role conversationv1.MessageEvent_Role) *conversationv1.MessageEvent {
	m := &conversationv1.MessageEvent{}
	m.SetId(uuid.NewString())
	m.SetType(conversationv1.MessageEvent_TYPE_CREATED)
	m.SetOrganizationId(testOrganizationID)
	m.SetProjectId(testProjectID.String())
	m.SetConversationId(uuid.NewString())
	m.SetMessageId(uuid.NewString())
	m.SetRole(role)
	m.SetMessageCreatedAt(testCreatedAt.Format(time.RFC3339Nano))
	return m
}

func storedMessage(m *conversationv1.MessageEvent, text string) stored {
	var row repo.ListMessagesForAgentEventsRow
	row.ID = uuid.MustParse(m.GetMessageId())
	row.ChatID = uuid.MustParse(m.GetConversationId())
	row.OrganizationID = m.GetOrganizationId()
	row.Role = strings.TrimPrefix(strings.ToLower(m.GetRole().String()), "role_")
	row.Content = text
	row.CreatedAt = conv.ToPGTimestamptz(testCreatedAt)
	row.ExternalChatID = conv.ToPGText("ses_" + row.ChatID.String())
	row.Source = conv.ToPGText("claude-code")
	row.MessageID = conv.ToPGText("prompt_01")
	row.Model = conv.ToPGText("claude-sonnet-4-5")
	row.UserID = "user-1"
	row.UserEmail = "person@example.test"
	row.ExternalUserID = "external-1"
	return stored{projectID: uuid.MustParse(m.GetProjectId()), row: row}
}

// capture records every published log record on a mock publisher.
func capture(t *testing.T) (*gcp.MockPublisher[*otelv1.InboundLogRecord], func() []*otelv1.InboundLogRecord) {
	t.Helper()
	var mu sync.Mutex
	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		mu.Lock()
		published = append(published, record)
		mu.Unlock()
	}).Return(gcp.NewSuccessPublishResult()).Maybe()
	return publisher, func() []*otelv1.InboundLogRecord {
		mu.Lock()
		defer mu.Unlock()
		return append([]*otelv1.InboundLogRecord(nil), published...)
	}
}

func newTestHandler(t *testing.T, messages MessageSource, blobs blobio.Reader, gate func(context.Context, string) (bool, error), publisher gcp.Publisher[*otelv1.InboundLogRecord]) *Handler {
	t.Helper()
	records := otelpub.NewLogger(publisher, resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String("gram-server")), Scope)
	t.Cleanup(records.Close)
	return NewHandler(testenv.NewLogger(t), messages, blobs, gate, records)
}

func batch(events ...*conversationv1.MessageEvent) ([]message, []error) {
	failures := make([]error, len(events))
	messages := make([]message, len(events))
	for i, e := range events {
		messages[i] = message{event: e, fail: func(err error) { failures[i] = err }}
	}
	return messages, failures
}

func attributeValue(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func requireNoFailures(t *testing.T, failures []error) {
	t.Helper()
	for i, failure := range failures {
		require.NoError(t, failure, "message %d", i)
	}
}

func TestScopeIsWhatTheDialectMatches(t *testing.T) {
	t.Parallel()
	require.Equal(t, dialect.ChatLogScope, Scope)
}

func TestHandlerLogsCreatedUserAndAssistantMessages(t *testing.T) {
	t.Parallel()
	prompt := event(conversationv1.MessageEvent_ROLE_USER)
	response := event(conversationv1.MessageEvent_ROLE_ASSISTANT)
	tool := event(conversationv1.MessageEvent_ROLE_TOOL)
	updated := event(conversationv1.MessageEvent_ROLE_USER)
	updated.SetType(conversationv1.MessageEvent_TYPE_ATTRIBUTION_UPDATED)
	source := storedMessages{}
	for _, m := range []*conversationv1.MessageEvent{prompt, response, tool, updated} {
		s := storedMessage(m, "fix the build for "+m.GetMessageId())
		source[s.row.ID] = s
	}
	publisher, published := capture(t)
	h := newTestHandler(t, source, nil, logsEnabled, publisher)

	messages, failures := batch(prompt, response, tool, updated)
	require.NoError(t, h.handleBatch(t.Context(), messages))
	requireNoFailures(t, failures)

	records := published()
	require.Len(t, records, 2, "tool rows and attribution updates are acknowledged without a record")
	byMessage := make(map[string]*otelv1.InboundLogRecord, len(records))
	for _, record := range records {
		byMessage[attributeValue(record, "gram.chat.message.id")] = record
	}
	for _, tc := range []struct {
		event *conversationv1.MessageEvent
		role  string
	}{{event: prompt, role: "user"}, {event: response, role: "assistant"}} {
		record, ok := byMessage[tc.event.GetMessageId()]
		require.True(t, ok, tc.role)
		row := source[uuid.MustParse(tc.event.GetMessageId())].row
		require.Equal(t, EventName, record.GetEventName())
		require.Equal(t, Scope, record.GetScope().GetName())
		require.Equal(t, row.Content, record.GetBody().GetStringValue())
		require.EqualValues(t, testCreatedAt.UnixNano(), record.GetTimeUnixNano())
		require.Equal(t, testOrganizationID, record.GetProvenance().GetOrganizationId())
		require.Equal(t, testProjectID.String(), record.GetProvenance().GetProjectId())
		require.Equal(t, tc.role, attributeValue(record, "gram.chat.role"))
		require.Equal(t, row.ChatID.String(), attributeValue(record, "gram.chat.id"))
		require.Equal(t, row.ExternalChatID.String, attributeValue(record, "gram.session.id"))
		require.Equal(t, chat.MessageTextKey(row.ExternalChatID.String, tc.role, row.Content), attributeValue(record, "gram.chat.message.key"))
		require.Equal(t, "prompt_01", attributeValue(record, "gram.hook.turn_id"))
		require.Equal(t, "claude-sonnet-4-5", attributeValue(record, "gen_ai.response.model"))
		require.Equal(t, "claude-code", attributeValue(record, "gram.hook.source"))
		require.Equal(t, "user-1", attributeValue(record, "user.id"))
		require.Equal(t, "person@example.test", attributeValue(record, "user.email"))
		require.Equal(t, "external-1", attributeValue(record, "gram.external_user.id"))
	}
}

func TestHandlerUsesTheSessionIdEveryLaneAgreesOn(t *testing.T) {
	t.Parallel()
	asserted := event(conversationv1.MessageEvent_ROLE_USER)
	minted := event(conversationv1.MessageEvent_ROLE_USER)
	source := storedMessages{}
	for _, m := range []*conversationv1.MessageEvent{asserted, minted} {
		s := storedMessage(m, "hello")
		s.row.ExternalChatID = conv.ToPGText("anthropic-inference:" + s.row.ChatID.String())
		s.row.MessageID = conv.ToPGTextEmpty("")
		if m == asserted {
			s.row.InferenceSessionID = conv.ToPGText("ses_asserted")
		}
		source[s.row.ID] = s
	}
	publisher, published := capture(t)
	h := newTestHandler(t, source, nil, logsEnabled, publisher)

	messages, failures := batch(asserted, minted)
	require.NoError(t, h.handleBatch(t.Context(), messages))
	requireNoFailures(t, failures)

	sessions := make(map[string]string)
	for _, record := range published() {
		sessions[attributeValue(record, "gram.chat.message.id")] = attributeValue(record, "gram.session.id")
		require.Empty(t, attributeValue(record, "gram.hook.turn_id"), "an empty message id states no turn")
	}
	require.Equal(t, "ses_asserted", sessions[asserted.GetMessageId()])
	require.Equal(t, minted.GetConversationId(), sessions[minted.GetMessageId()], "a minted inference chat id is not a session id; the chat id stands in")
}

func TestHandlerSkipsOrganizationsWithoutLogs(t *testing.T) {
	t.Parallel()
	m := event(conversationv1.MessageEvent_ROLE_USER)
	s := storedMessage(m, "hello")
	publisher, published := capture(t)
	disabled := func(context.Context, string) (bool, error) { return false, nil }
	h := newTestHandler(t, storedMessages{s.row.ID: s}, nil, disabled, publisher)

	messages, failures := batch(m)
	require.NoError(t, h.handleBatch(t.Context(), messages))
	requireNoFailures(t, failures)
	require.Empty(t, published())
}

func TestHandlerFailsTheTenantWhenTheGateOrReadFails(t *testing.T) {
	t.Parallel()
	first, second := event(conversationv1.MessageEvent_ROLE_USER), event(conversationv1.MessageEvent_ROLE_ASSISTANT)
	other := event(conversationv1.MessageEvent_ROLE_USER)
	other.SetOrganizationId("other-organization")
	source := storedMessages{}
	for _, m := range []*conversationv1.MessageEvent{first, second, other} {
		s := storedMessage(m, "hello")
		source[s.row.ID] = s
	}

	t.Run("gate", func(t *testing.T) {
		t.Parallel()
		publisher, published := capture(t)
		gate := func(_ context.Context, organizationID string) (bool, error) {
			if organizationID == testOrganizationID {
				return false, errors.New("features unavailable")
			}
			return true, nil
		}
		h := newTestHandler(t, source, nil, gate, publisher)
		messages, failures := batch(first, second, other)
		require.NoError(t, h.handleBatch(t.Context(), messages))
		require.ErrorContains(t, failures[0], "features unavailable")
		require.ErrorContains(t, failures[1], "features unavailable")
		require.NoError(t, failures[2])
		require.Len(t, published(), 1)
	})

	t.Run("read", func(t *testing.T) {
		t.Parallel()
		publisher, published := capture(t)
		h := newTestHandler(t, failingMessages{err: errors.New("database unavailable")}, nil, logsEnabled, publisher)
		messages, failures := batch(first, second)
		require.NoError(t, h.handleBatch(t.Context(), messages))
		require.ErrorContains(t, failures[0], "database unavailable")
		require.ErrorContains(t, failures[1], "database unavailable")
		require.Empty(t, published())
	})
}

func TestHandlerAcknowledgesWhatCannotBeLogged(t *testing.T) {
	t.Parallel()
	missing := event(conversationv1.MessageEvent_ROLE_USER)
	mismatched := event(conversationv1.MessageEvent_ROLE_USER)
	otherProject := event(conversationv1.MessageEvent_ROLE_USER)
	empty := event(conversationv1.MessageEvent_ROLE_USER)
	framingOnly := event(conversationv1.MessageEvent_ROLE_USER)
	toolRow := event(conversationv1.MessageEvent_ROLE_USER)
	malformed := event(conversationv1.MessageEvent_ROLE_USER)
	malformed.SetMessageId("not-a-uuid")

	source := storedMessages{}
	s := storedMessage(mismatched, "hello")
	s.row.ChatID = uuid.New()
	source[s.row.ID] = s
	s = storedMessage(otherProject, "hello")
	s.projectID = uuid.New()
	source[s.row.ID] = s
	s = storedMessage(empty, "")
	source[s.row.ID] = s
	s = storedMessage(framingOnly, "<system-reminder>context only</system-reminder>")
	source[s.row.ID] = s
	s = storedMessage(toolRow, "hello")
	s.row.Role = "tool"
	source[s.row.ID] = s

	publisher, published := capture(t)
	h := newTestHandler(t, source, nil, logsEnabled, publisher)
	messages, failures := batch(missing, mismatched, otherProject, empty, framingOnly, toolRow, malformed)
	require.NoError(t, h.handleBatch(t.Context(), messages))
	requireNoFailures(t, failures)
	require.Empty(t, published())
}

func TestHandlerRedeliveryPublishesTheSameRecord(t *testing.T) {
	t.Parallel()
	m := event(conversationv1.MessageEvent_ROLE_USER)
	s := storedMessage(m, "hello")
	publisher, published := capture(t)
	h := newTestHandler(t, storedMessages{s.row.ID: s}, nil, logsEnabled, publisher)

	for range 2 {
		messages, failures := batch(m)
		require.NoError(t, h.handleBatch(t.Context(), messages))
		requireNoFailures(t, failures)
	}

	records := published()
	require.Len(t, records, 2)
	require.NotEmpty(t, records[0].GetRecordId())
	require.Equal(t, records[0].GetRecordId(), records[1].GetRecordId())
	for _, record := range records {
		record.SetObservedTimeUnixNano(0)
	}
	require.True(t, proto.Equal(records[0], records[1]), "only the observed time may differ between deliveries")
}

func TestHandlerFailsOnlyTheMessageWhosePublishFails(t *testing.T) {
	t.Parallel()
	failing, fine := event(conversationv1.MessageEvent_ROLE_USER), event(conversationv1.MessageEvent_ROLE_ASSISTANT)
	source := storedMessages{}
	s := storedMessage(failing, "fail me")
	source[s.row.ID] = s
	s = storedMessage(fine, "publish me")
	source[s.row.ID] = s

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.MatchedBy(func(record *otelv1.InboundLogRecord) bool {
		return record.GetBody().GetStringValue() == "fail me"
	})).Return(errors.New("pubsub unavailable")).Once()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Once()
	h := newTestHandler(t, source, nil, logsEnabled, publisher)

	messages, failures := batch(failing, fine)
	require.NoError(t, h.handleBatch(t.Context(), messages))
	require.ErrorContains(t, failures[0], "pubsub unavailable")
	require.NoError(t, failures[1])
	publisher.AssertExpectations(t)
}

func TestHandlerReadsTextFromBlocksThenContentThenAsset(t *testing.T) {
	t.Parallel()
	blocks, projection, spilled := event(conversationv1.MessageEvent_ROLE_USER), event(conversationv1.MessageEvent_ROLE_USER), event(conversationv1.MessageEvent_ROLE_ASSISTANT)
	blobs := assetstest.NewTestBlobStore(t)
	asset := `[{"type":"text","text":"from the asset"}]`
	writer, assetURL, err := blobs.Write(t.Context(), testProjectID.String()+"/spilled.json", "application/json", int64(len(asset)))
	require.NoError(t, err)
	_, err = writer.Write([]byte(asset))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	source := storedMessages{}
	s := storedMessage(blocks, "projection")
	s.row.ContentRaw = []byte(`[{"type":"text","text":"first"},{"type":"text","text":"second"}]`)
	source[s.row.ID] = s
	s = storedMessage(projection, "projection only")
	s.row.ContentRaw = []byte(`[{"type":"tool_use","name":"Read"}]`)
	source[s.row.ID] = s
	s = storedMessage(spilled, "")
	s.row.ContentAssetUrl = conv.ToPGText(assetURL.String())
	source[s.row.ID] = s

	publisher, published := capture(t)
	h := newTestHandler(t, source, blobs, logsEnabled, publisher)
	messages, failures := batch(blocks, projection, spilled)
	require.NoError(t, h.handleBatch(t.Context(), messages))
	requireNoFailures(t, failures)

	bodies := make(map[string]string)
	for _, record := range published() {
		bodies[attributeValue(record, "gram.chat.message.id")] = record.GetBody().GetStringValue()
	}
	require.Equal(t, map[string]string{
		blocks.GetMessageId():     "first\n\nsecond",
		projection.GetMessageId(): "projection only",
		spilled.GetMessageId():    "from the asset",
	}, bodies)
}
