package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveReactionTool_PostsToReactionsRemove(t *testing.T) {
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
		descriptor: NewRemoveReactionTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callRemoveReaction,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"timestamp":"123.456",
		"name":":thumbsup:"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/reactions.remove", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "123.456", requestPayload.Get("timestamp"))
	require.Equal(t, "thumbsup", requestPayload.Get("name"))
	require.JSONEq(t, `{"ok":true}`, out.String())
}

func TestRemoveReactionTool_SurfacesNoReaction(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":false,"error":"no_reaction"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewRemoveReactionTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callRemoveReaction,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C","timestamp":"1.2","name":"x"
	}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "no_reaction")
}
