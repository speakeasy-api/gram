package stripe

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

func TestV2MeterEventClientBuildsStripeRequest(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, time.August, 28, 10, 11, 12, 345, time.FixedZone("test", 2*60*60))
	var captured *stripesdk.V2BillingMeterEventCreateParams
	client := &v2MeterEventClient{
		create: func(_ context.Context, params *stripesdk.V2BillingMeterEventCreateParams) (*stripesdk.V2BillingMeterEvent, error) {
			captured = params
			return new(stripesdk.V2BillingMeterEvent), nil
		},
	}

	err := client.CreateMeterEvent(t.Context(), V2MeterEventInput{
		Identifier: "reading-id",
		EventName:  "aicp_agent_session_storage_v1",
		CustomerID: "cus_test",
		Value:      42,
		Timestamp:  occurredAt,
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Equal(t, "reading-id", stripesdk.StringValue(captured.Identifier))
	require.Equal(t, "reading-id", stripesdk.StringValue(captured.IdempotencyKey))
	require.Equal(t, "aicp_agent_session_storage_v1", stripesdk.StringValue(captured.EventName))
	require.Equal(t, map[string]string{"stripe_customer_id": "cus_test", "value": "42"}, captured.Payload)
	require.Equal(t, occurredAt.UTC(), stripesdk.TimeValue(captured.Timestamp))
}

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

func TestV2MeterEventClientAcknowledgesDuplicate(t *testing.T) {
	t.Parallel()

	client := &v2MeterEventClient{
		create: func(context.Context, *stripesdk.V2BillingMeterEventCreateParams) (*stripesdk.V2BillingMeterEvent, error) {
			return nil, &stripesdk.V2RawError{
				Code:           "duplicate_meter_event",
				Type:           nil,
				Message:        "already submitted",
				UserMessage:    nil,
				HTTPStatusCode: http.StatusBadRequest,
				RequestID:      "req_test",
			}
		},
	}

	require.NoError(t, client.CreateMeterEvent(t.Context(), validV2MeterEventInput()))
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

func validV2MeterEventInput() V2MeterEventInput {
	return V2MeterEventInput{
		Identifier: "reading-id",
		EventName:  "aicp_agent_session_storage_v1",
		CustomerID: "cus_test",
		Value:      1,
		Timestamp:  time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC),
	}
}
