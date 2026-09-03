package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/sdkerrors"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"
)

func TestClassifySDKError_BadRequestIsSanitizedAndPermanent(t *testing.T) {
	t.Parallel()

	const canary = "embedding-input-must-not-reach-error"
	sdkErr := &sdkerrors.BadRequestResponseError{
		Error_: or.BadRequestResponseErrorData{
			Code:     http.StatusBadRequest,
			Message:  canary,
			Metadata: nil,
		},
		UserID: nil,
	}

	err := classifySDKError(context.Background(), fmt.Errorf("generate embedding: %w", sdkErr))
	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
	require.ErrorIs(t, err, ErrBadRequest)
	require.True(t, IsPermanentError(err))
	require.NotContains(t, err.Error(), canary)
}

func TestClassifySDKError_GenericRateLimitPreservesHeaders(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(t.Context(), "embedding")

	header := http.Header{}
	header.Set("X-RateLimit-Limit", "60")
	header.Set("X-RateLimit-Remaining", "0")
	header.Set("X-RateLimit-Reset", "1785748157000")
	header.Set("Retry-After", "12")
	sdkErr := &sdkerrors.APIError{
		Message:     "API error occurred",
		StatusCode:  http.StatusTooManyRequests,
		Body:        "rate-limit-details",
		RawResponse: &http.Response{Header: header},
	}

	err := classifySDKError(ctx, sdkErr)
	span.End()

	require.ErrorIs(t, err, ErrRateLimited)
	require.False(t, IsPermanentError(err))
	require.NotContains(t, err.Error(), sdkErr.Body)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	recorded := map[attribute.Key]string{}
	for _, kv := range spans[0].Attributes() {
		recorded[kv.Key] = kv.Value.Emit()
	}
	require.Equal(t, "60", recorded[attr.OpenRouterRateLimitLimitKey])
	require.Equal(t, "0", recorded[attr.OpenRouterRateLimitRemainingKey])
	require.Equal(t, "1785748157000", recorded[attr.OpenRouterRateLimitResetKey])
	require.Equal(t, "12", recorded[attr.OpenRouterRetryAfterKey])
}

func TestClassifySDKError_LeavesTransportErrorsUnchanged(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("connection reset")
	require.ErrorIs(t, classifySDKError(context.Background(), transportErr), transportErr)
}

func TestClassifySDKError_LeavesSuccessfulStatusProtocolErrorsUnchanged(t *testing.T) {
	t.Parallel()

	sdkErr := &sdkerrors.APIError{
		Message:     "unsupported response content type",
		StatusCode:  http.StatusOK,
		Body:        "text/html",
		RawResponse: &http.Response{StatusCode: http.StatusOK},
	}

	err := classifySDKError(t.Context(), sdkErr)
	require.Same(t, sdkErr, err)
	var httpErr *HTTPError
	require.NotErrorAs(t, err, &httpErr)
}
