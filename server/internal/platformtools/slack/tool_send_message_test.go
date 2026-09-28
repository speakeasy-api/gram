package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

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
		descriptor: NewSendMessageTool(nil, nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     sendMessageCall(nil),
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

func TestSendMessageTool_PassesBlockKitBlocks(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1.0"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSendMessageTool(nil, nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     sendMessageCall(nil),
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"text":"approve this?",
		"blocks":[
			{"type":"section","text":{"type":"mrkdwn","text":"approve this?"}},
			{"type":"actions","block_id":"approval","elements":[{"type":"button","action_id":"yes","text":{"type":"plain_text","text":"Yes"},"value":"approved"}]}
		]
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "approve this?", requestPayload.Get("text"))

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("blocks")), &blocks))
	require.Len(t, blocks, 2)
	require.Equal(t, "section", blocks[0]["type"])
	require.Equal(t, "actions", blocks[1]["type"])
	require.Equal(t, "approval", blocks[1]["block_id"])
}

type failingThreadRouter struct{}

func (failingThreadRouter) RouteSlackThreadToAssistant(context.Context, bgtriggers.AssistantThread, string, string) error {
	return errors.New("thread not found")
}

func TestSendMessageTool_RejectsRouteRepliesInThread(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewSendMessageTool(nil, nil).Descriptor(),
		client:     newAPIClient("http://unused.invalid", http.DefaultClient),
		callFn:     sendMessageCall(nil),
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"text":"hello",
		"thread_ts":"123.000",
		"route_replies":"new"
	}`), &out)
	require.ErrorContains(t, err, "only to new top-level messages")
}

func TestSendMessageTool_ReportsRoutingFailureWithoutFailingPost(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true,"channel":"C123","ts":"123.456"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSendMessageTool(nil, failingThreadRouter{}).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     sendMessageCall(failingThreadRouter{}),
	}

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ProjectID: &projectID})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})

	var out bytes.Buffer
	err := tool.Call(ctx, testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"text":"hello",
		"route_replies":"self"
	}`), &out)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "123.456", result["ts"])
	require.Contains(t, result["route_replies_error"], "thread not found")
}
