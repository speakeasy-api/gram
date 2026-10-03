package risk_analysis_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.temporal.io/sdk/testsuite"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func newLLMPub() *gcp.MockPublisher[*riskv1.LLMAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.LLMAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

// capturingPub records every message published through the returned mock so
// tests can assert exactly which lanes a batch dispatched to.
func capturingPub[M any](t *testing.T) (*gcp.MockPublisher[M], *[]M) {
	t.Helper()
	pub := gcp.NewMockPublisher[M]()
	var published []M
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			msg, ok := args.Get(1).(M)
			require.True(t, ok)
			published = append(published, msg)
		})
	return pub, &published
}

// countingPIIScanner records how many times the inline Presidio scan ran and
// returns no findings.
type countingPIIScanner struct {
	calls atomic.Int32
}

func (s *countingPIIScanner) AnalyzeBatch(_ context.Context, texts []string, _ []string, _ float64, _ func()) ([]scanners.Result, error) {
	s.calls.Add(1)
	return make([]scanners.Result, len(texts)), nil
}

type llmLanePublishers struct {
	llm      *[]*riskv1.LLMAnalysis
	gitleaks *[]*riskv1.GitleaksAnalysis
	presidio *[]*riskv1.PresidioAnalysis
}

// runLLMLaneBatch executes one AnalyzeBatch over messageIDs with the given
// flag provider, capturing what reached each analysis lane. analyzerEnabled
// is what the worker knows about GRAM_RISK_LLM_URL; llmPub overrides the
// captured LLM publisher when non-nil.
func runLLMLaneBatch(t *testing.T, conn *pgxpool.Pool, td testData, flags feature.Provider, analyzerEnabled bool, piiScanner risk_analysis.PIIScanner, llmPub gcp.Publisher[*riskv1.LLMAnalysis], messageIDs []uuid.UUID, sources []string) (risk_analysis.AnalyzeBatchResult, llmLanePublishers, error) {
	t.Helper()
	return runLLMLaneBatchWithMeter(t, conn, td, flags, analyzerEnabled, testenv.NewMeterProvider(t), piiScanner, llmPub, messageIDs, sources)
}

// newManualMeter returns a meter provider whose counters the test reads back
// through the returned reader.
func newManualMeter(t *testing.T) (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, reader
}

