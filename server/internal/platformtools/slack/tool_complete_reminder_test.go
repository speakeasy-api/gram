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

func TestCompleteReminderTool_CallsRemindersCompleteWithUserToken(t *testing.T) {
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
		descriptor: NewCompleteReminderTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callCompleteReminder,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"reminder":"Rm12345678"}`), io.Discard)
	require.NoError(t, err)
	require.Equal(t, "/reminders.complete", requestPath)
	require.Equal(t, "Rm12345678", requestPayload.Get("reminder"))
}

func TestCompleteReminderTool_RequiresReminderID(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewCompleteReminderTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callCompleteReminder,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "reminder")
}
