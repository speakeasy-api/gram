// Package mcpriskscantest builds the production MCP risk evaluator over test
// infrastructure.
package mcpriskscantest

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tooldisposition"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// NewEvaluator returns the database-backed MCP policy evaluator that
// production wires, with LLM-backed detectors replaced by their no-op
// classifiers and findings published nowhere.
func NewEvaluator(
	t *testing.T,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	cacheImpl cache.Cache,
	enc *encryption.Client,
) *mcpriskscan.Evaluator {
	t.Helper()

	scanner := NewRiskScanner(t, logger, tracerProvider, meterProvider, db, &ra.StubPIIScanner{}, promptinjection.NoopClassifier)

	return mcpriskscan.NewPolicyEvaluator(
		logger,
		tracerProvider,
		meterProvider,
		policycore.NewMCPPolicies(db, tooldisposition.New(logger, db, cacheImpl)),
		risk.NewMCPPolicyScanner(scanner, shadowmcp.NewClient(logger, db, cacheImpl, testenv.DefaultSiteURL(t))),
		gcp.NewNoopPublisher[*riskv1.Finding](),
		risk.NewMCPFindingEvidenceStore(db, enc),
		mcpriskscan.DefaultPolicyConfig,
	)
}

// NewRiskScanner returns the production risk scanner over db, with the prompt
// policy judge disabled and meter readings published nowhere. pii is nil when
// a test models a deployment without Presidio.
func NewRiskScanner(
	t *testing.T,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	pii ra.PIIScanner,
	classifier promptinjection.Classifier,
) *risk.Scanner {
	t.Helper()

	celEng, err := celenv.New()
	require.NoError(t, err)
	customRules := customruleanalyzer.NewScanner(db)
	scanner := risk.NewScanner(
		logger,
		tracerProvider,
		meterProvider,
		db,
		customRules,
		pii,
		promptinjection.NewScanner(logger, classifier),
		promptpolicy.NewScanner(logger, promptpolicy.NoopEvaluator),
		&feature.InMemory{},
		celEng,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)
	return scanner
}
