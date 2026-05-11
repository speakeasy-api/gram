package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// PromptInjectionClassifier returns a label + score for each input text.
// Implementations are expected to be safe for concurrent use and to honor the
// passed context for cancellation.
type PromptInjectionClassifier interface {
	Classify(ctx context.Context, texts []string) ([]ClassifierResult, error)
}

// ClassifierResult is one prediction returned by the L1 service. Score is the
// INJECTION-class probability (always — even when label == "SAFE"). Callers
// apply their own threshold before turning a result into a Finding.
type ClassifierResult struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// LabelInjection is the positive class returned by the deberta-v3 model.
const LabelInjection = "INJECTION"

// promptInjectionClassifierHTTPBatchSize is the per-request item cap. Matches the Python
// service's MAX_BATCH default. Keeps payload size and inference time per call
// bounded; AnalyzeBatch fan-out across batches when n > this.
const promptInjectionClassifierHTTPBatchSize = 50

// promptInjectionClassifierPerBatchConcurrency is the number of in-flight HTTP requests
// per Classify call. Mirrors the presidio client's perBatchRequestConcurrency.
const promptInjectionClassifierPerBatchConcurrency = 2

// DebertaClassifierClient calls the gram-pi-classifier sidecar's POST /detect.
// Like PresidioClient, this is a trusted cluster-internal service so the
// guardian policy is permissive on private IPs.
type DebertaClassifierClient struct {
	baseURL            string
	httpClient         *guardian.HTTPClient
	tracer             trace.Tracer
	logger             *slog.Logger
	requestConcurrency int
	requestDuration    metric.Float64Histogram
	requestFailures    metric.Int64Counter
}

// NewPromptInjectionClassifier builds a client pointing at the given base URL.
func NewPromptInjectionClassifier(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *DebertaClassifierClient {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/pi_classifier")

	requestDuration, _ := meter.Float64Histogram(
		"risk.pi_classifier.request_duration",
		metric.WithDescription("Duration of individual gram-pi-classifier /detect HTTP requests in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)

	requestFailures, _ := meter.Int64Counter(
		"risk.pi_classifier.failures",
		metric.WithDescription("Number of failed gram-pi-classifier /detect requests"),
		metric.WithUnit("{request}"),
	)

	unsafePolicy, _ := guardian.NewUnsafePolicy(tracerProvider, []string{})
	httpClient := unsafePolicy.PooledClient()

	return &DebertaClassifierClient{
		baseURL:            strings.TrimRight(baseURL, "/"),
		httpClient:         httpClient,
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/pi_classifier"),
		logger:             logger,
		requestConcurrency: promptInjectionClassifierPerBatchConcurrency,
		requestDuration:    requestDuration,
		requestFailures:    requestFailures,
	}
}

// NewPromptInjectionClassifierWithConcurrency lets tests pin the per-batch
// HTTP concurrency. Mirrors NewPresidioClientWithConcurrency.
func NewPromptInjectionClassifierWithConcurrency(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger, requestConcurrency int) *DebertaClassifierClient {
	c := NewPromptInjectionClassifier(baseURL, tracerProvider, meterProvider, logger)
	c.requestConcurrency = requestConcurrency
	return c
}

// StubClassifier is a no-op classifier used when --pi-classifier-url is empty.
// Its Classify reports every text as SAFE with score 0, so the L1 layer
// effectively turns off without changing call sites.
type StubClassifier struct{}

func (StubClassifier) Classify(_ context.Context, texts []string) ([]ClassifierResult, error) {
	out := make([]ClassifierResult, len(texts))
	for i := range out {
		out[i] = ClassifierResult{Label: "SAFE", Score: 0}
	}
	return out, nil
}

type detectRequest struct {
	Texts []string `json:"texts"`
}

type detectResponse struct {
	Results []ClassifierResult `json:"results"`
}

func (c *DebertaClassifierClient) Classify(ctx context.Context, texts []string) (_ []ClassifierResult, err error) {
	n := len(texts)
	if n == 0 {
		return nil, nil
	}

	ctx, span := c.tracer.Start(ctx, "pi_classifier.classify", trace.WithAttributes(
		attribute.Int("pi_classifier.batch_size", n),
		attribute.Int("pi_classifier.http_batch_size", promptInjectionClassifierHTTPBatchSize),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	results := make([]ClassifierResult, n)
	batches := chunkTextIndexes(n, promptInjectionClassifierHTTPBatchSize)
	workers := min(max(1, c.requestConcurrency), len(batches))

	ch := make(chan indexRange, len(batches))
	for _, batch := range batches {
		ch <- batch
	}
	close(ch)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for range workers {
		wg.Go(func() {
			for batch := range ch {
				if ctx.Err() != nil {
					return
				}
				out, callErr := c.detect(ctx, texts[batch.start:batch.end])
				if callErr != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = callErr
					}
					mu.Unlock()
					c.logger.WarnContext(ctx, "pi_classifier detect failed for batch, skipping",
						attr.SlogError(callErr),
					)
					continue
				}
				for i, r := range out {
					results[batch.start+i] = r
				}
			}
		})
	}

	wg.Wait()

	if firstErr != nil {
		return results, fmt.Errorf("pi_classifier detect: %w", firstErr)
	}
	return results, nil
}

func (c *DebertaClassifierClient) detect(ctx context.Context, texts []string) (_ []ClassifierResult, err error) {
	if len(texts) == 0 {
		return nil, nil
	}

	ctx, span := c.tracer.Start(ctx, "pi_classifier.detect")
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

	body, err := json.Marshal(detectRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal pi_classifier request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/detect", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create pi_classifier request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pi_classifier http request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pi_classifier returned status %d", resp.StatusCode)
	}

	var decoded detectResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode pi_classifier response: %w", err)
	}
	if len(decoded.Results) != len(texts) {
		return nil, fmt.Errorf("pi_classifier returned %d results for %d texts", len(decoded.Results), len(texts))
	}

	span.SetAttributes(
		attribute.Int("pi_classifier.http_batch_size", len(texts)),
	)
	return decoded.Results, nil
}
