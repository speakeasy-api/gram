package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// SourcePresidio is the source label written on every risk_results row
// produced by the Presidio path, including dead-letter sentinels.
const SourcePresidio = "presidio"

// DeadLetterRuleID was moved to rules.go to live alongside the other
// canonical rule id constants. Keep the comment here as a pointer for
// callers grepping presidio.go.

// DescribePresidioEntity returns the canonical (rule_id, description) for
// a Presidio finding. rawEntityType is Presidio's UPPER_SNAKE entity name.
func DescribePresidioEntity(rawEntityType string) (string, string) {
	ruleID := CanonicalPresidioRuleID(rawEntityType)
	desc, ok := presidioEntityDescriptions[ruleID]
	if !ok {
		desc = "Identified potentially sensitive personal information."
	}
	return guard(ruleID), desc
}

// DescribePresidioDeadLetter returns the canonical (rule_id, description)
// for a Presidio dead-letter sentinel row.
func DescribePresidioDeadLetter() (string, string) {
	return guard(DeadLetterRuleID), "Presidio could not analyze this message after exhausting its retry budget."
}

// presidioEntityDescriptions maps canonical Presidio rule ids to their
// human-readable, source-agnostic description. Lookup miss falls through
// to a generic PII string in DescribePresidioEntity.
var presidioEntityDescriptions = map[string]string{
	// Financial.
	prefixPII + "credit_card":    "Identified a credit card number, which may expose cardholder data.",
	prefixPII + "iban_code":      "Identified an International Bank Account Number, which may expose financial account data.",
	prefixPII + "us_bank_number": "Identified a US bank account number, which may expose financial account data.",
	prefixPII + "crypto":         "Identified a cryptocurrency wallet address.",

	// PII.
	prefixPII + "email_address": "Identified an email address.",
	prefixPII + "phone_number":  "Identified a telephone number.",
	prefixPII + "ip_address":    "Identified an IP address.",
	prefixPII + "mac_address":   "Identified a network interface (MAC) address.",
	prefixPII + "person":        "Identified a person name.",
	prefixPII + "location":      "Identified a location reference.",
	prefixPII + "date_time":     "Identified a date or time reference that may correlate with a person.",
	prefixPII + "nrp":           "Identified a nationality, religious, or political reference.",
	prefixPII + "url":           "Identified a URL that may carry sensitive context.",

	// Government identifiers.
	prefixPII + "us_ssn":            "Identified a US Social Security Number.",
	prefixPII + "us_passport":       "Identified a US passport number.",
	prefixPII + "us_driver_license": "Identified a US driver license number.",
	prefixPII + "us_itin":           "Identified a US Individual Taxpayer Identification Number.",
	prefixPII + "uk_nhs":            "Identified a UK National Health Service number.",
	prefixPII + "uk_nino":           "Identified a UK National Insurance Number.",
	prefixPII + "uk_passport":       "Identified a UK passport number.",
	prefixPII + "es_nif":            "Identified a Spanish personal tax identifier (NIF).",
	prefixPII + "it_fiscal_code":    "Identified an Italian personal fiscal code.",
	prefixPII + "au_tfn":            "Identified an Australian Tax File Number.",
	prefixPII + "in_pan":            "Identified an Indian Permanent Account Number.",
	prefixPII + "in_aadhaar":        "Identified an Indian Aadhaar identifier.",
	prefixPII + "sg_nric_fin":       "Identified a Singapore NRIC or FIN identifier.",

	// Healthcare.
	prefixPII + "medical_license":               "Identified a medical license number, which may expose protected health information.",
	prefixPII + "us_mbi":                        "Identified a US Medicare Beneficiary Identifier.",
	prefixPII + "us_npi":                        "Identified a US National Provider Identifier.",
	prefixPII + "medical_disease_disorder":      "Identified a disease or disorder reference that may expose protected health information.",
	prefixPII + "medical_medication":            "Identified a medication or drug reference that may expose protected health information.",
	prefixPII + "medical_therapeutic_procedure": "Identified a treatment or diagnostic procedure that may expose protected health information.",
	prefixPII + "medical_clinical_event":        "Identified a clinical event that may expose protected health information.",
	prefixPII + "medical_biological_attribute":  "Identified a biological attribute that may expose protected health information.",
	prefixPII + "medical_family_history":        "Identified a family medical history reference that may expose protected health information.",
}

