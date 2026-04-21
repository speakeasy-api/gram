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

func readForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	bodyBytes, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	form, err := url.ParseQuery(string(bodyBytes))
	require.NoError(t, err)
	return form
}

func testSlackEnv() toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{
			slackBotTokenEnvVar: "xoxb-test-token",
		}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}
}

func TestReadChannelMessagesTool_UsesSlackTokenFromEnv(t *testing.T) {
	t.Parallel()

	var authorization string
	var contentType string
	var requestPath string
	var requestPayload url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"messages":[{"ts":"123.456","text":"hello"}]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewReadChannelMessagesTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callReadChannelMessages,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"channel_id":"C123","limit":25}`), &out)
	require.NoError(t, err)

	require.Equal(t, "Bearer xoxb-test-token", authorization)
	require.Equal(t, "application/x-www-form-urlencoded", contentType)
	require.Equal(t, "/conversations.history", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "25", requestPayload.Get("limit"))
	require.JSONEq(t, `{"ok":true,"messages":[{"ts":"123.456","text":"hello"}]}`, out.String())
}

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

func TestSendMessageTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":"C123","ts":"123.456"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSendMessageTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSendMessage,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"text":"hello",
		"thread_ts":"123.000",
		"reply_broadcast":true,
		"unfurl_links":false,
		"unfurl_media":false
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.postMessage", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "hello", requestPayload.Get("text"))
	require.Equal(t, "123.000", requestPayload.Get("thread_ts"))
	require.Equal(t, "true", requestPayload.Get("reply_broadcast"))
	require.Equal(t, "false", requestPayload.Get("unfurl_links"))
	require.Equal(t, "false", requestPayload.Get("unfurl_media"))
	require.JSONEq(t, `{"ok":true,"channel":"C123","ts":"123.456"}`, out.String())
}

func TestSlackTool_MissingTokenReturnsHelpfulError(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewReadUserProfileTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callReadUserProfile,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"user_id":"U123"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, slackBotTokenEnvVar)
	require.ErrorContains(t, err, slackTokenEnvVar)
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

func TestSearchChannelsTool_ListsAndFiltersByQuery(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channels":[
			{"id":"C1","name":"general"},
			{"id":"C2","name":"eng-platform"},
			{"id":"C3","name":"eng-runtime"}
		]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSearchChannelsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSearchChannels,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"query":"eng","channel_types":["public_channel","private_channel"],"exclude_archived":true}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.list", requestPath)
	require.Equal(t, "public_channel,private_channel", requestPayload.Get("types"))
	require.Equal(t, "true", requestPayload.Get("exclude_archived"))
	require.Contains(t, out.String(), "eng-platform")
	require.Contains(t, out.String(), "eng-runtime")
	require.NotContains(t, out.String(), "general")
}

func TestSearchUsersTool_ListsAndFiltersByQuery(t *testing.T) {
	t.Parallel()

	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"members":[
			{"id":"U1","name":"alice","real_name":"Alice Example","profile":{"email":"alice@example.com"}},
			{"id":"U2","name":"bob","real_name":"Bob Other","profile":{"email":"bob@other.com"}}
		]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSearchUsersTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSearchUsers,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"query":"alice"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/users.list", requestPath)
	require.Contains(t, out.String(), "alice@example.com")
	require.NotContains(t, out.String(), "bob@other.com")
}
