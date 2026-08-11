package guardian

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ResilienceError is returned by clients configured with [WithResilience]
// when a request is denied before reaching the network. It unwraps to
// [ErrCircuitOpen] or [ErrRateLimited]; http.Client wraps it in a
// [*net/url.Error], so match with [errors.Is] and extract with [errors.AsType].
type ResilienceError struct {
	// Reason is ErrCircuitOpen or ErrRateLimited.
	Reason error

	// RetryAfter is the time until the request may be permitted again. Zero
	// means retry timing is unknown (e.g. a half-open circuit at trial
	// capacity).
	RetryAfter time.Duration
}

func (e *ResilienceError) Error() string {
	return fmt.Sprintf("request denied: %s: retry after %s", e.Reason, e.RetryAfter)
}

func (e *ResilienceError) Unwrap() error { return e.Reason }

// PartitionStrategy derives the base partition key segments for a request.
// Segments may contain any characters: the partition key encoding is
// self-delimiting (see [Partition.String]), so nothing needs escaping.
type PartitionStrategy func(req *http.Request) []string

// PartitionByHost is the default [PartitionStrategy]: it partitions by the
// request's hostname and port, so resilience state is scoped to upstream
// endpoint health.
func PartitionByHost() PartitionStrategy {
	return func(req *http.Request) []string {
		port := conv.Default(req.URL.Port(), conv.Ternary(req.URL.Scheme == "https", "443", "80"))

		return []string{strings.ToLower(req.URL.Hostname()), port}
	}
}

type subsetContextKey struct{}

// WithSubset returns a context whose requests are partitioned more finely:
// the given segments are appended to the partition key derived by the
// client's [PartitionStrategy]. Multiple calls compose by appending, so a
// subset can only ever narrow a partition, never merge two. Rate limit keys
// always include subset segments; breaker keys include them only when
// [BreakerPolicy.IncludeSubset] is set.
func WithSubset(ctx context.Context, segments ...string) context.Context {
	if len(segments) == 0 {
		return ctx
	}

	existing := subsetFrom(ctx)
	merged := make([]string, 0, len(existing)+len(segments))
	merged = append(merged, existing...)
	merged = append(merged, segments...)

	return context.WithValue(ctx, subsetContextKey{}, merged)
}

func subsetFrom(ctx context.Context) []string {
	segments, _ := ctx.Value(subsetContextKey{}).([]string)
	return segments
}

// NoLimit is the zero [Limit], which disables rate limiting for the client.
// Use it instead of spelling out a zero literal so that opt-outs stay
// discoverable with find-all-references.
func NoLimit() Limit {
	var zero Limit
	return zero
}

// NoBreaker is the zero [BreakerPolicy], which disables circuit breaking for
// the client. Use it instead of spelling out a zero literal so that opt-outs
// stay discoverable with find-all-references.
func NoBreaker() BreakerPolicy {
	var zero BreakerPolicy
	return zero
}

// ResilienceConfig configures the resilience layer added by [WithResilience].
type ResilienceConfig struct {
	// Partition derives the base partition key segments from each request.
	// Defaults to [PartitionByHost].
	Partition PartitionStrategy

	// Limit rate limits requests per partition. The zero value — spelled
	// [NoLimit] — disables rate limiting. The limiter key always includes
	// the full partition key, including subset, segments added: quota is a
	// per-caller concern.
	Limit Limit

	// Breaker circuit breaks requests per partition. The zero value — spelled
	// [NoBreaker] — disables circuit breaking. The breaker key excludes
	// subset segments unless [BreakerPolicy.IncludeSubset] is set: upstream
	// health is a per-endpoint concern.
	Breaker BreakerPolicy
}

type resilienceOptions struct {
	name   string
	config ResilienceConfig
}

// WithResilience layers rate limiting and circuit breaking into the client's
// transport under the given name. The name is the partition key namespace
// and is exposed as the gram.resilience.namespace attribute — alongside
// gram.resilience.partition and gram.resilience.subset — on outbound HTTP
// spans and resilience metrics, so use a stable, descriptive
// dependency name (e.g. "external-mcp"). Limiter and breaker state live on
// the [Policy], so short-lived clients built from the same Policy share
// state.
//
// Enforcement is opt-in at the Policy level: the defaults are [NoopLimiter]
// and [NoopBreaker], which admit everything, so this configuration is inert
// until the Policy is constructed with [WithLimiter] and/or [WithBreaker].
//
// Denied requests fail with a [ResilienceError] before reaching the network;
// match with [errors.Is] against [ErrCircuitOpen] or [ErrRateLimited]. When
// combined with retries, denials are not retried.
//
// Outcomes are classified at response-header time: transport errors (except
// context cancellation), 5xx, and 429 count as breaker failures; everything
// else, including other 4xx, counts as success. Failures mid-body are not
// observed.
func WithResilience(name string, config ResilienceConfig) func(*httpClientOptions) {
	return func(o *httpClientOptions) {
		o.resilience = &resilienceOptions{name: name, config: config}
	}
}

