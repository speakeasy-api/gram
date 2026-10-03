package remotemcptest

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
)

// NewProxyManager returns the production remote MCP proxy manager over db,
// with billing stubbed and product analytics sent nowhere.
func NewProxyManager(
	t *testing.T,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	policy *guardian.Policy,
	authzEngine *authz.Engine,
	telemLogger *telemetry.Logger,
	cacheImpl cache.Cache,
	scanEvaluator *mcpriskscan.Evaluator,
) *remotemcp.ProxyManager {
	t.Helper()

	checkpoint := mcptoolexecution.NewCheckpoint(db, mcptoolexecution.DefaultEvaluationTimeout, meterProvider, logger)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	return remotemcp.NewProxyManager(
		logger,
		tracerProvider,
		meterProvider,
		db,
		policy,
		authzEngine,
		posthog.New(t.Context(), logger, "", "", ""),
		telemLogger,
		billingClient,
		billingClient,
		mcpservers.NewToolDispositionCache(logger, db, cacheImpl),
		toolcallobserver.NoopSuccessRecorder{},
		toolfilter.NewSessionToolWitnessStore(logger, testenv.NewMemoryCache()),
		checkpoint,
		scanEvaluator,
	)
}
