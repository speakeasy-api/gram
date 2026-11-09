package middleware

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
	"golang.org/x/net/http/httpguts"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

const (
	// brotliCompressionLevel is the brotli quality for dynamic responses.
	// Compression runs on the request path for every eligible response, so
	// this favors CPU over ratio.
	brotliCompressionLevel = 4

	// brotliWindowBits sets a 512 KiB brotli window. The encoder's default
	// window allocates about 3.5 times as much per pooled encoder for no
	// reduction on bodies smaller than the window.
	brotliWindowBits = 19

	// gzipCompressionLevel is gzip's balance of CPU and ratio for clients that
	// do not accept brotli.
	gzipCompressionLevel = 6

	// minCompressibleBytes is the smallest body worth compressing. Encoding
	// framing makes very small bodies larger, and most small responses are
	// short JSON errors that would otherwise take a pooled encoder for nothing.
	minCompressibleBytes = 256

	// encodingBrotli is the Content-Encoding token for brotli.
	encodingBrotli = "br"

	// encodingGzip is the Content-Encoding token for gzip.
	encodingGzip = "gzip"

	// encodingXGzip is the alias RFC 9110 section 8.4.1.3 says to treat as gzip.
	encodingXGzip = "x-gzip"
)

// compressibleContentTypes lists the response media types worth compressing:
// text and structured data. Binary types such as images, archives, and
// protobuf are already dense, and text/event-stream is deliberately absent so
// SSE events leave the server the moment a handler flushes them.
var compressibleContentTypes = []string{
	"application/javascript",
	"application/json",
	"application/problem+json",
	"application/xml",
	"image/svg+xml",
	"text/css",
	"text/html",
	"text/javascript",
	"text/plain",
	"text/xml",
}

// ResponseCompression compresses responses with brotli or gzip according to
// the request's Accept-Encoding, preferring brotli when the client rates both
// equally. A response is compressed when its media type is in
// compressibleContentTypes and its body reaches minCompressibleBytes or the
// handler flushes it first. A compressed response loses Content-Length and
// Accept-Ranges and has its ETag weakened. Compressible media types gain
// Vary: Accept-Encoding whether or not this request was compressed.
//
// Responses that already carry Content-Encoding or Content-Range, forbid
// transformation with Cache-Control: no-transform, have no body by status, or
// answer HEAD pass through unchanged.
//
// The status and headers of a compressible response are held until the body
// size is known or the handler flushes. A panic before then leaves nothing
// committed, so an outer recovery middleware can still answer with a clean
// error.
func ResponseCompression() func(http.Handler) http.Handler {
	pools := map[string]*encoderPool{
		encodingBrotli: {pool: sync.Pool{New: nil}, newEncoder: newBrotliEncoder},
		encodingGzip:   {pool: sync.Pool{New: nil}, newEncoder: newGzipEncoder},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			encoding := ""
			if r.Method != http.MethodHead {
				encoding = negotiateEncoding(r.Header.Values("Accept-Encoding"))
			}

			writer := &compressResponseWriter{
				ResponseWriter: w,
				encoding:       encoding,
				pool:           pools[encoding],
				enc:            nil,
				status:         0,
				committed:      false,
				pending:        nil,
				declaredLength: -1,
				written:        0,
			}

			returned := false
			defer func() {
				if !returned {
					writer.releaseEncoder()
				}
			}()

			next.ServeHTTP(writer, r)
			returned = true
			writer.finish()
		})
	}
}

