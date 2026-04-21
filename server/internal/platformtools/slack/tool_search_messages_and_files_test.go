package slack

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func TestSearchMessagesAndFilesTool_RequiresUserToken(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewSearchMessagesAndFilesTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callSearchMessagesAndFiles,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackBotTokenEnvVar: "xoxb-bot-only"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"roadmap"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, slackUserTokenEnvVar)
	require.ErrorContains(t, err, "search:read")
}

func TestSearchMessagesAndFilesTool_CallsSearchAllWithUserToken(t *testing.T) {
	t.Parallel()

	var requestPath string
	var authorization string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authorization = r.Header.Get("Authorization")
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"messages":{"matches":[]},"files":{"matches":[]}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSearchMessagesAndFilesTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSearchMessagesAndFiles,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"launch plan","limit":25,"sort":"timestamp"}`), io.Discard)
	require.NoError(t, err)
	require.Equal(t, "/search.all", requestPath)
	require.Equal(t, "Bearer xoxp-user-token", authorization)
	require.Equal(t, "launch plan", requestPayload.Get("query"))
	require.Equal(t, "25", requestPayload.Get("count"))
	require.Equal(t, "timestamp", requestPayload.Get("sort"))
}
