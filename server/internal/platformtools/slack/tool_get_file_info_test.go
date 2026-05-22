package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetFileInfoTool_PassesPaginationFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"notes.txt"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetFileInfoTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetFileInfo,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"file":"F123",
		"cursor":"abc",
		"limit":50,
		"page":2,
		"count":25
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/files.info", requestPath)
	require.Equal(t, "F123", requestPayload.Get("file"))
	require.Equal(t, "abc", requestPayload.Get("cursor"))
	require.Equal(t, "50", requestPayload.Get("limit"))
	require.Equal(t, "2", requestPayload.Get("page"))
	require.Equal(t, "25", requestPayload.Get("count"))
	require.JSONEq(t, `{"ok":true,"file":{"id":"F123","name":"notes.txt"}}`, out.String())
}