// PIIScanner detects personally identifiable information in text.
type PIIScanner interface {
	// AnalyzeBatch sends multiple texts to the PII analyzer and returns
	// findings for each. The outer slice is indexed by input position.
	// When entities is non-empty, only those entity types are detected.
	//
	// Permanent per-message failures surface as a single Finding with
	// DeadLetterReason populated rather than as an error; the returned
	// error is non-nil only on outer-ctx cancellation.
	AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) ([][]Finding, error)
}

// presidioMaxMessageBytes serves a dual role:
//
//  1. It caps the size of a single text we will hand to Presidio. Anything
//     longer is truncated to a UTF-8 boundary before the request and a
//     warning is logged so operators can spot the offender.
//  2. It is the budget of the in-flight byte semaphore that bounds
//     concurrent /analyze requests against a single PresidioClient. Since
//     every request is capped at this size, the budget equals the cap and
//     calls serialize.
//
// Sized 2026-05-13 from production observations: ≤2 KB completes in <2 s,
// 80 KB takes ~30 s, 1 MB crashes the analyzer. 50 KB keeps us in the
// linear-latency band while we work on Presidio capacity. Raise (and
// optionally split into separate per-message-cap vs in-flight-budget
// constants) once the analyzer scales.
const presidioMaxMessageBytes = 50 * 1024

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

const (
	// retryMaxAttempts caps how many times a single text is sent to Presidio
	// before giving up and dead-lettering.
	retryMaxAttempts = 3

	// retryBaseBackoff is the initial inter-attempt sleep. Subsequent
	// attempts use full-jittered exponential backoff up to retryMaxBackoff.
	retryBaseBackoff = 100 * time.Millisecond

	// retryMaxBackoff caps the per-attempt jittered backoff so a stalled
	// upstream cannot drag the whole batch past the activity heartbeat.
	retryMaxBackoff = 1 * time.Second
)

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

// PresidioClient is the production PIIScanner implementation, calling the
// Presidio Analyzer HTTP API.
//
// AnalyzeBatch fans the input texts out to per-text goroutines and retries
// each one up to retryMaxAttempts times. Total in-flight HTTP request bytes
// are bounded by a process-shared byte-budget semaphore, and while blocked
// on the semaphore the client calls onProgress on a fixed cadence so the
// Temporal activity heartbeat stays alive.
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
	maxAttempts          int
	baseBackoff          time.Duration
	requestDuration      metric.Float64Histogram
	requestSize          metric.Int64Histogram
	requestFailures      metric.Int64Counter
	throttleWaitDuration metric.Float64Histogram
	attemptFailures      metric.Int64Counter
	deadLetters          metric.Int64Counter
	truncations          metric.Int64Counter
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

	attemptFailures, _ := meter.Int64Counter(
		"risk.presidio.attempt_failures",
		metric.WithDescription("Number of per-message Presidio attempts that failed before retry or dead-letter"),
		metric.WithUnit("{attempt}"),
	)

	deadLetters, _ := meter.Int64Counter(
		"risk.presidio.dead_letters",
		metric.WithDescription("Number of messages dead-lettered after exhausting the Presidio retry budget"),
		metric.WithUnit("{message}"),
	)

	truncations, _ := meter.Int64Counter(
		"risk.presidio.truncations",
		metric.WithDescription("Number of messages truncated to presidioMaxMessageBytes before being sent to Presidio"),
		metric.WithUnit("{message}"),
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
		throttle:             semaphore.NewWeighted(presidioMaxMessageBytes),
		throttleBudget:       presidioMaxMessageBytes,
		throttleHeartbeat:    presidioThrottleHeartbeatInterval,
		maxAttempts:          retryMaxAttempts,
		baseBackoff:          retryBaseBackoff,
		requestDuration:      requestDuration,
		requestSize:          requestSize,
		requestFailures:      requestFailures,
		throttleWaitDuration: throttleWaitDuration,
		attemptFailures:      attemptFailures,
		deadLetters:          deadLetters,
		truncations:          truncations,
	}
}

