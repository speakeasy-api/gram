package stripe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	stripesdk "github.com/stripe/stripe-go/v85"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	meterEventCustomerPayloadKey = "stripe_customer_id"
	meterEventValuePayloadKey    = "value"
)

// V2MeterEventErrorClass is a bounded classification for Stripe meter-event failures.
type V2MeterEventErrorClass string

const (
	V2MeterEventErrorInvalidRequest   V2MeterEventErrorClass = "invalid_request"
	V2MeterEventErrorAuthentication   V2MeterEventErrorClass = "authentication"
	V2MeterEventErrorConcurrency      V2MeterEventErrorClass = "concurrency"
	V2MeterEventErrorRateLimit        V2MeterEventErrorClass = "rate_limit"
	V2MeterEventErrorServer           V2MeterEventErrorClass = "server"
	V2MeterEventErrorContextCanceled  V2MeterEventErrorClass = "context_canceled"
	V2MeterEventErrorDeadlineExceeded V2MeterEventErrorClass = "deadline_exceeded"
	V2MeterEventErrorNetwork          V2MeterEventErrorClass = "network"
	V2MeterEventErrorUnknown          V2MeterEventErrorClass = "unknown"
)

// V2MeterEventError describes one classified Stripe meter-event failure.
type V2MeterEventError struct {
	// Class is a bounded failure category suitable for metric dimensions.
	Class V2MeterEventErrorClass

	// Code is Stripe's machine-readable error code, when available.
	Code string

	// HTTPStatusCode is Stripe's response status, or zero when no response arrived.
	HTTPStatusCode int

	// Err is the original failure.
	Err error
}

func (e *V2MeterEventError) Error() string {
	return fmt.Sprintf("stripe v2 meter event (%s): %v", e.Class, e.Err)
}

func (e *V2MeterEventError) Unwrap() error {
	return e.Err
}

// V2MeterEventInput is the provider-neutral data needed to create a Stripe v2 meter event.
type V2MeterEventInput struct {
	// Identifier is the globally unique event identifier and idempotency key.
	Identifier string

	// EventName identifies the Stripe meter event stream.
	EventName string

	// CustomerID identifies the Stripe customer receiving the usage.
	CustomerID string

	// Value is the positive integral usage quantity.
	Value int64

	// Timestamp is the billing-effective event time.
	Timestamp time.Time
}

// V2MeterEventClient creates synchronously validated Stripe v2 meter events.
type V2MeterEventClient interface {
	CreateMeterEvent(context.Context, V2MeterEventInput) error
}

type v2MeterEventCreateFunc func(context.Context, *stripesdk.V2BillingMeterEventCreateParams) (*stripesdk.V2BillingMeterEvent, error)

type v2MeterEventClient struct {
	create v2MeterEventCreateFunc
}

// NewV2MeterEventClient creates a Stripe v2 meter-event client using the repository HTTP policy.
func NewV2MeterEventClient(guardianPolicy *guardian.Policy, apiKey string) V2MeterEventClient {
	retries := guardian.DefaultRetryConfig()
	retries.WaitMax = 10 * time.Second
	retries.MaxAttempts = 1
	httpClient := guardianPolicy.PooledClient(
		guardian.WithRetryConfig(retries),
		guardian.WithResilience("stripe-meter-events", guardian.ResilienceConfig{
			Partition: guardian.PartitionByHost(),
			Limit: guardian.Limit{
				Rate:   800,
				Burst:  100,
				Period: time.Second,
			},
			Breaker: guardian.NoBreaker(),
		}),
	)
	httpClient.Timeout = 30 * time.Second

	backendConfig := new(stripesdk.BackendConfig)
	backendConfig.HTTPClient = httpClient
	// Guardian owns retries so the two retry layers cannot amplify each other.
	backendConfig.MaxNetworkRetries = stripesdk.Int64(0)
	backends := stripesdk.NewBackendsWithConfig(backendConfig)
	sdkClient := stripesdk.NewClient(apiKey, stripesdk.WithBackends(backends))

	return &v2MeterEventClient{create: sdkClient.V2BillingMeterEvents.Create}
}

// NewNoopV2MeterEventClient creates a local-development client that accepts events without external I/O.
func NewNoopV2MeterEventClient() V2MeterEventClient {
	return noopV2MeterEventClient{}
}

type noopV2MeterEventClient struct{}

