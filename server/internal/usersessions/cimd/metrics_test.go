package cimd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newObservedResolver builds a doc server plus a resolver whose metrics are
// readable through the returned ManualReader.
func newObservedResolver(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Resolver, *sdkmetric.ManualReader) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	resolver := newResolver(newFetchClientFrom(srv.Client()), meterProvider, testenv.NewLogger(t))
	return srv, resolver, reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

func counterPoints(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()

	m, ok := findMetric(rm, name)
	require.True(t, ok, "missing metric %q", name)
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "metric %q is not an int64 sum", name)
	return sum.DataPoints
}

func requireAttr(t *testing.T, set attribute.Set, key attribute.Key, want string) {
	t.Helper()

	value, ok := set.Value(key)
	require.True(t, ok, "missing attribute %q", key)
	require.Equal(t, want, value.AsString())
}

func requireNoAttr(t *testing.T, set attribute.Set, key attribute.Key) {
	t.Helper()

	_, ok := set.Value(key)
	require.False(t, ok, "unexpected attribute %q", key)
}

func TestResolverMetrics_Success(t *testing.T) {
	t.Parallel()

	var clientID string
	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(validDocumentJSON(clientID)); err != nil {
			t.Errorf("encode document: %v", err)
		}
	})
	clientID = srv.URL + "/client.json"
	srvURL, err := url.Parse(srv.URL)
	require.NoError(t, err)
	origin := srvURL.Host

	_, err = resolver.Resolve(t.Context(), clientID, noCache)
	require.NoError(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	require.Equal(t, int64(1), attempts[0].Value)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultSuccess))
	requireAttr(t, attempts[0].Attributes, attr.CIMDOriginKey, origin)

	durationMetric, ok := findMetric(rm, meterFetchDurationSeconds)
	require.True(t, ok)
	durationHistogram, ok := durationMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, durationHistogram.DataPoints, 1)
	require.Equal(t, uint64(1), durationHistogram.DataPoints[0].Count)
	requireAttr(t, durationHistogram.DataPoints[0].Attributes, attr.OutcomeKey, string(fetchResultSuccess))
	requireAttr(t, durationHistogram.DataPoints[0].Attributes, attr.CIMDOriginKey, origin)

	sizeMetric, ok := findMetric(rm, meterFetchResponseSize)
	require.True(t, ok)
	sizeHistogram, ok := sizeMetric.Data.(metricdata.Histogram[int64])
	require.True(t, ok)
	require.Len(t, sizeHistogram.DataPoints, 1)
	require.Equal(t, uint64(1), sizeHistogram.DataPoints[0].Count)
	require.Positive(t, sizeHistogram.DataPoints[0].Sum)
	requireAttr(t, sizeHistogram.DataPoints[0].Attributes, attr.CIMDOriginKey, origin)
}

func TestResolverMetrics_FetchError(t *testing.T) {
	t.Parallel()

	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.Error(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultFetchError))

	durationMetric, ok := findMetric(rm, meterFetchDurationSeconds)
	require.True(t, ok)
	durationHistogram, ok := durationMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, durationHistogram.DataPoints, 1)
	requireAttr(t, durationHistogram.DataPoints[0].Attributes, attr.OutcomeKey, string(fetchResultFetchError))

	// The 404 fails the fetch before the body read, so no size is recorded.
	_, ok = findMetric(rm, meterFetchResponseSize)
	require.False(t, ok)
}

func TestResolverMetrics_ParseError(t *testing.T) {
	t.Parallel()

	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write document: %v", err)
		}
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.Error(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultParseError))

	// The body was fully read before parsing failed, so its size is recorded.
	sizeMetric, ok := findMetric(rm, meterFetchResponseSize)
	require.True(t, ok)
	sizeHistogram, ok := sizeMetric.Data.(metricdata.Histogram[int64])
	require.True(t, ok)
	require.Len(t, sizeHistogram.DataPoints, 1)
	require.Equal(t, int64(len("not json")), sizeHistogram.DataPoints[0].Sum)
}

