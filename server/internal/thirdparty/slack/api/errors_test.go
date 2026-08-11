package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestError_EnvelopeCodesAreClientFaults(t *testing.T) {
	t.Parallel()

	// Every one of these was observed being logged at error level, drowning out
	// genuine faults in the MCP component's error rate.
	codes := []string{
		"thread_not_found",
		"already_reacted",
		"not_in_channel",
		"missing_scope",
		"invalid_blocks",
		"channel_not_found",
		"invalid_auth",
	}

	for _, code := range codes {
		err := newEnvelopeError("chat.postMessage", http.StatusOK, ResponseEnvelope{
			Ok:               false,
			Error:            code,
			Warning:          "",
			ResponseMetadata: nil,
		})
		require.True(t, err.ClientFault(), "%s is the caller's to fix", code)
		require.True(t, oops.IsClientFault(err), "%s must be visible to error boundaries", code)
	}
}

func TestError_SlackSideEnvelopeCodesStayServerFaults(t *testing.T) {
	t.Parallel()

	codes := []string{
		"internal_error",
		"fatal_error",
		"service_unavailable",
		"request_timeout",
		"org_login_required",
		"ratelimited",
		"rate_limited",
	}

	for _, code := range codes {
		err := newEnvelopeError("conversations.replies", http.StatusOK, ResponseEnvelope{
			Ok:               false,
			Error:            code,
			Warning:          "",
			ResponseMetadata: nil,
		})
		require.False(t, err.ClientFault(), "%s is a Slack-side failure", code)
		require.False(t, oops.IsClientFault(err))
	}
}

func TestError_SlackMigrationEnvelopeCodeStaysServerFault(t *testing.T) {
	t.Parallel()

	err := newEnvelopeError("team.info", http.StatusOK, ResponseEnvelope{
		Ok:    false,
		Error: "team_added_to_org",
	})

	require.False(t, err.ClientFault(), "a workspace migration is a Slack-side outage")
	require.False(t, oops.IsClientFault(err))
}

func TestError_UnnamedEnvelopeFailureStaysServerFault(t *testing.T) {
	t.Parallel()

	err := newEnvelopeError("reactions.add", http.StatusOK, ResponseEnvelope{
		Ok:               false,
		Error:            "",
		Warning:          "",
		ResponseMetadata: nil,
	})
	require.False(t, err.ClientFault(), "an unclassifiable refusal must keep error severity")
	require.Equal(t, "slack reactions.add: request failed", err.Error())
}

func TestError_StatusCodesFollowTheHTTPSplit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status      int
		clientFault bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, true},
		{http.StatusTooManyRequests, false},
		{http.StatusBadGateway, false},
		{http.StatusServiceUnavailable, false},
	}

	for _, tc := range cases {
		err := newStatusError("users.list", tc.status, []byte("upstream said no"))
		require.Equal(t, tc.clientFault, err.ClientFault(), "status %d", tc.status)
	}
}

func TestError_MessagesKeepTheirRefusalShape(t *testing.T) {
	t.Parallel()

	envelopeErr := newEnvelopeError("conversations.replies", http.StatusOK, ResponseEnvelope{
		Ok:      false,
		Error:   "thread_not_found",
		Warning: "",
		ResponseMetadata: &struct {
			Messages []string `json:"messages,omitempty"`
		}{Messages: []string{"no such thread"}},
	})
	require.Equal(t, "slack conversations.replies: thread_not_found | no such thread", envelopeErr.Error())

	statusErr := newStatusError("users.list", http.StatusBadGateway, []byte("bad gateway"))
	require.Equal(t, "slack users.list returned 502: bad gateway", statusErr.Error())
}

func TestClient_CallReturnsTypedEnvelopeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":false,"error":"thread_not_found"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())
	_, err := client.CallWithToken(t.Context(), "conversations.replies", map[string]any{"channel": "C1"}, "xoxb-test")

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "thread_not_found", apiErr.Code)
	require.Equal(t, "conversations.replies", apiErr.Method)
	require.Equal(t, http.StatusOK, apiErr.StatusCode)
	require.True(t, oops.IsClientFault(err))
}

func TestClient_CallReturnsTypedStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())
	_, err := client.CallWithToken(t.Context(), "users.list", nil, "xoxb-test")

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.Empty(t, apiErr.Code)
	require.False(t, oops.IsClientFault(err), "a Slack outage must keep error severity")
}
