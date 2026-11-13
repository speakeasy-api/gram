package middleware

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/speakeasy-api/gram/server/internal/requestip"
)

type originalIPHeadersKey struct{}

// originalIPHeaders transfers headers through tracing without retaining them in
// contexts that application handlers may detach for background work.
type originalIPHeaders struct {
	// header is cleared before application handlers can derive a context.
	header http.Header
}

// PublicHTTPTracing resolves the edge client IP and instruments a public
// listener together. It must not wrap the private ingress listener.
// OpenTelemetry sees only the resolved forwarding address at span creation,
// including sampling; application handlers retain the original headers and
// transport peer. Every public ingress must overwrite X-Client-IP.
func PublicHTTPTracing(serverName string, options ...otelhttp.Option) func(http.Handler) http.Handler {
	options = append([]otelhttp.Option{
		otelhttp.WithServerName(serverName),
		otelhttp.WithPublicEndpointFn(IsOTelPublicEndpoint),
	}, options...)

	return func(next http.Handler) http.Handler {
		traced := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Keep the span context installed by otelhttp while restoring the
			// application's headers, rather than restoring the original request.
			original, _ := r.Context().Value(originalIPHeadersKey{}).(*originalIPHeaders)
			restored := r.WithContext(r.Context())
			restored.Header = original.header
			original.header = nil
			next.ServeHTTP(w, restored)
		}), "http", options...)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := requestip.Resolve(r)
			ctx := requestip.WithContext(r.Context(), result)
			ctx = context.WithValue(ctx, originalIPHeadersKey{}, &originalIPHeaders{header: r.Header})
			forTracing := r.WithContext(ctx)
			forTracing.Header = r.Header.Clone()
			if forTracing.Header == nil {
				forTracing.Header = make(http.Header)
			}
			forTracing.Header.Del("X-Forwarded-For")
			if result.Addr.IsValid() {
				forTracing.Header.Set("X-Forwarded-For", result.String())
			}
			traced.ServeHTTP(w, forTracing)
		})
	}
}
