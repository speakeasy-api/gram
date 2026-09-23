package judgeshadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/scanners/jev"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

type Handler struct {
	logger      *slog.Logger
	flags       feature.Provider
	judge       *jev.Judge
	comparisons metric.Int64Counter
	duration    metric.Float64Histogram
	tokens      metric.Int64Counter
}

func NewHandler(logger *slog.Logger, flags feature.Provider, judge *jev.Judge, provider metric.MeterProvider) (*Handler, error) {
	meter := provider.Meter("github.com/speakeasy-api/gram/server/internal/scanners/judgeshadow")
	comparisons, err := meter.Int64Counter("risk.judge_shadow.comparisons")
	if err != nil {
		return nil, fmt.Errorf("create shadow comparison counter: %w", err)
	}
	duration, err := meter.Float64Histogram("risk.judge_shadow.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create shadow duration histogram: %w", err)
	}
	tokens, err := meter.Int64Counter("risk.judge_shadow.tokens")
	if err != nil {
		return nil, fmt.Errorf("create shadow token counter: %w", err)
	}
	return &Handler{logger: logger, flags: flags, judge: judge, comparisons: comparisons, duration: duration, tokens: tokens}, nil
}

// Handle records one physical attempt. Failures are unavailable, never clean.
// Provider failures are acknowledged: this experiment must not amplify an outage
// with retries or re-evaluate the baseline. Transport redelivery can still occur;
// comparison_id is stable and supports deduplication when analyzing logs.
func (h *Handler) Handle(ctx context.Context, m *riskv1.JudgeShadowAnalysis, _ gcp.MessageMetadata) error {
	if !Enabled(ctx, h.flags, m.GetDetector(), m.GetOrganizationId()) {
		return nil
	}
	if m.GetComparisonId() == "" || len(m.GetStateJson()) > maxStateBytes || !json.Valid(m.GetStateJson()) {
		return errors.New("invalid judge shadow event")
	}
	if m.GetBaselineOutcome() != "clean" && m.GetBaselineOutcome() != "match" && m.GetBaselineOutcome() != "unavailable" {
		return errors.New("invalid shadow baseline outcome")
	}
	started := time.Now()
	result, err := h.judge.Evaluate(ctx, m.GetOrganizationId(), m.GetDetector(), m.GetStateJson())
	elapsed := time.Since(started).Seconds()
	outcome := "unavailable"
	if err == nil {
		outcome = "clean"
		for _, probability := range result.Probabilities {
			if probability >= jev.Threshold {
				outcome = "match"
			}
		}
	}
	agreement := "not_comparable"
	if outcome != "unavailable" && m.GetBaselineOutcome() != "unavailable" {
		agreement = "disagree"
		if outcome == m.GetBaselineOutcome() {
			agreement = "agree"
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("detector", m.GetDetector()),
		attribute.String("baseline_outcome", m.GetBaselineOutcome()),
		attribute.String("shadow_outcome", outcome),
		attribute.String("agreement", agreement),
		attribute.String("question_version", jev.QuestionVersion),
	}
	h.comparisons.Add(ctx, 1, metric.WithAttributes(attrs...))
	h.duration.Record(ctx, elapsed, metric.WithAttributes(attribute.String("detector", m.GetDetector()), attribute.String("engine", "jev")))
	h.duration.Record(ctx, m.GetBaselineDurationSeconds(), metric.WithAttributes(attribute.String("detector", m.GetDetector()), attribute.String("engine", "baseline_batch")))
	if err == nil {
		h.tokens.Add(ctx, int64(result.InputTokens), metric.WithAttributes(attribute.String("detector", m.GetDetector()), attribute.String("direction", "input")))
		h.tokens.Add(ctx, int64(result.OutputTokens), metric.WithAttributes(attribute.String("detector", m.GetDetector()), attribute.String("direction", "output")))
	}
	// This allowlist intentionally excludes state, policy text, rationale, and errors
	// from the provider. Both positive and negative verdicts are retained.
	record := map[string]any{
		"comparison_id": m.GetComparisonId(), "detector": m.GetDetector(),
		"baseline_trace_id": m.GetBaselineTraceId(), "policy_hash": m.GetPolicyHash(),
		"baseline_outcome": m.GetBaselineOutcome(), "shadow_outcome": outcome, "agreement": agreement,
		"baseline_model": m.GetBaselineModel(), "shadow_model": result.Model,
		"question_version": jev.QuestionVersion, "threshold": jev.Threshold,
		"baseline_batch_duration_seconds": m.GetBaselineDurationSeconds(), "shadow_duration_seconds": elapsed,
		"probabilities": result.Probabilities, "input_tokens": result.InputTokens, "output_tokens": result.OutputTokens,
		"cost_usd":   result.CostUSD,
		"created_at": m.GetCreatedAt(),
	}
	logAttrs := []any{attr.SlogOrganizationID(m.GetOrganizationId()), attr.SlogProjectID(m.GetProjectId()), attr.SlogRiskJudgeShadow(record)}
	if err != nil {
		// Report a bounded error class; never serialize a provider-controlled error.
		errorClass := "provider_error"
		if errors.Is(err, context.DeadlineExceeded) {
			errorClass = "timeout"
		}
		if errors.Is(err, context.Canceled) {
			errorClass = "canceled"
		}
		if errors.Is(err, typesafe.ErrUnavailable) {
			errorClass = "not_configured"
		}
		logAttrs = append(logAttrs, attr.SlogError(errors.New(errorClass)))
	}
	h.logger.InfoContext(ctx, "Jev shadow comparison", logAttrs...)
	return nil
}
