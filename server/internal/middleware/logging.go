package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

// logSafeURL renders a request URL for logs and observability context with
// secret-bearing parts redacted. Several public capability-URL endpoints
// carry a live credential in a "token" query parameter (e.g. skills.getShared,
// assets.serveChatAttachmentSigned, chatSessions.revoke), and the public SPA
// page /shared/skills/<token> carries one as a path segment; logging either
// verbatim would leak reusable secrets into application logs.
func logSafeURL(u *url.URL) string {
	safe := *u
	changed := false

	if rest, ok := strings.CutPrefix(safe.Path, "/shared/skills/"); ok && rest != "" {
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = "REDACTED" + rest[i:]
		} else {
			rest = "REDACTED"
		}
		safe.Path = "/shared/skills/" + rest
		safe.RawPath = ""
		changed = true
	}

	if q := safe.Query(); q.Has("token") {
		q.Set("token", "REDACTED")
		safe.RawQuery = q.Encode()
		changed = true
	}

	if !changed {
		return u.String()
	}
	return safe.String()
}

func NewHTTPLoggingMiddleware(logger *slog.Logger) func(next http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("http_logging_middleware"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			requestID := conv.TruncateString(r.Header.Get("X-Request-ID"), 64)

			spanCtx := trace.SpanContextFromContext(ctx)
			if spanCtx.HasTraceID() {
				w.Header().Set("x-trace-id", spanCtx.TraceID().String())
			}

			if requestID != "" {
				w.Header().Set("x-request-id", requestID)
				trace.SpanFromContext(ctx).SetAttributes(attr.HTTPRequestID(requestID))
			}

			start := time.Now()

			referrer := r.Referer()
			referrerHost := ""
			if u, err := url.Parse(referrer); err == nil {
				referrerHost = u.Host
				// Referers can carry capability URLs (e.g. a browser on the
				// public skill share page reports its tokenized URL); redact
				// them like the request URL itself.
				referrer = logSafeURL(u)
			}

			safeURL := logSafeURL(r.URL)

			requestContext := &contextvalues.RequestContext{
				ReqID:       requestID,
				ReqURL:      safeURL,
				Host:        r.Host,
				Method:      r.Method,
				Referer:     conv.TruncateString(referrer, 400),
				RefererHost: conv.TruncateString(referrerHost, 400),
				UserAgent:   conv.TruncateString(r.UserAgent(), 400),
			}
			ctx = contextvalues.SetRequestContext(ctx, requestContext)

			rw := newResponseWriter(w)
			r = r.WithContext(ctx)
			attrs := []any{
				attr.SlogHTTPRequestMethod(r.Method),
				attr.SlogURLOriginal(safeURL),
				attr.SlogHostName(r.Host),
			}
			if requestContext.ReqID != "" {
				attrs = append(attrs, attr.SlogHTTPRequestID(requestContext.ReqID))
			}
			if requestContext.Referer != "" {
				attrs = append(attrs, attr.SlogHTTPRequestHeaderReferer(requestContext.Referer))
			}
			if requestContext.UserAgent != "" {
				attrs = append(attrs, attr.SlogHTTPRequestHeaderUserAgent(requestContext.UserAgent))
			}
			if requestContext.RefererHost != "" {
				attrs = append(attrs, attr.SlogHTTPReferrerHost(requestContext.RefererHost))
			}

			logger.InfoContext(ctx, "request", attrs...)

			next.ServeHTTP(rw, r)

			code := rw.statusCode
			if errors.Is(ctx.Err(), context.Canceled) {
				code = 499
			}

			attrs = append(attrs, attr.SlogHTTPResponseStatusCode(code), attr.SlogHTTPServerRequestDuration(time.Since(start).Seconds()))

			if code != rw.statusCode {
				attrs = append(attrs, attr.SlogHTTPResponseOriginalStatusCode(rw.statusCode))
			}

			proxied := conv.Default(rw.Header().Get(constants.HeaderProxiedResponse), "0")
			if ok, err := strconv.ParseBool(proxied); err == nil && ok {
				attrs = append(attrs, attr.SlogHTTPResponseExternal(true))
			}

			filtered := conv.Default(rw.Header().Get(constants.HeaderFilteredResponse), "0")
			if ok, err := strconv.ParseBool(filtered); err == nil && ok {
				attrs = append(attrs, attr.SlogHTTPResponseFiltered(true))
			}

			logger.InfoContext(ctx, "response", attrs...)
		})
	}
}