// negotiateEncoding picks brotli or gzip from the request's Accept-Encoding
// values, honoring the q-values of RFC 9110 section 12.5.3: a weight of 0
// excludes a coding and "*" covers any coding not listed explicitly. A
// malformed or out of range weight excludes its coding. The coding with the
// higher weight wins and brotli breaks ties. It returns "" when neither is
// acceptable, including when the header is absent.
func negotiateEncoding(values []string) string {
	weights := make(map[string]float64)
	for _, value := range values {
		for member := range strings.SplitSeq(value, ",") {
			name, params, _ := strings.Cut(member, ";")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if name == encodingXGzip {
				name = encodingGzip
			}

			weight := 1.0
			for param := range strings.SplitSeq(params, ";") {
				key, raw, ok := strings.Cut(param, "=")
				if !ok || strings.ToLower(strings.TrimSpace(key)) != "q" {
					continue
				}
				parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
				if err != nil || !(parsed >= 0 && parsed <= 1) {
					parsed = 0
				}
				weight = parsed
			}
			weights[name] = weight
		}
	}

	weightOf := func(encoding string) float64 {
		if weight, listed := weights[encoding]; listed {
			return weight
		}
		return weights["*"]
	}

	brotliWeight, gzipWeight := weightOf(encodingBrotli), weightOf(encodingGzip)
	switch {
	case brotliWeight > 0 && brotliWeight >= gzipWeight:
		return encodingBrotli
	case gzipWeight > 0:
		return encodingGzip
	default:
		return ""
	}
}

// resettableEncoder is the part of gzip.Writer and brotli.Writer this
// middleware relies on: streaming writes, a flush that emits buffered input, a
// close that writes the stream trailer, and a reset so the writer can be
// pooled across responses.
type resettableEncoder interface {
	io.WriteCloser
	Flush() error
	Reset(w io.Writer)
}

// encoderPool reuses encoders of one coding across responses.
type encoderPool struct {
	// pool holds idle encoders, each reset to discard its output.
	pool sync.Pool

	// newEncoder builds an encoder when the pool is empty.
	newEncoder func() resettableEncoder
}

func (p *encoderPool) get(dst io.Writer) resettableEncoder {
	enc, ok := p.pool.Get().(resettableEncoder)
	if !ok {
		enc = p.newEncoder()
	}
	enc.Reset(dst)
	return enc
}

// put drops the encoder's reference to the response writer before it waits in
// the pool.
func (p *encoderPool) put(enc resettableEncoder) {
	enc.Reset(io.Discard)
	p.pool.Put(enc)
}

func newBrotliEncoder() resettableEncoder {
	return brotli.NewWriterOptions(io.Discard, brotli.WriterOptions{
		Quality: brotliCompressionLevel,
		LGWin:   brotliWindowBits,
	})
}

// newGzipEncoder builds a gzip writer. gzipCompressionLevel is within gzip's
// range, so the error path is unreachable; a default-level writer keeps the
// request working rather than panicking if that ever changes.
func newGzipEncoder() resettableEncoder {
	w, err := gzip.NewWriterLevel(io.Discard, gzipCompressionLevel)
	if err != nil {
		return gzip.NewWriter(io.Discard)
	}
	return w
}

// compressResponseWriter decides how to send a response when its status is
// known. Ineligible responses commit immediately and pass through. Eligible
// ones buffer up to minCompressibleBytes, then either start compressing or, if
// the handler returns first, send the short body as is.
type compressResponseWriter struct {
	http.ResponseWriter

	// encoding is the negotiated Content-Encoding, or "" for none.
	encoding string

	// pool supplies the encoder for encoding; nil when encoding is "".
	pool *encoderPool

	// enc is non-nil from the start of compression until the response ends.
	enc resettableEncoder

	// status is the response status once the handler has chosen one; 0 before.
	status int

	// committed is true once the status and headers went to the wrapped writer.
	committed bool

	// pending holds body bytes while the size decision is open.
	pending []byte

	// declaredLength is the handler's Content-Length, or -1 when it set none.
	// Compression removes the header, so this writer enforces what net/http
	// would have: a body cannot exceed it, and one that falls short must not
	// look complete.
	declaredLength int64

	// written counts the uncompressed body bytes accepted from the handler.
	written int64
}

// addVary lists Accept-Encoding in Vary unless it is already there.
func addVary(header http.Header) {
	if !httpguts.HeaderValuesContainsToken(header["Vary"], "Accept-Encoding") {
		header.Add("Vary", "Accept-Encoding")
	}
}

func hasCompressibleContentType(header http.Header) bool {
	mediaType, _, _ := strings.Cut(header.Get("Content-Type"), ";")
	return slices.Contains(compressibleContentTypes, strings.ToLower(strings.TrimSpace(mediaType)))
}

