package guardian_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newResiliencePolicy(t *testing.T) *guardian.Policy {
	t.Helper()

	// The default blocklist rejects loopback addresses, which is where
	// httptest servers live.
	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithLimiter(guardian.NewInProcLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t))),
		guardian.WithBreaker(guardian.NewInProcBreaker(testenv.NewLogger(t), testenv.NewMeterProvider(t))),
	)
	require.NoError(t, err)

	return policy
}

func newStatusServer(t *testing.T, status *atomic.Int64) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	t.Cleanup(server.Close)

	return server, &hits
}

func doRequest(t *testing.T, ctx context.Context, client *guardian.HTTPClient, url string) (int, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("do request: %w", err)
	}

	require.NoError(t, resp.Body.Close())

	return resp.StatusCode, nil
}

func TestPolicy_Client_Resilience_BreakerTripsOnServerErrors(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        3,
			Window:               time.Minute,
			Delay:                time.Hour,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	for i := range 3 {
		code, err := doRequest(t, t.Context(), client, server.URL)
		require.NoError(t, err, "request %d should reach the server", i)
		require.Equal(t, http.StatusInternalServerError, code)
	}

	_, err := doRequest(t, t.Context(), client, server.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)

	var denial *guardian.ResilienceError
	require.ErrorAs(t, err, &denial)
	require.Positive(t, denial.RetryAfter)
}

func TestPolicy_Client_Resilience_RecoversWhenServerHeals(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusServiceUnavailable)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                25 * time.Millisecond,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	for range 2 {
		_, err := doRequest(t, t.Context(), client, server.URL)
		require.NoError(t, err)
	}

	_, err := doRequest(t, t.Context(), client, server.URL)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)

	// Once the upstream heals, the half-open trial succeeds and the circuit
	// closes again. Real time governs the open delay here (the request goes
	// through a live httptest server), so poll rather than sleep.
	status.Store(http.StatusOK)
	require.Eventually(t, func() bool {
		code, err := doRequest(t, t.Context(), client, server.URL)
		return err == nil && code == http.StatusOK
	}, 3*time.Second, 5*time.Millisecond)

	code, err := doRequest(t, t.Context(), client, server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)
}

func TestPolicy_Client_Resilience_RateLimited(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusOK)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.PerHour(1),
		Breaker:   guardian.NoBreaker(),
	}))

	code, err := doRequest(t, t.Context(), client, server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	_, err = doRequest(t, t.Context(), client, server.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, guardian.ErrRateLimited)

	var denial *guardian.ResilienceError
	require.ErrorAs(t, err, &denial)
	require.Positive(t, denial.RetryAfter)
}

func TestPolicy_Client_Resilience_SubsetSegmentsRateLimit(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusOK)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.PerHour(1),
		Breaker:   guardian.NoBreaker(),
	}))

	orgA := guardian.WithSubset(t.Context(), "org-a")
	orgB := guardian.WithSubset(t.Context(), "org-b")

	// Each subset gets its own rate limit bucket.
	_, err := doRequest(t, orgA, client, server.URL)
	require.NoError(t, err)
	_, err = doRequest(t, orgB, client, server.URL)
	require.NoError(t, err)

	_, err = doRequest(t, orgA, client, server.URL)
	require.ErrorIs(t, err, guardian.ErrRateLimited)
	_, err = doRequest(t, orgB, client, server.URL)
	require.ErrorIs(t, err, guardian.ErrRateLimited)
}

func TestPolicy_Client_Resilience_SpanPartitionAttributes(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	policy, err := guardian.NewUnsafePolicy(
		tracerProvider,
		[]string{},
		guardian.WithLimiter(guardian.NewInProcLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t))),
	)
	require.NoError(t, err)

	var status atomic.Int64
	status.Store(http.StatusOK)
	server, _ := newStatusServer(t, &status)

	client := policy.Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.PerSecond(10),
		Breaker:   guardian.NoBreaker(),
	}))

	code, err := doRequest(t, guardian.WithSubset(t.Context(), "org-a"), client, server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	// The outbound HTTP client span must carry every resilience dimension so
	// traces can be sliced the same way as the resilience metrics.
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	var httpSpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.SpanKind() == trace.SpanKindClient {
			httpSpan = span
		}
	}
	require.NotNil(t, httpSpan, "expected an HTTP client span to be recorded")

	attrs := make(map[attribute.Key]string)
	for _, kv := range httpSpan.Attributes() {
		attrs[kv.Key] = kv.Value.AsString()
	}
	require.Equal(t, "test-upstream", attrs[attr.ResilienceNamespaceKey])
	require.Equal(t, serverURL.Hostname()+":"+serverURL.Port(), attrs[attr.ResiliencePartitionKey])
	require.Equal(t, "org-a", attrs[attr.ResilienceSubsetKey])
}

