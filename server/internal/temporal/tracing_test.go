package temporal

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

func TestTracingWorkflowRollover(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		operation string
		err       error
		wantError bool
	}{
		{name: "success", operation: "RunWorkflow", err: nil, wantError: false},
		{name: "rollover", operation: "RunWorkflow", err: &workflow.ContinueAsNewError{}, wantError: false},
		{name: "wrapped rollover", operation: "RunWorkflow", err: fmt.Errorf("rollover: %w", &workflow.ContinueAsNewError{}), wantError: false},
		{name: "failure", operation: "RunWorkflow", err: errors.New("publication failed"), wantError: true},
		{name: "same text is not sentinel", operation: "RunWorkflow", err: errors.New("continue as new"), wantError: true},
		{name: "activity failure", operation: "RunActivity", err: errors.New("publication failed"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			tracer, err := opentelemetry.NewTracer(opentelemetry.TracerOptions{
				Tracer:      provider.Tracer("test"),
				SpanStarter: StartTracingSpan,
			})
			require.NoError(t, err)
			span, err := tracer.StartSpan(&interceptor.TracerStartSpanOptions{
				Parent: nil, Operation: tc.operation, Name: "test", Time: time.Now(),
				DependedOn: false, Tags: nil, FromHeader: false, ToHeader: false, IdempotencyKey: "",
			})
			require.NoError(t, err)
			// Exercise the SDK adapter's RecordError/SetStatus/End sequence.
			span.Finish(&interceptor.TracerFinishSpanOptions{Error: tc.err})
			ended := recorder.Ended()
			require.Len(t, ended, 1)
			if tc.wantError {
				require.Equal(t, codes.Error, ended[0].Status().Code)
				require.Equal(t, tc.err.Error(), ended[0].Status().Description)
				require.Len(t, ended[0].Events(), 1)
			} else {
				require.Equal(t, codes.Unset, ended[0].Status().Code)
				require.Empty(t, ended[0].Events())
			}
		})
	}
}
