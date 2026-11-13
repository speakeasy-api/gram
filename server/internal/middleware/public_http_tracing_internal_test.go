package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestPublicHTTPTracingReleasesHeadersFromContext(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	wrap := PublicHTTPTracing("test", otelhttp.WithTracerProvider(provider))

	req := httptest.NewRequest(http.MethodGet, "/mcp/test", nil)
	req.Header.Set("X-Client-IP", "203.0.113.7")
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	req.Header.Set("Authorization", "Bearer test-credential")
	req.Header.Set("Cookie", "session=test-session")
	original := req.Header.Clone()

	var detached context.Context
	var received http.Header
	var retainedAtHandler http.Header
	var holderFound bool
	wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		holder, ok := r.Context().Value(originalIPHeadersKey{}).(*originalIPHeaders)
		holderFound = ok
		if ok {
			retainedAtHandler = holder.header
		}
		detached = context.WithoutCancel(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, holderFound)
	require.Nil(t, retainedAtHandler, "release raw headers before application work can retain the context")
	require.NotNil(t, detached)
	holder, ok := detached.Value(originalIPHeadersKey{}).(*originalIPHeaders)
	require.True(t, ok)
	require.Nil(t, holder.header, "detached work must not keep credentials through the tracing bridge")
	require.Equal(t, original, received)
	require.Equal(t, original, req.Header)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, spans[0].SpanContext(), trace.SpanContextFromContext(detached))
}