func TestPolicy_Client_Resilience_BreakerIgnoresSubset(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                time.Hour,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	orgA := guardian.WithSubset(t.Context(), "org-a")
	for range 2 {
		_, err := doRequest(t, orgA, client, server.URL)
		require.NoError(t, err)
	}

	// Breaker state is host-scoped: another subset is denied too.
	orgB := guardian.WithSubset(t.Context(), "org-b")
	_, err := doRequest(t, orgB, client, server.URL)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)
}

func TestPolicy_Client_Resilience_BreakerIncludeSubset(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                time.Hour,
			SuccessThreshold:     1,
			IncludeSubset:        true,
		},
	}))

	orgA := guardian.WithSubset(t.Context(), "org-a")
	for range 2 {
		_, err := doRequest(t, orgA, client, server.URL)
		require.NoError(t, err)
	}

	_, err := doRequest(t, orgA, client, server.URL)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)

	// With IncludeSubset the breaker is scoped per subset, so org-b still
	// reaches the (failing) server.
	orgB := guardian.WithSubset(t.Context(), "org-b")
	code, err := doRequest(t, orgB, client, server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, code)
}

func TestPolicy_Client_Resilience_ClientErrorsDoNotTrip(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusNotFound)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                time.Hour,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	// 4xx responses prove the upstream is alive and never open the circuit.
	for i := range 5 {
		code, err := doRequest(t, t.Context(), client, server.URL)
		require.NoError(t, err, "request %d should be admitted", i)
		require.Equal(t, http.StatusNotFound, code)
	}
}

func TestPolicy_Client_Resilience_TooManyRequestsTrips(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusTooManyRequests)
	server, _ := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                time.Hour,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	// 429 means the upstream is shedding load and counts as failure.
	for range 2 {
		_, err := doRequest(t, t.Context(), client, server.URL)
		require.NoError(t, err)
	}

	_, err := doRequest(t, t.Context(), client, server.URL)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)
}

func TestPolicy_Client_Resilience_RetriesDoNotHammerOpenCircuit(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	server, hits := newStatusServer(t, &status)

	client := newResiliencePolicy(t).Client(
		guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
			Partition: nil,
			Limit:     guardian.NoLimit(),
			Breaker: guardian.BreakerPolicy{
				FailureRateThreshold: 1,
				MinThroughput:        1,
				Window:               time.Minute,
				Delay:                time.Hour,
				SuccessThreshold:     1,
				IncludeSubset:        false,
			},
		}),
		guardian.WithRetryConfig(&guardian.RetryConfig{
			WaitMin:      time.Millisecond,
			WaitMax:      2 * time.Millisecond,
			MaxAttempts:  3,
			CheckRetry:   nil,
			Backoff:      nil,
			ErrorHandler: nil,
			PrepareRetry: nil,
		}),
	)

	// The first attempt's failure opens the circuit; the retry is denied
	// before reaching the network and is not retried further.
	_, err := doRequest(t, t.Context(), client, server.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)
	require.Equal(t, int64(1), hits.Load(), "the open circuit must not be retried against the server")
}

func TestPolicy_Client_Resilience_DefaultsAreNoop(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	server, hits := newStatusServer(t, &status)

	// Without WithLimiter/WithBreaker the resilience configuration is inert:
	// even a tight limit and an always-tripping breaker policy deny nothing.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	client := policy.Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.PerHour(1),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        1,
			Window:               time.Minute,
			Delay:                time.Hour,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	for i := range 5 {
		code, err := doRequest(t, t.Context(), client, server.URL)
		require.NoError(t, err, "request %d should be admitted", i)
		require.Equal(t, http.StatusInternalServerError, code)
	}
	require.Equal(t, int64(5), hits.Load())
}

