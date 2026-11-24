package activities_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const shadowReadMessagesMetric = "chat.transcript_shadow.read.messages"

// titleShadowStore is a transcript shadow store for one test chat's
// organization, with a reader that collects its metrics.
type titleShadowStore struct {
	store  *transcriptstore.Store
	reader *sdkmetric.ManualReader
}

func newTitleShadowStore(t *testing.T, tc titleTestChat) titleShadowStore {
	t.Helper()

	keys := orgencryptiontest.New(t, tc.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), tc.orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	reader := sdkmetric.NewManualReader()
	store, err := transcriptstore.New(tc.conn, keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)
	return titleShadowStore{store: store, reader: reader}
}

// capture seals shadows of messages already written, as the chat writer does
// inside its message transaction.
func (s titleShadowStore) capture(t *testing.T, tc titleTestChat, messageIDs ...uuid.UUID) {
	t.Helper()

	tx, err := tc.conn.Begin(t.Context()) //nolint:glint // notestingrawsql: Capture runs inside the caller's message transaction; message rows come from generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, s.store.Capture(t.Context(), tx, tc.orgID, tc.projectID, messageIDs))
	require.NoError(t, tx.Commit(t.Context()))
}

// readMessages counts the messages read through shadows, by outcome.
func (s titleShadowStore) readMessages(t *testing.T) map[string]int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, s.reader.Collect(t.Context(), &collected))
	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != shadowReadMessagesMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				outcome, ok := point.Attributes.Value(attr.OutcomeKey)
				require.True(t, ok)
				counts[outcome.AsString()] += point.Value
			}
		}
	}
	return counts
}

