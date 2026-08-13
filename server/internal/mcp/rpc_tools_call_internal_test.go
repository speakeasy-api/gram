package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// upstreamRefusalError stands in for a typed upstream API error that attributes
// an outcome to the caller, e.g. Slack answering ok=false with thread_not_found.
type upstreamRefusalError struct {
	code   string
	caller bool
}

func (e *upstreamRefusalError) Error() string { return "upstream refused: " + e.code }

func (e *upstreamRefusalError) ClientFault() bool { return e.caller }

func recordedToolCallLogger(t *testing.T) (context.Context, *slog.Logger, *bytes.Buffer, *tracetest.SpanRecorder) {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, span := provider.Tracer("mcp-test").Start(t.Context(), "tools/call")
	t.Cleanup(func() { span.End() })

	return ctx, logger, &buf, recorder
}

func decodeLogEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry), "log output: %s", buf.String())
	return entry
}

func TestToolCallRejection_CallerFaultLogsAtWarn(t *testing.T) {
	t.Parallel()

	ctx, logger, buf, recorder := recordedToolCallLogger(t)

	// The chain a Slack refusal arrives in: the tool returns the API error, and
	// the platform runtime and the gateway each wrap it on the way out.
	cause := fmt.Errorf("execute platform tool: %w",
		fmt.Errorf("execute platform tool tools:platform/slack/add_reaction: %w",
			&upstreamRefusalError{code: "thread_not_found", caller: true}))

	rejected, ok := toolCallRejection(ctx, logger, cause)
	require.True(t, ok)
	require.Equal(t, oops.CodeBadRequest, rejected.Code)
	require.Equal(t, http.StatusBadRequest, rejected.HTTPStatus(ctx))

	entry := decodeLogEntry(t, buf)
	require.Equal(t, "WARN", entry["level"], "caller mistakes must not drive the component error rate")
	require.Contains(t, entry["error.message"], "thread_not_found", "the refusal must stay queryable")

	spans := recorder.Started()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code, "a caller mistake must not mark the span as errored")
	require.Empty(t, spans[0].Events())
}

func TestToolCallRejection_UnclassifiedFailureIsNotARejection(t *testing.T) {
	t.Parallel()

	ctx, logger, buf, _ := recordedToolCallLogger(t)

	rejected, ok := toolCallRejection(ctx, logger, errors.New("connection reset by peer"))
	require.False(t, ok)
	require.Nil(t, rejected)
	require.Empty(t, buf.String(), "the caller reports the server fault, so nothing is logged here")
}

func TestToolCallRejection_UpstreamServerRefusalIsNotARejection(t *testing.T) {
	t.Parallel()

	ctx, logger, _, _ := recordedToolCallLogger(t)

	cause := fmt.Errorf("execute platform tool: %w", &upstreamRefusalError{code: "internal_error", caller: false})
	rejected, ok := toolCallRejection(ctx, logger, cause)
	require.False(t, ok)
	require.Nil(t, rejected)
}

func TestPlatformToolCallError_ShareableClientFaultIsRejected(t *testing.T) {
	t.Parallel()

	ctx, logger, buf, _ := recordedToolCallLogger(t)
	cause := oops.E(oops.CodeBadRequest, &upstreamRefusalError{code: "thread_not_found", caller: true}, "upstream refused")

	err := platformToolCallError(ctx, logger, cause, attr.SlogToolName("platform_tool"))
	var rejected *oops.ShareableError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, oops.CodeBadRequest, rejected.Code)

	entry := decodeLogEntry(t, buf)
	require.Equal(t, "WARN", entry["level"], "caller faults must use the same warning path when already shareable")
}

func TestRecordToolCallErrorStatus_UsesShareableHTTPStatus(t *testing.T) {
	t.Parallel()

	rw := &toolCallResponseWriter{statusCode: http.StatusOK}
	err := fmt.Errorf("execute platform tool: %w", oops.E(oops.CodeBadRequest, nil, "invalid tool call"))

	recordToolCallErrorStatus(t.Context(), rw, err)

	require.Equal(t, http.StatusBadRequest, rw.statusCode)
}

func TestRecordToolCallErrorStatus_IgnoresUnclassifiedErrors(t *testing.T) {
	t.Parallel()

	rw := &toolCallResponseWriter{statusCode: http.StatusOK}

	recordToolCallErrorStatus(t.Context(), rw, errors.New("connection reset by peer"))

	require.Equal(t, http.StatusOK, rw.statusCode)
}
