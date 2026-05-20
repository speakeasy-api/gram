package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinMessageTool_PostsToPinsAdd(t *testing.T) {
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
		descriptor: NewPinMessageTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callPinMessage,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"timestamp":"123.456"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/pins.add", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "123.456", requestPayload.Get("timestamp"))
	require.JSONEq(t, `{"ok":true}`, out.String())
}

func TestPinMessageTool_RequiresFields(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewPinMessageTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callPinMessage,
	}

	cases := []struct {
		name    string
		payload string
		field   string
	}{
		{"missing channel", `{"timestamp":"1.2"}`, "channel_id"},
		{"missing timestamp", `{"channel_id":"C"}`, "timestamp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(tc.payload), &bytes.Buffer{})
			require.Error(t, err)
			require.ErrorContains(t, err, tc.field)
		})
	}
}