func (c titleTestChat) addMessageID(t *testing.T, role, content string) uuid.UUID {
	t.Helper()

	id, err := c.repo.CreateChatMessageReturningID(t.Context(), chatrepo.CreateChatMessageReturningIDParams{
		ChatID: c.chatID, ProjectID: uuid.NullUUID{UUID: c.projectID, Valid: true}, Role: role, Content: content,
		ToolCalls: nil, ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)
	return id
}

func (c titleTestChat) runWithShadow(t *testing.T, client *titleCompletionStub, store *transcriptstore.Store, readProjectIDs ...uuid.UUID) error {
	t.Helper()

	act := activities.NewGenerateChatTitle(testenv.NewLogger(t), c.conn, client).WithTranscriptShadowReads(store, readProjectIDs...)
	return act.Do(t.Context(), activities.GenerateChatTitleArgs{
		ChatID: c.chatID.String(), OrgID: c.orgID, ProjectID: c.projectID.String(),
	})
}

func TestGenerateChatTitle_ReadEnabledProjectReadsEncryptedShadows(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_shadow", chat.DefaultChatTitle)
	question := tc.addMessageID(t, "user", "How do I rotate the signing key for the staging webhook?")
	answer := tc.addMessageID(t, "assistant", "Create a new key, deploy it alongside the old one, then retire the old key.")
	shadows := newTitleShadowStore(t, tc)
	shadows.capture(t, tc, question, answer)

	stub := &titleCompletionStub{title: "Webhook Signing Key Rotation"}
	require.NoError(t, tc.runWithShadow(t, stub, shadows.store, tc.projectID))

	require.Equal(t, "Webhook Signing Key Rotation", tc.get(t).Title.String)
	require.Equal(t, "user: How do I rotate the signing key for the staging webhook?\n"+
		"assistant: Create a new key, deploy it alongside the old one, then retire the old key.", *stub.prompt.Load())
	require.Equal(t, map[string]int64{"success": 2}, shadows.readMessages(t))
}

func TestGenerateChatTitle_ReadEnabledProjectServesPlaintextWhenNoShadowExists(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_shadow_fallback", chat.DefaultChatTitle)
	tc.addMessageID(t, "user", "Which regions does the nightly export job run in?")
	answer := tc.addMessageID(t, "assistant", "It runs in two regions and writes to the shared archive bucket.")
	shadows := newTitleShadowStore(t, tc)
	// The question was written before capture began, so it has no shadow.
	shadows.capture(t, tc, answer)

	stub := &titleCompletionStub{title: "Nightly Export Regions"}
	require.NoError(t, tc.runWithShadow(t, stub, shadows.store, tc.projectID))

	require.Equal(t, "Nightly Export Regions", tc.get(t).Title.String)
	require.Equal(t, "user: Which regions does the nightly export job run in?\n"+
		"assistant: It runs in two regions and writes to the shared archive bucket.", *stub.prompt.Load())
	require.Equal(t, map[string]int64{"success": 1, "plaintext_fallback": 1}, shadows.readMessages(t))
}

func TestGenerateChatTitle_ReadEnabledProjectFailsOnAStaleShadow(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_shadow_stale", chat.DefaultChatTitle)
	question := tc.addMessageID(t, "user", "Why does the release checklist skip the smoke tests?")
	answer := tc.addMessageID(t, "assistant", "The smoke tests moved into the deploy pipeline last quarter.")
	shadows := newTitleShadowStore(t, tc)
	shadows.capture(t, tc, question, answer)
	// A write that skips capture leaves the shadow behind the plaintext.
	changed, err := fixturerepo.New(tc.conn).ChangeContent(t.Context(), answer, uuid.NullUUID{UUID: tc.projectID, Valid: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), changed)

	stub := &titleCompletionStub{title: "Release Checklist Smoke Tests"}
	err = tc.runWithShadow(t, stub, shadows.store, tc.projectID)

	require.ErrorIs(t, err, transcriptstore.ErrStale)
	// A stale shadow stays stale, so Temporal must not retry the activity.
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
	// The title is never generated from the plaintext the shadow disagrees with.
	require.Zero(t, stub.calls.Load())
	require.Equal(t, chat.DefaultChatTitle, tc.get(t).Title.String)
	require.Equal(t, map[string]int64{"stale": 2}, shadows.readMessages(t))
}

func TestGenerateChatTitle_OtherProjectsKeepReadingPlaintext(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_shadow_other", chat.DefaultChatTitle)
	question := tc.addMessageID(t, "user", "Can we pin the build image to a fixed digest?")
	answer := tc.addMessageID(t, "assistant", "Yes, reference the image by digest in the build config.")
	shadows := newTitleShadowStore(t, tc)
	shadows.capture(t, tc, question, answer)
	// Even a stale shadow is ignored where the project does not read shadows.
	_, err := fixturerepo.New(tc.conn).ChangeContent(t.Context(), answer, uuid.NullUUID{UUID: tc.projectID, Valid: true})
	require.NoError(t, err)

	stub := &titleCompletionStub{title: "Pin Build Image Digest"}
	require.NoError(t, tc.runWithShadow(t, stub, shadows.store, uuid.New()))

	require.Equal(t, "Pin Build Image Digest", tc.get(t).Title.String)
	require.Equal(t, "user: Can we pin the build image to a fixed digest?\nassistant: changed", *stub.prompt.Load())
	require.Empty(t, shadows.readMessages(t))
}

// Every later turn of a titled chat schedules title generation; it must leave
// the title alone without opening the transcript, so even a stale shadow does
// not fail the run.
func TestGenerateChatTitle_TitledChatOpensNoShadows(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_shadow_titled", "Warehouse Label Printer Fix")
	question := tc.addMessageID(t, "user", "The label printer in the warehouse keeps skipping every other label.")
	answer := tc.addMessageID(t, "assistant", "Recalibrate the gap sensor, then reload the roll with the labels facing out.")
	shadows := newTitleShadowStore(t, tc)
	shadows.capture(t, tc, question, answer)
	_, err := fixturerepo.New(tc.conn).ChangeContent(t.Context(), answer, uuid.NullUUID{UUID: tc.projectID, Valid: true})
	require.NoError(t, err)

	stub := &titleCompletionStub{title: "Unused"}
	require.NoError(t, tc.runWithShadow(t, stub, shadows.store, tc.projectID))

	require.Equal(t, "Warehouse Label Printer Fix", tc.get(t).Title.String)
	require.Zero(t, stub.calls.Load())
	require.Empty(t, shadows.readMessages(t))
}

// Only the turns sent to the model are opened, not the whole transcript.
func TestGenerateChatTitle_OpensOnlyTheTurnsItSends(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_shadow_turns", chat.DefaultChatTitle)
	ids := make([]uuid.UUID, 0, 10)
	for i := range 5 {
		ids = append(ids, tc.addMessageID(t, "user", fmt.Sprintf("Question %d about rotating the staging database password.", i)))
		ids = append(ids, tc.addMessageID(t, "assistant", fmt.Sprintf("Answer %d: rotate it in the secret store, then restart the app.", i)))
	}
	shadows := newTitleShadowStore(t, tc)
	shadows.capture(t, tc, ids...)

	stub := &titleCompletionStub{title: "Staging Password Rotation"}
	require.NoError(t, tc.runWithShadow(t, stub, shadows.store, tc.projectID))

	require.Equal(t, "Staging Password Rotation", tc.get(t).Title.String)
	require.Equal(t, map[string]int64{"success": 6}, shadows.readMessages(t))
}
