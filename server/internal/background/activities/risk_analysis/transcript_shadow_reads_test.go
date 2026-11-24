package risk_analysis_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.temporal.io/sdk/testsuite"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const (
	shadowPriorUserRequest = "please tidy the build directory"
	shadowRecentToolResult = "build directory listing: out/, cache/"
)

// shadowTrajectory is a scanned write tool call plus the user request and tool
// result that precede it, which the batch carries as context.
type shadowTrajectory struct {
	td        testData
	userID    uuid.UUID
	toolID    uuid.UUID
	currentID uuid.UUID
}

func seedShadowTrajectory(t *testing.T, conn *pgxpool.Pool) shadowTrajectory {
	t.Helper()
	td := seedTestData(t, conn, true)
	queries := testrepo.New(conn)
	insert := func(role, content string) uuid.UUID {
		id, err := queries.InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
			ChatID:    td.chatID,
			ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
			Role:      role,
			Content:   content,
		})
		require.NoError(t, err)
		return id
	}
	userID := insert("user", shadowPriorUserRequest)
	toolID := insert("tool", shadowRecentToolResult)
	// A write tool call keeps the message in the recommended PI scope.
	currentID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "rm -rf out/"})

	// Pin conversation order rather than relying on clock agreement.
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []uuid.UUID{userID, toolID, currentID} {
		err := queries.UpdateChatMessageCreatedAt(t.Context(), testrepo.UpdateChatMessageCreatedAtParams{
			CreatedAt: pgtype.Timestamptz{Time: base.Add(time.Duration(i) * time.Second), Valid: true},
			ID:        id,
		})
		require.NoError(t, err)
	}
	return shadowTrajectory{td: td, userID: userID, toolID: toolID, currentID: currentID}
}

// newShadowStore provisions the organization's transcript key and returns a
// store whose read metrics land in the returned reader.
func newShadowStore(t *testing.T, conn *pgxpool.Pool, orgID string) (*transcriptstore.Store, *sdkmetric.ManualReader) {
	t.Helper()
	keys := orgencryptiontest.New(t, conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	reader := sdkmetric.NewManualReader()
	store, err := transcriptstore.New(conn, keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)
	return store, reader
}

func captureShadows(t *testing.T, conn *pgxpool.Pool, store *transcriptstore.Store, td testData, messageIDs ...uuid.UUID) {
	t.Helper()
	tx, err := conn.Begin(t.Context()) //nolint:glint // notestingrawsql: Capture runs inside the caller's message transaction; message rows come from generated seed queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, store.Capture(t.Context(), tx, td.orgID, td.projectID, messageIDs))
	require.NoError(t, tx.Commit(t.Context()))
}

// shadowReadMessages sums chat.transcript_shadow.read.messages by outcome.
func shadowReadMessages(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "chat.transcript_shadow.read.messages" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range data.DataPoints {
				outcome, ok := point.Attributes.Value(attr.OutcomeKey)
				require.True(t, ok)
				if point.Value != 0 {
					counts[outcome.AsString()] += point.Value
				}
			}
		}
	}
	return counts
}

func shadowReadingAnalyzer(t *testing.T, conn *pgxpool.Pool, store *transcriptstore.Store, readProjects ...uuid.UUID) (*risk_analysis.AnalyzeBatch, *[]*riskv1.PromptInjectionAnalysis) {
	t.Helper()
	promptInjectionPub, published := capturingPromptInjectionPub(t)
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		&feature.InMemory{},
		newPresidioPub(),
		newGitleaksPub(),
		promptInjectionPub,
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)
	return ab.WithTranscriptShadowReads(store, readProjects...), published
}

