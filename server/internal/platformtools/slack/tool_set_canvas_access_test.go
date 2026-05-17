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

func TestSetCanvasAccessTool_SendsTargets(t *testing.T) {
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
		descriptor: NewSetCanvasAccessTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSetCanvasAccess,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"canvas_id":"F1",
		"access_level":"write",
		"channel_ids":["C1","C2"],
		"user_ids":["U1"]
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/canvases.access.set", requestPath)
	require.Equal(t, "F1", requestPayload.Get("canvas_id"))
	require.Equal(t, "write", requestPayload.Get("access_level"))
	require.Equal(t, "C1,C2", requestPayload.Get("channel_ids"))
	require.Equal(t, "U1", requestPayload.Get("user_ids"))
}

func TestSetCanvasAccessTool_RequiresTarget(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewSetCanvasAccessTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callSetCanvasAccess,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"canvas_id":"F1","access_level":"read"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "channel_ids")
	require.ErrorContains(t, err, "user_ids")
}
