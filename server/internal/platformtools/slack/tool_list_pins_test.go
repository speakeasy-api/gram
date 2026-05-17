package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListPinsTool_PostsToPinsList(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"items":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListPinsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListPins,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"channel_id":"C123"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/pins.list", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.JSONEq(t, `{"ok":true,"items":[]}`, out.String())
}

func TestListPinsTool_RequiresChannel(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewListPinsTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callListPins,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "channel_id")
}