// AnalyzeBatch fans the input texts out to per-text goroutines, retries each
// up to maxAttempts times against /analyze, and returns a results slice
// indexed by input position. Texts that exhaust the retry budget are
// returned as a single Finding with DeadLetterReason set so the caller can
// persist a dead-letter row.
//
// Returns a non-nil error only on outer-ctx cancellation. Per-message
// failures surface as DeadLetterReason sentinels so the rest of the batch
// can still write.
func (p *PresidioClient) AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) (_ [][]Finding, err error) {
	n := len(texts)
	if n == 0 {
		return nil, nil
	}

	// Short-circuit when every input is empty: /analyze would either 500 on
	// the empty array or return no findings, so we save the HTTP round-trip
	// and avoid the byte-semaphore wait.
	allEmpty := true
	for _, t := range texts {
		if t != "" {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		return make([][]Finding, n), nil
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
		attribute.Int("presidio.max_attempts", p.maxAttempts),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	results := make([][]Finding, n)
	var deadLetters atomic.Int64

	var wg sync.WaitGroup
	for i, text := range texts {
		wg.Go(func() {
			finding, dl := p.analyzeOne(ctx, i, text, entities, onProgress)
			results[i] = finding
			if dl {
				deadLetters.Add(1)
			}
		})
	}
	wg.Wait()

	span.SetAttributes(attribute.Int("presidio.dead_letters", int(deadLetters.Load())))

	if ctx.Err() != nil {
		return results, fmt.Errorf("presidio analyze batch canceled: %w", ctx.Err())
	}
	return results, nil
}

// analyzeOne runs the retry loop for a single text. Returns the per-text
// findings slice (real findings, or a single dead-letter sentinel) and a
// boolean indicating whether the result was a dead letter.
func (p *PresidioClient) analyzeOne(ctx context.Context, idx int, text string, entities []string, onProgress func()) ([]Finding, bool) {
	if onProgress != nil {
		onProgress()
	}

	if originalSize := len(text); originalSize > presidioMaxMessageBytes {
		text = truncateAtRuneBoundary(text, presidioMaxMessageBytes)
		p.logger.WarnContext(ctx, "presidio: truncating oversized message",
			attr.SlogRiskScanTextSize(originalSize),
			attr.SlogRiskScanBatchIndex(idx),
		)
		if p.truncations != nil {
			p.truncations.Add(ctx, 1)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, false
		}

		findings, err := p.analyzeOnce(ctx, text, entities, onProgress)
		if err == nil {
			return findings, false
		}

		lastErr = err
		if p.attemptFailures != nil {
			p.attemptFailures.Add(ctx, 1)
		}

		// Bail only when the outer ctx is cancelled — inner per-request
		// timeouts (analyzeRequestTimeout) and other transient errors
		// should consume retry budget instead.
		if ctx.Err() != nil {
			return nil, false
		}

		if attempt == p.maxAttempts {
			break
		}

		p.logger.WarnContext(ctx, "presidio analyze attempt failed, retrying",
			attr.SlogError(err),
			attr.SlogRiskScanAttempt(attempt),
			attr.SlogRiskScanMaxAttempts(p.maxAttempts),
			attr.SlogRiskScanTextSize(len(text)),
		)

		if !sleepCtx(ctx, computeRetryBackoff(p.baseBackoff, attempt-1)) {
			return nil, false
		}
	}

	p.logger.WarnContext(ctx, "presidio dead-letter: exhausted retry budget",
		attr.SlogError(lastErr),
		attr.SlogRiskScanMaxAttempts(p.maxAttempts),
		attr.SlogRiskScanTextSize(len(text)),
		attr.SlogRiskScanBatchIndex(idx),
	)
	if p.deadLetters != nil {
		p.deadLetters.Add(ctx, 1)
	}

	ruleID, description := DescribePresidioDeadLetter()
	return []Finding{{
		Source:           SourcePresidio,
		RuleID:           ruleID,
		Description:      description,
		Match:            "",
		StartPos:         0,
		EndPos:           0,
		Tags:             nil,
		Confidence:       0,
		DeadLetterReason: lastErr.Error(),
		toolCallID:       "",
	}}, true
}

// analyzeOnce issues a single POST /analyze for one text, gated by the
// byte-budget semaphore. Returns Presidio's findings on 200, or an error
// (HTTP failure, non-200, decode failure) on anything else. No retry.
func (p *PresidioClient) analyzeOnce(ctx context.Context, text string, entities []string, onProgress func()) ([]Finding, error) {
	cost := requestByteCost(text, p.throttleBudget)
	if err := p.acquireThrottle(ctx, cost, onProgress); err != nil {
		return nil, err
	}
	defer p.throttle.Release(cost)

	return p.analyze(ctx, text, entities)
}

// requestByteCost returns the semaphore cost for a single-text request,
// capped to the budget so an oversized message cannot deadlock a fresh
// client whose semaphore has full capacity but cannot satisfy an N>budget
// request. Floor of 1 keeps the cost positive for semaphore.TryAcquire.
func requestByteCost(text string, budget int64) int64 {
	cost := min(max(int64(len(text)), 1), budget)
	return cost
}

// acquireThrottle blocks until the byte semaphore can satisfy the request,
// or ctx is cancelled. Uses TryAcquire + sleep instead of
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
		Text:     []string{text},
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
	if len(results) != 1 {
		return nil, fmt.Errorf("presidio returned %d result sets for 1 text", len(results))
	}

	findings := convertPresidioFindings(text, results[0])
	span.SetAttributes(attribute.Int("presidio.findings_count", len(findings)))
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

		if isPresidioFalsePositive(r.EntityType, match) {
			continue
		}

		// Convert rune offsets to byte offsets for storage.
		startByte := len(string(runes[:start]))
		endByte := len(string(runes[:end]))

		ruleID, description := DescribePresidioEntity(r.EntityType)
		findings = append(findings, Finding{
			RuleID:           ruleID,
			Description:      description,
			Match:            match,
			StartPos:         startByte,
			EndPos:           endByte,
			Tags:             []string{"pii"},
			Source:           SourcePresidio,
			Confidence:       r.Score,
			DeadLetterReason: "",
			toolCallID:       "",
		})
	}
	return findings
}

