package middleware

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// compressibleJSON is large and repetitive enough that any encoder shrinks it.
var compressibleJSON = `{"status":"ok","items":[` + strings.Repeat(`{"id":1,"name":"widget","enabled":true},`, 100) + `{}]}`

// clientTimeout bounds real-socket tests, so a response stuck in a buffer fails
// the test rather than hanging it.
const clientTimeout = 10 * time.Second

func jsonHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
}

func serveCompressed(t *testing.T, handler http.Handler, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/rpc/things.list", nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	ResponseCompression()(handler).ServeHTTP(rec, request)
	return rec
}

func decodeBrotli(t *testing.T, body []byte) string {
	t.Helper()

	decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
	require.NoError(t, err)
	return string(decoded)
}

func decodeGzip(t *testing.T, body []byte) string {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(decoded)
}

// newDecoder wraps a compressed stream in the reader for its coding.
func newDecoder(t *testing.T, encoding string, r io.Reader) io.Reader {
	t.Helper()

	if encoding == encodingBrotli {
		return brotli.NewReader(r)
	}
	reader, err := gzip.NewReader(r)
	require.NoError(t, err)
	return reader
}

// rawGet requests server over a real connection with the client's transparent
// decompression off, so the test sees the bytes the server sent. The caller
// closes the response body.
func rawGet(t *testing.T, server *httptest.Server, acceptEncoding string) *http.Response {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), clientTimeout)
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	request.Header.Set("Accept-Encoding", acceptEncoding)

	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	response, err := client.Do(request)
	require.NoError(t, err)
	return response
}

func TestResponseCompression_PrefersBrotli(t *testing.T) {
	t.Parallel()

	rec := serveCompressed(t, jsonHandler(compressibleJSON), "gzip, deflate, br, zstd")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "br", rec.Header().Get(constants.HeaderContentEncoding))
	require.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")
	require.Empty(t, rec.Header().Get("Content-Length"))
	require.Less(t, rec.Body.Len(), len(compressibleJSON))
	require.JSONEq(t, compressibleJSON, decodeBrotli(t, rec.Body.Bytes()))
}

func TestResponseCompression_FallsBackToGzip(t *testing.T) {
	t.Parallel()

	rec := serveCompressed(t, jsonHandler(compressibleJSON), "gzip, deflate")

	require.Equal(t, "gzip", rec.Header().Get(constants.HeaderContentEncoding))
	require.Less(t, rec.Body.Len(), len(compressibleJSON))
	require.JSONEq(t, compressibleJSON, decodeGzip(t, rec.Body.Bytes()))
}

func TestResponseCompression_CompressesHTML(t *testing.T) {
	t.Parallel()

	page := "<!doctype html><html><body>" + strings.Repeat("<p>Connect this MCP server.</p>", 50) + "</body></html>"
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	})

	rec := serveCompressed(t, handler, "br")

	require.Equal(t, "br", rec.Header().Get(constants.HeaderContentEncoding))
	require.Equal(t, page, decodeBrotli(t, rec.Body.Bytes()))
}

func TestResponseCompression_NegotiatesAcceptEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		acceptEncoding string
		wantEncoding   string
	}{
		{name: "absent", acceptEncoding: "", wantEncoding: ""},
		{name: "identity only", acceptEncoding: "identity", wantEncoding: ""},
		{name: "deflate only", acceptEncoding: "deflate", wantEncoding: ""},
		{name: "brotli excluded", acceptEncoding: "br;q=0, gzip", wantEncoding: "gzip"},
		{name: "gzip weighted higher", acceptEncoding: "gzip;q=1, br;q=0.5", wantEncoding: "gzip"},
		{name: "equal weights prefer brotli", acceptEncoding: "gzip;q=0.8, br;q=0.8", wantEncoding: "br"},
		{name: "wildcard", acceptEncoding: "*", wantEncoding: "br"},
		{name: "wildcard with brotli excluded", acceptEncoding: "*, br;q=0", wantEncoding: "gzip"},
		{name: "everything excluded", acceptEncoding: "*;q=0", wantEncoding: ""},
		{name: "case insensitive", acceptEncoding: "GZIP", wantEncoding: "gzip"},
		{name: "substring is not a match", acceptEncoding: "brick", wantEncoding: ""},
		{name: "x-gzip alias", acceptEncoding: "x-gzip", wantEncoding: "gzip"},
		{name: "not a number excludes", acceptEncoding: "gzip;q=NaN, br;q=0.5", wantEncoding: "br"},
		{name: "out of range excludes", acceptEncoding: "gzip;q=2", wantEncoding: ""},
		{name: "empty weight excludes", acceptEncoding: "gzip;q=", wantEncoding: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serveCompressed(t, jsonHandler(compressibleJSON), tt.acceptEncoding)

			require.Equal(t, tt.wantEncoding, rec.Header().Get(constants.HeaderContentEncoding))
			require.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding", "caches must key on Accept-Encoding even for an uncompressed copy")
			switch tt.wantEncoding {
			case "":
				require.JSONEq(t, compressibleJSON, rec.Body.String())
			case encodingBrotli:
				require.JSONEq(t, compressibleJSON, decodeBrotli(t, rec.Body.Bytes()))
			case encodingGzip:
				require.JSONEq(t, compressibleJSON, decodeGzip(t, rec.Body.Bytes()))
			}
		})
	}
}

