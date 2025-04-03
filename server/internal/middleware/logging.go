package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
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
	return rw.ResponseWriter.Write(b)
}

func NewHTTPLoggingMiddleware(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			start := time.Now()

			reqAttrs := make([]any, 0, 2+len(r.Header))
			reqAttrs = append(reqAttrs, slog.String("method", r.Method))
			reqAttrs = append(reqAttrs, slog.String("url", r.URL.String()))
			for k, v := range r.Header {
				reqAttrs = append(reqAttrs, slog.String(k, strings.Join(v, ",")))
			}

			logger.InfoContext(ctx, "request", reqAttrs...)

			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r)

			resAttrs := make([]any, 0, 4+len(r.Header))
			resAttrs = append(resAttrs, slog.String("method", r.Method))
			resAttrs = append(resAttrs, slog.String("url", r.URL.String()))
			resAttrs = append(resAttrs, slog.Int("status", rw.statusCode))
			resAttrs = append(resAttrs, slog.String("duration", time.Since(start).String()))
			for k, v := range rw.Header() {
				resAttrs = append(resAttrs, slog.String(k, strings.Join(v, ",")))
			}

			logger.InfoContext(ctx, "response", resAttrs...)
		})
	}
}

func NewGoaLoggingMiddleware(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			start := time.Now()

			reqAttrs := make([]any, 0, 2+len(r.Header))
			reqAttrs = append(reqAttrs, slog.String("method", r.Method))
			reqAttrs = append(reqAttrs, slog.String("url", r.URL.String()))
			for k, v := range r.Header {
				reqAttrs = append(reqAttrs, slog.String(k, strings.Join(v, ",")))
			}

			logger.InfoContext(ctx, "request", reqAttrs...)

			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r)

			resAttrs := make([]any, 0, 4+len(r.Header))
			resAttrs = append(resAttrs, slog.String("method", r.Method))
			resAttrs = append(resAttrs, slog.String("url", r.URL.String()))
			resAttrs = append(resAttrs, slog.Int("status", rw.statusCode))
			resAttrs = append(resAttrs, slog.String("duration", time.Since(start).String()))
			for k, v := range rw.Header() {
				resAttrs = append(resAttrs, slog.String(k, strings.Join(v, ",")))
			}

			logger.InfoContext(ctx, "response", resAttrs...)
		})
	}
}
