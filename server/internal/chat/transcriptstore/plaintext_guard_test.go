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
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
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

// canary replaces a captured message's plaintext with a marker and forges the
// shadow's source hash to match it, so the SQL freshness check passes. Every
// reader must still refuse to serve the marker.
const canary = "CANARY plaintext that must never be served"

func TestNoReaderServesForgedPlaintext(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	_, err := f.db.Exec(t.Context(), `UPDATE chat_messages SET content = $1 WHERE id = $2`, canary, id) //nolint:glint // notestingrawsql: plants the canary in plaintext behind the shadow.
	require.NoError(t, err)
	n, err := fixturerepo.New(f.db).ForgeSourceHash(t.Context(), id, project(f.project), f.org)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	transcripts := NewTranscripts(f.store, f.project)
	ctx := t.Context()
	readers := map[string]func() (any, error){
		"ListLatestGenerationChatMessages": func() (any, error) {
			return transcripts.ListLatestGenerationChatMessages(ctx, f.db, f.project, f.chat)
		},
		"ListChatMessagesByGeneration": func() (any, error) {
			return transcripts.ListChatMessagesByGeneration(ctx, f.db, f.project, f.chat, 0)
		},
		"ListChatMessagesAfterPage": func() (any, error) {
			return transcripts.ListChatMessagesAfterPage(ctx, f.db, ChatPage{ProjectID: f.project, ChatID: f.chat, Generation: 0, Cursor: pgtype.Int8{Int64: 0, Valid: false}, Limit: 10})
		},
		"ListChatMessagesBeforePage": func() (any, error) {
			return transcripts.ListChatMessagesBeforePage(ctx, f.db, ChatPage{ProjectID: f.project, ChatID: f.chat, Generation: 0, Cursor: pgtype.Int8{Int64: 0, Valid: false}, Limit: 10})
		},
		"ListRiskWindowedMessages": func() (any, error) {
			return transcripts.ListRiskWindowedMessages(ctx, f.db, RiskWindow{ProjectID: f.project, ChatID: f.chat, Generation: 0, ContextSize: 2, RiskMessageIDs: []uuid.UUID{id}, RiskContentPartIDs: []uuid.UUID{}})
		},
		"ListSearchWindowedMessages": func() (any, error) {
			return transcripts.ListSearchWindowedMessages(ctx, f.db, SearchWindow{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "CANARY", MatchLimit: 10, ContextSize: 2})
		},
		"ListChatMessagesForMatch": func() (any, error) {
			return transcripts.ListChatMessagesForMatch(ctx, f.db, f.project, f.chat, 0)
		},
		"GetKitProjectionMessage": func() (any, error) {
			return transcripts.GetKitProjectionMessage(ctx, f.db, f.project, id)
		},
		"ListChatTranscriptMessagesPage": func() (any, error) {
			return transcripts.ListChatTranscriptMessagesPage(ctx, f.db, TranscriptPage{ProjectID: f.project, ChatID: f.chat, CursorCreatedAt: pgtype.Timestamptz{Valid: false}, CursorSeq: pgtype.Int8{Valid: false}, CursorID: uuid.NullUUID{Valid: false}, Limit: 10})
		},
		"GetMessageContentBatch": func() (any, error) {
			return transcripts.GetMessageContentBatch(ctx, f.db, f.project, []uuid.UUID{id})
		},
		"GetChatMessageForUnmask": func() (any, error) {
			return transcripts.GetChatMessageForUnmask(ctx, f.db, f.project, id)
		},
		"GetJudgeMessageWindow": func() (any, error) {
			return transcripts.GetJudgeMessageWindow(ctx, f.db, JudgeWindowParams{AnchorID: id, ChatID: f.chat, ProjectID: f.project, OrganizationID: f.org})
		},
		"LoadEvaluationMessages": func() (any, error) {
			return transcripts.LoadEvaluationMessages(ctx, f.db, LoadEvaluationMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageIds: []uuid.UUID{id}})
		},
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := read()
			require.ErrorIs(t, err, ErrStale, "a forged source hash must not let plaintext through")
			require.NotContains(t, strings.ToLower(fmtAny(result)), strings.ToLower(canary))
		})
	}

	// The same reads for a project off the list serve the plaintext, which
	// proves the canary is really in place.
	plain, err := NewTranscripts(f.store).ListLatestGenerationChatMessages(ctx, f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Equal(t, canary, plain[0].Content)
}

func fmtAny(v any) string { return fmt.Sprintf("%+v", v) }
