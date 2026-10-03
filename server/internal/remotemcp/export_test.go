package remotemcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan/mcpriskscantest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/risk"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
)

var testInfra *testenv.Environment

// SetTestInfra shares the external test package's launched infrastructure with internal tests.
func SetTestInfra(env *testenv.Environment) { testInfra = env }

// SetBeforeClaim runs f between the claim's list and re-read, inside the locked update transaction.
func (s *Service) SetBeforeClaim(f func(holderPID uint32, previousURL string)) { s.beforeClaim = f }

func newUnsafePolicyForTest(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy
}

func newRiskScanEvaluatorForTest(t *testing.T) *mcpriskscan.Evaluator {
	t.Helper()
	logger := testenv.NewLogger(t)
	conn, err := testInfra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return mcpriskscantest.NewEvaluator(t, logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, testenv.NewMemoryCache(), testenv.NewEncryptionClient(t))
}

func newMCPFindingEvidenceForTest(t *testing.T) *risk.MCPFindingEvidenceStore {
	t.Helper()
	conn, err := testInfra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return risk.NewMCPFindingEvidenceStore(conn, testenv.NewEncryptionClient(t))
}

func newProxyManagerForTest(t *testing.T, policy *guardian.Policy, evaluator *mcpriskscan.Evaluator) *ProxyManager {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	conn, err := testInfra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	checkpoint := mcptoolexecution.NewCheckpoint(conn, mcptoolexecution.DefaultEvaluationTimeout, meterProvider, logger)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	return NewProxyManager(
		logger,
		tracerProvider,
		meterProvider,
		conn,
		policy,
		authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		posthog.New(t.Context(), logger, "", "", ""),
		tm.NewStub(logger),
		billingClient,
		billingClient,
		mcpservers.NewToolDispositionCache(logger, conn, testenv.NewMemoryCache()),
		toolcallobserver.NoopSuccessRecorder{},
		toolfilter.NewSessionToolWitnessStore(logger, testenv.NewMemoryCache()),
		checkpoint,
		evaluator,
	)
}
