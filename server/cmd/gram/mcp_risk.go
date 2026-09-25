package gram

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/background"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	gramopenrouter "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func newMCPRiskEvaluator(
	c *cli.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	redisClient *redis.Client,
	features feature.Provider,
	completions gramopenrouter.CompletionClient,
	publishers *background.Publishers,
	shadowMCPClient *shadowmcp.Client,
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
	scanner, err := risk.NewScanner(
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
		nil,
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
	)
	return evaluator, scanner, nil
}
