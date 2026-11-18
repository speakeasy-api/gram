package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// RequestBodyLimit refuses request bodies over limit bytes with
// 413 Request Entity Too Large: before the handler runs when Content-Length
// says so, and otherwise as the body is read. Behind an edge that only checks
// Content-Length, this is what stops a chunked upload.
//
// In the read-time case the handler's body read fails with
// *http.MaxBytesError, which handlers that already size-check bodies keep
// handling as before. Whatever status the handler then writes is replaced by
// the 413 as long as it had not sent headers yet. A handler that streams a
// response before reading the body keeps its own status.
func RequestBodyLimit(logger *slog.Logger, limit int64) func(http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("request_body_limit_middleware"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				writeRequestTooLarge(logger, w, r, limit)
				return
			}
			if r.Body == nil || r.Body == http.NoBody {
				next.ServeHTTP(w, r)
				return
			}

			writer := &requestTooLargeWriter{
				ResponseWriter: w,
				reject:         func() { writeRequestTooLarge(logger, w, r, limit) },
				exceeded:       atomic.Bool{},
				wroteHeader:    false,
				replaced:       false,
			}
			r.Body = &limitedBody{
				ReadCloser: http.MaxBytesReader(w, r.Body, limit),
				exceeded:   &writer.exceeded,
			}
			next.ServeHTTP(writer, r)

			// A handler that gives up on the body without answering would
			// otherwise send an empty 200.
			if writer.exceeded.Load() && !writer.wroteHeader {
				writer.WriteHeader(http.StatusRequestEntityTooLarge)
			}
		})
	}
}

// writeRequestTooLarge answers with the same JSON error shape every Goa
// endpoint uses, so clients parse a 413 like any other error. Headers a handler
// set for the response it meant to send are removed, since they do not describe
// this one.
//
// On HTTP/1 the connection closes because the unread remainder of a
// multi-megabyte upload is not worth draining to keep it alive. HTTP/2 resets
// just the stream on its own, while Connection: close would retire a connection
// other requests share.
func writeRequestTooLarge(logger *slog.Logger, w http.ResponseWriter, r *http.Request, limit int64) {
	serviceErr := oops.E(oops.CodeRequestTooLarge, nil, "request body exceeds %d bytes", limit).AsGoa(r.Context())
	payload := goahttp.NewErrorResponse(r.Context(), serviceErr)

	header := w.Header()
	for _, name := range []string{"Accept-Ranges", "Cache-Control", constants.HeaderContentEncoding, "Content-Length", "ETag", "Last-Modified", "Trailer"} {
		header.Del(name)
	}
	header.Set("Content-Type", "application/json")
	if r.ProtoMajor == 1 {
		header.Set("Connection", "close")
	}
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.ErrorContext(r.Context(), "encode request too large response", attr.SlogError(err))
	}
}

// limitedBody records when the underlying http.MaxBytesReader cut the body
// off, so the response writer knows to answer 413.
type limitedBody struct {
	io.ReadCloser

	// exceeded is shared with the requestTooLargeWriter of the same request.
	exceeded *atomic.Bool
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			b.exceeded.Store(true)
		}
	}
	return n, err //nolint:wrapcheck // An io.Reader wrapper must preserve sentinel error identity.
}

// requestTooLargeWriter replaces the handler's response with a 413 once the
// request body limit was exceeded, as long as no headers were sent yet.
type requestTooLargeWriter struct {
	http.ResponseWriter

	// reject writes the 413 to the wrapped writer.
	reject func()

	// exceeded is set by limitedBody when the body read hit the limit.
	exceeded atomic.Bool

	// wroteHeader tracks whether the response headers have been sent, by
	// WriteHeader, first body Write, or Flush.
	wroteHeader bool

	// replaced is true once the 413 was written in the handler's place; the
	// handler's own writes are then discarded.
	replaced bool
}

func (w *requestTooLargeWriter) WriteHeader(code int) {
	// 1xx other than 101 is informational: net/http sends it and leaves the
	// final status open, so the decision stays pending for the next call.
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	if w.wroteHeader {
		if !w.replaced {
			w.ResponseWriter.WriteHeader(code)
		}
		return
	}
	w.wroteHeader = true

	if w.exceeded.Load() {
		w.replaced = true
		w.reject()
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *requestTooLargeWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.replaced {
		return len(b), nil
	}

	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("requestTooLargeWriter.Write: %w", err)
	}
	return n, nil
}

// Flush is forwarded explicitly so streaming handlers behind this middleware
// keep working: embedding http.ResponseWriter hides the wrapped writer's Flush
// from type asserts. A flush commits the response headers, so an exceeded limit is decided here
// the same way an implicit WriteHeader would.
func (w *requestTooLargeWriter) Flush() {
	if !w.wroteHeader && w.exceeded.Load() {
		w.WriteHeader(http.StatusOK)
	}
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err == nil {
		w.wroteHeader = true
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *requestTooLargeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
