package aiintegrations

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

const (
	testExternalOrgID = "org-test"
	testWorkspaceID   = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// providerStub stands in for every provider API the probes call: one server,
// routed by request path (and event_type, which is what separates the two
// workspace-scoped ChatGPT feeds).
type providerStub struct {
	server *httptest.Server
	mu     sync.Mutex
	routes []string
}

type stubRoute struct {
	status int
	body   string
}

func newProviderStub(t *testing.T, routes map[string]stubRoute) *providerStub {
	t.Helper()

	stub := &providerStub{server: nil, mu: sync.Mutex{}, routes: nil}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Path
		if eventType := r.URL.Query().Get("event_type"); eventType != "" {
			route += "?event_type=" + eventType
		}

		stub.mu.Lock()
		stub.routes = append(stub.routes, route)
		stub.mu.Unlock()

		response, ok := routes[route]
		if !ok {
			response = stubRoute{status: http.StatusOK, body: "{}"}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		body := response.body
		if body == "" {
			body = "{}"
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *providerStub) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.routes...)
}

func newTestCredentialVerifier(t *testing.T, baseURL string) *CredentialVerifier {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	verifier := NewCredentialVerifier(testenv.NewLogger(t), policy)
	verifier.cursorBaseURL = baseURL
	verifier.anthropicBaseURL = baseURL
	verifier.codexBaseURL = baseURL
	return verifier
}

func TestVerifyCredentialsAcceptsAKeyEveryScheduleCanRead(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, nil)
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderChatGPTCompliance,
		APIKey:                 "chatgpt-key",
		ExternalOrganizationID: conv.PtrEmpty(testWorkspaceID),
	})

	require.Empty(t, rejections)
	// Both workspace feeds are probed, each asking for a single log file.
	require.ElementsMatch(t, []string{
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CONVERSATION_MESSAGE",
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CODEX_LOG",
	}, stub.requested())
}

// A key without Compliance API entitlement is refused by every feed the
// chatgpt_compliance config runs. That is the shape of the incident this
// check exists for: two schedules, one bad key, six failed polls.
func TestVerifyCredentialsReportsEveryRefusedChatGPTFeed(t *testing.T) {
	t.Parallel()

	body := `{"detail":"API key not authorized for enterprise logs"}`
	stub := newProviderStub(t, map[string]stubRoute{
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CONVERSATION_MESSAGE": {status: http.StatusForbidden, body: body},
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CODEX_LOG":            {status: http.StatusForbidden, body: body},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderChatGPTCompliance,
		APIKey:                 "chatgpt-key",
		ExternalOrganizationID: conv.PtrEmpty(testWorkspaceID),
	})

	require.Len(t, rejections, 2)
	require.Equal(t, ScheduleChatGPTCompliance, rejections[0].Schedule)
	require.Equal(t, ScheduleCodexCloudSessions, rejections[1].Schedule)
	// The provider's own explanation is what the user needs to fix the key.
	require.Contains(t, rejections[0].Err.Error(), "API key not authorized for enterprise logs")
	require.Contains(t, rejections[0].Err.Error(), "403 Forbidden")
	require.Contains(t, rejections[0].Err.Error(), ScheduleChatGPTCompliance)
}

// Anthropic entitles the Compliance API and the Admin Analytics reports
// separately, so a key can legitimately read one and not the other. Only the
// refused feed is reported; the save itself still lands.
func TestVerifyCredentialsReportsOnlyTheRefusedAnthropicFeed(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/v1/compliance/activities": {status: http.StatusForbidden, body: `{"error":{"message":"compliance api not enabled"}}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderAnthropicCompliance,
		APIKey:                 "anthropic-key",
		ExternalOrganizationID: conv.PtrEmpty(testExternalOrgID),
	})

	require.Len(t, rejections, 1)
	require.Equal(t, ScheduleAnthropicCompliance, rejections[0].Schedule)
	require.Contains(t, rejections[0].Err.Error(), "compliance api not enabled")
	require.ElementsMatch(t, []string{
		"/v1/compliance/activities",
		"/v1/organizations/analytics/user_usage_report",
		"/v1/organizations/analytics/user_cost_report",
	}, stub.requested())
}

func TestVerifyCredentialsReportsRefusedCursorKey(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/teams/filtered-usage-events": {status: http.StatusUnauthorized, body: `{"error":"invalid api key"}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderCursor,
		APIKey:                 "cursor-key",
		ExternalOrganizationID: nil,
	})

	require.Len(t, rejections, 1)
	require.Equal(t, ScheduleCursor, rejections[0].Schedule)
	require.Contains(t, rejections[0].Err.Error(), "invalid api key")
}

// A provider having a bad minute says nothing about the credentials, so the
// probe stays silent and the save proceeds. Cursor's client has no HTTP-level
// retries, which keeps this case quick.
func TestVerifyCredentialsIgnoresProviderOutage(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/teams/filtered-usage-events": {status: http.StatusInternalServerError, body: `{"error":"boom"}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	require.Empty(t, verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderCursor,
		APIKey:                 "cursor-key",
		ExternalOrganizationID: nil,
	}))
}

func TestProviderRejectedCredentialsMatchesConfigurationRefusals(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		got, ok := providerRejectedCredentials(&codexapi.HTTPError{StatusCode: status, Status: "", Body: ""})
		require.True(t, ok, status)
		require.Equal(t, status, got)
	}

	// A fixed minimal probe that comes back 400/422 is complaining about the
	// probe, not the credentials, so unlike the poll path it must not block a
	// save. Throttling and outages are equally inconclusive.
	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError} {
		_, ok := providerRejectedCredentials(&anthropicapi.HTTPError{StatusCode: status, Status: "", Body: ""})
		require.False(t, ok, status)
	}

	_, ok := providerRejectedCredentials(&cursorapi.RateLimitError{Status: "429 Too Many Requests", RetryAfter: 0, Page: 1})
	require.False(t, ok)
}
