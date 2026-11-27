package transcriptstore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func plaintextReadPoints(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.DataPoint[int64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterPlaintextReads {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			return sum.DataPoints
		}
	}
	return nil
}

func TestPlaintextGuardReportsListedProjectWithoutStore(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	other := uuid.New()

	// No guard: a listed project's read fails rather than serve plaintext.
	_, err := NewReadOnlyTranscripts(f.project).ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.ErrorIs(t, err, ErrPlaintextRead)

	// A project off the list reads plaintext and reports nothing.
	_, err = NewReadOnlyTranscripts(other).ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)

	// Strict (tests and local development): fails and counts.
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	strict, err := NewReadOnlyTranscripts(f.project).WithPlaintextGuard(testenv.NewLogger(t), provider, true)
	require.NoError(t, err)
	_, err = strict.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.ErrorIs(t, err, ErrPlaintextRead)

	// Production: serves plaintext, counts, and logs.
	lenient, err := NewReadOnlyTranscripts(f.project).WithPlaintextGuard(testenv.NewLogger(t), provider, false)
	require.NoError(t, err)
	messages, err := lenient.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	points := plaintextReadPoints(t, reader)
	require.Len(t, points, 1)
	require.Equal(t, int64(2), points[0].Value)
	require.Equal(t, attribute.NewSet(
		attr.ProjectID(f.project.String()),
		attr.TranscriptShadowReader("ListLatestGenerationChatMessages"),
		attr.Outcome(outcomeUnopened),
	), points[0].Attributes)
}

// canary replaces a captured message's plaintext with a marker behind its
// current shadow, so the two disagree. In shadow mode the plaintext row is the
// source of truth: every reader must notice the disagreement, count it as
// stale, and serve the plaintext.
const canary = "CANARY plaintext edited behind its shadow"

// staleReads returns the stale count of chat.transcript_shadow.read.messages.
func staleReads(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	var stale int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterReadMessages {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				if outcome, ok := point.Attributes.Value(attr.OutcomeKey); ok && outcome.AsString() == string(outcomeStale) {
					stale += point.Value
				}
			}
		}
	}
	return stale
}

// meteredTranscripts reads the fixture project's shadows through a store with
// its own metric reader, so parallel subtests count only their own reads.
func meteredTranscripts(t *testing.T, f *fixture) (*Transcripts, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	store, err := New(f.db, f.keys.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), provider)
	require.NoError(t, err)
	return NewTranscripts(store, f.project), reader
}

// requireEveryReaderCompares runs each reader on its own metered store and
// checks it served the plaintext canary and counted the disagreement.
func requireEveryReaderCompares(t *testing.T, f *fixture, readers map[string]func(*Transcripts) (any, error)) {
	t.Helper()
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			transcripts, metrics := meteredTranscripts(t, f)
			result, err := read(transcripts)
			require.NoError(t, err)
			require.Contains(t, strings.ToLower(fmtAny(result)), strings.ToLower(canary), "the plaintext row is served")
			require.Positive(t, staleReads(t, metrics), "the disagreeing shadow is counted as stale")
		})
	}
}

func TestEveryReaderComparesShadowWithPlaintext(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	plantCanary(t, f, id)

	ctx := t.Context()
	readers := map[string]func(*Transcripts) (any, error){
		"ListLatestGenerationChatMessages": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListLatestGenerationChatMessages(ctx, f.db, f.project, f.chat)
		},
		"ListChatMessagesByGeneration": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListChatMessagesByGeneration(ctx, f.db, f.project, f.chat, 0)
		},
		"ListChatMessagesAfterPage": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListChatMessagesAfterPage(ctx, f.db, ChatPage{ProjectID: f.project, ChatID: f.chat, Generation: 0, Cursor: pgtype.Int8{Int64: 0, Valid: false}, Limit: 10})
		},
		"ListChatMessagesBeforePage": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListChatMessagesBeforePage(ctx, f.db, ChatPage{ProjectID: f.project, ChatID: f.chat, Generation: 0, Cursor: pgtype.Int8{Int64: 0, Valid: false}, Limit: 10})
		},
		"ListRiskWindowedMessages": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListRiskWindowedMessages(ctx, f.db, RiskWindow{ProjectID: f.project, ChatID: f.chat, Generation: 0, ContextSize: 2, RiskMessageIDs: []uuid.UUID{id}, RiskContentPartIDs: []uuid.UUID{}})
		},
		"ListSearchWindowedMessages": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListSearchWindowedMessages(ctx, f.db, SearchWindow{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "CANARY", MatchLimit: 10, ContextSize: 2})
		},
		"ListChatMessagesForMatch": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListChatMessagesForMatch(ctx, f.db, f.project, f.chat, 0)
		},
		"GetKitProjectionMessage": func(transcripts *Transcripts) (any, error) {
			return transcripts.GetKitProjectionMessage(ctx, f.db, f.project, id)
		},
		"ListChatTranscriptMessagesPage": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListChatTranscriptMessagesPage(ctx, f.db, TranscriptPage{ProjectID: f.project, ChatID: f.chat, CursorCreatedAt: pgtype.Timestamptz{Valid: false}, CursorSeq: pgtype.Int8{Valid: false}, CursorID: uuid.NullUUID{Valid: false}, Limit: 10})
		},
		"GetMessageContentBatch": func(transcripts *Transcripts) (any, error) {
			return transcripts.GetMessageContentBatch(ctx, f.db, f.project, []uuid.UUID{id})
		},
		"GetChatMessageForUnmask": func(transcripts *Transcripts) (any, error) {
			return transcripts.GetChatMessageForUnmask(ctx, f.db, f.project, id)
		},
		"GetJudgeMessageWindow": func(transcripts *Transcripts) (any, error) {
			return transcripts.GetJudgeMessageWindow(ctx, f.db, JudgeWindowParams{AnchorID: id, ChatID: f.chat, ProjectID: f.project, OrganizationID: f.org})
		},
		"LoadEvaluationMessages": func(transcripts *Transcripts) (any, error) {
			return transcripts.LoadEvaluationMessages(ctx, f.db, LoadEvaluationMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageIds: []uuid.UUID{id}})
		},
	}
	requireEveryReaderCompares(t, f, readers)

	// The same reads for a project off the list serve the plaintext, which
	// proves the canary is really in place.
	plain, err := NewTranscripts(f.store).ListLatestGenerationChatMessages(ctx, f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Equal(t, canary, plain[0].Content)
}