// resilienceTransport admits requests through a rate limiter and a circuit
// breaker before delegating to the next round tripper. It is stateless: the
// limiter and breaker it references belong to the Policy that built it.
type resilienceTransport struct {
	next    http.RoundTripper
	name    string
	config  ResilienceConfig
	limiter Limiter
	breaker Breaker
}

var _ http.RoundTripper = (*resilienceTransport)(nil)

func (t *resilienceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	strategy := t.config.Partition
	if strategy == nil {
		strategy = PartitionByHost()
	}

	key := NewPartition(t.name, strategy(req)...)
	subset := subsetFrom(ctx)

	// The outbound HTTP span does not exist yet — otelhttp creates it inside
	// t.next — so hand the full partition identity down via the request
	// context for resilienceSpanAnnotator to stamp on the span.
	req = req.WithContext(context.WithValue(ctx, partitionContextKey{}, key.WithSubset(subset...)))

	limit := t.config.Limit
	hasRateLimit := limit != NoLimit()

	if hasRateLimit {
		result, err := t.limiter.AllowN(ctx, key.WithSubset(subset...), limit, 1)
		if err != nil {
			return nil, fmt.Errorf("resilience: rate limit check: %w", err)
		}
		if result.Allowed == 0 {
			return nil, &ResilienceError{Reason: ErrRateLimited, RetryAfter: result.RetryAfter}
		}
	}

	breaker := t.config.Breaker
	hasBreaker := breaker != NoBreaker()

	report := func(bool) {}
	if hasBreaker {
		breakerKey := key
		if breaker.IncludeSubset {
			breakerKey = key.WithSubset(subset...)
		}

		result, err := t.breaker.Allow(ctx, breakerKey, breaker)
		if err != nil {
			return nil, fmt.Errorf("resilience: circuit breaker check: %w", err)
		}
		if !result.Allowed {
			return nil, &ResilienceError{Reason: ErrCircuitOpen, RetryAfter: result.RetryAfter}
		}

		report = result.Report
	}

	resp, err := t.next.RoundTrip(req)
	report(classifyFailure(resp, err))
	if err != nil {
		return nil, fmt.Errorf("resilience round trip: %w", err)
	}

	return resp, nil
}

// partitionContextKey carries the fully derived [Partition] from
// [resilienceTransport] to the [resilienceSpanAnnotator] sitting inside
// otelhttp.
type partitionContextKey struct{}

// resilienceSpanAnnotator sits between otelhttp and the base transport, where
// the request context carries the outbound HTTP span, and stamps the
// resilience partition dimensions on it so traces can be sliced the same way
// as the resilience metrics.
type resilienceSpanAnnotator struct {
	next http.RoundTripper
}

var _ http.RoundTripper = (*resilienceSpanAnnotator)(nil)

func (a *resilienceSpanAnnotator) RoundTrip(req *http.Request) (*http.Response, error) {
	if key, ok := req.Context().Value(partitionContextKey{}).(Partition); ok {
		trace.SpanFromContext(req.Context()).SetAttributes(
			attr.ResilienceNamespace(key.Namespace()),
			attr.ResiliencePartition(key.Partition()),
			attr.ResilienceSubset(key.Subset()),
		)
	}

	resp, err := a.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("annotated round trip: %w", err)
	}

	return resp, nil
}

// classifyFailure reports whether an outcome counts against the circuit
// breaker. Transport errors mean the upstream is unreachable and 5xx/429 mean
// it is failing or shedding load; other statuses — including 4xx, which
// prove the upstream is alive — and context cancellation count as successes.
func classifyFailure(resp *http.Response, err error) bool {
	if err != nil {
		return !errors.Is(err, context.Canceled)
	}

	return resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests
}

// noRetryOnResilienceDenial stops retryablehttp from retrying requests denied
// by the resilience layer: retrying an open circuit or an exhausted rate
// limit only hammers a dependency already known to be unavailable.
func noRetryOnResilienceDenial(next retryablehttp.CheckRetry) retryablehttp.CheckRetry {
	if next == nil {
		next = retryablehttp.DefaultRetryPolicy
	}

	return func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if errors.Is(err, ErrCircuitOpen) || errors.Is(err, ErrRateLimited) {
			return false, err
		}

		return next(ctx, resp, err)
	}
}
