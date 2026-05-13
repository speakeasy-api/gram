package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// presidioInflightByteBudget caps the total bytes of /analyze request payloads
// concurrently in flight against a single PresidioClient. Tuned conservatively;
// raise if Presidio capacity grows. The retry orchestrator on top of this
// client ultimately bounds parallelism through this semaphore — there is no
// separate request-count limit.
const presidioInflightByteBudget int64 = 1 << 20 // 1 MiB

// presidioThrottleHeartbeatInterval is how often the byte-throttle wait loop
// calls onProgress while blocked. Must stay well below the Temporal activity
// HeartbeatTimeout (60s in drain_risk_analysis.go) so a queue of large
// messages cannot starve heartbeats.
const presidioThrottleHeartbeatInterval = 1 * time.Second

// Per-request timeout for /analyze. Tuned 2026-05-12 from production
// risk.presidio.request_duration data: healthy avg <1s, healthy max 5s typical
// (occasional 40-75s tail), degraded calls observed at 60-180s. 30s detects
// degradation aggressively while clearing healthy traffic with ~6x margin.
// Must stay <= AnalyzeBatch HeartbeatTimeout in drain_risk_analysis.go (60s),
// otherwise a single stalled call can starve the activity heartbeat.
const analyzeRequestTimeout = 30 * time.Second

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

// PresidioClient calls the Presidio Analyzer HTTP API.
//
// Each AnalyzeBatch call makes exactly one HTTP POST to /analyze — no
// internal sub-batching, no recursive bisect, no retry. Total in-flight
// request bytes are bounded by a process-shared byte-budget semaphore; while
// blocked waiting for capacity the client calls onProgress on a fixed cadence
// so the Temporal activity heartbeat stays alive.
//
// Presidio is a trusted cluster-internal service, so the client uses an
// unsafe guardian policy with an empty blocklist. The default policy blocks
// RFC 1918 private ranges (10.0.0.0/8) which Kubernetes ClusterIPs fall into.
type PresidioClient struct {
	baseURL              string
	httpClient           *guardian.HTTPClient
	tracer               trace.Tracer
	logger               *slog.Logger
	requestTimeout       time.Duration
	throttle             *semaphore.Weighted
	throttleBudget       int64
	throttleHeartbeat    time.Duration
	requestDuration      metric.Float64Histogram
	requestSize          metric.Int64Histogram
	requestFailures      metric.Int64Counter
	throttleWaitDuration metric.Float64Histogram
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

	// Bucket boundaries span 1 KiB to 4 MiB on powers of 4 to cover both
	// typical chat-message batches and tail payloads dominated by
	// oversized tool-call JSON.
	requestSize, _ := meter.Int64Histogram(
		"risk.presidio.request_size",
		metric.WithDescription("Size of Presidio /analyze HTTP request bodies in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(1024, 4096, 16384, 65536, 262144, 1048576, 4194304),
	)

	requestFailures, _ := meter.Int64Counter(
		"risk.presidio.failures",
		metric.WithDescription("Number of failed Presidio /analyze requests"),
		metric.WithUnit("{request}"),
	)

	throttleWaitDuration, _ := meter.Float64Histogram(
		"risk.presidio.throttle_wait_duration",
		metric.WithDescription("Time spent waiting for the in-flight byte-budget semaphore before issuing a Presidio /analyze request"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.1, 0.5, 1, 2.5, 5, 10, 30),
	)

	// Empty blocklist allows connections to private IPs (Kubernetes ClusterIPs).
	unsafePolicy, _ := guardian.NewUnsafePolicy(tracerProvider, []string{})
	httpClient := unsafePolicy.PooledClient()

	return &PresidioClient{
		baseURL:              strings.TrimRight(baseURL, "/"),
		httpClient:           httpClient,
		tracer:               tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio"),
		logger:               logger,
		requestTimeout:       analyzeRequestTimeout,
		throttle:             semaphore.NewWeighted(presidioInflightByteBudget),
		throttleBudget:       presidioInflightByteBudget,
		throttleHeartbeat:    presidioThrottleHeartbeatInterval,
		requestDuration:      requestDuration,
		requestSize:          requestSize,
		requestFailures:      requestFailures,
		throttleWaitDuration: throttleWaitDuration,
	}
}

// AnalyzeBatch issues a single POST /analyze for the provided texts. The
// caller decides batch shape; the orchestrator that wraps this client calls
// it with one text per goroutine so failures isolate to a single message.
//
// Concurrent calls are bounded by the process-shared in-flight byte budget.
// onProgress fires while blocked on the semaphore so callers driving a
// Temporal activity heartbeat stay alive even under back-pressure.
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
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	cost := requestByteCost(texts, p.throttleBudget)
	if err := p.acquireThrottle(ctx, cost, onProgress); err != nil {
		return make([][]Finding, n), err
	}
	defer p.throttle.Release(cost)

	results, err := p.analyze(ctx, texts, entities)
	if err != nil {
		// Preserve the caller invariant that the results slice is always
		// indexable up to len(texts) even on failure — the orchestrator
		// reads results[0] when a per-text call returns. Empty slots stay
		// nil so dead-letter logic can treat the message as un-analyzed.
		return make([][]Finding, n), err
	}
	return results, nil
}

// requestByteCost returns the semaphore cost for the request payload, capped
// to the budget so a single oversized message cannot deadlock a fresh client
// whose semaphore has full capacity but cannot satisfy an N>budget request.
func requestByteCost(texts []string, budget int64) int64 {
	var cost int64
	for _, t := range texts {
		cost += int64(len(t))
	}
	if cost < 1 {
		cost = 1
	}
	if cost > budget {
		cost = budget
	}
	return cost
}

// acquireThrottle blocks until the byte semaphore can satisfy the request,
// or ctx is cancelled. It uses a TryAcquire + sleep loop instead of
// semaphore.Acquire(ctx) so it can fire onProgress periodically while
// waiting — Acquire blocks until cancellation and would let the activity
// heartbeat lapse under sustained back-pressure.
func (p *PresidioClient) acquireThrottle(ctx context.Context, cost int64, onProgress func()) error {
	start := time.Now()
	for {
		if p.throttle.TryAcquire(cost) {
			if p.throttleWaitDuration != nil {
				p.throttleWaitDuration.Record(ctx, time.Since(start).Seconds())
			}
			return nil
		}
		if onProgress != nil {
			onProgress()
		}
		if !sleepCtx(ctx, p.throttleHeartbeat) {
			return fmt.Errorf("presidio throttle wait: %w", ctx.Err())
		}
	}
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

	if p.requestSize != nil {
		p.requestSize.Record(ctx, int64(len(body)))
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.baseURL+"/analyze", bytes.NewReader(body))
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
			RuleID:           r.EntityType,
			Description:      "PII detected: " + r.EntityType,
			Match:            match,
			StartPos:         startByte,
			EndPos:           endByte,
			Tags:             []string{"pii", strings.ToLower(r.EntityType)},
			Source:           SourcePresidio,
			Confidence:       r.Score,
			DeadLetterReason: "",
		})
	}
	return findings
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

// isCancelErr is shared by the retry orchestrator to decide whether to keep
// trying after a transient PresidioClient failure.
func isCancelErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