func TestResponseCompression_LeavesEventStreamsAlone(t *testing.T) {
	t.Parallel()

	var flushErrs []error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flushErrs = append(flushErrs, http.NewResponseController(w).Flush())
		_, _ = io.WriteString(w, "data: first\n\n")
		flushErrs = append(flushErrs, http.NewResponseController(w).Flush())
		_, _ = io.WriteString(w, "data: second\n\n")
	})

	rec := serveCompressed(t, handler, "br, gzip")

	require.Equal(t, []error{nil, nil}, flushErrs)
	require.True(t, rec.Flushed)
	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Empty(t, rec.Header().Values("Vary"))
	require.Equal(t, "data: first\n\ndata: second\n\n", rec.Body.String())
}

// Over a real connection an SSE event must arrive while the handler is still
// running, which a recorder cannot show.
func TestResponseCompression_DeliversEventsImmediately(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(ResponseCompression()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: first\n")
		_ = http.NewResponseController(w).Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n")
	})))
	t.Cleanup(server.Close)

	response := rawGet(t, server, "br, gzip")
	defer o11y.NoLogDefer(func() error { return response.Body.Close() })
	require.Empty(t, response.Header.Get(constants.HeaderContentEncoding))

	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	close(release)
	require.NoError(t, err)
	require.Equal(t, "data: first\n", first)

	second, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: second\n", second)
}

// A compressed response the handler flushes must reach the client as decodable
// data while the handler is still running.
func TestResponseCompression_FlushDeliversDecodableData(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{encodingBrotli, encodingGzip} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			release := make(chan struct{})
			server := httptest.NewServer(ResponseCompression()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, "first chunk\n")
				_ = http.NewResponseController(w).Flush()
				<-release
				_, _ = io.WriteString(w, "second chunk\n")
			})))
			t.Cleanup(server.Close)

			response := rawGet(t, server, encoding)
			defer o11y.NoLogDefer(func() error { return response.Body.Close() })
			require.Equal(t, encoding, response.Header.Get(constants.HeaderContentEncoding))

			reader := bufio.NewReader(newDecoder(t, encoding, response.Body))
			first, err := reader.ReadString('\n')
			close(release)
			require.NoError(t, err)
			require.Equal(t, "first chunk\n", first)

			rest, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, "second chunk\n", string(rest))
		})
	}
}

// Flushing before any write opens the stream. The label must already be on the
// response, or the bytes written after it would arrive compressed and unlabeled.
func TestResponseCompression_FlushBeforeFirstWriteLabelsResponse(t *testing.T) {
	t.Parallel()

	var flushErr error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		flushErr = http.NewResponseController(w).Flush()
		_, _ = io.WriteString(w, `{"late":true}`)
	})

	rec := serveCompressed(t, handler, "gzip")

	require.NoError(t, flushErr)
	require.Equal(t, "gzip", rec.Header().Get(constants.HeaderContentEncoding))
	require.JSONEq(t, `{"late":true}`, decodeGzip(t, rec.Body.Bytes()))
}

// A flushed stream that never writes still has to be a valid encoded stream.
func TestResponseCompression_FlushWithoutBodyIsAValidStream(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{encodingBrotli, encodingGzip} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = http.NewResponseController(w).Flush()
			})

			rec := serveCompressed(t, handler, encoding)

			require.Equal(t, encoding, rec.Header().Get(constants.HeaderContentEncoding))
			decoded, err := io.ReadAll(newDecoder(t, encoding, bytes.NewReader(rec.Body.Bytes())))
			require.NoError(t, err)
			require.Empty(t, decoded)
		})
	}
}