func TestResolverMetrics_OversizedDocumentRecordsCap(t *testing.T) {
	t.Parallel()

	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"padding":%q`, strings.Repeat("a", maxDocumentBytes+1)); err != nil {
			t.Errorf("write oversized document: %v", err)
		}
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.Error(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultFetchError))

	sizeMetric, ok := findMetric(rm, meterFetchResponseSize)
	require.True(t, ok)
	sizeHistogram, ok := sizeMetric.Data.(metricdata.Histogram[int64])
	require.True(t, ok)
	require.Len(t, sizeHistogram.DataPoints, 1)
	require.Equal(t, int64(maxDocumentBytes), sizeHistogram.DataPoints[0].Sum, "cap hits record the cap itself")
}

func TestResolverMetrics_ValidationErrorWithReason(t *testing.T) {
	t.Parallel()

	var clientID string
	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		doc := validDocumentJSON(clientID)
		doc["client_id"] = clientID + "?other"
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(doc); err != nil {
			t.Errorf("encode document: %v", err)
		}
	})
	clientID = srv.URL + "/client.json"

	_, err := resolver.Resolve(t.Context(), clientID, noCache)
	require.Error(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultValidationError))

	failures := counterPoints(t, rm, meterValidationFailures)
	require.Len(t, failures, 1)
	require.Equal(t, int64(1), failures[0].Value)
	requireAttr(t, failures[0].Attributes, attr.CIMDValidationReasonKey, string(reasonClientIDMismatch))
}

// TestResolverMetrics_URLSyntaxFailure pins the pre-fetch shape: the attempt
// counts as a validation error with its reason, but no origin attribute is
// recorded (the attacker-controlled URL never parsed) and no duration point
// exists (nothing was fetched).
func TestResolverMetrics_URLSyntaxFailure(t *testing.T) {
	t.Parallel()

	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("syntactically invalid client_id must never be fetched")
	})

	_, err := resolver.Resolve(t.Context(), srv.URL, noCache) // no path component
	require.Error(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultValidationError))
	requireNoAttr(t, attempts[0].Attributes, attr.CIMDOriginKey)

	_, ok := findMetric(rm, meterFetchDurationSeconds)
	require.False(t, ok)

	failures := counterPoints(t, rm, meterValidationFailures)
	require.Len(t, failures, 1)
	requireAttr(t, failures[0].Attributes, attr.CIMDValidationReasonKey, string(reasonClientIDMissingPath))
}

// TestResolverMetrics_CacheHit pins the short-circuit shape: the attempt is
// counted with its origin, but no duration point exists, so cache hits never
// skew the latency percentiles of the requests that actually left the
// process.
func TestResolverMetrics_CacheHit(t *testing.T) {
	t.Parallel()

	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a fresh cache must not reach the document host")
	})
	srvURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	_, err = resolver.Resolve(t.Context(), srv.URL+"/client.json", CacheState{
		ExpiresAt: time.Now().Add(time.Hour),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	require.Equal(t, int64(1), attempts[0].Value)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultCached))
	requireAttr(t, attempts[0].Attributes, attr.CIMDOriginKey, srvURL.Host)

	_, ok := findMetric(rm, meterFetchDurationSeconds)
	require.False(t, ok, "no upstream request ran, so no duration is recorded")

	_, ok = findMetric(rm, meterFetchResponseSize)
	require.False(t, ok)
}

// TestResolverMetrics_ConditionalNotModified pins the revalidation shape: an
// upstream request ran, so it is timed, but no body was read, so no size is
// recorded.
func TestResolverMetrics_ConditionalNotModified(t *testing.T) {
	t.Parallel()

	srv, resolver, reader := newObservedResolver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"v1"` {
			t.Errorf("expected a conditional request, got If-None-Match %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	})
	srvURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	result, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeNotModified, result.Outcome)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultConditionalNotModified))
	requireAttr(t, attempts[0].Attributes, attr.CIMDOriginKey, srvURL.Host)

	durationMetric, ok := findMetric(rm, meterFetchDurationSeconds)
	require.True(t, ok, "a request left the process, so it is timed")
	durationHistogram, ok := durationMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, durationHistogram.DataPoints, 1)
	requireAttr(t, durationHistogram.DataPoints[0].Attributes, attr.OutcomeKey, string(fetchResultConditionalNotModified))

	_, ok = findMetric(rm, meterFetchResponseSize)
	require.False(t, ok, "a 304 reads no body, so no size is recorded")
}

// TestMetrics_NamesPinned pins the wire spellings of the metric names so a
// const rename cannot silently break dashboards built on them.
func TestMetrics_NamesPinned(t *testing.T) {
	t.Parallel()

	require.Equal(t, "cimd.fetch.attempts", meterFetchAttempts)
	require.Equal(t, "cimd.fetch.duration_seconds", meterFetchDurationSeconds)
	require.Equal(t, "cimd.fetch.response_size", meterFetchResponseSize)
	require.Equal(t, "cimd.validation.failures", meterValidationFailures)
}

// TestMetrics_CacheResultLabelsPinned pins the wire spellings of the two
// cache outcomes, which dashboards use both to read cache effectiveness and
// to restrict fetch-failure ratios to the attempts that actually fetched.
func TestMetrics_CacheResultLabelsPinned(t *testing.T) {
	t.Parallel()

	require.Equal(t, "cached", string(fetchResultCached))
	require.Equal(t, "conditional_not_modified", string(fetchResultConditionalNotModified))
}

// TestMetrics_ReservedResultLabels pins the provisional label spelling that a
// follow-up issue will emit — rate_limited (AIS-215) — so dashboards built on
// this vocabulary stay valid when the emitting code lands. AIS-371's
// admission_denied is deliberately absent: admission records to its own
// cimd.admission.decisions counter, since a denial means no fetch ran and so
// has no place under fetch.attempts.
func TestMetrics_ReservedResultLabels(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m := newMetrics(meterProvider, testenv.NewLogger(t))

	require.Equal(t, "rate_limited", string(fetchResultRateLimited))
	m.RecordAttempt(ctx, "client.example.com", fetchResultRateLimited)

	rm := collectMetrics(t, reader)

	attempts := counterPoints(t, rm, meterFetchAttempts)
	require.Len(t, attempts, 1)
	requireAttr(t, attempts[0].Attributes, attr.OutcomeKey, string(fetchResultRateLimited))
	requireAttr(t, attempts[0].Attributes, attr.CIMDOriginKey, "client.example.com")
}
