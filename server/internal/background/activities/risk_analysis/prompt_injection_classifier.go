package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// PromptInjectionClassifier returns one verdict per input text. The
// concrete implementation may be local or remote; callers must not assume
// either. See DebertaClassifier for the production implementation.
type PromptInjectionClassifier interface {
	Classify(ctx context.Context, texts []string) ([]ClassifierVerdict, error)
}

// ClassifierVerdict is a single classification result. Score is the model's
// reported probability that the corresponding input is an injection attempt
// (0.0–1.0); Injection is true when the model's argmax is the INJECTION class.
type ClassifierVerdict struct {
	Injection bool
	Score     float64
}

// DebertaClassifier calls a self-hosted deberta-v3-base-prompt-injection
// FastAPI service. Cluster-internal, unauthenticated — uses an unsafe
// guardian policy (empty blocklist) so Kubernetes ClusterIPs (RFC 1918)
// resolve.
type DebertaClassifier struct {
	baseURL         string
	httpClient      *guardian.HTTPClient
	tracer          trace.Tracer
	logger          *slog.Logger
	requestDuration metric.Float64Histogram
	requestFailures metric.Int64Counter
}

type debertaRequest struct {
	Texts []string `json:"texts"`
}

type debertaResult struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// NewDebertaClassifier creates a classifier client pointed at the given
// base URL.
func NewDebertaClassifier(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *DebertaClassifier {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/prompt_injection_classifier")

	requestDuration, _ := meter.Float64Histogram(
		"risk.prompt_injection.classifier_duration",
		metric.WithDescription("Duration of prompt-injection classifier HTTP requests in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)

	requestFailures, _ := meter.Int64Counter(
		"risk.prompt_injection.classifier_failures",
		metric.WithDescription("Number of failed prompt-injection classifier requests"),
		metric.WithUnit("{request}"),
	)

	unsafePolicy, _ := guardian.NewUnsafePolicy(tracerProvider, []string{})
	httpClient := unsafePolicy.PooledClient()

	return &DebertaClassifier{
		baseURL:         strings.TrimRight(baseURL, "/"),
		httpClient:      httpClient,
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/prompt_injection_classifier"),
		logger:          logger,
		requestDuration: requestDuration,
		requestFailures: requestFailures,
	}
}

func (c *DebertaClassifier) Classify(ctx context.Context, texts []string) (_ []ClassifierVerdict, err error) {
	if len(texts) == 0 {
		return nil, nil
	}

	ctx, span := c.tracer.Start(ctx, "prompt_injection.classify", trace.WithAttributes(
		attribute.Int("classifier.batch_size", len(texts)),
	))
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if c.requestDuration != nil {
			c.requestDuration.Record(ctx, duration.Seconds())
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			if c.requestFailures != nil {
				c.requestFailures.Add(ctx, 1)
			}
		}
		span.End()
	}()

	body, err := json.Marshal(debertaRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal classifier request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/classify", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create classifier request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("classifier http request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("classifier returned status %d", resp.StatusCode)
	}

	var raw []debertaResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode classifier response: %w", err)
	}
	if len(raw) != len(texts) {
		return nil, fmt.Errorf("classifier returned %d verdicts for %d texts", len(raw), len(texts))
	}

	out := make([]ClassifierVerdict, len(raw))
	for i, r := range raw {
		out[i] = ClassifierVerdict{
			Injection: strings.EqualFold(r.Label, "INJECTION"),
			Score:     r.Score,
		}
	}
	return out, nil
}

// NoopClassifier is wired in when no classifier URL is configured. It
// returns SAFE verdicts for every input. Policies that opt into the
// classifier (UseModelClassifier=true) will surface a configuration error
// from DetectPromptInjection rather than silently downgrading.
type NoopClassifier struct{}

func (NoopClassifier) Classify(_ context.Context, texts []string) ([]ClassifierVerdict, error) {
	return make([]ClassifierVerdict, len(texts)), nil
}

// IsNoopClassifier reports whether the given classifier is the no-op
// fallback. Used by the orchestrator to fail loud when a policy opts
// into the classifier without a real backend configured.
func IsNoopClassifier(c PromptInjectionClassifier) bool {
	_, ok := c.(NoopClassifier)
	return ok
}
