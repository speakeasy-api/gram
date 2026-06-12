package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetThreadStatusTool_PostsToSetStatus(t *testing.T) {
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
		descriptor: NewSetThreadStatusTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSetThreadStatus,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"thread_ts":"123.456",
		"status":"is ordering pizza...",
		"loading_messages":["Calling DoorDash..."]
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/assistant.threads.setStatus", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel_id"))
	require.Equal(t, "123.456", requestPayload.Get("thread_ts"))
	require.Equal(t, "is ordering pizza...", requestPayload.Get("status"))
	require.JSONEq(t, `["Calling DoorDash..."]`, requestPayload.Get("loading_messages"))
	require.JSONEq(t, `{"ok":true}`, out.String())
}

func TestSetThreadStatusTool_DefaultsLoadingMessagesToStatus(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSetThreadStatusTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSetThreadStatus,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"thread_ts":"123.456",
		"status":"is ordering pizza..."
	}`), &out)
	require.NoError(t, err)

	// Omitting loading_messages makes Slack rotate its own defaults, so the
	// tool must pin the indicator to the status text.
	require.JSONEq(t, `["is ordering pizza..."]`, requestPayload.Get("loading_messages"))
}

func TestSetThreadStatusTool_EmptyStatusClearsIndicator(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSetThreadStatusTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSetThreadStatus,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"thread_ts":"123.456",
		"status":""
	}`), &out)
	require.NoError(t, err)

	// An empty status clears the indicator; loading_messages must be omitted
	// so the tool doesn't pin a blank phrase to a cleared status.
	require.Equal(t, "", requestPayload.Get("status"))
	require.True(t, requestPayload.Has("status"))
	require.False(t, requestPayload.Has("loading_messages"))
	require.JSONEq(t, `{"ok":true}`, out.String())
}

func TestSetThreadStatusTool_ClampsLoadingMessagesToOne(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSetThreadStatusTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSetThreadStatus,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"thread_ts":"123.456",
		"status":"is ordering pizza...",
		"loading_messages":["Calling DoorDash...","Tipping the driver...","Waiting at the door..."]
	}`), &out)
	require.NoError(t, err)

	require.JSONEq(t, `["Calling DoorDash..."]`, requestPayload.Get("loading_messages"))
}