func TestPolicy_Client_Resilience_WithRedisLimiter(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusOK)
	server, _ := newStatusServer(t, &status)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithLimiter(guardian.NewRedisRateLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClient)),
	)
	require.NoError(t, err)

	// Random resilience name: redis bucket state outlives the test binary,
	// so a fixed partition would fail on repeated runs (-count) against the
	// same container.
	client := policy.Client(guardian.WithResilience(uuid.NewString(), guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.PerHour(1),
		Breaker:   guardian.NoBreaker(),
	}))

	code, err := doRequest(t, t.Context(), client, server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	_, err = doRequest(t, t.Context(), client, server.URL)
	require.ErrorIs(t, err, guardian.ErrRateLimited)
}

// denyAllBreaker rejects every admission; it proves WithBreaker swaps the
// Policy's breaker implementation.
type denyAllBreaker struct{}

var _ guardian.Breaker = denyAllBreaker{}

func (denyAllBreaker) Allow(ctx context.Context, key guardian.Partition, policy guardian.BreakerPolicy) (guardian.BreakerResult, error) {
	return guardian.BreakerResult{
		State:      guardian.BreakerStateOpen,
		Allowed:    false,
		RetryAfter: time.Minute,
		Report:     func(bool) {},
	}, nil
}

func TestPolicy_Client_Resilience_WithBreaker(t *testing.T) {
	t.Parallel()

	var status atomic.Int64
	status.Store(http.StatusOK)
	server, hits := newStatusServer(t, &status)

	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithBreaker(denyAllBreaker{}),
	)
	require.NoError(t, err)

	client := policy.Client(guardian.WithResilience("test-upstream", guardian.ResilienceConfig{
		Partition: nil,
		Limit:     guardian.NoLimit(),
		Breaker: guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        1,
			Window:               time.Minute,
			Delay:                time.Minute,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		},
	}))

	_, err = doRequest(t, t.Context(), client, server.URL)
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)
	require.Equal(t, int64(0), hits.Load(), "denied requests must never reach the server")
}

func TestPartitionByHost(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://Example.COM/some/path", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"example.com", "443"}, guardian.PartitionByHost()(req))

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/other", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"example.com", "80"}, guardian.PartitionByHost()(req))

	// IPv6 literals pass through verbatim: the partition key encoding is
	// self-delimiting, so ':' needs no escaping.
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "https://[::1]:8443/x", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"::1", "8443"}, guardian.PartitionByHost()(req))
}

func TestPartition_Structure(t *testing.T) {
	t.Parallel()

	key := guardian.NewPartition("svc", "example.com", "443")
	require.Equal(t, "svc", key.Namespace())
	require.Equal(t, "example.com:443", key.Partition())
	require.Empty(t, key.Subset())
	require.Equal(t, "pk:3:svc:11:example.com:3:443", key.String())

	sub := key.WithSubset("org-a").WithSubset("proj-b")
	require.Equal(t, "svc", sub.Namespace())
	require.Equal(t, "example.com:443", sub.Partition())
	require.Equal(t, "org-a:proj-b", sub.Subset())
	require.Equal(t, "pk:3:svc:11:example.com:3:443|5:org-a:6:proj-b", sub.String())

	// WithSubset copies: the original key is unchanged.
	require.Empty(t, key.Subset())

	require.Equal(t, "pk:3:svc", guardian.NewPartition("svc").String())
	require.Equal(t, "pk:3:svc|5:org-a", guardian.NewPartition("svc").WithSubset("org-a").String())
}

func TestResilienceError_Unwrap(t *testing.T) {
	t.Parallel()

	err := &guardian.ResilienceError{Reason: guardian.ErrCircuitOpen, RetryAfter: time.Second}
	require.ErrorIs(t, err, guardian.ErrCircuitOpen)
	require.NotErrorIs(t, err, guardian.ErrRateLimited)
}
