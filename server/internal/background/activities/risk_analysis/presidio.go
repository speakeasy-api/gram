package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
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
	Text     []string `json:"text"`
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

// Tuned to Presidio capacity 2026-05-01. Fleet-wide cap is perPodAnalyzeBatchConcurrency.
const perBatchRequestConcurrency = 4

// /analyze accepts a string or array; array returns ordered nested list. Bounds blast radius on retry-bisect.
const presidioHTTPBatchSize = 50

// Keep jitter small: bisection is bounded, but sleeping still counts against the activity timeout.
const presidioRetryBackoff = 100 * time.Millisecond

const presidioRetryBackoffCap = 1 * time.Second

// PresidioClient calls the Presidio Analyzer HTTP API.
// Presidio is a trusted cluster-internal service, so the client uses an
// unsafe guardian policy with an empty blocklist. The default policy blocks
// RFC 1918 private ranges (10.0.0.0/8) which Kubernetes ClusterIPs fall into.
type PresidioClient struct {
	baseURL            string
	httpClient         *guardian.HTTPClient
	tracer             trace.Tracer
	logger             *slog.Logger
	requestConcurrency int
	retryBackoff       time.Duration
	requestDuration    metric.Float64Histogram
	requestFailures    metric.Int64Counter
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
		baseURL:            strings.TrimRight(baseURL, "/"),
		httpClient:         httpClient,
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio"),
		logger:             logger,
		requestConcurrency: perBatchRequestConcurrency,
		retryBackoff:       presidioRetryBackoff,
		requestDuration:    requestDuration,
		requestFailures:    requestFailures,
	}
}

// NewPresidioClientWithConcurrency is like NewPresidioClient but allows
// overriding the per-batch HTTP request concurrency. Used for benchmarking
// and tests; the retry backoff is disabled so test sweeps don't pay 500ms
// per bisect step.
func NewPresidioClientWithConcurrency(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger, requestConcurrency int) *PresidioClient {
	c := NewPresidioClient(baseURL, tracerProvider, meterProvider, logger)
	c.requestConcurrency = requestConcurrency
	c.retryBackoff = 0
	return c
}

// presidioEntityBlacklist is the set of Presidio entity types we refuse to
// scan for regardless of what's stored on the policy.
//
//   - PERSON: Presidio's NER-backed person detection trips on common
//     capitalized words ("Bash", "Read", proper nouns inside code
//     identifiers, etc.) and would deny legitimate tool calls / pollute
//     batch findings. Re-enable once we have a confidence threshold or a
//     scoped allow-list.
var presidioEntityBlacklist = map[string]struct{}{
	"PERSON": {},
}

