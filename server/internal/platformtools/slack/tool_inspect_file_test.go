package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInspectFileTool_ReturnsMetadataAndInlineImage(t *testing.T) {
	t.Parallel()

	pngBytes := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/files.info", func(w http.ResponseWriter, r *http.Request) {
		payload := readForm(t, r)
		require.Equal(t, "F123", payload.Get("file"))
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"ok":true,"file":{"id":"F123","name":"cat.png","title":"a cat","mimetype":"image/png","size":%d,"url_private_download":"%s/download"}}`, len(pngBytes), server.URL)
		require.NoError(t, err)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer xoxb-test-token", r.Header.Get("Authorization"))
		_, err := w.Write(pngBytes)
		require.NoError(t, err)
	})

	tool := &slackTool{
		descriptor: NewInspectFileTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callInspectFile,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"file_id":"F123"}`), &out)
	require.NoError(t, err)

	var result struct {
		File struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Mimetype string `json:"mimetype"`
			Size     int    `json:"size"`
		} `json:"file"`
		Note        string `json:"note"`
		InlineImage struct {
			FileID    string `json:"file_id"`
			MimeType  string `json:"mime_type"`
			SizeBytes int    `json:"size_bytes"`
			DataURI   string `json:"data_uri"`
		} `json:"gram_inline_image"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "F123", result.File.ID)
	require.Equal(t, "cat.png", result.File.Name)
	require.Equal(t, "image/png", result.File.Mimetype)
	require.Equal(t, len(pngBytes), result.File.Size)
	require.Contains(t, result.Note, "attached")
	require.Equal(t, "F123", result.InlineImage.FileID)
	require.Equal(t, "image/png", result.InlineImage.MimeType)
	require.Equal(t, len(pngBytes), result.InlineImage.SizeBytes)
	require.True(t, strings.HasPrefix(result.InlineImage.DataURI, "data:image/png;base64,"))
}

func TestInspectFileTool_RejectsMissingFileID(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewInspectFileTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callInspectFile,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	require.ErrorContains(t, err, "file_id is required")
}

func TestInspectFileTool_PropagatesNonImageRejection(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/files.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"ok":true,"file":{"id":"F9","name":"doc.pdf","mimetype":"application/pdf","size":64,"url_private_download":"%s/download"}}`, server.URL)
		require.NoError(t, err)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("%PDF-1.7 not an image"))
		require.NoError(t, err)
	})

	tool := &slackTool{
		descriptor: NewInspectFileTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callInspectFile,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"file_id":"F9"}`), io.Discard)
	require.ErrorContains(t, err, "not an allowed image type")
}
