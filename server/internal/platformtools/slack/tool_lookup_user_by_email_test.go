package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupUserByEmailTool_SendsEmail(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"user":{"id":"U123","profile":{"email":"alice@example.com"}}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewLookupUserByEmailTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callLookupUserByEmail,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"email":"alice@example.com"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/users.lookupByEmail", requestPath)
	require.Equal(t, "alice@example.com", requestPayload.Get("email"))
	require.Contains(t, out.String(), `"id":"U123"`)
}

func TestLookupUserByEmailTool_RequiresEmail(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewLookupUserByEmailTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callLookupUserByEmail,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "email")
}
