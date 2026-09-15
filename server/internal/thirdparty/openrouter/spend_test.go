package openrouter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testSpendKeyHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type spendRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f spendRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newSpendTestClient(t *testing.T, handler http.Handler) *OpenRouter {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	client := New(
		testenv.NewLogger(t),
		tracerProvider,
		policy,
		nil,
		"test",
		"management-key",
		nil,
		nil,
		nil,
		nil,
	)
	client.baseURL = server.URL
	return client
}

func TestGetDailySpendUsesAnalyticsQuery(t *testing.T) {
	t.Parallel()

	client := newSpendTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/analytics/query", r.URL.Path)
		assert.Equal(t, "Bearer management-key", r.Header.Get("Authorization"))

		var request analyticsSpendRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, []string{"total_usage"}, request.Metrics)
		assert.Equal(t, "day", request.Granularity)
		assert.Equal(t, []analyticsSpendFilter{{Field: "api_key_id", Operator: "eq", Value: testSpendKeyHash}}, request.Filters)
		assert.Equal(t, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), request.TimeRange.Start)
		assert.Equal(t, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC), request.TimeRange.End)
		assert.Equal(t, 3, request.Limit)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"data": [
					{"date__day":"2026-08-12","total_usage":"1.2300"},
					{"created_at__day":"2026-08-10T00:00:00Z","total_usage":1e-3}
				],
				"metadata":{"truncated":false}
			}
		}`))
	}))

	result, err := client.GetDailySpend(
		t.Context(),
		testSpendKeyHash,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.Equal(t, DailySpendSourceAnalytics, result.Source)
	require.Equal(t, []DailySpendDay{
		{Day: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), SpendUSD: "0.001"},
		{Day: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), SpendUSD: "1.23"},
	}, result.Days)
}

func TestGetDailySpendFallsBackToActivityAndSumsExactly(t *testing.T) {
	t.Parallel()

	var activityRequests atomic.Int32
	client := newSpendTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer management-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/analytics/query" {
			_, _ = w.Write([]byte(`{"data":{"data":[],"metadata":{"truncated":false},"warnings":["filter could not be resolved"]}}`))
			return
		}

		assert.Equal(t, "/v1/activity", r.URL.Path)
		assert.Equal(t, testSpendKeyHash, r.URL.Query().Get("api_key_hash"))
		activityRequests.Add(1)
		switch r.URL.Query().Get("date") {
		case "2026-08-10":
			_, _ = w.Write([]byte(`{"data":[{"date":"2026-08-10","usage":0.1},{"date":"2026-08-10","usage":"0.20"}]}`))
		case "2026-08-11":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "2026-08-12":
			_, _ = w.Write([]byte(`{"data":[{"date":"2026-08-12","usage":1e-3}]}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))

	result, err := client.GetDailySpend(
		t.Context(),
		testSpendKeyHash,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.Equal(t, DailySpendSourceActivity, result.Source)
	require.Equal(t, int32(3), activityRequests.Load())
	require.Equal(t, []DailySpendDay{
		{Day: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), SpendUSD: "0.3"},
		{Day: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC), SpendUSD: "0"},
		{Day: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), SpendUSD: "0.001"},
	}, result.Days)
}

func TestAnalyticsValidationTriggersActivityFallback(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing envelope": `{"unexpected":true}`,
		"truncated":        `{"data":{"data":[],"metadata":{"truncated":true}}}`,
		"warning":          `{"data":{"data":[],"metadata":{},"warnings":["warning"]}}`,
		"missing day":      `{"data":{"data":[{"total_usage":1}],"metadata":{}}}`,
		"conflicting day":  `{"data":{"data":[{"date__day":"2026-08-10","created_at__day":"2026-08-11","total_usage":1}],"metadata":{}}}`,
		"duplicate day":    `{"data":{"data":[{"date__day":"2026-08-10","total_usage":1},{"date__day":"2026-08-10","total_usage":2}],"metadata":{}}}`,
		"out of range":     `{"data":{"data":[{"date__day":"2026-08-09","total_usage":1}],"metadata":{}}}`,
		"negative spend":   `{"data":{"data":[{"date__day":"2026-08-10","total_usage":-1}],"metadata":{}}}`,
		"invalid spend":    `{"data":{"data":[{"date__day":"2026-08-10","total_usage":"NaN"}],"metadata":{}}}`,
	}

	for name, analyticsResponse := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newSpendTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/analytics/query" {
					_, _ = w.Write([]byte(analyticsResponse))
					return
				}
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))

			result, err := client.GetDailySpend(
				t.Context(),
				testSpendKeyHash,
				time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
				time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
			)
			require.NoError(t, err)
			require.Equal(t, DailySpendSourceActivity, result.Source)
			require.Equal(t, []DailySpendDay{{
				Day:      time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
				SpendUSD: "0",
			}}, result.Days)
		})
	}
}