func (noopV2MeterEventClient) CreateMeterEvent(context.Context, V2MeterEventInput) error {
	return nil
}

func (c *v2MeterEventClient) CreateMeterEvent(ctx context.Context, input V2MeterEventInput) error {
	if input.Identifier == "" {
		return invalidV2MeterEventInput("missing_identifier", "identifier is required")
	}
	if input.EventName == "" {
		return invalidV2MeterEventInput("missing_event_name", "event name is required")
	}
	if input.CustomerID == "" {
		return invalidV2MeterEventInput("missing_customer", "customer id is required")
	}
	if input.Value <= 0 {
		return invalidV2MeterEventInput("invalid_value", "value must be positive")
	}
	if input.Timestamp.IsZero() {
		return invalidV2MeterEventInput("missing_timestamp", "timestamp is required")
	}

	params := new(stripesdk.V2BillingMeterEventCreateParams)
	params.EventName = stripesdk.String(input.EventName)
	params.Identifier = stripesdk.String(input.Identifier)
	params.Payload = map[string]string{
		meterEventCustomerPayloadKey: input.CustomerID,
		meterEventValuePayloadKey:    strconv.FormatInt(input.Value, 10),
	}
	params.Timestamp = new(input.Timestamp.UTC())
	params.SetIdempotencyKey(input.Identifier)

	if _, err := c.create(ctx, params); err != nil {
		classified := classifyV2MeterEventError(err)
		if classified.Code == "duplicate_meter_event" {
			return nil
		}
		return classified
	}
	return nil
}

func invalidV2MeterEventInput(code, message string) error {
	return &V2MeterEventError{
		Class:          V2MeterEventErrorInvalidRequest,
		Code:           code,
		HTTPStatusCode: 0,
		Err:            errors.New(message),
	}
}

func classifyV2MeterEventError(err error) *V2MeterEventError {
	if errors.Is(err, context.Canceled) {
		return &V2MeterEventError{Class: V2MeterEventErrorContextCanceled, Code: "", HTTPStatusCode: 0, Err: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &V2MeterEventError{Class: V2MeterEventErrorDeadlineExceeded, Code: "", HTTPStatusCode: 0, Err: err}
	}

	if errors.Is(err, guardian.ErrRateLimited) {
		return &V2MeterEventError{Class: V2MeterEventErrorRateLimit, Code: "guardian_rate_limited", HTTPStatusCode: 0, Err: err}
	}
	if rateLimitErr, ok := errors.AsType[*stripesdk.RateLimitError](err); ok {
		statusCode := http.StatusTooManyRequests
		if rateLimitErr.LastResponse != nil {
			statusCode = rateLimitErr.LastResponse.StatusCode
		}
		return &V2MeterEventError{Class: V2MeterEventErrorRateLimit, Code: rateLimitErr.Code, HTTPStatusCode: statusCode, Err: err}
	}

	if rawErr, ok := errors.AsType[*stripesdk.V2RawError](err); ok {
		return &V2MeterEventError{
			Class:          classifyV2MeterEventStatus(rawErr.HTTPStatusCode),
			Code:           rawErr.Code,
			HTTPStatusCode: rawErr.HTTPStatusCode,
			Err:            err,
		}
	}

	if stripeErr, ok := errors.AsType[*stripesdk.Error](err); ok {
		return &V2MeterEventError{
			Class:          classifyV2MeterEventStatus(stripeErr.HTTPStatusCode),
			Code:           string(stripeErr.Code),
			HTTPStatusCode: stripeErr.HTTPStatusCode,
			Err:            err,
		}
	}

	if _, ok := errors.AsType[net.Error](err); ok {
		return &V2MeterEventError{Class: V2MeterEventErrorNetwork, Code: "", HTTPStatusCode: 0, Err: err}
	}

	return &V2MeterEventError{Class: V2MeterEventErrorUnknown, Code: "", HTTPStatusCode: 0, Err: err}
}

func classifyV2MeterEventStatus(statusCode int) V2MeterEventErrorClass {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return V2MeterEventErrorAuthentication
	case statusCode == http.StatusConflict:
		return V2MeterEventErrorConcurrency
	case statusCode == http.StatusTooManyRequests:
		return V2MeterEventErrorRateLimit
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return V2MeterEventErrorInvalidRequest
	case statusCode >= http.StatusInternalServerError:
		return V2MeterEventErrorServer
	default:
		return V2MeterEventErrorUnknown
	}
}
