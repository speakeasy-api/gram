package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func mintingTool(t *testing.T, slackBaseURL string, httpClient *http.Client, enc *encryption.Client, serverURL *url.URL) *slackTool {
	t.Helper()
	return &slackTool{
		descriptor: NewGetFileURLTool(nil, enc, serverURL).Descriptor(),
		client:     newAPIClient(slackBaseURL, httpClient),
		callFn: func(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
			return callGetFileURL(ctx, client, enc, serverURL, env, payload, wr)
		},
	}
}

func TestGetFileURLTool_MintsSignedDownloadURL(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/files.info", func(w http.ResponseWriter, r *http.Request) {
		payload := readForm(t, r)
		assert.Equal(t, "F123", payload.Get("file"))
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"ok":true,"file":{"id":"F123","name":"cat.png","title":"a cat","mimetype":"image/png","size":64,"url_private_download":"%s/download"}}`, server.URL)
		assert.NoError(t, err)
	})

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.example")
	require.NoError(t, err)

	tool := mintingTool(t, server.URL, server.Client(), enc, serverURL)

	var out bytes.Buffer
	require.NoError(t, tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"file_id":"F123"}`), &out))

	var result struct {
		File struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Mimetype string `json:"mimetype"`
			Size     int64  `json:"size"`
		} `json:"file"`
		DownloadURL string `json:"download_url"`
		ExpiresAt   string `json:"expires_at"`
		Note        string `json:"note"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "F123", result.File.ID)
	require.Equal(t, "cat.png", result.File.Name)
	require.Equal(t, "image/png", result.File.Mimetype)
	require.EqualValues(t, 64, result.File.Size)
	require.Contains(t, result.Note, "inspect_asset")

	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))

	minted, err := url.Parse(result.DownloadURL)
	require.NoError(t, err)
	require.Equal(t, "gram.example", minted.Host)
	require.Equal(t, "/platform/slack/files", minted.Path)

	plaintext, err := enc.Decrypt(minted.Query().Get("t"))
	require.NoError(t, err)
	var sealed sealedFileFetch
	require.NoError(t, json.Unmarshal([]byte(plaintext), &sealed))
	require.Equal(t, "F123", sealed.FileID)
	require.NotEmpty(t, sealed.Token)
	require.Greater(t, sealed.ExpiresAt, time.Now().Unix())
}

func TestGetFileURLTool_UnconfiguredMintingErrors(t *testing.T) {
	t.Parallel()

	tool := mintingTool(t, "https://slack.example", nil, nil, nil)

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"file_id":"F123"}`), &out)
	require.ErrorContains(t, err, "not configured")
}