func fmtAny(v any) string { return fmt.Sprintf("%+v", v) }

// plantCanary captures a message, then puts the canary in its plaintext behind
// the shadow, which SQL still treats as current.
func plantCanary(t *testing.T, f *fixture, id uuid.UUID) {
	t.Helper()
	f.capture(t, id)
	_, err := f.db.Exec(t.Context(), `UPDATE chat_messages SET content = $1 WHERE id = $2`, canary, id) //nolint:glint // notestingrawsql: plants the canary in plaintext behind the shadow.
	require.NoError(t, err)
}

func TestEveryOtherReaderComparesShadowWithPlaintext(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	calls, result, prompt, later := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{calls, result, prompt, later} {
		f.add(t, id)
	}
	// Shape the fixtures: a tool result answering the call's "example" ID, a
	// user prompt, an owned chat and a later message to evaluate history for.
	// None of these columns is a body field.
	//nolint:glint // notestingrawsql: fixture columns outside the transcript body.
	_, err := f.db.Exec(t.Context(), `
UPDATE chat_messages SET role = 'tool', tool_call_id = 'example' WHERE id = $1;
`, result)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE chat_messages SET role = 'user' WHERE id = $1`, prompt) //nolint:glint // notestingrawsql: fixture role outside the transcript fingerprint.
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE chat_messages SET created_at = clock_timestamp() + interval '1 hour' WHERE id = $1`, later) //nolint:glint // notestingrawsql: orders the evaluated message after its history.
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE chats SET user_id = 'owner' WHERE id = $1`, f.chat) //nolint:glint // notestingrawsql: makes the fixture chat owned for recall.
	require.NoError(t, err)
	f.capture(t, calls, later)
	for _, id := range []uuid.UUID{result, prompt} {
		plantCanary(t, f, id)
	}

	ctx := t.Context()
	targets, err := NewTranscripts(f.store).LoadEvaluationMessages(ctx, f.db, LoadEvaluationMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageIds: []uuid.UUID{later}})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	target := targets[0]

	readers := map[string]func(*Transcripts) (any, error){
		"GetToolCallSummaryContext": func(transcripts *Transcripts) (any, error) {
			return transcripts.GetToolCallSummaryContext(ctx, f.db, ToolCallSummaryContext{ProjectID: f.project, ChatID: f.chat, MessageID: calls, ToolCallID: "example"})
		},
		"ListClaudeUserMessagesForPromptAttachmentParent": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListClaudeUserMessagesForPromptAttachmentParent(ctx, f.db, f.project, f.chat)
		},
		"SearchPlatformChatMessages": func(transcripts *Transcripts) (any, error) {
			return transcripts.SearchPlatformChatMessages(ctx, f.db, SearchPlatformChatMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org,
				FromTime: pgtype.Timestamptz{Time: target.CreatedAt.Time.Add(-48 * 3600e9), InfinityModifier: pgtype.Finite, Valid: true}, ToTime: pgtype.Timestamptz{Time: target.CreatedAt.Time.Add(48 * 3600e9), InfinityModifier: pgtype.Finite, Valid: true},
				ChatIds: []uuid.UUID{}, OwnUserID: "", OwnedAgentIds: []uuid.UUID{}, OwnerUserID: "", Source: "", Pattern: "", ParticipantID: "", ParticipantProvider: "",
				HasCursor: false, OldestFirst: true, CursorAt: pgtype.Timestamptz{Valid: false}, CursorID: uuid.Nil, RowLimit: 10})
		},
		"ReadPlatformChatMessages": func(transcripts *Transcripts) (any, error) {
			return transcripts.ReadPlatformChatMessages(ctx, f.db, ReadPlatformChatMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageID: uuid.NullUUID{Valid: false}, ChatID: f.chat, ContextSize: 10, FollowingLimit: 10})
		},
		"ListOwnedChatTranscriptMessagesForRecall": func(transcripts *Transcripts) (any, error) {
			return transcripts.ListOwnedChatTranscriptMessagesForRecall(ctx, f.db, ListOwnedChatTranscriptMessagesForRecallParams{OpenShadows: false, ChatID: f.chat, ProjectID: project(f.project), OrganizationID: f.org, UserID: "owner", RowLimit: 10})
		},
		"LoadEvaluationHistory": func(transcripts *Transcripts) (any, error) {
			return transcripts.LoadEvaluationHistory(ctx, f.db, LoadEvaluationHistoryParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, ChatID: f.chat, Generation: target.Generation, TargetID: target.ID, TargetCreatedAt: target.CreatedAt, TargetSeq: target.Seq, HistoryLimit: 10, ContentBudget: 1 << 20})
		},
	}
	requireEveryReaderCompares(t, f, readers)
}
