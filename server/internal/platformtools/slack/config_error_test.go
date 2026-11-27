package slack

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func emptySlackEnv() toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		GramEmail:  "",
		GramChatID: "",
	}
}

// slackAnswering serves every Slack method with body.
func slackAnswering(t *testing.T, body string) *apiClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return newAPIClient(server.URL, server.Client())
}

func requireConfigError(t *testing.T, err error, message string) {
	t.Helper()
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "want a shareable error, got %v", err)
	require.Equal(t, oops.CodeFailedPrecondition, shareable.Code)
	require.Equal(t, message, shareable.Error())
	require.True(t, oops.IsClientFault(err), "configuration is not a Speakeasy fault")
}

func TestSlackTool_MissingBotTokenSaysWhatToSetAndWho(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewSearchChannelsTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callSearchChannels,
	}

	err := tool.Call(t.Context(), emptySlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	requireConfigError(t, err, "server not configured: missing environment variable SLACK_BOT_TOKEN (or SLACK_USER_TOKEN or SLACK_TOKEN). An admin of the server's project must add it in the server's environment in Speakeasy, or the client must send it in its MCP headers.")
}

func TestSlackTool_MissingUserTokenNamesUserToken(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewSearchMessagesAndFilesTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callSearchMessagesAndFiles,
	}

	err := tool.Call(t.Context(), emptySlackEnv(), bytes.NewBufferString(`{"query":"launch"}`), io.Discard)
	requireConfigError(t, err, "server not configured: missing environment variable SLACK_USER_TOKEN (or SLACK_TOKEN), a Slack user token (search needs its search:read scope). An admin of the server's project must add it in the server's environment in Speakeasy, or the client must send it in its MCP headers.")
}

func TestSlackTool_MissingScopeNamesTheScope(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewSearchChannelsTool(nil).Descriptor(),
		client:     slackAnswering(t, `{"ok":false,"error":"missing_scope","needed":"channels:read","provided":"chat:write"}`),
		callFn:     callSearchChannels,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	requireConfigError(t, err, "server not configured: its Slack token lacks the channels:read scope, which conversations.list needs. A Slack workspace admin must add the scope to the Slack app and reinstall the app. An admin of the server's project must then update the token in the server's environment in Speakeasy, or the client must send it in its MCP headers.")
}

func TestSlackTool_RejectedTokenSaysToReplaceIt(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "not_allowed_token_type"} {
		tool := &slackTool{
			descriptor: NewListEmojiTool(nil).Descriptor(),
			client:     slackAnswering(t, `{"ok":false,"error":"`+code+`"}`),
			callFn:     callListEmoji,
		}

		err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
		requireConfigError(t, err, "server not configured: Slack rejected its token ("+code+"). An admin of the server's project must replace it in the server's environment in Speakeasy, or the client must send it in its MCP headers.")
	}
}

func TestSlackTool_OtherFailuresKeepTheirError(t *testing.T) {
	t.Parallel()

	refused := &slackTool{
		descriptor: NewListEmojiTool(nil).Descriptor(),
		client:     slackAnswering(t, `{"ok":false,"error":"channel_not_found"}`),
		callFn:     callListEmoji,
	}
	err := refused.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	require.EqualError(t, err, "slack emoji.list: channel_not_found")
	_, shareable := errors.AsType[*oops.ShareableError](err)
	require.False(t, shareable)

	slackDown := &slackTool{
		descriptor: NewListEmojiTool(nil).Descriptor(),
		client:     slackAnswering(t, `{"ok":false,"error":"internal_error"}`),
		callFn:     callListEmoji,
	}
	err = slackDown.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	require.False(t, oops.IsClientFault(err), "a Slack-side failure stays a server fault")
	_, shareable = errors.AsType[*oops.ShareableError](err)
	require.False(t, shareable)
}

type staticProjectToken string

func (s staticProjectToken) ProjectBotToken(_ context.Context, _ string, _ uuid.UUID) (string, error) {
	return string(s), nil
}

// The Speakeasy Slack app's token never comes from the server's environment,
// so its tools must not tell anyone to fix that environment.
func TestSpeakeasyAppTool_RejectedTokenGivesNoEnvironmentAdvice(t *testing.T) {
	t.Parallel()

	tool := &speakeasyAppTool{
		inner: &slackTool{
			descriptor: NewChatGetPermalinkTool(nil).Descriptor(),
			client:     slackAnswering(t, `{"ok":false,"error":"invalid_auth"}`),
			callFn:     callGetPermalink,
		},
		tokens: staticProjectToken("xoxb-app-token"),
	}
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", ProjectID: &projectID})

	err := tool.Call(ctx, emptySlackEnv(), bytes.NewBufferString(`{"channel_id":"C1","message_ts":"1.2"}`), io.Discard)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "server's environment")
	_, shareable := errors.AsType[*oops.ShareableError](err)
	require.False(t, shareable)
}

type staticAssistantSlackCreds AssistantSlackCredentials

func (s staticAssistantSlackCreds) ResolveForCaller(_ context.Context, _ string, _ uuid.UUID) (AssistantSlackCredentials, error) {
	return AssistantSlackCredentials(s), nil
}

type errRoundTripper struct{ err error }

func (rt errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, rt.err
}

func TestAssistantSlackTool_RejectsControlCharsAndScrubsLeakedTokens(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org",
		ProjectID:            &projectID,
	})

	t.Run("rejects control character before request", func(t *testing.T) {
		t.Parallel()
		badToken := "xoxb-secret\ninjected"
		tool := &assistantSlackTool{
			inner: &slackTool{
				descriptor: NewListEmojiTool(nil).Descriptor(),
				client:     newAPIClient("https://slack.test.invalid", &http.Client{Transport: errRoundTripper{err: errors.New("must not reach transport")}}),
				callFn:     callListEmoji,
			},
			credentials: staticAssistantSlackCreds{BotToken: badToken},
		}
		err := tool.Call(ctx, emptySlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
		require.Error(t, err)
		shareable, ok := errors.AsType[*oops.ShareableError](err)
		require.True(t, ok)
		require.Equal(t, oops.CodeFailedPrecondition, shareable.Code)
		require.NotContains(t, err.Error(), badToken)
	})

	t.Run("scrubs token echoed in transport error", func(t *testing.T) {
		t.Parallel()
		token := "xoxb-transport-secret"
		tool := &assistantSlackTool{
			inner: &slackTool{
				descriptor: NewListEmojiTool(nil).Descriptor(),
				client:     newAPIClient("https://slack.test.invalid", &http.Client{Transport: errRoundTripper{err: errors.New("dial failed with header " + token)}}),
				callFn:     callListEmoji,
			},
			credentials: staticAssistantSlackCreds{BotToken: token},
		}
		err := tool.Call(ctx, emptySlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
		require.EqualError(t, err, "Slack request failed")
		require.NotContains(t, err.Error(), token)
	})

	t.Run("scrubs token echoed in non-200 Slack response body", func(t *testing.T) {
		t.Parallel()
		token := "xoxb-http500-secret"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream proxy echoed " + token))
		}))
		t.Cleanup(server.Close)
		tool := &assistantSlackTool{
			inner: &slackTool{
				descriptor: NewListEmojiTool(nil).Descriptor(),
				client:     newAPIClient(server.URL, server.Client()),
				callFn:     callListEmoji,
			},
			credentials: staticAssistantSlackCreds{BotToken: token},
		}
		err := tool.Call(ctx, emptySlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
		require.EqualError(t, err, "Slack request failed")
		require.NotContains(t, err.Error(), token)
	})
}
