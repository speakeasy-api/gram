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

// PIIScanner detects personally identifiable information in text.
type PIIScanner interface {
	// AnalyzeBatch sends multiple texts to the PII analyzer and returns
	// findings for each. The outer slice is indexed by input position.
	// When entities is non-empty, only those entity types are detected.
	AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) ([][]Finding, error)
}

// presidioRequest is the payload sent to POST /analyze.
type presidioRequest struct {
	Text     string   `json:"text"`
	Language string   `json:"language"`
	ScoreMin float64  `json:"score_threshold"`
	Entities []string `json:"entities,omitempty"`
}

// presidioResult is a single entity returned by the analyzer.
type presidioResult struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// presidioMaxWorkers is the default concurrency limit for Presidio HTTP
// requests. Presidio scanning is network-bound, not CPU-bound, so we use a
// higher limit than runtime.NumCPU().
const presidioMaxWorkers = 100

// PresidioClient calls the Presidio Analyzer HTTP API.
// Presidio is a trusted cluster-internal service, so the client uses an
// unsafe guardian policy with an empty blocklist. The default policy blocks
// RFC 1918 private ranges (10.0.0.0/8) which Kubernetes ClusterIPs fall into.
type PresidioClient struct {
	baseURL         string
	httpClient      *guardian.HTTPClient
	tracer          trace.Tracer
	logger          *slog.Logger
	maxWorkers      int
	requestDuration metric.Float64Histogram
	requestFailures metric.Int64Counter
}

// NewPresidioClient creates a client pointing at the given base URL.
func NewPresidioClient(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *PresidioClient {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio")

	requestDuration, _ := meter.Float64Histogram(
		"risk.presidio.request_duration",
		metric.WithDescription("Duration of individual Presidio /analyze HTTP requests in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)

	requestFailures, _ := meter.Int64Counter(
		"risk.presidio.failures",
		metric.WithDescription("Number of failed Presidio /analyze requests"),
		metric.WithUnit("{request}"),
	)

	// Empty blocklist allows connections to private IPs (Kubernetes ClusterIPs).
	unsafePolicy, _ := guardian.NewUnsafePolicy(tracerProvider, []string{})
	httpClient := unsafePolicy.PooledClient()

	return &PresidioClient{
		baseURL:         strings.TrimRight(baseURL, "/"),
		httpClient:      httpClient,
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio"),
		logger:          logger,
		maxWorkers:      presidioMaxWorkers,
		requestDuration: requestDuration,
		requestFailures: requestFailures,
	}
}

// NewPresidioClientWithWorkers is like NewPresidioClient but allows overriding
// the concurrency limit. Used for benchmarking.
func NewPresidioClientWithWorkers(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger, maxWorkers int) *PresidioClient {
	c := NewPresidioClient(baseURL, tracerProvider, meterProvider, logger)
	c.maxWorkers = maxWorkers
	return c
}

func (p *PresidioClient) AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) (_ [][]Finding, err error) {
	n := len(texts)
	if n == 0 {
		return nil, nil
	}

	ctx, span := p.tracer.Start(ctx, "presidio.analyzeBatch", trace.WithAttributes(
		attribute.Int("presidio.batch_size", n),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	results := make([][]Finding, n)
	workers := min(p.maxWorkers, n)

	// Pre-fill a buffered channel with indices so workers can pull the next
	// item without coordination. Closing it causes workers to exit when the
	// channel drains.
	ch := make(chan int, n)
	for i := range n {
		ch <- i
	}
	close(ch)

	var wg sync.WaitGroup

	// Fan out workers that each drain items from ch until it's empty.
	// Individual failures are logged and skipped; results[idx] stays nil
	// for that text, which the caller treats as "no findings".
	for range workers {
		wg.Go(func() {
			for idx := range ch {
				findings, err := p.analyze(ctx, texts[idx], entities)
				if err != nil {
					p.logger.WarnContext(ctx, "presidio analyze failed for text, skipping",
						attr.SlogError(err),
					)
					continue
				}
				results[idx] = findings
				if onProgress != nil {
					onProgress()
				}
			}
		})
	}

	wg.Wait()
	return results, nil
}

func (p *PresidioClient) analyze(ctx context.Context, text string, entities []string) (_ []Finding, err error) {
	ctx, span := p.tracer.Start(ctx, "presidio.analyze")
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if p.requestDuration != nil {
			p.requestDuration.Record(ctx, duration.Seconds())
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			if p.requestFailures != nil {
				p.requestFailures.Add(ctx, 1)
			}
		}
		span.End()
	}()

	body, err := json.Marshal(presidioRequest{
		Text:     text,
		Language: "en",
		ScoreMin: 0.5,
		Entities: entities,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal presidio request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create presidio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("presidio http request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("presidio returned status %d", resp.StatusCode)
	}

	var results []presidioResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode presidio response: %w", err)
	}

	// Presidio returns character (rune) offsets, not byte offsets.
	// Convert to runes for correct slicing, then map back to byte positions.
	runes := []rune(text)

	findings := make([]Finding, 0, len(results))
	for _, r := range results {
		// Clamp offsets to valid rune range to prevent out-of-bounds panics.
		start := max(0, min(r.Start, len(runes)))
		end := max(start, min(r.End, len(runes)))

		match := string(runes[start:end])

		// Convert rune offsets to byte offsets for storage.
		startByte := len(string(runes[:start]))
		endByte := len(string(runes[:end]))

		findings = append(findings, Finding{
			RuleID:      r.EntityType,
			Description: "PII detected: " + r.EntityType,
			Match:       match,
			StartPos:    startByte,
			EndPos:      endByte,
			Tags:        []string{"pii", strings.ToLower(r.EntityType)},
			Source:      "presidio",
			Confidence:  r.Score,
		})
	}
	span.SetAttributes(attribute.Int("presidio.findings_count", len(findings)))
	return findings, nil
}

// StubPIIScanner is a no-op implementation for environments without Presidio.
type StubPIIScanner struct{}

func (s *StubPIIScanner) AnalyzeBatch(_ context.Context, texts []string, _ []string, _ func()) ([][]Finding, error) {
	results := make([][]Finding, len(texts))
	for i := range texts {
		results[i] = nil
	}
	return results, nil
}
