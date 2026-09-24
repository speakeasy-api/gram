package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestV2MeterEventClientClassifiesConcurrencyError(t *testing.T) {
	t.Parallel()

	client := &v2MeterEventClient{
		create: func(context.Context, *stripesdk.V2BillingMeterEventCreateParams) (*stripesdk.V2BillingMeterEvent, error) {
			return nil, &stripesdk.V2RawError{
				Code:           "too_many_concurrent_requests",
				Type:           nil,
				Message:        "try again",
				UserMessage:    nil,
				HTTPStatusCode: http.StatusConflict,
				RequestID:      "req_test",
			}
		},
	}

	err := client.CreateMeterEvent(t.Context(), validV2MeterEventInput())
	require.Error(t, err)
	var classified *V2MeterEventError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, V2MeterEventErrorConcurrency, classified.Class)
	require.Equal(t, "too_many_concurrent_requests", classified.Code)
	require.Equal(t, http.StatusConflict, classified.HTTPStatusCode)
}

func TestV2MeterEventClientAcknowledgesDuplicateWithoutErrorLog(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	client, requests := newV2MeterEventClientForResponse(t, logger, http.StatusBadRequest, `{
		"error": {
			"code": "duplicate_meter_event",
			"message": "already submitted"
		}
	}`)

	require.NoError(t, client.CreateMeterEvent(t.Context(), validV2MeterEventInput()))
	require.Equal(t, int64(1), requests.Load())
	require.Empty(t, logs.String())
}

func TestV2MeterEventClientLogsRealFailureWithStructuredLogger(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(&o11y.ContextHandler{
		Handler:     slog.NewJSONHandler(&logs, nil),
		DataDogAttr: true,
	}).With(attr.SlogComponent("stripe-test"))
	client, _ := newV2MeterEventClientForResponse(t, logger, http.StatusBadRequest, `{
		"error": {
			"code": "invalid_request",
			"message": "not a duplicate_meter_event"
		}
	}`)

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     false,
	})
	ctx := trace.ContextWithSpanContext(t.Context(), spanContext)
	err := client.CreateMeterEvent(ctx, validV2MeterEventInput())
	require.Error(t, err)
	var record map[string]any
	decoder := json.NewDecoder(&logs)
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&record))
	require.Equal(t, "ERROR", record["level"])
	require.Equal(t, "stripe-test", record[string(attr.ComponentKey)])
	require.Equal(t, "invalid_request", record[string(attr.StripeErrorCodeKey)])
	require.Equal(t, json.Number("400"), record[string(attr.HTTPResponseStatusCodeKey)])
	require.Contains(t, record[string(attr.ErrorMessageKey)], "not a duplicate_meter_event")
	require.Equal(t, spanContext.TraceID().String(), record[string(attr.TraceIDKey)])
	require.Equal(t, spanContext.SpanID().String(), record[string(attr.SpanIDKey)])
}

func TestV2MeterEventClientClassifiesInvalidInput(t *testing.T) {
	t.Parallel()

	client := &v2MeterEventClient{
		create: func(context.Context, *stripesdk.V2BillingMeterEventCreateParams) (*stripesdk.V2BillingMeterEvent, error) {
			return nil, errors.New("must not be called")
		},
	}
	input := validV2MeterEventInput()
	input.Value = 0

	err := client.CreateMeterEvent(t.Context(), input)
	require.Error(t, err)
	var classified *V2MeterEventError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, V2MeterEventErrorInvalidRequest, classified.Class)
	require.Equal(t, "invalid_value", classified.Code)
	require.Zero(t, classified.HTTPStatusCode)
}

func TestV2MeterEventClientClassifiesGuardianRateLimit(t *testing.T) {
	t.Parallel()

	client := &v2MeterEventClient{
		create: func(context.Context, *stripesdk.V2BillingMeterEventCreateParams) (*stripesdk.V2BillingMeterEvent, error) {
			return nil, &url.Error{
				Op:  http.MethodPost,
				URL: "https://api.stripe.com/v2/billing/meter_events",
				Err: &guardian.ResilienceError{Reason: guardian.ErrRateLimited, RetryAfter: time.Second},
			}
		},
	}

	err := client.CreateMeterEvent(t.Context(), validV2MeterEventInput())
	require.Error(t, err)
	var classified *V2MeterEventError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, V2MeterEventErrorRateLimit, classified.Class)
	require.Equal(t, "guardian_rate_limited", classified.Code)
	require.Zero(t, classified.HTTPStatusCode)
}

func TestV2MeterEventClientPreservesStripeRateLimitCodeAfterGuardianRetries(t *testing.T) {
	t.Parallel()

	client, requests := newV2MeterEventClientForResponse(t, testenv.NewLogger(t), http.StatusTooManyRequests, `{
		"error": {
			"type": "rate_limit",
			"code": "meter_event_rate_limit",
			"message": "too many requests"
		}
	}`)

	err := client.CreateMeterEvent(t.Context(), validV2MeterEventInput())
	require.Error(t, err)
	require.Equal(t, int64(2), requests.Load())
	var classified *V2MeterEventError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, V2MeterEventErrorRateLimit, classified.Class)
	require.Equal(t, "meter_event_rate_limit", classified.Code)
	require.Equal(t, http.StatusTooManyRequests, classified.HTTPStatusCode)
}

func TestV2MeterEventClientPreservesStripeServerCodeAfterGuardianRetries(t *testing.T) {
	t.Parallel()

	client, requests := newV2MeterEventClientForResponse(t, testenv.NewLogger(t), http.StatusServiceUnavailable, `{
		"error": {
			"code": "api_unavailable",
			"message": "temporarily unavailable"
		}
	}`)

	err := client.CreateMeterEvent(t.Context(), validV2MeterEventInput())
	require.Error(t, err)
	require.Equal(t, int64(2), requests.Load())
	var classified *V2MeterEventError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, V2MeterEventErrorServer, classified.Class)
	require.Equal(t, "api_unavailable", classified.Code)
	require.Equal(t, http.StatusServiceUnavailable, classified.HTTPStatusCode)
}

func newV2MeterEventClientForResponse(t *testing.T, logger *slog.Logger, statusCode int, body string) (V2MeterEventClient, *atomic.Int64) {
	t.Helper()

	requests := new(atomic.Int64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return newV2MeterEventClient(logger, guardianPolicy, "sk_test_placeholder", server.URL), requests
}

func validV2MeterEventInput() V2MeterEventInput {
	return V2MeterEventInput{
		Identifier: "reading-id",
		EventName:  "tum",
		CustomerID: "cus_test",
		Value:      1,
		Timestamp:  time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC),
	}
}
