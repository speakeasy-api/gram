package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"go.opentelemetry.io/otel/trace"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("responseWriter.Write: %w", err)
	}

	return n, nil
}

func NewHTTPLoggingMiddleware(logger *slog.Logger) func(next http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("http"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			spanCtx := trace.SpanContextFromContext(ctx)
			if spanCtx.HasTraceID() {
				w.Header().Set("x-trace-id", spanCtx.TraceID().String())
			}

			start := time.Now()

			logger.InfoContext(ctx, "request", attr.SlogHTTPRequestMethod(r.Method), attr.SlogURLOriginal(r.URL.String()))
			requestContext := &contextvalues.RequestContext{
				ReqURL: r.URL.String(),
				Host:   r.Host,
				Method: r.Method,
			}
			ctx = contextvalues.SetRequestContext(ctx, requestContext)

			rw := newResponseWriter(w)
			r = r.WithContext(ctx)
			next.ServeHTTP(rw, r)

			attrs := []any{
				attr.SlogHTTPRequestMethod(r.Method),
				attr.SlogURLOriginal(r.URL.String()),
				attr.SlogHTTPResponseStatusCode(rw.statusCode),
				attr.SlogHTTPServerRequestDuration(time.Since(start).Seconds()),
				attr.SlogHostName(r.Host),
			}

			proxied := conv.Default(rw.Header().Get(gateway.HeaderProxiedResponse), "0")
			if ok, err := strconv.ParseBool(proxied); err == nil && ok {
				attrs = append(attrs, attr.SlogHTTPResponseExternal(true))
			}

			filtered := conv.Default(rw.Header().Get(gateway.HeaderFilteredResponse), "0")
			if ok, err := strconv.ParseBool(filtered); err == nil && ok {
				attrs = append(attrs, attr.SlogHTTPResponseFiltered(true))
			}

			logger.InfoContext(ctx, "response", attrs...)
		})
	}
}