func TestResponseCompression_LeavesShortBodiesAlone(t *testing.T) {
	t.Parallel()

	body := `{"name":"not_found"}`
	rec := serveCompressed(t, jsonHandler(body), "br, gzip")

	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")
	require.JSONEq(t, body, rec.Body.String())
}

func TestResponseCompression_CompressesAcrossSmallWrites(t *testing.T) {
	t.Parallel()

	chunk := strings.Repeat("x", minCompressibleBytes/2+1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, chunk)
		_, _ = io.WriteString(w, chunk)
	})

	rec := serveCompressed(t, handler, "gzip")

	require.Equal(t, "gzip", rec.Header().Get(constants.HeaderContentEncoding))
	require.Equal(t, chunk+chunk, decodeGzip(t, rec.Body.Bytes()))
}

func TestResponseCompression_LeavesEncodedResponsesAlone(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(constants.HeaderContentEncoding, "gzip")
		_, _ = io.WriteString(w, "already-encoded-bytes")
	})

	rec := serveCompressed(t, handler, "br, gzip")

	require.Equal(t, "gzip", rec.Header().Get(constants.HeaderContentEncoding))
	require.Equal(t, "already-encoded-bytes", rec.Body.String())
}

func TestResponseCompression_LeavesBinaryContentAlone(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", "9")
		_, _ = io.WriteString(w, "zip-bytes")
	})

	rec := serveCompressed(t, handler, "br, gzip")

	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Empty(t, rec.Header().Values("Vary"))
	require.Equal(t, "9", rec.Header().Get("Content-Length"))
	require.Equal(t, "zip-bytes", rec.Body.String())
}

func TestResponseCompression_LeavesBodilessStatusesAlone(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
			})

			rec := serveCompressed(t, handler, "br, gzip")

			require.Equal(t, status, rec.Code)
			require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
			require.Zero(t, rec.Body.Len())
		})
	}
}

func TestResponseCompression_LeavesEmptyResponsesUnlabeled(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})

	rec := serveCompressed(t, handler, "br, gzip")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Zero(t, rec.Body.Len())
}

func TestResponseCompression_LeavesHeadAlone(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodHead, "/rpc/things.list", nil)
	request.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	ResponseCompression()(jsonHandler(compressibleJSON)).ServeHTTP(rec, request)

	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
}

func TestResponseCompression_LeavesPartialContentAlone(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Range", "bytes 0-999/5000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, strings.Repeat("x", 1000))
	})

	rec := serveCompressed(t, handler, "br, gzip")

	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Equal(t, 1000, rec.Body.Len())
}

func TestResponseCompression_HonorsNoTransform(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-transform")
		_, _ = io.WriteString(w, compressibleJSON)
	})

	rec := serveCompressed(t, handler, "br, gzip")

	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.JSONEq(t, compressibleJSON, rec.Body.String())
}

// A strong ETag promises identical bytes, which the compressed and uncompressed
// copies of a response are not.
func TestResponseCompression_WeakensStrongETag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		etag string
		want string
	}{
		{name: "strong", etag: `"abc"`, want: `W/"abc"`},
		{name: "already weak", etag: `W/"abc"`, want: `W/"abc"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("ETag", tt.etag)
				w.Header().Set("Accept-Ranges", "bytes")
				_, _ = io.WriteString(w, compressibleJSON)
			})

			rec := serveCompressed(t, handler, "gzip")

			require.Equal(t, tt.want, rec.Header().Get("ETag"))
			require.Empty(t, rec.Header().Get("Accept-Ranges"))
		})
	}
}

func TestResponseCompression_KeepsStrongETagWhenNotCompressing(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"abc"`)
		_, _ = io.WriteString(w, compressibleJSON)
	})

	rec := serveCompressed(t, handler, "")

	require.Equal(t, `"abc"`, rec.Header().Get("ETag"))
}