// ipv6ShortFormFP matches IPv6 strings of the form "<hex>::" — a single
// hex group of up to four chars followed immediately by "::" and nothing
// else (e.g. "b::", "dead::", "1::"). Production risk_results analysis
// showed Presidio greedily flagging these as IP_ADDRESS whenever the
// pattern appeared in code, hex dumps, or text, and none of them
// represent an address anyone meaningfully uses.
var ipv6ShortFormFP = regexp.MustCompile(`(?i)^[0-9a-f]{1,4}::$`)

// isPresidioFalsePositive filters Presidio matches the policy author
// would treat as noise. It currently drops:
//   - the IPv6/IPv4 unspecified address in any spelling (`::`, `::0`,
//     `0:0:0:0:0:0:0:0`, `0.0.0.0`), via net/netip;
//   - loopback addresses (`127.0.0.0/8`, `::1`), via net/netip;
//   - IPv6 short-form strings of shape "<hex>::" (e.g. "b::", "dead::"),
//     which dominate Presidio's IP_ADDRESS noise on prod.
func isPresidioFalsePositive(entityType, match string) bool {
	if entityType != "IP_ADDRESS" {
		return false
	}
	trimmed := strings.TrimSpace(match)
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		if addr.IsUnspecified() || addr.IsLoopback() {
			return true
		}
	}
	if ipv6ShortFormFP.MatchString(trimmed) {
		return true
	}
	return false
}

// computeRetryBackoff returns a full-jittered exponential backoff for the
// given attempt index (0-based): uniform in [0, min(cap, base*2^attempt)).
// Returns 0 when base is 0 so tests can disable the wait.
func computeRetryBackoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	backoff := base
	for range attempt {
		backoff *= 2
		if backoff >= retryMaxBackoff {
			backoff = retryMaxBackoff
			break
		}
	}
	return time.Duration(rand.Int64N(int64(backoff))) // #nosec G404 -- jitter, not security-sensitive
}

// truncateAtRuneBoundary returns the longest prefix of s whose byte length is
// <= n and that does not split a UTF-8 rune. Returns s unchanged when it
// already fits.
func truncateAtRuneBoundary(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
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

func isCancelErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// StubPIIScanner is a no-op implementation for environments without Presidio.
type StubPIIScanner struct{}

func (s *StubPIIScanner) AnalyzeBatch(_ context.Context, texts []string, _ []string, _ func()) ([][]Finding, error) {
	return make([][]Finding, len(texts)), nil
}
