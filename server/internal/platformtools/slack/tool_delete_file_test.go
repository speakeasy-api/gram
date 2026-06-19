package slack

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteFileTool_PostsFileID(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewDeleteFileTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callDeleteFile,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"file":"F123"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/files.delete", requestPath)
	require.Equal(t, "F123", requestPayload.Get("file"))
	require.JSONEq(t, `{"ok":true}`, out.String())
}

func TestDeleteFileTool_RejectsMissingFile(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewDeleteFileTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callDeleteFile,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "file is required")
}
