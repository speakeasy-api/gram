package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// testBodyLimit keeps most cases small; the acceptance test below uses the
// real listener limit.
const testBodyLimit = 1024

// chunkedBody returns a body of n zero bytes whose length httptest cannot
// see, so the request carries no Content-Length, like a chunked upload.
func chunkedBody(n int) io.Reader {
	return io.MultiReader(bytes.NewReader(make([]byte, n)))
}

// requestTooLargeResponse is the error shape every Goa endpoint answers with.
type requestTooLargeResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func requireRequestTooLarge(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "close", rec.Header().Get("Connection"))
	require.Empty(t, rec.Header().Get("Content-Length"), "the handler's length does not describe the 413 body")

	var payload requestTooLargeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, string(oops.CodeRequestTooLarge), payload.Name)
	require.Contains(t, payload.Message, "request body exceeds")
	// Goa serializes error fields in lowercase; clients match on that shape.
	require.Contains(t, rec.Body.String(), `"name":"request_too_large"`)
}

// readAllHandler mimics a Goa endpoint: it reads the body, and on failure
// answers 400 with a decode error, which the middleware must turn into 413.
func readAllHandler(t *testing.T, gotBody *[]byte, readErr *error) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		*gotBody = body
		*readErr = err
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "34")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"name":"decode_payload","id":"x"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequestBodyLimit_RejectsDeclaredOversizeBody(t *testing.T) {
	t.Parallel()

	called := false
	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", bytes.NewReader(make([]byte, testBodyLimit+1)))
	require.Equal(t, int64(testBodyLimit+1), request.ContentLength)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)

	require.False(t, called, "the handler must not run for a body the edge would already have refused")
	requireRequestTooLarge(t, rec)
}

func TestRequestBodyLimit_RejectsChunkedOversizeBody(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	var readErr error
	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(readAllHandler(t, &gotBody, &readErr))

	request := httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", chunkedBody(testBodyLimit+1))
	require.Equal(t, int64(-1), request.ContentLength)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)

	var tooLarge *http.MaxBytesError
	require.ErrorAs(t, readErr, &tooLarge, "handlers that size-check bodies themselves keep seeing MaxBytesError")
	require.Len(t, gotBody, testBodyLimit)
	requireRequestTooLarge(t, rec)
	require.NotContains(t, rec.Body.String(), "decode_payload", "the handler's 400 body must be discarded")
}

func TestRequestBodyLimit_AcceptsBodyAtLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body func() io.Reader
	}{
		{name: "declared", body: func() io.Reader { return bytes.NewReader(make([]byte, testBodyLimit)) }},
		{name: "chunked", body: func() io.Reader { return chunkedBody(testBodyLimit) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotBody []byte
			var readErr error
			handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(readAllHandler(t, &gotBody, &readErr))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", tt.body()))

			require.NoError(t, readErr)
			require.Len(t, gotBody, testBodyLimit)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Empty(t, rec.Header().Get("Connection"))
		})
	}
}

// A 16 MiB upload is refused whether or not it declares its length, which is
// the case the edge cannot cover on its own.
func TestRequestBodyLimit_RejectsSixteenMiBUpload(t *testing.T) {
	t.Parallel()

	const sixteenMiB = 16 * constants.MiB

	tests := []struct {
		name string
		body func() io.Reader
	}{
		{name: "with Content-Length", body: func() io.Reader { return bytes.NewReader(make([]byte, sixteenMiB)) }},
		{name: "chunked", body: func() io.Reader { return chunkedBody(sixteenMiB) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotBody []byte
			var readErr error
			handler := RequestBodyLimit(testenv.NewLogger(t), constants.MaxRequestBodyBytes)(readAllHandler(t, &gotBody, &readErr))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", tt.body()))

			requireRequestTooLarge(t, rec)
			require.LessOrEqual(t, len(gotBody), int(constants.MaxRequestBodyBytes))
		})
	}
}

