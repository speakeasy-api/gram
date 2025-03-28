package middleware

import (
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/internal/log"
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

func RequestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logger := log.From(r.Context())

		logger.Info("Request received",
			"method", r.Method,
			"url", r.URL.String(),
			"headers", r.Header,
		)

		// Capture response status
		rw := newResponseWriter(w)
		next.ServeHTTP(rw, r)

		// Log response details
		logger.Info("Response sent",
			"method", r.Method,
			"url", r.URL.String(),
			"headers", rw.Header(),
			"status", rw.statusCode,
			"duration", time.Since(start),
		)
	})
}