// llmPolicyEvaluations sums risk.llm.policy_evaluations for the async scan
// mode and the given outcome.
func llmPolicyEvaluations(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	want := []attribute.KeyValue{attr.RiskScanMode(llmanalyzer.ScanModeAsync), attr.Outcome(outcome)}
	var total int64
	for _, scope := range data.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "risk.llm.policy_evaluations" {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				matched := true
				for _, kv := range want {
					if got, ok := dp.Attributes.Value(kv.Key); !ok || got != kv.Value {
						matched = false
						break
					}
				}
				if matched {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func runLLMLaneBatchWithMeter(t *testing.T, conn *pgxpool.Pool, td testData, flags feature.Provider, analyzerEnabled bool, meterProvider metric.MeterProvider, piiScanner risk_analysis.PIIScanner, llmPub gcp.Publisher[*riskv1.LLMAnalysis], messageIDs []uuid.UUID, sources []string) (risk_analysis.AnalyzeBatchResult, llmLanePublishers, error) {
	t.Helper()
	capturedLLMPub, llmPublished := capturingPub[*riskv1.LLMAnalysis](t)
	gitleaksPub, gitleaksPublished := capturingPub[*riskv1.GitleaksAnalysis](t)
	presidioPub, presidioPublished := capturingPub[*riskv1.PresidioAnalysis](t)
	if llmPub == nil {
		llmPub = capturedLLMPub
	}

	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		meterProvider,
		conn,
		nil,
		piiScanner,
		nil,
		nil,
		nil,
		flags,
		presidioPub,
		gitleaksPub,
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		newCustomRulesPub(),
		llmPub,
		newFindingsPub(),
		mustCustomRuleScanner(t, conn),
		mustCELEngine(t),
		nil,
		nil,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		analyzerEnabled,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       messageIDs,
		ContentPartIDs:   nil,
		Sources:          sources,
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	pubs := llmLanePublishers{llm: llmPublished, gitleaks: gitleaksPublished, presidio: presidioPublished}
	if err != nil {
		return risk_analysis.AnalyzeBatchResult{}, pubs, fmt.Errorf("execute analyze batch: %w", err)
	}
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	return result, pubs, nil
}

func insertUserMessage(t *testing.T, conn *pgxpool.Pool, td testData, content string) uuid.UUID {
	t.Helper()
	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   content,
	})
	require.NoError(t, err)
	return msgID
}

func TestAnalyzeBatch_LLMAnalyzer_FlagOffKeepsLegacyEngines(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	pii := &countingPIIScanner{}
	result, pubs, err := runLLMLaneBatch(t, conn, td, &feature.InMemory{}, true, pii, nil, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 1, result.Findings, "inline gitleaks still scans when the flag is off")
	assert.Empty(t, *pubs.llm, "no LLM analysis request without the flag")
	assert.Len(t, *pubs.gitleaks, 1)
	assert.Len(t, *pubs.presidio, 1)
	assert.Equal(t, int32(1), pii.calls.Load())
}

func TestAnalyzeBatch_LLMAnalyzer_FlagOnRoutesCoveredSources(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	td = seedCustomRulePolicySelection(t, conn, td, "custom.acme_token", `content.matchRegex("ACME-[A-Z0-9]{8}")`)

	// The first message would match inline gitleaks; under the flag only the
	// custom rule may produce a finding for it.
	first := insertUserMessage(t, conn, td, "deploy ACME-ABC12345 with AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")
	second := insertUserMessage(t, conn, td, "nothing to see here")

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, td.orgID, true)
	pii := &countingPIIScanner{}
	sources := []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio, risk_analysis.SourceCustom}
	result, pubs, err := runLLMLaneBatch(t, conn, td, flags, true, pii, nil, []uuid.UUID{first, second}, sources)
	require.NoError(t, err)

	assert.Equal(t, 2, result.Processed)
	assert.Equal(t, 1, result.Findings, "only the custom rule scans inline under the flag")
	assert.Empty(t, *pubs.gitleaks, "legacy gitleaks lane is not dispatched under the flag")
	assert.Empty(t, *pubs.presidio, "legacy presidio lane is not dispatched under the flag")
	assert.Equal(t, int32(0), pii.calls.Load(), "inline presidio does not run under the flag")

	require.Len(t, *pubs.llm, 2, "exactly one LLM analysis request per message")
	byMessage := map[string]*riskv1.LLMAnalysis{}
	for _, req := range *pubs.llm {
		byMessage[req.GetChatMessageId()] = req
	}
	require.Contains(t, byMessage, first.String())
	require.Contains(t, byMessage, second.String())
	for _, req := range *pubs.llm {
		assert.Equal(t, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio}, req.GetSources())
		assert.Equal(t, "llm_analyzer_stream", req.GetExecutionPath())
		assert.Equal(t, td.orgID, req.GetOrganizationId())
		assert.Equal(t, td.orgID, req.GetOrganizationSlug())
		assert.Equal(t, td.projectID.String(), req.GetProjectId())
		assert.Equal(t, td.policyID.String(), req.GetRiskPolicyId())
		assert.Equal(t, td.policyVersion, req.GetRiskPolicyVersion())
		assert.Equal(t, td.policyID.String(), req.GetOriginRiskPolicyId())
		assert.Equal(t, td.policyVersion, req.GetOriginRiskPolicyVersion())
		assert.Equal(t, td.chatID.String(), req.GetChatId())
		assert.Equal(t, message.User, req.GetMessageType())
		assert.NotEmpty(t, req.GetRequestId())
		assert.NotEmpty(t, req.GetCreatedAt())
		assert.Empty(t, req.GetMessageLinkReason())
		assert.Empty(t, req.GetToolCalls())
		assert.False(t, req.GetContentTruncated())
	}
	assert.Equal(t, "nothing to see here", byMessage[second.String()].GetBody())
	assert.Equal(t, "nothing to see here", byMessage[second.String()].GetContent())
	assert.Contains(t, byMessage[first.String()].GetBody(), "ASIAZ2XY3WNBQR5TUVWX", "the covered-source text reaches the LLM lane")
	assert.Contains(t, byMessage[first.String()].GetContent(), "ASIAZ2XY3WNBQR5TUVWX")

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "custom rule findings still land in Postgres; LLM findings are ClickHouse-only")
	assert.Equal(t, risk_analysis.SourceCustom, rows[0].Source)
	assert.Equal(t, "custom.acme_token", rows[0].RuleID.String)
}

