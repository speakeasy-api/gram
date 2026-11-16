package codemode

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRunnerConnectionTraceOmitsTransportErrorDetails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		failure error
		outcome string
	}{
		{name: "transport error", failure: errors.New("credential-do-not-log"), outcome: "error"},
		{name: "network deadline", failure: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, outcome: "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			ctx, parent := provider.Tracer("test").Start(t.Context(), "request")
			defer parent.End()
			client, err := NewRunnerClient("http://127.0.0.1:1", strings.Repeat("t", 32), &net.Dialer{
				ControlContext: func(context.Context, string, string, syscall.RawConn) error {
					return tc.failure
				},
			})
			require.NoError(t, err)
			_, _, err = client.connect(ctx)
			require.ErrorIs(t, err, tc.failure)
			spans := recorder.Ended()
			require.Len(t, spans, 1)
			span := spans[0]
			require.Equal(t, "codemode.runner.connect", span.Name())
			require.Equal(t, parent.SpanContext().TraceID(), span.SpanContext().TraceID())
			require.Equal(t, parent.SpanContext().SpanID(), span.Parent().SpanID())
			require.Equal(t, codes.Error, span.Status().Code)
			require.Equal(t, tc.outcome, span.Status().Description)
			require.Len(t, span.Attributes(), 1)
			require.Equal(t, attr.CodeOutcome(tc.outcome), span.Attributes()[0])
			require.Empty(t, span.Events(), "transport errors must not become exception events")
		})
	}
}

func TestRunnerTraceClassifiesExecutionFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		code    string
		outcome string
		started bool
	}{
		{code: "deadline_exceeded", outcome: "timeout", started: true},
		{code: "cancelled", outcome: "cancelled", started: true},
		{code: "capacity_exceeded", outcome: "capacity", started: false},
		{code: "admission_refused", outcome: "refused", started: false},
		{code: "runtime_unavailable", outcome: "unavailable", started: false},
		{code: "untrusted-runner-code", outcome: "error", started: true},
	} {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			ctx, parent := provider.Tracer("test").Start(t.Context(), "request")
			defer parent.End()
			endpoint := startedRunner(t, tc.started, func(string) []runnerMessage {
				return []runnerMessage{{Type: "error", Code: tc.code, Message: "sensitive-runner-message"}}
			}, func(runnerMessage) bool { return false })
			client, err := NewRunnerClient(endpoint, strings.Repeat("t", 32), &net.Dialer{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			result := client.Run(ctx, uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
			require.NotNil(t, result.Error)
			require.Equal(t, tc.code, result.Error.Code)
			found := false
			for _, span := range recorder.Ended() {
				if span.Name() != "codemode.runner.run" {
					continue
				}
				found = true
				if tc.outcome == "cancelled" {
					require.Equal(t, codes.Unset, span.Status().Code)
					require.Empty(t, span.Status().Description)
				} else {
					require.Equal(t, codes.Error, span.Status().Code)
					require.Equal(t, tc.outcome, span.Status().Description)
				}
				require.Len(t, span.Attributes(), 1)
				require.Equal(t, attr.CodeOutcome(tc.outcome), span.Attributes()[0])
				require.Empty(t, span.Events())
			}
			require.True(t, found)
		})
	}
}