// Once headers are on the wire the response cannot be replaced, so a handler
// that streams before reading the body keeps its own status.
func TestRequestBodyLimit_KeepsCommittedResponses(t *testing.T) {
	t.Parallel()

	var readErr, flushErr error
	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flushErr = http.NewResponseController(w).Flush()
		_, readErr = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, "event: done\n\n")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/server", chunkedBody(testBodyLimit+1)))

	require.NoError(t, flushErr)
	require.Error(t, readErr)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "event: done\n\n", rec.Body.String())
	require.True(t, rec.Flushed)
}

func TestRequestBodyLimit_PassesBodilessRequests(t *testing.T) {
	t.Parallel()

	var gotBody io.ReadCloser
	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = r.Body
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rpc/things.list", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, http.NoBody, gotBody, "a bodiless request keeps http.NoBody, which handlers compare against")
}

// A handler that answers 413 itself after catching MaxBytesError still ends
// up with exactly one 413 and the middleware's body.
func TestRequestBodyLimit_HandlerOwn413IsReplacedConsistently(t *testing.T) {
	t.Parallel()

	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "too big", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", chunkedBody(testBodyLimit+1)))

	requireRequestTooLarge(t, rec)
}

// A handler that stops at the read error without answering must not turn an
// oversized upload into an empty 200.
func TestRequestBodyLimit_AnswersWhenHandlerReturnsSilently(t *testing.T) {
	t.Parallel()

	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", chunkedBody(testBodyLimit+1)))

	requireRequestTooLarge(t, rec)
}

func TestRequestBodyLimit_FlushAfterExceededAnswers413(t *testing.T) {
	t.Parallel()

	var flushErr error
	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		flushErr = http.NewResponseController(w).Flush()
		_, _ = io.WriteString(w, "discarded")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/server", chunkedBody(testBodyLimit+1)))

	require.NoError(t, flushErr)
	requireRequestTooLarge(t, rec)
	require.NotContains(t, rec.Body.String(), "discarded")
}

// Headers the handler set for the response it meant to send do not describe the
// 413. A stale Content-Encoding would make the JSON body undecodable.
func TestRequestBodyLimit_ClearsHandlerResponseHeaders(t *testing.T) {
	t.Parallel()

	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(constants.HeaderContentEncoding, "gzip")
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", chunkedBody(testBodyLimit+1)))

	requireRequestTooLarge(t, rec)
	require.Empty(t, rec.Header().Get(constants.HeaderContentEncoding))
	require.Empty(t, rec.Header().Get("ETag"))
	require.Empty(t, rec.Header().Get("Cache-Control"))
}

// Over HTTP/2 net/http treats Connection: close as a request to retire the whole
// connection, which other streams may share.
func TestRequestBodyLimit_DoesNotCloseHTTP2Connections(t *testing.T) {
	t.Parallel()

	handler := RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodPost, "/rpc/assets.upload", bytes.NewReader(make([]byte, testBodyLimit+1)))
	request.ProtoMajor = 2
	request.ProtoMinor = 0
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Empty(t, rec.Header().Get("Connection"))
}

// The two middlewares together over a real connection, ordered as the server
// mounts them, with a chunked upload the client really sends without a length.
func TestRequestBodyLimit_RejectsChunkedUploadThroughCompression(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	var readErr error
	server := httptest.NewServer(ResponseCompression()(RequestBodyLimit(testenv.NewLogger(t), testBodyLimit)(readAllHandler(t, &gotBody, &readErr))))
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, chunkedBody(64*constants.KiB))
	require.NoError(t, err)
	request.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	response, err := client.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	require.EqualValues(t, 0, request.ContentLength, "the client must really have sent no length")
	require.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
	require.True(t, response.Close, "HTTP/1 connections close after a refused upload")

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var payload requestTooLargeResponse
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, string(oops.CodeRequestTooLarge), payload.Name)
}