func TestAnalyzeBatch_LLMAnalyzer_ToolCallMessageCarriesToolCalls(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "rm -rf /tmp/build"})

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, td.orgID, true)
	result, pubs, err := runLLMLaneBatch(t, conn, td, flags, true, &countingPIIScanner{}, nil, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive, risk_analysis.SourceGitleaks})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 0, result.Findings, "cli_destructive does not scan inline under the flag")
	require.Len(t, *pubs.llm, 1)
	req := (*pubs.llm)[0]
	assert.Equal(t, []string{risk_analysis.SourceGitleaks, risk_analysis.SourceCLIDestructive}, req.GetSources())
	assert.Equal(t, message.ToolRequest, req.GetMessageType())
	assert.Empty(t, req.GetBody())
	assert.Equal(t, "Bash", req.GetToolName())
	assert.Equal(t, "call_1", req.GetToolCallId())
	require.Len(t, req.GetToolCalls(), 1)
	assert.Equal(t, "call_1", req.GetToolCalls()[0].GetId())
	assert.Equal(t, "Bash", req.GetToolCalls()[0].GetName())
	assert.Contains(t, req.GetToolCalls()[0].GetArguments(), "rm -rf /tmp/build")
	assert.Contains(t, req.GetContent(), "rm -rf /tmp/build", "content keeps the legacy scan surface for parity")
}

func TestAnalyzeBatch_LLMAnalyzer_PublishFailureFailsActivity(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "hello")

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, td.orgID, true)
	failing := gcp.NewMockPublisher[*riskv1.LLMAnalysis]()
	failing.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("topic unavailable")))

	_, pubs, err := runLLMLaneBatch(t, conn, td, flags, true, &countingPIIScanner{}, failing, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm analyzer scan dispatch")
	assert.Contains(t, err.Error(), "topic unavailable")
	assert.Empty(t, *pubs.gitleaks)
}

func TestAnalyzeBatch_LLMAnalyzer_FlagOnAnalyzerDisabledFallsBackToLegacyEngines(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	td = seedCustomRulePolicySelection(t, conn, td, "custom.acme_token", `content.matchRegex("ACME-[A-Z0-9]{8}")`)
	msgID := insertUserMessage(t, conn, td, "deploy ACME-ABC12345 with AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	// The organization is on the flag, but this worker has no analyzer
	// configured (GRAM_RISK_LLM_URL empty): the streams consumer would ack
	// every request without findings, so the batch must keep the legacy
	// engines rather than leave the covered sources unscanned.
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, td.orgID, true)
	pii := &countingPIIScanner{}
	sources := []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio, risk_analysis.SourceCustom}
	result, pubs, err := runLLMLaneBatch(t, conn, td, flags, false, pii, nil, []uuid.UUID{msgID}, sources)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 2, result.Findings, "inline gitleaks and the custom rule both scan when the analyzer is unconfigured")
	assert.Empty(t, *pubs.llm, "no LLM analysis request without an analyzer to serve it")
	assert.Len(t, *pubs.gitleaks, 1, "legacy gitleaks lane is dispatched")
	assert.Len(t, *pubs.presidio, 1, "legacy presidio lane is dispatched")
	assert.Equal(t, int32(1), pii.calls.Load(), "inline presidio runs")

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2, "legacy and custom rule findings land in Postgres")
	sourcesSeen := map[string]bool{}
	for _, row := range rows {
		sourcesSeen[row.Source] = true
	}
	assert.True(t, sourcesSeen[risk_analysis.SourceGitleaks], "gitleaks finding is stored")
	assert.True(t, sourcesSeen[risk_analysis.SourceCustom], "custom rule finding is stored")
}

