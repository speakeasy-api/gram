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

func TestDeleteCanvasTool_SendsCanvasID(t *testing.T) {
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
		descriptor: NewDeleteCanvasTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callDeleteCanvas,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"canvas_id":"F42"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/canvases.delete", requestPath)
	require.Equal(t, "F42", requestPayload.Get("canvas_id"))
}

func TestDeleteCanvasTool_RequiresCanvasID(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewDeleteCanvasTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callDeleteCanvas,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "canvas_id")
}
