// Package middleware provides the small set of HTTP/Goa middleware dev-idp
// needs. Logging is plog-backed via stdlib slog; recovery returns a JSON 500
// with the panic message (dev-idp is dev-only and verbose errors are
// useful). Goa endpoint middleware passes errors through unchanged.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// NewRecovery catches panics from downstream handlers, logs the stack and
// returns a JSON 500. Sentinel http.ErrAbortHandler is re-raised so the
// stdlib server can handle it.
func NewRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = logger.With(slog.String("component", "recovery"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if errVal, ok := rec.(error); ok && errors.Is(errVal, http.ErrAbortHandler) {
					panic(rec)
				}

				logger.LogAttrs(r.Context(), slog.LevelError,
					"recovered from panic",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Connection", "close")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"name":    "internal",
					"message": fmt.Sprintf("panic: %v", rec),
				})
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// NewHTTPLogging logs one INFO line per request with method, path, status
// and duration.
func NewHTTPLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = logger.With(slog.String("component", "http"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			code := rw.status
			if errors.Is(r.Context().Err(), context.Canceled) {
				code = 499
			}

			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", code),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// MapErrors is a Goa endpoint middleware that translates dev-idp's
// internal *oops.ShareableError into the Goa wire shape. Plain errors
// pass through unchanged (Goa's HTTP encoder + dev-idp's permissive
// error encoder turn them into a verbose 500 response).
func MapErrors() func(goa.Endpoint) goa.Endpoint {
	return func(next goa.Endpoint) goa.Endpoint {
		return func(ctx context.Context, req any) (any, error) {
			val, err := next(ctx, req)

			var se *oops.ShareableError
			if err != nil && errors.As(err, &se) {
				return nil, se.AsGoa()
			}
			return val, err
		}
	}
}

// TraceMethods is a no-op endpoint middleware kept for API parity with
// the gram-server middleware. dev-idp uses a noop tracer so there is
// nothing to record.
func TraceMethods(_ trace.Tracer) func(goa.Endpoint) goa.Endpoint {
	return func(next goa.Endpoint) goa.Endpoint {
		return func(ctx context.Context, req any) (any, error) {
			return next(ctx, req)
		}
	}
}