// TestAnalyzeBatch_LLMAnalyzer_ShadowModeRunsLegacyAndPublishesShadowLane
// pins the batch half of the shadow engine mode: the legacy engines scan and
// publish exactly as in the off mode, and the same messages are additionally
// published to the LLM lane marked shadow.
func TestAnalyzeBatch_LLMAnalyzer_ShadowModeRunsLegacyAndPublishesShadowLane(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMShadow)
	pii := &countingPIIScanner{}
	meterProvider, reader := newManualMeter(t)
	result, pubs, err := runLLMLaneBatchWithMeter(t, conn, td, flags, true, meterProvider, pii, nil, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 1, result.Findings, "inline gitleaks still scans and enforces under shadow")
	assert.Len(t, *pubs.gitleaks, 1, "the legacy gitleaks lane is still dispatched")
	assert.Len(t, *pubs.presidio, 1, "the legacy presidio lane is still dispatched")
	assert.Equal(t, int32(1), pii.calls.Load(), "inline presidio still runs")

	require.Len(t, *pubs.llm, 1, "one shadow LLM analysis request per message")
	req := (*pubs.llm)[0]
	assert.True(t, req.GetShadow(), "the request is marked shadow")
	assert.Equal(t, "llm_shadow_stream", req.GetExecutionPath())
	assert.Equal(t, msgID.String(), req.GetChatMessageId())
	assert.Equal(t, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio}, req.GetSources())
	assert.Equal(t, td.orgID, req.GetOrganizationSlug())
	assert.Contains(t, req.GetBody(), "ASIAZ2XY3WNBQR5TUVWX")

	assert.Equal(t, int64(1), llmPolicyEvaluations(t, reader, "shadow_published"))
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "published"))
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "fallback_legacy"))

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the legacy gitleaks finding still lands in Postgres")
	assert.Equal(t, risk_analysis.SourceGitleaks, rows[0].Source)
}

func TestAnalyzeBatch_LLMAnalyzer_ShadowModeAnalyzerDisabledSkipsShadowLane(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMShadow)
	pii := &countingPIIScanner{}
	meterProvider, reader := newManualMeter(t)
	result, pubs, err := runLLMLaneBatchWithMeter(t, conn, td, flags, false, meterProvider, pii, nil, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Findings, "the legacy engines enforce as in the off mode")
	assert.Empty(t, *pubs.llm, "no shadow request without an analyzer to serve it")
	assert.Len(t, *pubs.gitleaks, 1)
	assert.Len(t, *pubs.presidio, 1)
	assert.Equal(t, int32(1), pii.calls.Load())
	assert.Equal(t, int64(1), llmPolicyEvaluations(t, reader, "shadow_skipped"))
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "fallback_legacy"), "the shadow mode has nothing to fall back from")
}

func TestAnalyzeBatch_LLMAnalyzer_OffVariantBeatsBooleanFlag(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	// The boolean read is only the transition rule for a key without
	// variants; an explicit off variant keeps the legacy engines.
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, td.orgID, true)
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMOff)
	pii := &countingPIIScanner{}
	result, pubs, err := runLLMLaneBatch(t, conn, td, flags, true, pii, nil, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Findings)
	assert.Empty(t, *pubs.llm, "no LLM analysis request in the off mode")
	assert.Len(t, *pubs.gitleaks, 1)
	assert.Len(t, *pubs.presidio, 1)
	assert.Equal(t, int32(1), pii.calls.Load())
}

func TestAnalyzeBatch_LLMAnalyzer_LLMVariantRoutesCoveredSources(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMLLM)
	pii := &countingPIIScanner{}
	meterProvider, reader := newManualMeter(t)
	result, pubs, err := runLLMLaneBatchWithMeter(t, conn, td, flags, true, meterProvider, pii, nil, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePresidio})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Findings, "no inline engine scans the covered sources in the llm mode")
	assert.Empty(t, *pubs.gitleaks)
	assert.Empty(t, *pubs.presidio)
	assert.Equal(t, int32(0), pii.calls.Load())
	require.Len(t, *pubs.llm, 1)
	req := (*pubs.llm)[0]
	assert.False(t, req.GetShadow(), "the llm mode enforces, so the request is not shadow")
	assert.Equal(t, "llm_analyzer_stream", req.GetExecutionPath())
	assert.Equal(t, int64(1), llmPolicyEvaluations(t, reader, "published"))
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "shadow_published"))
}

