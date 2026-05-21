package slack

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func TestAddReminderTool_RequiresUserToken(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewAddReminderTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callAddReminder,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackBotTokenEnvVar: "xoxb-bot-only"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"text":"ping","time":"in 5 minutes"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, slackUserTokenEnvVar)
}

func TestAddReminderTool_CallsRemindersAddWithRecurrence(t *testing.T) {
	t.Parallel()

	var requestPath string
	var authorization string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authorization = r.Header.Get("Authorization")
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"reminder":{"id":"Rm123","text":"ship the PR"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewAddReminderTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callAddReminder,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{
		"text":"ship the PR",
		"time":"every Thursday at 9am",
		"recurrence":{"frequency":"weekly","weekdays":["thursday"]}
	}`), io.Discard)
	require.NoError(t, err)
	require.Equal(t, "/reminders.add", requestPath)
	require.Equal(t, "Bearer xoxp-user-token", authorization)
	require.Equal(t, "ship the PR", requestPayload.Get("text"))
	require.Equal(t, "every Thursday at 9am", requestPayload.Get("time"))

	var recurrence map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("recurrence")), &recurrence))
	require.Equal(t, "weekly", recurrence["frequency"])
	require.Equal(t, []any{"thursday"}, recurrence["weekdays"])
}

func TestAddReminderTool_RequiresTextAndTime(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewAddReminderTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callAddReminder,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"time":"in 5 minutes"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "text")
}
