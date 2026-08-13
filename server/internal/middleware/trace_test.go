package middleware_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func recordTraceMethodSpan(t *testing.T, endpoint goa.Endpoint) (sdktrace.ReadOnlySpan, error) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx := context.WithValue(t.Context(), goa.ServiceKey, "test-service")
	ctx = context.WithValue(ctx, goa.MethodKey, "test-method")
	_, err := middleware.TraceMethods(provider.Tracer("test"))(endpoint)(ctx, nil)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	return spans[0], err
}

func TestTraceMethods_LogWarnPlainErrorSuppressesSpanError(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	err := errors.New("invalid credentials")

	span, got := recordTraceMethodSpan(t, func(ctx context.Context, _ any) (any, error) {
		return nil, oops.LogWarn(ctx, logger, err)
	})

	require.ErrorIs(t, got, err)
	require.Equal(t, codes.Unset, span.Status().Code)
	require.Empty(t, span.Events())
}

func TestTraceMethods_LogErrorPlainErrorRecordsOnce(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	err := errors.New("database unavailable")

	span, got := recordTraceMethodSpan(t, func(ctx context.Context, _ any) (any, error) {
		return nil, oops.LogError(ctx, logger, err)
	})

	require.ErrorIs(t, got, err)
	require.Equal(t, codes.Error, span.Status().Code)
	require.Equal(t, err.Error(), span.Status().Description)
	require.Len(t, span.Events(), 1)
}

func TestTraceMethods_LoggedWarningDoesNotSuppressDifferentError(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	warning := errors.New("invalid credentials")
	err := errors.New("database unavailable")

	span, got := recordTraceMethodSpan(t, func(ctx context.Context, _ any) (any, error) {
		_ = oops.LogWarn(ctx, logger, warning)
		return nil, err
	})

	require.ErrorIs(t, got, err)
	require.Equal(t, codes.Error, span.Status().Code)
	require.Equal(t, err.Error(), span.Status().Description)
	require.Len(t, span.Events(), 1)
}
