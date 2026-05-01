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

// presidioRequest is the payload sent to POST /analyze. Text is sent as a
// list so Presidio runs the whole batch through analyze_iterator with a
// single recognizer load instead of reloading on every text.
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

const (
	// presidioDefaultBatchSize is how many texts go into a single POST /analyze.
	// Each call loads recognizers once and analyzes the batch with Presidio's
	// internal multiprocessing, so larger batches mean less overhead per text.
	presidioDefaultBatchSize = 50

	// presidioDefaultMaxConcurrentRequests caps in-flight POST /analyze calls.
	// Presidio's gunicorn worker is sync, so high concurrency just queues up
	// and stalls until pods get killed; a small ceiling avoids that pile-up.
	presidioDefaultMaxConcurrentRequests = 4

	// presidioHeartbeatInterval is how often AnalyzeBatch fires onProgress
	// while requests are in flight. Must be well under the Temporal activity
	// HeartbeatTimeout (30s in DrainRiskAnalysisWorkflow) so a slow Presidio
	// response doesn't trip the timeout.
	presidioHeartbeatInterval = 10 * time.Second
)

// PresidioClient calls the Presidio Analyzer HTTP API.
// Presidio is a trusted cluster-internal service, so the client uses an
// unsafe guardian policy with an empty blocklist. The default policy blocks
// RFC 1918 private ranges (10.0.0.0/8) which Kubernetes ClusterIPs fall into.
type PresidioClient struct {
	baseURL               string
	httpClient            *guardian.HTTPClient
	tracer                trace.Tracer
	logger                *slog.Logger
	batchSize             int
	maxConcurrentRequests int
	requestDuration       metric.Float64Histogram
	requestFailures       metric.Int64Counter
}

// NewPresidioClient creates a client pointing at the given base URL.
func NewPresidioClient(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *PresidioClient {
	return NewPresidioClientWithLimits(baseURL, tracerProvider, meterProvider, logger, presidioDefaultBatchSize, presidioDefaultMaxConcurrentRequests)
}

// NewPresidioClientWithLimits is like NewPresidioClient but allows overriding
// the batch size (texts per HTTP request) and max concurrent in-flight HTTP
// requests. Non-positive values fall back to the package defaults.
func NewPresidioClientWithLimits(baseURL string, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger, batchSize, maxConcurrentRequests int) *PresidioClient {
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

	if batchSize <= 0 {
		batchSize = presidioDefaultBatchSize
	}
	if maxConcurrentRequests <= 0 {
		maxConcurrentRequests = presidioDefaultMaxConcurrentRequests
	}

	return &PresidioClient{
		baseURL:               strings.TrimRight(baseURL, "/"),
		httpClient:            httpClient,
		tracer:                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio"),
		logger:                logger,
		batchSize:             batchSize,
		maxConcurrentRequests: maxConcurrentRequests,
		requestDuration:       requestDuration,
		requestFailures:       requestFailures,
	}
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
		attribute.Int("presidio.text_count", n),
		attribute.Int("presidio.batch_size", p.batchSize),
		attribute.Int("presidio.max_concurrent_requests", p.maxConcurrentRequests),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Heartbeat ticker so Temporal activities calling AnalyzeBatch keep their
	// heartbeat fresh while requests sit in flight. Without this, a slow
	// Presidio response (e.g. queued behind a sync gunicorn worker) can blow
	// past the activity HeartbeatTimeout.
	if onProgress != nil {
		ticker := time.NewTicker(presidioHeartbeatInterval)
		done := make(chan struct{})
		defer func() {
			close(done)
			ticker.Stop()
		}()
		go func() {
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					onProgress()
				}
			}
		}()
	}

	results := make([][]Finding, n)
	sem := make(chan struct{}, p.maxConcurrentRequests)
	var wg sync.WaitGroup

	// Slice the input into batches and fan out batched HTTP calls. Individual
	// chunk failures are logged and skipped; results[idx] stays nil for the
	// affected texts, which the caller treats as "no findings".
	for start := 0; start < n; start += p.batchSize {
		end := min(start+p.batchSize, n)
		chunk := texts[start:end]
		offset := start

		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()
			chunkFindings, err := p.analyzeChunk(ctx, chunk, entities)
			if err != nil {
				p.logger.WarnContext(ctx, "presidio analyze chunk failed, skipping",
					attr.SlogError(err),
				)
				return
			}
			for i, f := range chunkFindings {
				results[offset+i] = f
			}
		}()
	}

	wg.Wait()
	return results, nil
}

// analyzeChunk POSTs a batch of texts to /analyze and decodes the per-text
// findings list. Presidio returns one inner result list per input text when
// the request body's text field is a list.
func (p *PresidioClient) analyzeChunk(ctx context.Context, texts []string, entities []string) (_ [][]Finding, err error) {
	ctx, span := p.tracer.Start(ctx, "presidio.analyze", trace.WithAttributes(
		attribute.Int("presidio.chunk_size", len(texts)),
	))
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

	var raw [][]presidioResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode presidio response: %w", err)
	}
	if len(raw) != len(texts) {
		return nil, fmt.Errorf("presidio response length mismatch: got %d entries for %d inputs", len(raw), len(texts))
	}

	out := make([][]Finding, len(texts))
	totalFindings := 0
	for i, results := range raw {
		// Presidio returns character (rune) offsets, not byte offsets.
		// Convert to runes for correct slicing, then map back to byte positions.
		runes := []rune(texts[i])

		findings := make([]Finding, 0, len(results))
		for _, r := range results {
			// Clamp offsets to valid rune range to prevent out-of-bounds panics.
			s := max(0, min(r.Start, len(runes)))
			e := max(s, min(r.End, len(runes)))

			match := string(runes[s:e])

			// Convert rune offsets to byte offsets for storage.
			startByte := len(string(runes[:s]))
			endByte := len(string(runes[:e]))

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
		out[i] = findings
		totalFindings += len(findings)
	}
	span.SetAttributes(attribute.Int("presidio.findings_count", totalFindings))
	return out, nil
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
