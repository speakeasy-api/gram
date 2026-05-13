package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetReactionsTool_PostsToReactionsGet(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"type":"message","message":{"reactions":[{"name":"thumbsup","users":["U1"],"count":1}]}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetReactionsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetReactions,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"timestamp":"123.456",
		"full":true
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/reactions.get", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "123.456", requestPayload.Get("timestamp"))
	require.Equal(t, "true", requestPayload.Get("full"))
	require.Contains(t, out.String(), "thumbsup")
}

func TestGetReactionsTool_OmitsFullWhenUnset(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetReactionsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetReactions,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C","timestamp":"1.2"
	}`), &bytes.Buffer{})
	require.NoError(t, err)
	require.Empty(t, requestPayload.Get("full"))
}
