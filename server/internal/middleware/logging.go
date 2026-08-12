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
	// wroteHeader tracks whether the response headers have been sent (via
	// WriteHeader, first body Write, or Flush). Once sent, net/http
	// ignores further WriteHeader calls, so the recorded statusCode must
	// not change either — a late error-path WriteHeader would otherwise
	// relabel an already-sent response in access logs.
	wroteHeader bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		wroteHeader:    false,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.wroteHeader = true
	n, err := rw.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("responseWriter.Write: %w", err)
	}

	return n, nil
}

// Flush must be forwarded explicitly: embedding the http.ResponseWriter
// interface hides the underlying writer's Flush from type asserts, which
// silently disables streaming (SSE events sit in the server's write buffer
// until it fills) for every handler behind this middleware.
func (rw *responseWriter) Flush() {
	// ResponseController finds flush support through FlushError and Unwrap
	// chains that a direct http.Flusher assert would miss. A successful
	// flush commits the response headers on the wire; an unsupported or
	// failed flush commits nothing, so a later WriteHeader must still count.
	if err := http.NewResponseController(rw.ResponseWriter).Flush(); err == nil {
		rw.wroteHeader = true
	}
}

// Unwrap lets http.ResponseController reach controls of the underlying
// writer that this wrapper does not forward.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// redactedQueryParams names the query parameters whose values are stripped
// before a URL reaches application logs or the observability context.
// Credentials and personal data are both redacted, for different reasons: a
// logged credential stays replayable by anyone who can read the log, while
// logged personal data is an exposure that log access control does not undo.
//
// Matching is exact and case-sensitive, the same matching Goa applies when
// binding a query parameter to a payload attribute.
var redactedQueryParams = map[string]bool{
	// Live capability token on public share and signed-asset URLs.
	"token": true,

	// Email address on auth.login and agent.getPlugins.
	"email": true,

	// Chat search terms, which are user-typed free text.
	"search": true,
}

// logSafeURL renders a request URL for logs and observability context with
// credentials and personal data redacted. Query parameters named in
// redactedQueryParams are replaced with a placeholder. The public SPA page
// /shared/skills/<token> carries a live credential as a path segment rather
// than a parameter, so it is redacted separately.
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

	if q, redacted := redactRawQuery(safe.RawQuery); redacted {
		safe.RawQuery = q
		changed = true
	}

	if !changed {
		return u.String()
	}
	return safe.String()
}

// redactRawQuery replaces the value of every parameter named in
// redactedQueryParams and reports whether it changed anything, leaving the
// rest of the query byte for byte as it arrived.
//
// It works on the raw string rather than url.Values on purpose. ParseQuery
// silently discards any pair holding a semicolon or an invalid percent escape,
// which would hide the very parameter that needs redacting and let the caller
// fall back to logging the untouched original. Round-tripping through
// Values.Encode also reorders keys, rewrites the escaping of untouched values,
// and drops the malformed pairs outright, so a logged URL stops matching the
// request it describes.
//
// Only "&" separates parameters here, matching the parser. ";" is handled
// inside a parameter instead, by redactRegion.
func redactRawQuery(raw string) (string, bool) {
	// Skipping the rewrite keeps the ordinary request from allocating, but the
	// skip is only safe once nothing can decode into a denylisted name. A
	// percent escape is the one thing that can (?em%61il= is ?email=), so a
	// query carrying any escape goes through the full scan, which compares
	// decoded keys. Without one, a name absent from the raw text cannot appear
	// after decoding either, and the substring test decides. That test can
	// still hit a false positive (?xemail=1), costing only a scan that finds
	// nothing to change.
	if !strings.ContainsRune(raw, '%') {
		possible := false
		for name := range redactedQueryParams {
			if strings.Contains(raw, name) {
				possible = true
				break
			}
		}

		if !possible {
			return raw, false
		}
	}

	var b strings.Builder
	b.Grow(len(raw))
	changed := false

	for start := 0; start <= len(raw); {
		end := strings.IndexByte(raw[start:], '&')
		if end < 0 {
			end = len(raw)
		} else {
			end += start
		}

		region, redacted := redactRegion(raw[start:end])
		b.WriteString(region)
		changed = changed || redacted

		if end < len(raw) {
			b.WriteByte('&')
		}
		start = end + 1
	}

	if !changed {
		return raw, false
	}

	return b.String(), true
}

// redactRegion redacts one "&"-delimited slice of a query, reporting whether
// it changed anything.
//
// Go stopped accepting ";" as a parameter separator, so a semicolon now sits
// inside whatever value contains it and ParseQuery discards that pair whole.
// Both facts have to hold at once here: a denylisted name appearing after a
// semicolon still has to be caught, because the parser would have hidden the
// pair entirely, and a denylisted value that merely contains semicolons has to
// be replaced past them rather than truncated at the first one. So the scan
// looks at every ";"-separated segment, and the first denylisted name it finds
// consumes the rest of the region.
func redactRegion(region string) (string, bool) {
	for start := 0; start <= len(region); {
		end := strings.IndexByte(region[start:], ';')
		if end < 0 {
			end = len(region)
		} else {
			end += start
		}

		key, _, hasValue := strings.Cut(region[start:end], "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			// An unescapable key cannot match a denylist entry by its decoded
			// name, so compare the raw spelling rather than skipping the pair.
			name = key
		}

		if hasValue && redactedQueryParams[name] {
			return region[:start] + key + "=REDACTED", true
		}

		start = end + 1
	}

	return region, false
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