func executeShadowBatch(t *testing.T, ab *risk_analysis.AnalyzeBatch, tr shadowTrajectory) error {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)
	_, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        tr.td.projectID,
		OrganizationID:   tr.td.orgID,
		RiskPolicyID:     tr.td.policyID,
		PolicyVersion:    tr.td.policyVersion,
		MessageIDs:       []uuid.UUID{tr.currentID},
		Sources:          []string{risk_analysis.SourcePromptInjection},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	if err != nil {
		return fmt.Errorf("execute analyze batch: %w", err)
	}
	return nil
}

func requirePublishedTrajectory(t *testing.T, published []*riskv1.PromptInjectionAnalysis, tr shadowTrajectory) {
	t.Helper()
	require.Len(t, published, 1)
	require.Equal(t, tr.currentID.String(), published[0].GetChatMessageId())
	require.Equal(t, shadowPriorUserRequest, published[0].GetPriorUserRequest())
	require.Equal(t, shadowRecentToolResult, published[0].GetRecentUntrustedContent())
	// The scanned tool call's arguments come from the batch row's body.
	require.Equal(t, "Bash", published[0].GetToolName())
	require.Contains(t, published[0].GetBody(), "rm -rf out/")
}

func TestAnalyzeBatch_ReadEnabledProjectScansEncryptedShadows(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	tr := seedShadowTrajectory(t, conn)
	store, reader := newShadowStore(t, conn, tr.td.orgID)
	captureShadows(t, conn, store, tr.td, tr.userID, tr.toolID, tr.currentID)

	ab, published := shadowReadingAnalyzer(t, conn, store, tr.td.projectID)
	require.NoError(t, executeShadowBatch(t, ab, tr))

	requirePublishedTrajectory(t, *published, tr)
	// The scanned message and both context messages open in one read.
	require.Equal(t, map[string]int64{"success": 3}, shadowReadMessages(t, reader))
}

func TestAnalyzeBatch_ReadEnabledProjectScansPlaintextWhenNoShadowExists(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	tr := seedShadowTrajectory(t, conn)
	store, reader := newShadowStore(t, conn, tr.td.orgID)
	// The context messages predate capture and have no shadow.
	captureShadows(t, conn, store, tr.td, tr.currentID)

	ab, published := shadowReadingAnalyzer(t, conn, store, tr.td.projectID)
	require.NoError(t, executeShadowBatch(t, ab, tr))

	requirePublishedTrajectory(t, *published, tr)
	require.Equal(t, map[string]int64{"success": 1, "plaintext_fallback": 2}, shadowReadMessages(t, reader))
}

func TestAnalyzeBatch_ReadEnabledProjectFailsOnAStaleShadow(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	tr := seedShadowTrajectory(t, conn)
	store, reader := newShadowStore(t, conn, tr.td.orgID)
	captureShadows(t, conn, store, tr.td, tr.userID, tr.toolID, tr.currentID)
	// A write that skips capture leaves the context message's shadow stale.
	_, err := fixturerepo.New(conn).ChangeContent(t.Context(), tr.userID, uuid.NullUUID{UUID: tr.td.projectID, Valid: true})
	require.NoError(t, err)

	ab, published := shadowReadingAnalyzer(t, conn, store, tr.td.projectID)
	err = executeShadowBatch(t, ab, tr)
	require.Error(t, err)
	require.ErrorContains(t, err, transcriptstore.ErrStale.Error())

	// An existing shadow that fails verification never falls back to plaintext.
	require.Empty(t, *published)
	require.Equal(t, map[string]int64{"stale": 3}, shadowReadMessages(t, reader))
}

func TestAnalyzeBatch_OtherProjectsKeepScanningPlaintext(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	tr := seedShadowTrajectory(t, conn)
	store, reader := newShadowStore(t, conn, tr.td.orgID)

	// The store reads only for another project, so this one never opens one.
	ab, published := shadowReadingAnalyzer(t, conn, store, uuid.New())
	require.NoError(t, executeShadowBatch(t, ab, tr))

	requirePublishedTrajectory(t, *published, tr)
	require.Empty(t, shadowReadMessages(t, reader))
}
