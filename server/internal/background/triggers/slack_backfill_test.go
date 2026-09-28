package triggers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
)

func TestReadSlackThreadContextReadsGapAndDropsOwnMessages(t *testing.T) {
	t.Parallel()

	forms := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		forms <- r.PostForm
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true,"has_more":true,"messages":[
			{"ts":"0.5","user":"U0","text":"thread parent already seen"},
			{"ts":"1.5","user":"U1","text":"first"},
			{"ts":"1.6","user":"UBOT","text":"my own reply"},
			{"ts":"1.65","bot_id":"BSELF","text":"my own bot post"},
			{"ts":"1.7","user":"U2","text":"second"}
		]}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	logger := slog.New(slog.DiscardHandler) //nolint:forbidigo // GG006: testenv imports this package
	app := &App{
		logger:      logger,
		slackClient: slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
	}
	got := app.readSlackThreadContext(t.Context(), logger, "xoxb-test-token", "1.0", slackTriggerEvent{
		ChannelID: "C1",
		ThreadID:  "1.0",
		Timestamp: "2.0",
		BotUserID: "UBOT",
		SelfBotID: "BSELF",
	})

	form := <-forms
	require.Equal(t, "C1", form.Get("channel"))
	require.Equal(t, "1.0", form.Get("ts"))
	require.Equal(t, "1.0", form.Get("oldest"))
	require.Equal(t, "2.0", form.Get("latest"))
	require.Equal(t, slackThreadContext{
		Messages: []slackThreadContextMessage{
			{Ts: "1.5", UserID: "U1", BotID: "", Text: "first"},
			{Ts: "1.7", UserID: "U2", BotID: "", Text: "second"},
		},
		Truncated:   true,
		Unavailable: false,
	}, got)
}

func TestReadSlackThreadContextMarksFailureUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":false,"error":"ratelimited"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	logger := slog.New(slog.DiscardHandler) //nolint:forbidigo // GG006: testenv imports this package
	app := &App{
		logger:      logger,
		slackClient: slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
	}
	got := app.readSlackThreadContext(t.Context(), logger, "xoxb-test-token", "", slackTriggerEvent{ChannelID: "C1", ThreadID: "1.0", Timestamp: "2.0"})
	require.True(t, got.Unavailable)
	require.Empty(t, got.Messages)
}
