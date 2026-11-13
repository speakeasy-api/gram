package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/requestip"
)

// ipSampler observes attributes before a span can be corrected by a handler.
type ipSampler struct {
	attributes []attribute.KeyValue
}

func (s *ipSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	s.attributes = append([]attribute.KeyValue(nil), p.Attributes...)
	return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
}

func (*ipSampler) Description() string { return "capture client attribution" }

func TestPublicHTTPTracingClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "edge", values: []string{"203.0.113.7"}, want: "203.0.113.7"},
		{name: "absent", want: "192.0.2.1"},
		{name: "malformed", values: []string{"bad"}, want: "192.0.2.1"},
		{name: "duplicates", values: []string{"203.0.113.7", "203.0.113.8"}, want: "192.0.2.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sampler := &ipSampler{}
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler), sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			wrap := middleware.PublicHTTPTracing("test", otelhttp.WithTracerProvider(provider))
			req := httptest.NewRequest(http.MethodGet, "/mcp/test", nil)
			req.RemoteAddr = "192.0.2.1:4321"
			req.Header["X-Client-Ip"] = tt.values
			req.Header.Set("X-Forwarded-For", "198.51.100.9, 192.0.2.2")
			req.Header.Set("X-Real-IP", "198.51.100.8")
			original := req.Header.Clone()
			var handled *http.Request
			wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handled = r
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(httptest.NewRecorder(), req)
			require.NotNil(t, handled)
			require.Equal(t, original, handled.Header)
			require.Equal(t, "192.0.2.1:4321", handled.RemoteAddr)
			result, found := requestip.FromContext(handled.Context())
			require.True(t, found)
			require.Equal(t, tt.want, result.String())
			handlerSpan := trace.SpanContextFromContext(handled.Context())
			require.Equal(t, original, req.Header)
			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, spans[0].SpanContext(), handlerSpan)
			for _, attrs := range [][]attribute.KeyValue{sampler.attributes, spans[0].Attributes()} {
				set := attribute.NewSet(attrs...)
				client, ok := set.Value(attribute.Key("client.address"))
				require.True(t, ok)
				require.Equal(t, tt.want, client.AsString())
				peer, ok := set.Value(attribute.Key("network.peer.address"))
				require.True(t, ok)
				require.Equal(t, "192.0.2.1", peer.AsString())
			}
		})
	}
}