// filterEntities removes blacklisted entity types from the caller's list.
// Returns nil unchanged so Presidio's default entity set still applies for
// callers that didn't pin a list. Returns an empty (non-nil) slice when the
// caller pinned a list and every entry was blacklisted, so AnalyzeBatch can
// short-circuit instead of falling back to the unbounded default scan.
func filterEntities(entities []string) []string {
	if entities == nil {
		return nil
	}
	out := make([]string, 0, len(entities))
	for _, e := range entities {
		if _, blocked := presidioEntityBlacklist[e]; blocked {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (p *PresidioClient) AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) (_ [][]Finding, err error) {
	n := len(texts)
	if n == 0 {
		return nil, nil
	}

	// Apply the entity blacklist at the lowest level so every caller (hook
	// scanner + Temporal drain activity) inherits the same policy.
	filtered := filterEntities(entities)
	if len(entities) > 0 && len(filtered) == 0 {
		// Caller pinned only blacklisted entities; nothing to scan for.
		return make([][]Finding, n), nil
	}
	entities = filtered

	ctx, span := p.tracer.Start(ctx, "presidio.analyzeBatch", trace.WithAttributes(
		attribute.Int("presidio.batch_size", n),
		attribute.Int("presidio.http_batch_size", presidioHTTPBatchSize),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	results := make([][]Finding, n)
	batches := chunkTextIndexes(n, presidioHTTPBatchSize)
	workers := min(max(1, p.requestConcurrency), len(batches))

	ch := make(chan indexRange, len(batches))
	for _, batch := range batches {
		ch <- batch
	}
	close(ch)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for batch := range ch {
				p.analyzeRange(ctx, texts, entities, batch, results, onProgress)
			}
		})
	}

	wg.Wait()
	return results, nil
}

func (p *PresidioClient) analyzeRange(ctx context.Context, texts []string, entities []string, batch indexRange, results [][]Finding, onProgress func()) {
	if ok, err := p.tryAnalyzeRange(ctx, texts, entities, batch, results, onProgress); !ok {
		p.splitFailedRange(ctx, texts, entities, batch, results, onProgress, err, 0)
	}
}

func (p *PresidioClient) tryAnalyzeRange(ctx context.Context, texts []string, entities []string, batch indexRange, results [][]Finding, onProgress func()) (bool, error) {
	if onProgress != nil {
		onProgress()
	}

	findings, err := p.analyze(ctx, texts[batch.start:batch.end], entities)
	if err != nil {
		return false, err
	}

	for i, f := range findings {
		results[batch.start+i] = f
		if onProgress != nil {
			onProgress()
		}
	}
	return true, nil
}

func (p *PresidioClient) splitFailedRange(ctx context.Context, texts []string, entities []string, batch indexRange, results [][]Finding, onProgress func(), cause error, depth int) {
	if ctx.Err() != nil {
		return
	}
	if batch.end-batch.start == 1 {
		p.logger.WarnContext(ctx, "presidio analyze failed for text, skipping",
			attr.SlogError(cause),
		)
		if onProgress != nil {
			onProgress()
		}
		return
	}

	p.logger.WarnContext(ctx, "presidio analyze failed for text batch, splitting",
		attr.SlogError(cause),
	)

	if !sleepCtx(ctx, computePresidioBackoff(p.retryBackoff, depth)) {
		return
	}

	mid := batch.start + ((batch.end - batch.start) / 2)
	left := indexRange{start: batch.start, end: mid}
	right := indexRange{start: mid, end: batch.end}

	leftOK, leftErr := p.tryAnalyzeRange(ctx, texts, entities, left, results, onProgress)
	rightOK, rightErr := p.tryAnalyzeRange(ctx, texts, entities, right, results, onProgress)
	if !leftOK {
		p.splitFailedRange(ctx, texts, entities, left, results, onProgress, leftErr, depth+1)
	}
	if !rightOK {
		p.splitFailedRange(ctx, texts, entities, right, results, onProgress, rightErr, depth+1)
	}
}

// computePresidioBackoff returns a full-jittered exponential backoff for the
// given split depth: uniform in [0, min(cap, base*2^depth)). Returns 0 when
// base is 0 (tests disable backoff that way).
func computePresidioBackoff(base time.Duration, depth int) time.Duration {
	if base <= 0 {
		return 0
	}
	backoff := base
	for range depth {
		backoff *= 2
		if backoff >= presidioRetryBackoffCap {
			backoff = presidioRetryBackoffCap
			break
		}
	}
	return time.Duration(rand.Int64N(int64(backoff))) // #nosec G404 -- jitter, not security-sensitive
}

// sleepCtx pauses for d, returning false if ctx is cancelled before the
// timer fires. A non-positive d is treated as no sleep.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

type indexRange struct {
	start int
	end   int
}

func chunkTextIndexes(n, size int) []indexRange {
	if n == 0 {
		return nil
	}
	var batches []indexRange
	for start := 0; start < n; start += size {
		end := min(start+size, n)
		batches = append(batches, indexRange{start: start, end: end})
	}
	return batches
}

func (p *PresidioClient) analyze(ctx context.Context, texts []string, entities []string) (_ [][]Finding, err error) {
	// /analyze 500s on empty array ("No text provided"). Short-circuit.
	if len(texts) == 0 {
		return nil, nil
	}

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
		Text:     texts,
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

	var results [][]presidioResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode presidio response: %w", err)
	}
	if len(results) != len(texts) {
		return nil, fmt.Errorf("presidio returned %d result sets for %d texts", len(results), len(texts))
	}

	findings := make([][]Finding, len(texts))
	findingsCount := 0
	for i, text := range texts {
		findings[i] = convertPresidioFindings(text, results[i])
		findingsCount += len(findings[i])
	}
	span.SetAttributes(
		attribute.Int("presidio.http_batch_size", len(texts)),
		attribute.Int("presidio.findings_count", findingsCount),
	)
	return findings, nil
}

func convertPresidioFindings(text string, results []presidioResult) []Finding {
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
	return findings
}

// StubPIIScanner is a no-op implementation for environments without Presidio.
type StubPIIScanner struct{}

func (s *StubPIIScanner) AnalyzeBatch(_ context.Context, texts []string, _ []string, _ func()) ([][]Finding, error) {
	return make([][]Finding, len(texts)), nil
}