func (w *compressResponseWriter) WriteHeader(code int) {
	// 1xx other than 101 is informational: net/http sends it and leaves the
	// final status open.
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = code

	header := w.Header()
	bodiless := code == http.StatusSwitchingProtocols || code == http.StatusNoContent || code == http.StatusNotModified
	if bodiless || header.Get(constants.HeaderContentEncoding) != "" || header.Get("Content-Range") != "" || !hasCompressibleContentType(header) {
		w.commit()
		return
	}

	addVary(header)

	if raw := header.Get("Content-Length"); raw != "" {
		if declared, err := strconv.ParseInt(raw, 10, 64); err == nil && declared >= 0 {
			w.declaredLength = declared
		}
	}

	shortBody := w.declaredLength >= 0 && w.declaredLength < minCompressibleBytes
	if w.encoding == "" || shortBody || httpguts.HeaderValuesContainsToken(header["Cache-Control"], "no-transform") {
		w.commit()
	}
}

// commit sends the status and headers without compression.
func (w *compressResponseWriter) commit() {
	w.committed = true
	w.ResponseWriter.WriteHeader(w.status)
}

// startCompressing labels the response, sends its headers, and feeds the
// encoder whatever was buffered.
func (w *compressResponseWriter) startCompressing() error {
	header := w.Header()
	header.Set(constants.HeaderContentEncoding, w.encoding)
	header.Del("Content-Length")
	header.Del("Accept-Ranges")
	if etag := header.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		header.Set("ETag", "W/"+etag)
	}

	w.enc = w.pool.get(w.ResponseWriter)
	w.commit()

	pending := w.pending
	w.pending = nil
	if _, err := w.enc.Write(pending); err != nil {
		return fmt.Errorf("compress response: %w", err)
	}
	return nil
}

func (w *compressResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.committed && w.enc == nil {
		n, err := w.ResponseWriter.Write(p)
		if err != nil {
			return n, fmt.Errorf("compressResponseWriter.Write: %w", err)
		}
		return n, nil
	}

	if w.declaredLength >= 0 && w.written+int64(len(p)) > w.declaredLength {
		return 0, http.ErrContentLength
	}

	if !w.committed {
		if len(w.pending)+len(p) < minCompressibleBytes {
			w.pending = append(w.pending, p...)
			w.written += int64(len(p))
			return len(p), nil
		}
		if err := w.startCompressing(); err != nil {
			return 0, err
		}
	}

	n, err := w.enc.Write(p)
	w.written += int64(n)
	if err != nil {
		return n, fmt.Errorf("compress response: %w", err)
	}
	return n, nil
}

// Flush is forwarded explicitly so streaming handlers behind this middleware
// keep working: embedding http.ResponseWriter hides the wrapped writer's Flush
// from type asserts. A flush ends the size decision, because a flushed body may
// keep growing, so an eligible response starts compressing here even when
// empty. The encoder then emits a complete block, which lets a flushed message
// reach the client immediately.
func (w *compressResponseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.committed && w.startCompressing() != nil {
		return
	}
	if w.enc != nil && w.enc.Flush() != nil {
		return
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// finish completes the response once the handler has returned normally. Encoder
// errors are not reported: they mean the client went away.
func (w *compressResponseWriter) finish() {
	if w.status != 0 && !w.committed {
		w.commit()
		_, _ = w.ResponseWriter.Write(w.pending)
		w.pending = nil
		return
	}
	if w.enc == nil {
		return
	}

	// A body shorter than its declared length is a failed response, such as a
	// proxied upstream that broke mid-copy. net/http would close the
	// connection; a clean stream end here would present truncated data as
	// complete.
	if w.declaredLength >= 0 && w.written < w.declaredLength {
		w.releaseEncoder()
		panic(http.ErrAbortHandler)
	}

	_ = w.enc.Close()
	w.releaseEncoder()
}

// releaseEncoder returns the encoder to the pool without writing a stream
// trailer, so a response abandoned mid-way cannot decode as complete.
func (w *compressResponseWriter) releaseEncoder() {
	if w.enc == nil {
		return
	}
	w.pool.put(w.enc)
	w.enc = nil
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *compressResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
