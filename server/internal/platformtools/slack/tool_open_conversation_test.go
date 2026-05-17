package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenConversationTool_OpensWithUsers(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":{"id":"D123"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewOpenConversationTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callOpenConversation,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"users":["U1","U2"],
		"return_im":true
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.open", requestPath)
	require.Equal(t, "U1,U2", requestPayload.Get("users"))
	require.Equal(t, "true", requestPayload.Get("return_im"))
	require.Empty(t, requestPayload.Get("channel"))
}

func TestOpenConversationTool_RejectsMissingAndBothInputs(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewOpenConversationTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callOpenConversation,
	}

	cases := []struct {
		name    string
		payload string
		needle  string
	}{
		{"missing both", `{}`, "either channel_id or users"},
		{"both supplied", `{"channel_id":"D123","users":["U1"]}`, "only one"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(tc.payload), &bytes.Buffer{})
			require.Error(t, err)
			require.ErrorContains(t, err, tc.needle)
		})
	}
}