// Compression removes Content-Length, so a proxied body that stops short of its
// declared length would otherwise end as a clean, complete-looking stream. The
// client sees the failure as soon as it has headers: on the request when nothing
// was flushed, and on the body read when something was.
func TestResponseCompression_AbortsWhenBodyFallsShortOfContentLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		flush    bool
	}{
		{name: "brotli unflushed", encoding: encodingBrotli, flush: false},
		{name: "brotli flushed", encoding: encodingBrotli, flush: true},
		{name: "gzip unflushed", encoding: encodingGzip, flush: false},
		{name: "gzip flushed", encoding: encodingGzip, flush: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := strings.Repeat("partial tool output\n", 100)
			server := httptest.NewServer(NewRecovery(testenv.NewLogger(t))(ResponseCompression()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", strconv.Itoa(len(body)*2))
				_, _ = io.WriteString(w, body)
				if tt.flush {
					_ = http.NewResponseController(w).Flush()
				}
			}))))
			t.Cleanup(server.Close)

			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			request.Header.Set("Accept-Encoding", tt.encoding)

			client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
			response, err := client.Do(request)
			if err == nil {
				t.Cleanup(func() { _ = response.Body.Close() })
				_, err = io.ReadAll(response.Body)
			}
			require.Error(t, err, "the client must see a failed transfer, not a truncated success")
		})
	}
}

func TestResponseCompression_RejectsWritesPastContentLength(t *testing.T) {
	t.Parallel()

	var writeErr error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "300")
		_, writeErr = io.WriteString(w, strings.Repeat("x", 301))
	})

	serveCompressed(t, handler, "gzip")

	require.ErrorIs(t, writeErr, http.ErrContentLength)
}

// A panic before the response is committed leaves room for a clean error, even
// after the handler chose a status and wrote part of a body.
func TestResponseCompression_PanicBeforeCommitAnswersPlainError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"partial":`)
		panic(errors.New("boom"))
	})

	request := httptest.NewRequest(http.MethodGet, "/rpc/things.list", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	NewRecovery(testenv.NewLogger(t))(ResponseCompression()(handler)).ServeHTTP(rec, request)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Contains(t, rec.Body.String(), "unexpected")
	require.NotContains(t, rec.Body.String(), "partial")
}

// Once compressed bytes are on the wire a panic cannot produce a clean error,
// but the client must not be able to read the partial body as complete.
func TestResponseCompression_PanicAfterCommitFailsDecoding(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{encodingBrotli, encodingGzip} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(NewRecovery(testenv.NewLogger(t))(ResponseCompression()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, compressibleJSON)
				_ = http.NewResponseController(w).Flush()
				panic(errors.New("boom"))
			}))))
			t.Cleanup(server.Close)

			response := rawGet(t, server, encoding)
			defer o11y.NoLogDefer(func() error { return response.Body.Close() })
			require.Equal(t, encoding, response.Header.Get(constants.HeaderContentEncoding))

			raw, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			_, err = io.ReadAll(newDecoder(t, encoding, bytes.NewReader(raw)))
			require.Error(t, err)
		})
	}
}

// deadlineRecorder supports a control net/http's ResponseController reaches
// only by unwrapping, unlike Flush which the wrapper handles itself.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlineSet bool
}

func (r *deadlineRecorder) SetWriteDeadline(time.Time) error {
	r.deadlineSet = true
	return nil
}

func TestResponseCompression_UnwrapsForResponseController(t *testing.T) {
	t.Parallel()

	var deadlineErr error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deadlineErr = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
	})

	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), deadlineSet: false}
	ResponseCompression()(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.NoError(t, deadlineErr)
	require.True(t, rec.deadlineSet)
}

// failingWriter fails every write, like a client that went away.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client went away") }

// Encoders are pooled, so one that failed or finished on a previous response
// must produce a clean stream after Reset. This drives the encoders directly
// because sync.Pool may drop items, so a request-level test cannot force reuse.
func TestEncoderPool_ReusedEncoderProducesCleanStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		encoding   string
		newEncoder func() resettableEncoder
	}{
		{encoding: encodingBrotli, newEncoder: newBrotliEncoder},
		{encoding: encodingGzip, newEncoder: newGzipEncoder},
	}
	for _, tt := range tests {
		t.Run(tt.encoding, func(t *testing.T) {
			t.Parallel()

			enc := tt.newEncoder()

			enc.Reset(failingWriter{})
			_, _ = io.WriteString(enc, compressibleJSON)
			require.Error(t, enc.Flush())

			var finished bytes.Buffer
			enc.Reset(&finished)
			_, err := io.WriteString(enc, "first response")
			require.NoError(t, err)
			require.NoError(t, enc.Close())

			var reused bytes.Buffer
			enc.Reset(&reused)
			_, err = io.WriteString(enc, "second response")
			require.NoError(t, err)
			require.NoError(t, enc.Close())

			decoded, err := io.ReadAll(newDecoder(t, tt.encoding, bytes.NewReader(reused.Bytes())))
			require.NoError(t, err)
			require.Equal(t, "second response", string(decoded))
		})
	}
}