// TestAnalyzeBatch_LLMAnalyzer_ShadowPublishFailureKeepsLegacyResults pins
// that the shadow lane never gates enforcement: an LLM transport outage costs
// the comparison lane its messages, counted as shadow_publish_error, while
// the legacy engines' inline findings still land and the activity succeeds.
func TestAnalyzeBatch_LLMAnalyzer_ShadowPublishFailureKeepsLegacyResults(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID := insertUserMessage(t, conn, td, "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMShadow)
	failing := gcp.NewMockPublisher[*riskv1.LLMAnalysis]()
	failing.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("topic unavailable")))

	meterProvider, reader := newManualMeter(t)
	result, pubs, err := runLLMLaneBatchWithMeter(t, conn, td, flags, true, meterProvider, &countingPIIScanner{}, failing, []uuid.UUID{msgID}, []string{risk_analysis.SourceGitleaks})
	require.NoError(t, err, "a shadow publish failure never fails the batch")
	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 1, result.Findings, "the inline gitleaks finding still enforces")
	assert.Len(t, *pubs.gitleaks, 1, "the legacy gitleaks lane is still dispatched")
	assert.Equal(t, int64(1), llmPolicyEvaluations(t, reader, "shadow_publish_error"))
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "shadow_published"))

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the legacy gitleaks finding still lands in Postgres")
	assert.Equal(t, risk_analysis.SourceGitleaks, rows[0].Source)
}

// TestAnalyzeBatch_LLMAnalyzer_ShadowPartialPublishCountsBothOutcomes pins
// that a partially acknowledged shadow publish counts the acknowledged
// messages as shadow_published and only the rest as shadow_publish_error, so
// the comparison lane's coverage denominator stays exact.
func TestAnalyzeBatch_LLMAnalyzer_ShadowPartialPublishCountsBothOutcomes(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	first := insertUserMessage(t, conn, td, "hello one")
	second := insertUserMessage(t, conn, td, "hello two")

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMShadow)
	partial := gcp.NewMockPublisher[*riskv1.LLMAnalysis]()
	partial.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("topic unavailable"))).Once()
	partial.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())

	meterProvider, reader := newManualMeter(t)
	result, _, err := runLLMLaneBatchWithMeter(t, conn, td, flags, true, meterProvider, &countingPIIScanner{}, partial, []uuid.UUID{first, second}, []string{risk_analysis.SourceGitleaks})
	require.NoError(t, err, "a partial shadow publish never fails the batch")
	assert.Equal(t, 2, result.Processed)
	assert.Equal(t, int64(1), llmPolicyEvaluations(t, reader, "shadow_published"), "the acknowledged message is counted")
	assert.Equal(t, int64(1), llmPolicyEvaluations(t, reader, "shadow_publish_error"), "only the unacknowledged message is counted as an error")
}

// TestAnalyzeBatch_LLMAnalyzer_PartialPublishFailsActivityWithoutCounting
// pins the enforcing lane's side of the partial-publish accounting: the
// activity fails so the retry republishes the whole batch, and nothing is
// counted as published, or the acknowledged messages would be counted twice.
func TestAnalyzeBatch_LLMAnalyzer_PartialPublishFailsActivityWithoutCounting(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	first := insertUserMessage(t, conn, td, "hello one")
	second := insertUserMessage(t, conn, td, "hello two")

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, td.orgID, feature.VariantRiskLLMLLM)
	partial := gcp.NewMockPublisher[*riskv1.LLMAnalysis]()
	partial.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("topic unavailable"))).Once()
	partial.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())

	meterProvider, reader := newManualMeter(t)
	_, _, err := runLLMLaneBatchWithMeter(t, conn, td, flags, true, meterProvider, &countingPIIScanner{}, partial, []uuid.UUID{first, second}, []string{risk_analysis.SourceGitleaks})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm analyzer scan dispatch")
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "published"), "a partial enforcing publish counts nothing before the retry")
	assert.Equal(t, int64(0), llmPolicyEvaluations(t, reader, "shadow_publish_error"))
}
