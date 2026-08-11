package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// discardTestLogger substitutes for testenv.NewLogger: testenv imports
// platformtools, so this package cannot use it without an import cycle.
func discardTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler) //nolint:forbidigo // GG006: testenv is unreachable from here (import cycle)
}

func sealFetch(t *testing.T, enc *encryption.Client, fetch sealedFileFetch) string {
	t.Helper()
	payload, err := json.Marshal(fetch)
	require.NoError(t, err)
	blob, err := enc.Encrypt(payload)
	require.NoError(t, err)
	return blob
}

func proxyRequest(t *testing.T, proxy *FileProxy, blob string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/platform/slack/files"
	if blob != "" {
		target += "?t=" + url.QueryEscape(blob)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	proxy.serveFile(rec, req)
	return rec
}

func TestFileProxy_ServesSealedFile(t *testing.T) {
	t.Parallel()

	pngBytes := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/files.info", func(w http.ResponseWriter, r *http.Request) {
		payload := readForm(t, r)
		assert.Equal(t, "F123", payload.Get("file"))
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"ok":true,"file":{"id":"F123","name":"cat.png","title":"a cat","mimetype":"image/png","size":%d,"url_private_download":"%s/download"}}`, len(pngBytes), server.URL)
		assert.NoError(t, err)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer xoxb-sealed-token", r.Header.Get("Authorization"))
		_, err := w.Write(pngBytes)
		assert.NoError(t, err)
	})

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	proxy := &FileProxy{
		logger: discardTestLogger(),
		enc:    enc,
		client: newAPIClient(server.URL, server.Client()),
	}

	blob := sealFetch(t, enc, sealedFileFetch{
		FileID:    "F123",
		Token:     "xoxb-sealed-token",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	rec := proxyRequest(t, proxy, blob)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, pngBytes, rec.Body.Bytes())
}

func TestFileProxy_RejectsExpiredToken(t *testing.T) {
	t.Parallel()

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	proxy := &FileProxy{logger: discardTestLogger(), enc: enc, client: newAPIClient("https://slack.example", nil)}

	blob := sealFetch(t, enc, sealedFileFetch{
		FileID:    "F123",
		Token:     "xoxb-sealed-token",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	rec := proxyRequest(t, proxy, blob)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestFileProxy_RejectsGarbageToken(t *testing.T) {
	t.Parallel()

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	proxy := &FileProxy{logger: discardTestLogger(), enc: enc, client: newAPIClient("https://slack.example", nil)}

	rec := proxyRequest(t, proxy, "not-a-sealed-blob")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestFileProxy_RejectsMissingToken(t *testing.T) {
	t.Parallel()

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	proxy := &FileProxy{logger: discardTestLogger(), enc: enc, client: newAPIClient("https://slack.example", nil)}

	rec := proxyRequest(t, proxy, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}
