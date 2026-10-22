package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var bodyBearingHooksRoutes = []string{
	"/rpc/hooks.claude",
	"/rpc/hooks.cursor",
	"/rpc/hooks.codex",
	"/rpc/hooks.ingest",
	"/rpc/hooks.uploadSkillContent",
	"/rpc/hooks.skillFeedback",
	"/rpc/hooks.otel/v1/logs",
	"/rpc/hooks.otel/v1/metrics",
}

func newHooksHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	_, ti := newTestHooksService(t)
	mux := goahttp.NewMuxer()
	Attach(mux, ti.service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func postHooks(t *testing.T, server *httptest.Server, path string, body []byte, contentEncoding string) (int, goahttp.ErrorResponse) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var errBody goahttp.ErrorResponse
	_ = json.Unmarshal(raw, &errBody)
	return resp.StatusCode, errBody
}

func TestHooksRoutes_RejectGzipBombBeforeDecodingWithoutCredentials(t *testing.T) {
	t.Parallel()
	server := newHooksHTTPServer(t)

	bomb := gzipBytes(t, bytes.Repeat([]byte(" "), maxHooksRequestBodyBytes+1))
	require.Less(t, len(bomb), 64*1024, "the compressed body must be small for the test to be meaningful")

	for _, path := range bodyBearingHooksRoutes {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			status, body := postHooks(t, server, path, bomb, "gzip")
			require.Equal(t, http.StatusRequestEntityTooLarge, status)
			require.Equal(t, string(oops.CodeRequestTooLarge), body.Name)
		})
	}
}

func TestHooksRoutes_RejectOversizedPlainBody(t *testing.T) {
	t.Parallel()
	server := newHooksHTTPServer(t)

	status, body := postHooks(t, server, "/rpc/hooks.claude", bytes.Repeat([]byte(" "), maxHooksRequestBodyBytes+1), "")
	require.Equal(t, http.StatusRequestEntityTooLarge, status)
	require.Equal(t, string(oops.CodeRequestTooLarge), body.Name)
}

func TestHooksRoutes_AcceptLargeCompressedEventWithinLimit(t *testing.T) {
	t.Parallel()
	server := newHooksHTTPServer(t)

	// Larger than the skill-upload limit but well under the default, so this
	// also shows the stricter limit stays specific to skill uploads.
	prompt := strings.Repeat("x", 2*maxSkillUploadRequestBodyBytes)
	payload, err := json.Marshal(map[string]string{"hook_event_name": "UserPromptSubmit", "prompt": prompt})
	require.NoError(t, err)

	// Unauthenticated Claude hooks are answered rather than refused, so a 200
	// shows the body was decoded and the event handled.
	status, _ := postHooks(t, server, "/rpc/hooks.claude", gzipBytes(t, payload), "gzip")
	require.Equal(t, http.StatusOK, status)
}

func TestHooksRoutes_SkillUploadKeepsStricterDecompressedLimit(t *testing.T) {
	t.Parallel()
	server := newHooksHTTPServer(t)

	content, err := json.Marshal(strings.Repeat("x", maxSkillUploadRequestBodyBytes))
	require.NoError(t, err)
	oversized := []byte(`{"schema_version":"hook.skill-content.v1","raw_sha256":"` + strings.Repeat("0", 64) + `","content":` + string(content) + `}`)
	status, body := postHooks(t, server, "/rpc/hooks.uploadSkillContent", gzipBytes(t, oversized), "gzip")
	require.Equal(t, http.StatusRequestEntityTooLarge, status)
	require.Equal(t, string(oops.CodeRequestTooLarge), body.Name)

	small := []byte(`{"schema_version":"hook.skill-content.v1","raw_sha256":"` + strings.Repeat("0", 64) + `","content":"x"}`)
	status, body = postHooks(t, server, "/rpc/hooks.uploadSkillContent", gzipBytes(t, small), "gzip")
	require.Equal(t, http.StatusUnauthorized, status, "decoding should succeed and reach authentication")
	require.Equal(t, string(oops.CodeUnauthorized), body.Name)
}

// endlessReader yields an unbounded stream and counts how much was taken.
type endlessReader struct{ read int64 }

func (r *endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	r.read += int64(len(p))
	return len(p), nil
}

func (r *endlessReader) Close() error { return nil }

func TestHooksRequestDecoder_StopsReadingUnboundedBodyAtLimit(t *testing.T) {
	t.Parallel()

	src := &endlessReader{read: 0}
	r := httptest.NewRequest(http.MethodPost, "/rpc/hooks.claude", nil)
	r.Body = src
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/json")

	var out map[string]any
	err := newHooksRequestDecoder(testenv.NewLogger(t))(r).Decode(&out)
	var se *goa.ServiceError
	require.ErrorAs(t, err, &se)
	require.Equal(t, string(oops.CodeRequestTooLarge), se.Name)
	require.LessOrEqual(t, src.read, int64(maxHooksRequestBodyBytes)+1)
}

func TestHooksRequestDecoder_RejectsDeclaredOversizedBodyWithoutReading(t *testing.T) {
	t.Parallel()

	src := &endlessReader{read: 0}
	r := httptest.NewRequest(http.MethodPost, "/rpc/hooks.uploadSkillContent", nil)
	r.Body = src
	r.ContentLength = maxSkillUploadRequestBodyBytes + 1

	var out map[string]any
	err := newHooksRequestDecoder(testenv.NewLogger(t))(r).Decode(&out)
	var se *goa.ServiceError
	require.ErrorAs(t, err, &se)
	require.Equal(t, string(oops.CodeRequestTooLarge), se.Name)
	require.Zero(t, src.read)
}

func TestReadAtMost(t *testing.T) {
	t.Parallel()

	b, err := readAtMost(strings.NewReader("abcd"), 4)
	require.NoError(t, err)
	require.Equal(t, "abcd", string(b))

	_, err = readAtMost(strings.NewReader("abcde"), 4)
	require.ErrorIs(t, err, errHooksRequestBodyTooLarge)
}
