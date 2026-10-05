package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	gramopenrouter "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func newRiskEnforcementDispatcher(
	ctx context.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	redisClient *redis.Client,
	broker pubSubBroker,
	features feature.Provider,
) (risk.EnforcementDispatcher, func(context.Context) error) {
	// Failures return an untyped nil so the scanner's nil check sees no dispatcher.
	inbox, err := enforcereply.New(ctx, logger, tracerProvider, meterProvider, enforcereply.Config{
		RedisOptions: *redisClient.Options(),
		ReplicaID:    "",
		PollInterval: 0,
		DrainGate:    nil,
	})
	if err != nil {
		logger.ErrorContext(ctx, "pub/sub enforcement disabled: create reply inbox", attr.SlogError(err))
		return nil, nil
	}

	dispatcher, err := enforcereply.NewDispatcher(ctx, logger, meterProvider, broker, inbox, enforcereply.DispatcherConfig{
		WaitTimeout: 0,
		LaneWaitTimeout: map[riskv1.EnforcementScanner]time.Duration{ //nolint:exhaustive // an override list is partial by definition; other lanes use WaitTimeout
			riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER: enforcereply.DefaultLLMAnalyzerWaitTimeout,
		},
		Flags: features,
	})
	if err != nil {
		logger.ErrorContext(ctx, "pub/sub enforcement disabled: create dispatcher", attr.SlogError(err))
		_ = inbox.Close()
		return nil, nil
	}

	return dispatcher, func(ctx context.Context) error {
		dispatcherErr := dispatcher.Close(ctx)
		inboxErr := inbox.Close()
		return errors.Join(dispatcherErr, inboxErr)
	}
}

func newMCPRiskEvaluator(
	c *cli.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	enc *encryption.Client,
	redisClient *redis.Client,
	features feature.Provider,
	enforcementDispatcher risk.EnforcementDispatcher,
	completions gramopenrouter.CompletionClient,
	publishers *background.Publishers,
	shadowMCPClient *shadowmcp.Client,
	toolIOLogsEnabled telemetry.FeatureChecker,
) (*mcpriskscan.Evaluator, *risk.Scanner, error) {
	var piiScanner ra.PIIScanner
	if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
		piiScanner = ra.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
	}
	judgeLimiter := gramopenrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(redisClient))
	piScanner := promptinjection.NewScanner(logger, piopenrouter.New(logger, tracerProvider, meterProvider, completions, judgeLimiter).Classify)
	promptPolicyScanner := promptpolicy.NewScanner(logger, ppopenrouter.New(logger, tracerProvider, meterProvider, completions, judgeLimiter).Evaluate)
	celEngine, err := celenv.New()
	if err != nil {
		return nil, nil, fmt.Errorf("create MCP risk CEL engine: %w", err)
	}
	customRulesScanner, err := customruleanalyzer.NewScanner(db)
	if err != nil {
		return nil, nil, fmt.Errorf("create MCP custom rules scanner: %w", err)
	}
	scanner, err := risk.NewScannerWithEnforcementDispatcher(
		logger,
		tracerProvider,
		meterProvider,
		db,
		customRulesScanner,
		piiScanner,
		piScanner,
		promptPolicyScanner,
		features,
		celEngine,
		enforcementDispatcher,
		metering.NewRiskRecorder(publishers.MeterReadings),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create MCP risk scanner: %w", err)
	}
	evaluator := mcpriskscan.NewPolicyEvaluator(
		logger,
		tracerProvider,
		meterProvider,
		policycore.NewWithToolAnnotations(db, mcpservers.NewToolDispositionCache(logger, db, cache.NewRedisCacheAdapter(redisClient))),
		risk.NewMCPPolicyScanner(scanner, shadowMCPClient),
		publishers.RiskFindings,
		mcpriskscan.DefaultPolicyConfig,
		mcpriskscan.WithMCPFindingEvidenceWriter(risk.NewMCPFindingEvidenceStore(db, enc), mcpriskscan.PayloadStorageCheck(toolIOLogsEnabled)),
	)
	return evaluator, scanner, nil
}
