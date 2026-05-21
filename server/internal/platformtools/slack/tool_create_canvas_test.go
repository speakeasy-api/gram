package slack

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateCanvasTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"canvas_id":"F0123"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewCreateCanvasTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callCreateCanvas,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"title":"Quarterly Plan",
		"channel_id":"C100",
		"document_content":{"type":"markdown","markdown":"# Plan\n\nIntro."}
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/canvases.create", requestPath)
	require.Equal(t, "Quarterly Plan", requestPayload.Get("title"))
	require.Equal(t, "C100", requestPayload.Get("channel_id"))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("document_content")), &doc))
	require.Equal(t, "markdown", doc["type"])
	require.Equal(t, "# Plan\n\nIntro.", doc["markdown"])
	require.JSONEq(t, `{"ok":true,"canvas_id":"F0123"}`, out.String())
}

func TestCreateCanvasTool_AllowsEmptyPayload(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"canvas_id":"F0999"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewCreateCanvasTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callCreateCanvas,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &out)
	require.NoError(t, err)

	require.Empty(t, requestPayload.Get("title"))
	require.Empty(t, requestPayload.Get("channel_id"))
	require.Empty(t, requestPayload.Get("document_content"))
}