func TestGetDailySpendReportsPrimaryAndFallbackFailuresWithoutSecrets(t *testing.T) {
	t.Parallel()

	client := newSpendTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	_, err := client.GetDailySpend(
		t.Context(),
		testSpendKeyHash,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "query OpenRouter analytics")
	require.ErrorContains(t, err, "query OpenRouter activity fallback")
	assert.NotContains(t, err.Error(), testSpendKeyHash)
	assert.NotContains(t, err.Error(), "management-key")
}

func TestGetDailySpendRedactsTransportErrors(t *testing.T) {
	t.Parallel()

	client := newSpendTestClient(t, http.NotFoundHandler())
	transportCause := errors.New("dial failed")
	client.orClient = &http.Client{Transport: spendRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: request.Method, URL: request.URL.String(), Err: transportCause}
	})}

	_, err := client.GetDailySpend(
		t.Context(),
		testSpendKeyHash,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, transportCause)
	assert.NotContains(t, err.Error(), testSpendKeyHash)
	assert.NotContains(t, err.Error(), "management-key")
	assert.NotContains(t, err.Error(), "request failed for")
}

func TestGetDailySpendRejectsInvalidInputsBeforeRequest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		keyHash string
		start   time.Time
		end     time.Time
	}{
		"invalid hash": {
			keyHash: "not-a-key-hash",
			start:   time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
		},
		"non-midnight bound": {
			keyHash: testSpendKeyHash,
			start:   time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC),
			end:     time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
		},
		"empty range": {
			keyHash: testSpendKeyHash,
			start:   time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			client := newSpendTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			_, err := client.GetDailySpend(t.Context(), test.keyHash, test.start, test.end)
			require.Error(t, err)
			require.Zero(t, requests.Load())
		})
	}
}

func TestActivityFallbackRejectsMalformedRows(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		response string
		error    string
	}{
		"missing data": {
			response: `{"unexpected":true}`,
			error:    "response omitted required fields",
		},
		"wrong day": {
			response: `{"data":[{"date":"2026-08-09","usage":1}]}`,
			error:    "response contained an invalid day",
		},
		"negative usage": {
			response: `{"data":[{"date":"2026-08-10","usage":-1}]}`,
			error:    "amount is negative",
		},
		"invalid usage": {
			response: `{"data":[{"date":"2026-08-10","usage":"NaN"}]}`,
			error:    "amount is not a decimal",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newSpendTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/analytics/query" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(test.response))
			}))

			_, err := client.GetDailySpend(
				t.Context(),
				testSpendKeyHash,
				time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
				time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, test.error)
		})
	}
}

func TestParseAndFormatSpendAmount(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`0`:             "0",
		`1`:             "1",
		`1.2300`:        "1.23",
		`0.00000000001`: "0.00000000001",
		`1e-3`:          "0.001",
		`"12.3400"`:     "12.34",
		`"1.2E3"`:       "1200",
	}
	for input, expected := range tests {
		amount, err := parseSpendAmount(json.RawMessage(input))
		require.NoError(t, err)
		require.Equal(t, expected, formatSpendAmount(amount))
	}

	for _, input := range []string{`null`, `true`, `[]`, `""`, `"1/2"`, `-0.1`, `"NaN"`} {
		_, err := parseSpendAmount(json.RawMessage(input))
		require.Error(t, err, input)
	}
}

func TestDevelopmentGetDailySpendReturnsNoRows(t *testing.T) {
	t.Parallel()

	result, err := NewDevelopment("local-key").GetDailySpend(
		t.Context(),
		testSpendKeyHash,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.Empty(t, result.Days)
	require.Equal(t, DailySpendSourceAnalytics, result.Source)
}
